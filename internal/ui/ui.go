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
	ink          = lipgloss.NewStyle().Foreground(lipgloss.Color("#E4EFF5"))
	muted        = lipgloss.NewStyle().Foreground(lipgloss.Color("#889EAC"))
	cyan         = lipgloss.NewStyle().Foreground(lipgloss.Color("#79DFDD"))
	amber        = lipgloss.NewStyle().Foreground(lipgloss.Color("#F6C46B"))
	red          = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7182"))
	rule         = lipgloss.NewStyle().Foreground(lipgloss.Color("#365263"))
	selected     = lipgloss.NewStyle().Foreground(lipgloss.Color("#0A2630")).Background(lipgloss.Color("#79DFDD")).Bold(true)
	selectedLive = lipgloss.NewStyle().Foreground(lipgloss.Color("#302716")).Background(lipgloss.Color("#F6C46B")).Bold(true)
	shade        = lipgloss.NewStyle().Foreground(lipgloss.Color("#A8BBC7"))
)

type changedMsg struct{}
type motionMsg struct{ epoch uint64 }
type exitedMsg struct{ err error }
type rescannedMsg struct {
	graph graph.Graph
	err   error
}

type Model struct {
	Graph         graph.Graph
	Store         *trace.Store
	Exited        <-chan error
	Requests      []trace.Request
	Live          bool
	Activity      bool
	Flow          bool
	FlowIndex     int
	MotionFrame   int
	PulseStep     int
	PulseActive   bool
	PulseDone     bool
	MotionEpoch   uint64
	ReducedMotion bool
	paths         []flowPath
	Current       string
	Cursor        int
	Scroll        int
	Width         int
	Height        int
	Searching     bool
	Search        string
	Help          bool
	AppStatus     string
	Notice        string
	Endpoint      string
	DetailScroll  int
}

func NewMap(g graph.Graph) Model {
	return Model{Graph: g, Current: "project:root", Cursor: -1, AppStatus: "Map ready", ReducedMotion: os.Getenv("NODRYL_REDUCED_MOTION") == "1", paths: flowPathsFor(g)}
}

func NewLive(g graph.Graph, store *trace.Store, exited <-chan error) Model {
	m := NewMap(g)
	m.Store, m.Exited, m.Live = store, exited, true
	m.Requests = store.Snapshot()
	m.AppStatus = "App running"
	return m
}

func NewObserved(g graph.Graph, store *trace.Store, endpoint string) Model {
	m := NewMap(g)
	m.Store, m.Live, m.Endpoint = store, true, endpoint
	m.Requests = store.Snapshot()
	m.AppStatus = "Receiver ready"
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
	if m.Exited == nil {
		return waitChanged(m.Store)
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
		if m.Flow {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "?":
				m.Help = !m.Help
			case "tab":
				return m.cycleTab()
			case "f", "esc":
				return m.toggleFlow()
			case "p", " ":
				m.triggerPulse()
			case "up", "k":
				m.moveFlow(-1)
			case "down", "j":
				m.moveFlow(1)
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.Help = !m.Help
		case "tab":
			return m.cycleTab()
		case "f":
			return m.toggleFlow()
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
				m.DetailScroll = 0
				m.adjustScroll()
			}
		case "down", "j":
			if m.Cursor+1 < m.itemCount() {
				m.Cursor++
				m.DetailScroll = 0
				m.adjustScroll()
			}
		case "pgup":
			m.DetailScroll = max(0, m.DetailScroll-max(m.Height/2, 1))
		case "pgdown":
			if m.Activity {
				m.DetailScroll += max(m.Height/2, 1)
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
	case tea.MouseMsg:
		if m.Flow && !m.Help && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y == flowButtonRow(m.Height) && msg.X >= 2 && msg.X < 26 {
			m.triggerPulse()
		}
	case motionMsg:
		if !m.Flow || msg.epoch != m.MotionEpoch || m.ReducedMotion {
			break
		}
		m.MotionFrame++
		m.advancePulse()
		return m, nextMotion(m.MotionEpoch)
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
			m.paths = flowPathsFor(msg.graph)
			if _, ok := m.Graph.NodeByID(m.Current); !ok {
				m.Current = "project:root"
			}
			m.Cursor, m.Scroll, m.DetailScroll = -1, 0, 0
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
	visible := max(m.Height-12, 4)
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
	if m.Flow {
		return m.flowView(width, height)
	}
	var out strings.Builder
	project := filepath.Base(m.Graph.Root)
	counts := m.counts()
	brand := cyan.Bold(true).Render("  ◈ NODRYL") + muted.Render("  /  ") + ink.Bold(true).Render(project)
	status := "STATIC MAP"
	if m.Live {
		status = "● " + m.AppStatus
	}
	out.WriteString(fit(brand+"  "+amber.Render(status), width) + "\n")
	mapTab, flowTab, activityTab := cyan.Bold(true).Render("▰ Map"), muted.Render("▱ Flow"), muted.Render("▱ Activity")
	if m.Activity {
		mapTab, activityTab = muted.Render("▱ Map"), amber.Bold(true).Render("▰ Activity")
	}
	stats := fmt.Sprintf("%d files   %d languages   %d links", counts.files, counts.languages, len(m.Graph.Edges))
	if m.Live {
		stats += fmt.Sprintf("   %d traces", len(m.Requests))
	}
	out.WriteString(fit("  "+mapTab+"   "+flowTab+"   "+activityTab+"    "+shade.Render(stats), width) + "\n")
	out.WriteString(rule.Render(strings.Repeat("━", width)) + "\n")
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
	out.WriteString(fit("  "+muted.Render("⌁")+" "+ink.Render(crumb), width) + "\n")
	if m.Notice != "" {
		out.WriteString(fit("  "+amber.Render(m.Notice), width) + "\n")
	}
	bodyHeight := max(height-7, 4)
	if m.Notice != "" {
		bodyHeight--
	}
	if width >= 76 {
		leftWidth := max(width*34/100, 27)
		rightWidth := width - leftWidth - 5
		left := m.listLines(leftWidth, bodyHeight)
		right := m.detailLines(rightWidth, bodyHeight)
		for i := 0; i < bodyHeight; i++ {
			out.WriteString("  " + fit(left[i], leftWidth) + " " + rule.Render("┃") + " " + fit(right[i], rightWidth) + "\n")
		}
	} else {
		listHeight := max(bodyHeight/3, 3)
		for _, line := range m.listLines(width, listHeight) {
			out.WriteString(fit(line, width) + "\n")
		}
		out.WriteString(rule.Render(strings.Repeat("─", max(width, 1))) + "\n")
		for _, line := range m.detailLines(width, max(bodyHeight-listHeight-1, 0)) {
			out.WriteString(fit(line, width) + "\n")
		}
	}
	out.WriteString(rule.Render(strings.Repeat("━", width)) + "\n")
	footer := "Tab views   f flow   ↑↓ move   ↵ open   ← back   / find   s source   r rescan   ? help   q quit"
	if m.Live {
		footer = "Pg↑↓ detail   " + footer
	}
	if width < 105 {
		footer = "Tab flow  ↑↓ move  ↵ open  / find  s source  ? help  q quit"
		if m.Live {
			footer = "Tab views  f flow  ↑↓ move  / find  ? help  q quit"
		}
	}
	if width < 70 {
		footer = "f flow  ↑↓ move  / find  ? help  q quit"
		if m.Live {
			footer = "Tab views  f flow  ? help  q quit"
		}
	}
	out.WriteString(fit("  "+muted.Render(footer), width) + "\n")
	if m.Endpoint != "" {
		out.WriteString(fit("  "+amber.Render("Receive traces at "+m.Endpoint+"/v1/traces"), width))
	} else {
		out.WriteString(fit("  "+muted.Render("Code map from repository  •  live flow from observed traces"), width))
	}
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
		lines = append(lines, amber.Bold(true).Render(fmt.Sprintf("  ◆ Recent requests  /  %d", len(items))), rule.Render(strings.Repeat("─", max(width-1, 1))))
		if len(items) == 0 {
			lines = append(lines, "", muted.Render("  No traces yet."), muted.Render("  Send OTLP spans here."))
		}
		for i := m.Scroll; i < len(items) && len(lines) < height; i++ {
			item := items[i]
			failed := false
			for _, step := range item.Spans {
				failed = failed || step.Error
			}
			mark := "●"
			if failed {
				mark = "✕"
			}
			label := fmt.Sprintf("%s  %s  %s", mark, item.Start.Format("15:04:05"), item.Name)
			if i == m.Cursor {
				lines = append(lines, selectedLive.Render(fit(" "+clip(label, width-2), width)))
			} else {
				style := amber
				if failed {
					style = red
				}
				lines = append(lines, "  "+style.Render(clip(label, width-2)))
			}
		}
	} else {
		items := m.items()
		lines = append(lines, cyan.Bold(true).Render(fmt.Sprintf("  ◆ Repository  /  %d", len(items))), rule.Render(strings.Repeat("─", max(width-1, 1))))
		if m.Search == "" {
			overview := "◈  Project overview"
			if m.Cursor == -1 {
				lines = append(lines, selected.Render(fit(" "+overview, width)))
			} else {
				lines = append(lines, "  "+cyan.Render(overview))
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
				lines = append(lines, selected.Render(fit(" "+clip(label, width-2), width)))
			} else {
				lines = append(lines, "  "+shade.Render(clip(label, width-2)))
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
			return padLines([]string{amber.Bold(true).Render("  ◉ Live signal"), rule.Render(strings.Repeat("─", max(width-1, 1))), "", ink.Render("  Waiting for backend activity"), "", muted.Render("  Make a request to your app. Its observed path"), muted.Render("  will appear here, step by step.")}, height)
		}
		lines = append(lines, amber.Bold(true).Render("  ◉ "+clip(request.Name, max(width-5, 1))), rule.Render(strings.Repeat("─", max(width-1, 1))))
		lines = append(lines, muted.Render(fmt.Sprintf("  %s   •   %d observed steps", request.Start.Format("15:04:05.000"), len(request.Spans))), "")
		lines = append(lines, m.flowLines(request, width)...)
		if source := m.selectedSource(); source != "" {
			lines = append(lines, "", cyan.Render("  ↗ Source  "+clip(source, max(width-12, 4))))
		}
		return scrollLines(lines, height, m.DetailScroll)
	}
	node, ok := m.selectedNode()
	if !ok {
		return padLines([]string{muted.Render("Select a component to inspect it.")}, height)
	}
	title := node.Label
	if node.Kind == "project" {
		title = filepath.Base(m.Graph.Root)
		return m.overviewLines(width, height)
	}
	lines = append(lines, cyan.Bold(true).Render("  ◈ "+clip(title, max(width-5, 1))), rule.Render(strings.Repeat("─", max(width-1, 1))))
	meta := strings.Title(node.Kind)
	if node.Language != "" {
		meta += "  ·  " + node.Language
	}
	lines = append(lines, muted.Render("  "+meta))
	if node.File != "" {
		lines = append(lines, muted.Render("  "+clip(node.File, max(width-2, 1))))
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
		lines = append(lines, ink.Bold(true).Render(fmt.Sprintf("  Inside  /  %d", len(contained))))
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
		lines = append(lines, ink.Bold(true).Render(fmt.Sprintf("  Connections  /  %d out · %d in", len(relationships), len(users))), "")
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

func (m Model) overviewLines(width, height int) []string {
	counts := m.counts()
	routes, dependencies, dirs := 0, 0, 0
	for _, node := range m.Graph.Nodes {
		switch node.Kind {
		case "route":
			routes++
		case "package":
			dependencies++
		case "directory":
			dirs++
		}
	}
	lines := []string{cyan.Bold(true).Render("  ◈ System atlas"), rule.Render(strings.Repeat("─", max(width-1, 1)))}
	if width >= 88 {
		lines = append(lines, "")
	}
	lines = append(lines, ink.Bold(true).Render("  "+clip(filepath.Base(m.Graph.Root), max(width-2, 1))))
	if width >= 88 {
		lines = append(lines, muted.Render("  Explore the structure, then follow real requests."), "")
	}
	lines = append(lines,
		amber.Bold(true).Render(fmt.Sprintf("  %d", routes))+muted.Render(" routes")+"    "+cyan.Bold(true).Render(fmt.Sprintf("%d", counts.files))+muted.Render(" files")+"    "+ink.Bold(true).Render(fmt.Sprintf("%d", dependencies))+muted.Render(" dependencies"),
		muted.Render(fmt.Sprintf("  %d dirs  /  %d languages  /  %d links", dirs, counts.languages, len(m.Graph.Edges))),
	)
	if width >= 88 {
		lines = append(lines, "")
	}
	lines = append(lines, rule.Render("  ━━ Entry paths "+strings.Repeat("━", max(width-19, 0))))
	lines = append(lines, m.entryLines(width)...)
	lines = append(lines, "", rule.Render("  ━━ Language terrain "+strings.Repeat("━", max(width-23, 0))))
	lines = append(lines, m.languageBars(width)...)
	if width >= 88 {
		lines = append(lines, "", rule.Render("  ━━ How to explore "+strings.Repeat("━", max(width-20, 0))))
		lines = append(lines, muted.Render("  Select a component to see its connections."))
		if m.Live {
			lines = append(lines, amber.Render("  Press Tab to follow observed backend activity."))
		} else {
			lines = append(lines, muted.Render("  Press / to find a route, file, or dependency."))
		}
	}
	if len(m.Graph.Warnings) > 0 {
		lines = append(lines, "", amber.Render(fmt.Sprintf("  %d scan warnings; inspect map --json for details", len(m.Graph.Warnings))))
	}
	return padLines(lines, height)
}

func (m Model) entryLines(width int) []string {
	entries := []graph.Node{}
	for _, node := range m.Graph.Nodes {
		if node.Kind == "route" {
			entries = append(entries, node)
		}
	}
	if len(entries) == 0 {
		for _, edge := range m.Graph.Outgoing("project:root") {
			if edge.Kind == "entrypoint" {
				if node, ok := m.Graph.NodeByID(edge.To); ok {
					entries = append(entries, node)
				}
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Label < entries[j].Label })
	if len(entries) == 0 {
		return []string{muted.Render("  No route or entrypoint was identified in code."), muted.Render("  Search files to inspect the backend structure.")}
	}
	lines := []string{}
	for i, entry := range entries {
		if i >= 4 {
			lines = append(lines, muted.Render(fmt.Sprintf("  + %d more entries in the map", len(entries)-i)))
			break
		}
		connector := "├"
		if i == min(len(entries), 4)-1 {
			connector = "└"
		}
		lines = append(lines, cyan.Render("  "+connector+"─● "+clip(entry.Label, max(width-7, 1))))
		calls := m.Graph.Targets(entry.ID, "contains-call")
		for j, call := range calls {
			if j >= 2 {
				lines = append(lines, muted.Render(fmt.Sprintf("  │   └── + %d more calls", len(calls)-j)))
				break
			}
			lines = append(lines, muted.Render("  │   └──")+amber.Render("▶ "+clip(call.Label, max(width-13, 1))))
		}
	}
	return lines
}

func scrollLines(lines []string, height, offset int) []string {
	if len(lines) <= height {
		return padLines(lines, height)
	}
	offset = min(offset, len(lines)-height)
	return padLines(lines[offset:], height)
}

func (m Model) flowLines(request trace.Request, width int) []string {
	lines := []string{ink.Bold(true).Render("  Request path")}
	if len(request.Spans) == 0 {
		return append(lines, muted.Render("  No steps reported."))
	}
	byID := map[string]trace.Span{}
	maxDuration := time.Duration(0)
	for _, span := range request.Spans {
		byID[span.ID] = span
		if span.Duration > maxDuration {
			maxDuration = span.Duration
		}
	}
	previousService := ""
	for _, span := range request.Spans {
		depth := 0
		parent := span.ParentID
		seen := map[string]bool{}
		for parent != "" && depth < 5 && !seen[parent] {
			seen[parent] = true
			ancestor, ok := byID[parent]
			if !ok {
				break
			}
			depth++
			parent = ancestor.ParentID
		}
		if span.Service != previousService {
			lines = append(lines, "", muted.Render("  "+clip(span.Service, max(width-2, 1))))
			previousService = span.Service
		}
		indent := strings.Repeat("  ", depth)
		mark := "●"
		style := amber
		if span.Error {
			mark, style = "✕", red
		}
		labelWidth := max(width-19-depth*2, 8)
		label := "  " + indent + mark + " " + clip(span.Name, labelWidth)
		if span.System != "" {
			label += "  [" + clip(span.System, 12) + "]"
		}
		lines = append(lines, style.Render(clip(label, max(width-2, 1))))
		barWidth := min(18, max(width-24-depth*2, 4))
		filled := barWidth
		if maxDuration > 0 {
			filled = max(1, int(float64(span.Duration)/float64(maxDuration)*float64(barWidth)))
		}
		bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
		lines = append(lines, muted.Render("  "+indent+"  ")+style.Render(bar)+muted.Render("  "+span.Duration.Round(time.Millisecond).String()))
	}
	return lines
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
		"Tab             Cycle Map, Flow, and Activity", "f               Open or close Flow", "p / Space       Send an animated example request", "Mouse           Click Send request in Flow", "Page Up/Down    Scroll the selected trace detail", "?               Close this help", "q               Quit", "", "Evidence", "  ─ code         Found in the repository", "  ● runtime      Observed while the app ran",
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
