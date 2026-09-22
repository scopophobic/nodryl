package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"nodryl/internal/graph"
	"nodryl/internal/scan"
	"nodryl/internal/trace"
)

var (
	ink      = lipgloss.NewStyle().Foreground(lipgloss.Color("#DCE8EF"))
	muted    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8294A2"))
	cyan     = lipgloss.NewStyle().Foreground(lipgloss.Color("#95E5DE"))
	amber    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F2C77D"))
	red      = lipgloss.NewStyle().Foreground(lipgloss.Color("#F17885"))
	rule     = lipgloss.NewStyle().Foreground(lipgloss.Color("#485766"))
	selected = lipgloss.NewStyle().Foreground(lipgloss.Color("#14252D")).Background(lipgloss.Color("#95E5DE")).Bold(true)
)

type changedMsg struct{}
type exitedMsg struct{ err error }
type rescannedMsg struct {
	graph graph.Graph
	err   error
}

type Model struct {
	Graph     graph.Graph
	Store     *trace.Store
	Exited    <-chan error
	Requests  []trace.Request
	Live      bool
	Activity  bool
	Current   string
	Cursor    int
	Scroll    int
	Width     int
	Height    int
	Searching bool
	Search    string
	Help      bool
	AppStatus string
	Notice    string
}

func NewMap(g graph.Graph) Model {
	return Model{Graph: g, Current: "project:root", Cursor: -1, AppStatus: "Map ready"}
}

func NewLive(g graph.Graph, store *trace.Store, exited <-chan error) Model {
	m := NewMap(g)
	m.Store, m.Exited, m.Live = store, exited, true
	m.Requests = store.Snapshot()
	m.AppStatus = "App running"
	return m
}

// New keeps the original live constructor available to callers.
func New(g graph.Graph, store *trace.Store, exited <-chan error) Model {
	return NewLive(g, store, exited)
}

func (m Model) Init() tea.Cmd {
	if !m.Live {
		return nil
	}
	return tea.Batch(waitChanged(m.Store), waitExited(m.Exited))
}

func waitChanged(store *trace.Store) tea.Cmd {
	return func() tea.Msg { <-store.Changed(); return changedMsg{} }
}
func waitExited(ch <-chan error) tea.Cmd { return func() tea.Msg { return exitedMsg{err: <-ch} } }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.Searching {
			return m.updateSearch(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.Help = !m.Help
		case "tab":
			if m.Live {
				m.Activity = !m.Activity
				m.Cursor, m.Scroll = 0, 0
				if !m.Activity {
					m.Cursor = -1
				}
				m.Search = ""
			}
		case "/":
			m.Searching, m.Search, m.Cursor, m.Scroll = true, "", 0, 0
		case "esc":
			if m.Help {
				m.Help = false
			} else if m.Search != "" {
				m.Search, m.Cursor, m.Scroll = "", 0, 0
			} else {
				m.goUp()
			}
		case "backspace", "left", "h":
			m.goUp()
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
				m.adjustScroll()
			}
		case "down", "j":
			if m.Cursor+1 < m.itemCount() {
				m.Cursor++
				m.adjustScroll()
			}
		case "enter", "right", "l":
			if !m.Activity {
				if node, ok := m.selectedNode(); ok && node.Kind == "directory" {
					m.Current, m.Search, m.Cursor, m.Scroll = node.ID, "", -1, 0
				}
			}
		case "g":
			m.Current, m.Search, m.Cursor, m.Scroll = "project:root", "", -1, 0
		case "s":
			if node, ok := m.sourceNode(); ok {
				return m, openSource(m.Graph.Root, node)
			}
		case "r":
			m.Notice = "Scanning project…"
			root := m.Graph.Root
			return m, func() tea.Msg { g, err := scan.Project(root); return rescannedMsg{graph: g, err: err} }
		}
	case changedMsg:
		m.Requests = m.Store.Snapshot()
		if m.Cursor >= m.itemCount() && m.Cursor > 0 {
			m.Cursor = m.itemCount() - 1
		}
		return m, waitChanged(m.Store)
	case exitedMsg:
		if msg.err != nil {
			m.AppStatus = "App exited: " + msg.err.Error()
		} else {
			m.AppStatus = "App exited"
		}
	case rescannedMsg:
		if msg.err != nil {
			m.Notice = "Scan failed: " + msg.err.Error()
		} else {
			m.Graph, m.Notice = msg.graph, "Map refreshed"
			if _, ok := m.Graph.NodeByID(m.Current); !ok {
				m.Current = "project:root"
			}
			m.Cursor, m.Scroll = -1, 0
		}
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.Searching, m.Search, m.Cursor, m.Scroll = false, "", 0, 0
	case "enter":
		m.Searching = false
	case "backspace":
		runes := []rune(m.Search)
		if len(runes) > 0 {
			m.Search = string(runes[:len(runes)-1])
		}
		m.Cursor, m.Scroll = 0, 0
	default:
		if msg.Type == tea.KeyRunes {
			m.Search += string(msg.Runes)
			m.Cursor, m.Scroll = 0, 0
		}
	}
	return m, nil
}

func (m *Model) goUp() {
	if m.Activity {
		return
	}
	if m.Search != "" {
		m.Search, m.Cursor, m.Scroll = "", 0, 0
		return
	}
	if m.Current == "project:root" {
		return
	}
	node, ok := m.Graph.NodeByID(m.Current)
	if !ok {
		m.Current = "project:root"
		return
	}
	parent := filepath.ToSlash(filepath.Dir(node.File))
	if parent == "." {
		m.Current = "project:root"
	} else {
		m.Current = "dir:" + parent
	}
	m.Cursor, m.Scroll = -1, 0
}

func (m Model) items() []graph.Node {
	items := []graph.Node{}
	if m.Search != "" {
		needle := strings.ToLower(m.Search)
		for _, node := range m.Graph.Nodes {
			if node.Kind != "project" && (strings.Contains(strings.ToLower(node.Label), needle) || strings.Contains(strings.ToLower(node.File), needle)) {
				items = append(items, node)
			}
		}
	} else {
		for _, edge := range m.Graph.Outgoing(m.Current) {
			if edge.Kind == "contains" {
				if node, ok := m.Graph.NodeByID(edge.To); ok {
					items = append(items, node)
				}
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == "directory" && items[j].Kind != "directory" {
			return true
		}
		if items[j].Kind == "directory" && items[i].Kind != "directory" {
			return false
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	return items
}

func (m Model) activityItems() []trace.Request {
	if m.Search == "" {
		return m.Requests
	}
	out := []trace.Request{}
	for _, request := range m.Requests {
		if strings.Contains(strings.ToLower(request.Name), strings.ToLower(m.Search)) {
			out = append(out, request)
		}
	}
	return out
}

func (m Model) itemCount() int {
	if m.Activity {
		return len(m.activityItems())
	}
	return len(m.items())
}

func (m Model) selectedNode() (graph.Node, bool) {
	items := m.items()
	if m.Cursor >= 0 && m.Cursor < len(items) {
		return items[m.Cursor], true
	}
	return m.Graph.NodeByID(m.Current)
}

func (m Model) selectedRequest() (trace.Request, bool) {
	items := m.activityItems()
	if m.Cursor >= 0 && m.Cursor < len(items) {
		return items[m.Cursor], true
	}
	return trace.Request{}, false
}

func (m Model) sourceNode() (graph.Node, bool) {
	if !m.Activity {
		node, ok := m.selectedNode()
		return node, ok && node.File != "" && node.Kind != "directory"
	}
	request, ok := m.selectedRequest()
	if !ok {
		return graph.Node{}, false
	}
	for _, node := range m.Graph.Nodes {
		if node.Kind == "route" && node.Label == request.Name {
			return node, true
		}
	}
	return graph.Node{}, false
}

func (m Model) selectedSource() string {
	if node, ok := m.sourceNode(); ok {
		return fmt.Sprintf("%s:%d", filepath.Join(m.Graph.Root, node.File), node.Line)
	}
	return ""
}

func openSource(root string, node graph.Node) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil
	}
	file := filepath.Join(root, node.File)
	args := append([]string{}, parts[1:]...)
	switch filepath.Base(parts[0]) {
	case "code", "code-insiders":
		args = append(args, "--goto", fmt.Sprintf("%s:%d", file, node.Line))
	case "vi", "vim", "nvim":
		args = append(args, fmt.Sprintf("+%d", max(node.Line, 1)), file)
	default:
		args = append(args, file)
	}
	return tea.ExecProcess(exec.Command(parts[0], args...), nil)
}

func (m *Model) adjustScroll() {
	visible := max(m.Height-8, 5)
	if m.Cursor < m.Scroll {
		m.Scroll = m.Cursor
	}
	if m.Cursor >= m.Scroll+visible {
		m.Scroll = m.Cursor - visible + 1
	}
}

func (m Model) View() string {
	width, height := m.Width, m.Height
	if width < 1 {
		width = 100
	}
	if height < 1 {
		height = 30
	}
	if m.Help {
		return m.helpView(width, height)
	}
	if width < 44 || height < 12 {
		return fit(cyan.Render("◉ nodryl")+"  "+muted.Render("Terminal too small; enlarge to explore"), width)
	}
	var out strings.Builder
	project := filepath.Base(m.Graph.Root)
	mode := "Map"
	if m.Activity {
		mode = "Activity"
	}
	counts := m.counts()
	header := cyan.Bold(true).Render("◉ nodryl") + "  " + ink.Render(project) + "   " + muted.Render(mode)
	stats := fmt.Sprintf("%d files  %d languages  %d links", counts.files, counts.languages, len(m.Graph.Edges))
	out.WriteString(fit(header+"  "+muted.Render(stats), width) + "\n")
	out.WriteString(rule.Render(strings.Repeat("─", max(width, 1))) + "\n")
	crumb := project
	if m.Current != "project:root" && !m.Activity {
		crumb += " / " + strings.TrimPrefix(m.Current, "dir:")
	}
	if m.Activity {
		crumb += " / requests"
	}
	if m.Searching {
		crumb = "Find: " + m.Search + "▏"
	} else if m.Search != "" {
		crumb = "Matches for “" + m.Search + "”  ·  Esc clears"
	}
	out.WriteString(fit(ink.Render(crumb), width) + "\n")
	if m.Notice != "" {
		out.WriteString(fit(amber.Render(m.Notice), width) + "\n")
	}
	bodyHeight := max(height-6, 8)
	if m.Notice != "" {
		bodyHeight--
	}
	if width >= 88 {
		leftWidth := max(width*38/100, 30)
		rightWidth := width - leftWidth - 3
		left := m.listLines(leftWidth, bodyHeight)
		right := m.detailLines(rightWidth, bodyHeight)
		for i := 0; i < bodyHeight; i++ {
			out.WriteString(fit(left[i], leftWidth) + " " + rule.Render("│") + " " + fit(right[i], rightWidth) + "\n")
		}
	} else {
		listHeight := max(bodyHeight/2, 4)
		for _, line := range m.listLines(width, listHeight) {
			out.WriteString(fit(line, width) + "\n")
		}
		out.WriteString(rule.Render(strings.Repeat("─", max(width, 1))) + "\n")
		for _, line := range m.detailLines(width, max(bodyHeight-listHeight-1, 2)) {
			out.WriteString(fit(line, width) + "\n")
		}
	}
	out.WriteString(rule.Render(strings.Repeat("─", max(width, 1))) + "\n")
	footer := "↑↓ move   ↵ open   ← back   / find   s source   r rescan   ? help   q quit"
	if m.Live {
		footer = "Tab map/activity   " + footer
	}
	out.WriteString(fit(muted.Render(footer), width))
	return out.String()
}

type summary struct{ files, languages int }

func (m Model) counts() summary {
	languages := map[string]bool{}
	files := 0
	for _, node := range m.Graph.Nodes {
		if node.Kind == "module" || node.Kind == "manifest" || node.Kind == "file" {
			files++
		}
		if node.Language != "" && node.Kind == "module" {
			languages[node.Language] = true
		}
	}
	return summary{files, len(languages)}
}

func (m Model) listLines(width, height int) []string {
	lines := []string{}
	if m.Activity {
		items := m.activityItems()
		lines = append(lines, amber.Bold(true).Render(fmt.Sprintf("Requests  %d", len(items))), "")
		if len(items) == 0 {
			lines = append(lines, muted.Render("No requests yet. Use the app to create one."))
		}
		for i := m.Scroll; i < len(items) && len(lines) < height; i++ {
			item := items[i]
			label := fmt.Sprintf("%s  %s", item.Start.Format("15:04:05"), item.Name)
			if i == m.Cursor {
				lines = append(lines, selected.Render(fit("▌ "+label, width)))
			} else {
				lines = append(lines, "  "+clip(label, width-2))
			}
		}
	} else {
		items := m.items()
		lines = append(lines, cyan.Bold(true).Render(fmt.Sprintf("Components  %d", len(items))), "")
		if m.Search == "" {
			overview := "◉  Overview"
			if m.Cursor == -1 {
				lines = append(lines, selected.Render(fit("▌ "+overview, width)))
			} else {
				lines = append(lines, "  "+overview)
			}
		}
		if len(items) == 0 {
			lines = append(lines, muted.Render("Nothing here. Press ← to go back."))
		}
		for i := m.Scroll; i < len(items) && len(lines) < height; i++ {
			node := items[i]
			glyph := glyphFor(node.Kind)
			label := glyph + "  " + node.Label
			if node.Kind == "directory" {
				label += "/"
			}
			if i == m.Cursor {
				lines = append(lines, selected.Render(fit("▌ "+label, width)))
			} else {
				lines = append(lines, " "+clip(label, width-1))
			}
		}
	}
	return padLines(lines, height)
}

func (m Model) detailLines(width, height int) []string {
	lines := []string{}
	if m.Activity {
		request, ok := m.selectedRequest()
		if !ok {
			return padLines([]string{amber.Bold(true).Render("Activity"), "", muted.Render("Waiting for live evidence.")}, height)
		}
		lines = append(lines, amber.Bold(true).Render(request.Name), muted.Render(request.Start.Format("15:04:05.000")+fmt.Sprintf("  ·  %d observed steps", len(request.Spans))), "")
		for _, span := range request.Spans {
			label := "● " + span.Name
			if span.System != "" {
				label += "  [" + span.System + "]"
			}
			label += "  " + span.Duration.Round(time.Millisecond).String()
			if span.Error {
				lines = append(lines, red.Render(label+"  failed"))
			} else {
				lines = append(lines, clip(label, width))
			}
		}
		if source := m.selectedSource(); source != "" {
			lines = append(lines, "", muted.Render("Source  "+source))
		}
		return padLines(lines, height)
	}
	node, ok := m.selectedNode()
	if !ok {
		return padLines([]string{muted.Render("Select a component to inspect it.")}, height)
	}
	title := node.Label
	if node.Kind == "project" {
		title = filepath.Base(m.Graph.Root)
	}
	lines = append(lines, cyan.Bold(true).Render(title))
	meta := strings.Title(node.Kind)
	if node.Language != "" {
		meta += "  ·  " + node.Language
	}
	lines = append(lines, muted.Render(meta))
	if node.File != "" {
		lines = append(lines, muted.Render(node.File))
	}
	lines = append(lines, "")
	children := m.Graph.Outgoing(node.ID)
	contained, relationships := []graph.Edge{}, []graph.Edge{}
	for _, edge := range children {
		if edge.Kind == "contains" {
			contained = append(contained, edge)
		} else {
			relationships = append(relationships, edge)
		}
	}
	if len(contained) > 0 {
		lines = append(lines, ink.Bold(true).Render(fmt.Sprintf("Inside  %d", len(contained))))
		if node.Kind == "project" {
			lines = append(lines, m.languageBars(width)...)
		}
		for i, edge := range contained {
			if i >= 3 {
				lines = append(lines, muted.Render("  … more; open to explore"))
				break
			}
			if target, ok := m.Graph.NodeByID(edge.To); ok {
				lines = append(lines, "  ├─ "+target.Label)
			}
		}
		lines = append(lines, "")
	}
	incoming := m.Graph.Incoming(node.ID)
	users := []graph.Edge{}
	for _, edge := range incoming {
		if edge.Kind != "contains" {
			users = append(users, edge)
		}
	}
	if len(relationships) > 0 || len(users) > 0 {
		lines = append(lines, ink.Bold(true).Render(fmt.Sprintf("Signal map  %d out · %d in", len(relationships), len(users))), "")
		for i, edge := range users {
			if i >= 3 {
				lines = append(lines, muted.Render("  ↑ more incoming links"))
				break
			}
			if from, ok := m.Graph.NodeByID(edge.From); ok {
				lines = append(lines, muted.Render("  "+clip(from.Label, max(width-12, 4))+"  ─"+edge.Kind+"→"))
			}
		}
		lines = append(lines, cyan.Bold(true).Render("  ◉ "+clip(title, max(width-5, 4))))
		for i, edge := range relationships {
			if i >= 5 {
				lines = append(lines, muted.Render("  ↓ more outgoing links"))
				break
			}
			if target, ok := m.Graph.NodeByID(edge.To); ok {
				lines = append(lines, "  └─"+edge.Kind+"→ "+clip(target.Label, max(width-14, 4)))
			}
		}
		lines = append(lines, "", muted.Render("  ─ code evidence · from repository"))
	}
	if len(contained) == 0 && len(relationships) == 0 && len(users) == 0 {
		lines = append(lines, muted.Render("No relationships detected for this component."))
	}
	if node.File != "" && node.Kind != "directory" {
		lines = append(lines, "", muted.Render("Press s to open source."))
	}
	return padLines(lines, height)
}

func (m Model) languageBars(width int) []string {
	counts := map[string]int{}
	for _, node := range m.Graph.Nodes {
		if node.Kind == "module" && node.Language != "" {
			counts[node.Language]++
		}
	}
	type languageCount struct {
		name  string
		count int
	}
	ordered := []languageCount{}
	for name, count := range counts {
		ordered = append(ordered, languageCount{name, count})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].count != ordered[j].count {
			return ordered[i].count > ordered[j].count
		}
		return ordered[i].name < ordered[j].name
	})
	lines := []string{}
	for i, entry := range ordered {
		if i >= 4 {
			break
		}
		bar := strings.Repeat("━", min(entry.count, max(width/5, 1)))
		lines = append(lines, fmt.Sprintf("  %-11s %s %d", entry.name, cyan.Render(bar), entry.count))
	}
	return lines
}

func (m Model) helpView(width, height int) string {
	lines := []string{
		cyan.Bold(true).Render("Nodryl  /  keys"), "",
		"↑ ↓ or j k     Move through components", "Enter or →      Open a directory", "← or Backspace Go to parent", "g               Return to project root",
		"/               Find a component or request", "Esc             Clear search or go back", "s               Open selected source", "r               Rescan the project",
		"Tab             Switch map and activity", "?               Close this help", "q               Quit", "", "Evidence", "  ─ code         Found in the repository", "  ● runtime      Observed while the app ran",
	}
	var out strings.Builder
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out.WriteString(fit(lines[i], width))
		}
		out.WriteString("\n")
	}
	return out.String()
}

func glyphFor(kind string) string {
	switch kind {
	case "directory":
		return "▸"
	case "manifest":
		return "◆"
	case "route":
		return "⇢"
	case "symbol":
		return "ƒ"
	case "package":
		return "◇"
	case "module":
		return "·"
	default:
		return "·"
	}
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) > width {
		return ansi.Truncate(value, width, "…")
	}
	return value
}

func fit(value string, width int) string {
	value = clip(value, width)
	if gap := width - ansi.StringWidth(value); gap > 0 {
		value += strings.Repeat(" ", gap)
	}
	return value
}

func padLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
