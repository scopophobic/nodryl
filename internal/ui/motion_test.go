package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"nodryl/internal/graph"
)

func motionTestGraph() graph.Graph {
	return graph.Graph{Root: "/project", Nodes: []graph.Node{
		{ID: "route:checkout", Kind: "route", Label: "GET /checkout", File: "server.ts", Line: 4},
		{ID: "call:db", Kind: "database", Label: "db.query", File: "server.ts", Line: 8},
		{ID: "call:http", Kind: "outbound-http", Label: "http.get", File: "server.ts", Line: 12},
	}, Edges: []graph.Edge{
		{From: "route:checkout", To: "call:db", Kind: "contains-call"},
		{From: "route:checkout", To: "call:http", Kind: "contains-call"},
	}}
}

func TestFlowPulseAndReturn(t *testing.T) {
	m := NewMap(motionTestGraph())
	m.Width, m.Height = 100, 26
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if !m.Flow || cmd == nil {
		t.Fatal("Flow did not start its animation clock")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Flow studio", "GET /checkout", "db.query", "http.get", "Send request"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Flow omits %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.Width {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if phase, _ := m.pulsePhase(4); phase != "request" {
		t.Fatalf("phase = %q", phase)
	}
	for i := 0; i <= 3*framesPerHop; i++ {
		updated, _ = m.Update(motionMsg{epoch: m.MotionEpoch})
		m = updated.(Model)
	}
	if phase, _ := m.pulsePhase(4); phase != "response" {
		t.Fatalf("phase = %q", phase)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Result returning") {
		t.Fatal("response motion is not visible")
	}
	for m.PulseActive {
		updated, _ = m.Update(motionMsg{epoch: m.MotionEpoch})
		m = updated.(Model)
	}
	if !m.PulseDone || !strings.Contains(ansi.Strip(m.View()), "Round trip complete") {
		t.Fatal("round trip did not finish")
	}
}

func TestAmbientDotsAndMouseTrigger(t *testing.T) {
	m := NewMap(motionTestGraph())
	m.Width, m.Height = 80, 24
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	first := ansi.Strip(m.edgeLine(0, 8, "idle", 0))
	updated, _ = m.Update(motionMsg{epoch: m.MotionEpoch})
	m = updated.(Model)
	updated, _ = m.Update(motionMsg{epoch: m.MotionEpoch})
	m = updated.(Model)
	second := ansi.Strip(m.edgeLine(0, 8, "idle", 0))
	if first == second {
		t.Fatal("ambient dot did not move")
	}
	updated, _ = m.Update(tea.MouseMsg{X: 5, Y: flowButtonRow(m.Height), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if !m.PulseActive {
		t.Fatal("Send request button did not trigger")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if m.Searching {
		t.Fatal("hidden map search opened from Flow")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if m.Flow {
		t.Fatal("Flow did not close")
	}
	updated, _ = m.Update(motionMsg{epoch: m.MotionEpoch - 1})
	m = updated.(Model)
	if m.Flow {
		t.Fatal("stale tick reopened Flow")
	}
}

func TestFlowIllustrationAndReducedMotion(t *testing.T) {
	m := NewMap(graph.Graph{Root: "/project"})
	m.ReducedMotion, m.Width, m.Height = true, 80, 24
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("reduced motion scheduled a tick")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Example flow") || !strings.Contains(view, "no entry path was found") {
		t.Fatal("illustrative fallback is not labeled")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if m.PulseActive || !m.PulseDone {
		t.Fatal("reduced motion started an animation")
	}
}

func TestFlowFitsNarrowTerminal(t *testing.T) {
	m := NewMap(motionTestGraph())
	m.Flow, m.Width, m.Height = true, 44, 24
	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != m.Height {
		t.Fatalf("got %d rows, want %d", len(lines), m.Height)
	}
	if !strings.Contains(view, "Send request") || !strings.Contains(view, "GET /checkout") {
		t.Fatal("narrow Flow lost its controls or path")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.Width {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}
