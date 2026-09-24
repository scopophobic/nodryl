package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"nodryl/internal/graph"
)

const framesPerHop = 10

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

var (
	particleDim = lipgloss.NewStyle().Foreground(lipgloss.Color("#527080"))
	returnColor = lipgloss.NewStyle().Foreground(lipgloss.Color("#91B9FF"))
	requestGlow = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF1C9")).Background(lipgloss.Color("#554023")).Bold(true)
	returnGlow  = lipgloss.NewStyle().Foreground(lipgloss.Color("#E7EEFF")).Background(lipgloss.Color("#27415E")).Bold(true)
)

type flowPath struct {
	Title    string
	Stages   []string
	Source   string
	Inferred bool
}

func nextMotion(epoch uint64) tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return motionMsg{epoch: epoch} })
}

func (m Model) cycleTab() (tea.Model, tea.Cmd) {
	if m.Flow {
		m.leaveFlow()
		m.Activity = m.Live
		m.Cursor, m.Scroll, m.DetailScroll = 0, 0, 0
		if !m.Activity {
			m.Cursor = -1
		}
		return m, nil
	}
	if m.Activity {
		m.Activity = false
		m.Cursor, m.Scroll, m.DetailScroll = -1, 0, 0
		return m, nil
	}
	return m.enterFlow()
}

func (m Model) toggleFlow() (tea.Model, tea.Cmd) {
	if m.Flow {
		m.leaveFlow()
		m.Activity = false
		return m, nil
	}
	return m.enterFlow()
}

func (m Model) enterFlow() (tea.Model, tea.Cmd) {
	m.Flow, m.Activity = true, false
	m.Search, m.Searching = "", false
	m.MotionEpoch++
	if m.ReducedMotion {
		return m, nil
	}
	return m, nextMotion(m.MotionEpoch)
}

func (m *Model) leaveFlow() {
	m.Flow = false
	m.MotionEpoch++
}

func (m *Model) moveFlow(direction int) {
	count := len(m.flowPaths())
	m.FlowIndex = max(0, min(m.FlowIndex+direction, count-1))
	m.PulseActive, m.PulseDone, m.PulseStep = false, false, 0
}

func (m *Model) triggerPulse() {
	m.PulseActive, m.PulseDone, m.PulseStep = !m.ReducedMotion, m.ReducedMotion, 0
}

func (m *Model) advancePulse() {
	if !m.PulseActive {
		return
	}
	m.PulseStep++
	hops := len(m.selectedFlowPath().Stages) - 1
	if m.PulseStep > 2*hops*framesPerHop {
		m.PulseActive, m.PulseDone, m.PulseStep = false, true, 0
	}
}

func (m Model) flowPaths() []flowPath {
	if len(m.paths) > 0 {
		return m.paths
	}
	return flowPathsFor(m.Graph)
}

func flowPathsFor(g graph.Graph) []flowPath {
	entries := []graph.Node{}
	byID := make(map[string]graph.Node, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
		if node.Kind == "route" {
			entries = append(entries, node)
		}
	}
	callsByEntry := map[string][]graph.Node{}
	for _, edge := range g.Edges {
		if edge.Kind == "contains-call" {
			if node, ok := byID[edge.To]; ok {
				callsByEntry[edge.From] = append(callsByEntry[edge.From], node)
			}
		}
	}
	if len(entries) == 0 {
		for _, edge := range g.Edges {
			if edge.From == "project:root" && edge.Kind == "entrypoint" {
				if node, ok := byID[edge.To]; ok {
					entries = append(entries, node)
				}
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Label < entries[j].Label })
	paths := make([]flowPath, 0, len(entries))
	for _, entry := range entries {
		path := flowPath{Title: entry.Label, Stages: []string{"Caller", entry.Label}, Inferred: true}
		if entry.File != "" {
			path.Source = fmt.Sprintf("%s:%d", entry.File, entry.Line)
		}
		calls := callsByEntry[entry.ID]
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].File == calls[j].File {
				return calls[i].Line < calls[j].Line
			}
			return calls[i].File < calls[j].File
		})
		for i, call := range calls {
			if i >= 2 {
				break
			}
			path.Stages = append(path.Stages, call.Label)
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return []flowPath{{Title: "Example backend request", Stages: []string{"Client", "API", "Service", "Database"}}}
	}
	return paths
}

func (m Model) selectedFlowPath() flowPath {
	paths := m.flowPaths()
	return paths[max(0, min(m.FlowIndex, len(paths)-1))]
}

func (m Model) pulsePhase(stages int) (string, int) {
	if !m.PulseActive {
		return "idle", 0
	}
	outbound := (stages - 1) * framesPerHop
	if m.PulseStep <= outbound {
		return "request", m.PulseStep
	}
	return "response", m.PulseStep - outbound
}

func flowButtonRow(height int) int {
	if height < 1 {
		height = 30
	}
	return height - 3
}

func (m Model) flowView(width, height int) string {
	path := m.selectedFlowPath()
	paths := m.flowPaths()
	phase, progress := m.pulsePhase(len(path.Stages))
	var lines []string
	brand := cyan.Bold(true).Render("  ◈ NODRYL") + muted.Render("  /  ") + ink.Bold(true).Render("Flow studio")
	lines = append(lines, fit(brand, width))
	activity := muted.Render("▱ Activity")
	if !m.Live {
		activity = muted.Render("· Activity")
	}
	lines = append(lines, fit("  "+muted.Render("▱ Map")+"   "+amber.Bold(true).Render("▰ Flow")+"   "+activity+"    "+shade.Render(filepathBase(m.Graph.Root)), width))
	lines = append(lines, rule.Render(strings.Repeat("━", width)), "")
	lines = append(lines, fit(amber.Bold(true).Render(fmt.Sprintf("  ◆ %s", path.Title))+muted.Render(fmt.Sprintf("   %d / %d", min(m.FlowIndex+1, len(paths)), len(paths))), width))
	context := "Code links in source order  •  animation is illustrative"
	if !path.Inferred {
		context = "Example flow  •  no entry path was found in code"
	}
	lines = append(lines, fit("  "+muted.Render(context), width), "")
	lines = append(lines, fit("  "+amber.Bold(true).Render("OUTBOUND")+rule.Render("  ━━ request enters the backend "+strings.Repeat("━", max(width-43, 0))), width))
	lines = append(lines, "")
	if width >= len(path.Stages)*12+(len(path.Stages)-1)*8+4 {
		lines = append(lines, m.horizontalFlow(path.Stages, width, phase, progress)...)
	} else {
		lines = append(lines, m.verticalFlow(path.Stages, width, phase, progress)...)
	}
	lines = append(lines, "")
	status := "Ready to send an example request"
	if m.PulseDone {
		status = "Round trip complete"
	}
	if phase == "request" {
		status = "Request moving through the backend"
	}
	if phase == "response" {
		status = "Result returning to the caller"
	}
	lines = append(lines, fit("  "+ink.Bold(true).Render(status), width))
	if path.Source != "" {
		lines = append(lines, fit("  "+cyan.Render("Code  "+path.Source), width))
	}
	if m.ReducedMotion {
		lines = append(lines, fit("  "+muted.Render("Reduced motion is enabled"), width))
	}
	for len(lines) < height-4 {
		lines = append(lines, "")
	}
	if len(lines) > height-4 {
		lines = lines[:height-4]
	}
	lines = append(lines, rule.Render(strings.Repeat("━", width)))
	button := "[ p  Send request ]"
	lines = append(lines, fit("  "+selectedLive.Render(button)+"   "+muted.Render("click or press p  •  ↑↓ choose route"), width))
	lines = append(lines, fit("  "+particleDim.Render("· ambient guide")+"   "+amber.Render("◆ request")+"   "+returnColor.Render("◆ response"), width))
	lines = append(lines, fit("  "+muted.Render("Tab change view   f map   ? help   q quit"), width))
	return strings.Join(lines, "\n")
}

func filepathBase(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func (m Model) horizontalFlow(stages []string, width int, phase string, progress int) []string {
	count := len(stages)
	edgeWidth := 8
	boxWidth := min(24, (width-4-(count-1)*edgeWidth)/count)
	total := count*boxWidth + (count-1)*edgeWidth
	indent := strings.Repeat(" ", max((width-total)/2, 0))
	top, middle, bottom := indent, indent, indent
	for i, label := range stages {
		style := rule
		if phase == "request" && abs(progress-i*framesPerHop) <= 1 {
			style = requestGlow
		}
		if phase == "response" && abs(progress-(count-1-i)*framesPerHop) <= 1 {
			style = returnGlow
		}
		top += style.Render("╭" + strings.Repeat("─", boxWidth-2) + "╮")
		middle += style.Render("│" + fit(clip(label, boxWidth-4), boxWidth-2) + "│")
		bottom += style.Render("╰" + strings.Repeat("─", boxWidth-2) + "╯")
		if i < count-1 {
			gap := strings.Repeat(" ", edgeWidth)
			top += gap
			bottom += gap
			middle += m.edgeLine(i, edgeWidth, phase, progress)
		}
	}
	return []string{fit(top, width), fit(middle, width), fit(bottom, width), "", fit("  "+returnColor.Render("INBOUND")+rule.Render("  ━━ result returns to caller"), width), fit(m.responseRail(total, boxWidth, edgeWidth, count, phase, progress, indent), width)}
}

func (m Model) edgeLine(index, width int, phase string, progress int) string {
	chars := []rune(strings.Repeat("─", width))
	chars[width-1] = '▶'
	ambient := (m.MotionFrame/2 + index*2) % (width - 1)
	chars[ambient] = '·'
	bright := -1
	if phase == "request" && progress/framesPerHop == index && progress%framesPerHop > 0 {
		bright = min(width-2, progress%framesPerHop*(width-1)/framesPerHop)
		chars[bright] = '◆'
	}
	var out strings.Builder
	for i, char := range chars {
		switch {
		case i == bright:
			out.WriteString(amber.Bold(true).Render(string(char)))
		case i == ambient:
			out.WriteString(particleDim.Render(string(char)))
		default:
			out.WriteString(rule.Render(string(char)))
		}
	}
	return out.String()
}

func (m Model) responseRail(total, boxWidth, edgeWidth, count int, phase string, progress int, indent string) string {
	first, last := boxWidth/2, (count-1)*(boxWidth+edgeWidth)+boxWidth/2
	chars := []rune(strings.Repeat(" ", total))
	for i := first; i <= last; i++ {
		chars[i] = '━'
	}
	chars[first] = '◀'
	bright := -1
	if phase == "response" {
		bright = last - progress*(last-first)/((count-1)*framesPerHop)
		chars[bright] = '◆'
	}
	var out strings.Builder
	out.WriteString(indent)
	for i, char := range chars {
		if i == bright {
			out.WriteString(returnColor.Bold(true).Render(string(char)))
		} else {
			out.WriteString(particleDim.Render(string(char)))
		}
	}
	return out.String()
}

func (m Model) verticalFlow(stages []string, width int, phase string, progress int) []string {
	lines := []string{}
	for i, label := range stages {
		style := cyan
		if phase == "request" && abs(progress-i*framesPerHop) <= 1 {
			style = requestGlow
		}
		if phase == "response" && abs(progress-(len(stages)-1-i)*framesPerHop) <= 1 {
			style = returnGlow
		}
		lines = append(lines, fit("  "+style.Render("◆ "+clip(label, max(width-10, 1))), width))
		if i < len(stages)-1 {
			mark := particleDim.Render("·")
			if phase == "request" && progress/framesPerHop == i {
				mark = amber.Render("◆")
			}
			lines = append(lines, fit("  "+rule.Render("│")+"  "+mark, width))
		}
	}
	lines = append(lines, fit("  "+returnColor.Render("↑ result returns along the same path"), width))
	if phase == "response" {
		lines = append(lines, fit("  "+returnColor.Render("◆ returning"), width))
	}
	return lines
}
