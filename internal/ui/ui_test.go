package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"nodryl/internal/graph"
	"nodryl/internal/trace"
)

func TestFocusedRequestLinksRouteSource(t *testing.T) {
	g := graph.Graph{Root: "/project", Nodes: []graph.Node{{ID: "route:checkout", Label: "GET /checkout", Kind: "route", File: "src/server.ts", Line: 7}}}
	m := New(g, trace.NewStore(), make(chan error))
	m.Activity = true
	m.Cursor = 0
	m.Requests = []trace.Request{{Name: "GET /checkout", Spans: []trace.Span{{Name: "GET /checkout"}}}}
	if got := m.selectedSource(); got != "/project/src/server.ts:7" {
		t.Fatalf("source = %q", got)
	}
	if !strings.Contains(m.View(), "GET /checkout") {
		t.Fatal("request detail missing")
	}
}

func TestObservedFlowShowsServiceTimingAndFailure(t *testing.T) {
	m := NewObserved(graph.Graph{Root: "/project"}, trace.NewStore(), "http://127.0.0.1:4318")
	m.Activity, m.Cursor, m.Width, m.Height = true, 0, 100, 28
	m.Requests = []trace.Request{{Name: "GET /checkout", Spans: []trace.Span{
		{ID: "root", Name: "GET /checkout", Service: "checkout-api", Duration: 20 * time.Millisecond},
		{ID: "child", ParentID: "root", Name: "payment", Service: "checkout-api", Duration: 8 * time.Millisecond, Error: true},
	}}}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Recent requests", "Request path", "checkout-api", "payment", "8ms", "Receive traces at"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q from view", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.Width {
			t.Fatalf("line exceeds terminal width: %q", line)
		}
	}
}

func TestOverviewShowsDetectedEntryPath(t *testing.T) {
	g := graph.Graph{Root: "/project", Nodes: []graph.Node{
		{ID: "project:root", Kind: "project", Label: "project"},
		{ID: "route:checkout", Kind: "route", Label: "GET /checkout"},
		{ID: "call:payment", Kind: "outbound-http", Label: "http.get"},
	}, Edges: []graph.Edge{{From: "route:checkout", To: "call:payment", Kind: "contains-call"}}}
	m := NewMap(g)
	m.Width, m.Height = 120, 30
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Entry paths") || !strings.Contains(view, "GET /checkout") || !strings.Contains(view, "http.get") {
		t.Fatal("overview omits detected backend path")
	}
}

func TestMapOpensDirectoryAndShowsUnknownFile(t *testing.T) {
	g := graph.Graph{Root: "/project", Nodes: []graph.Node{
		{ID: "project:root", Label: "project", Kind: "project"},
		{ID: "dir:docs", Label: "docs", Kind: "directory", File: "docs"},
		{ID: "file:docs/sketch.xyz", Label: "sketch.xyz", Kind: "file", File: "docs/sketch.xyz"},
	}, Edges: []graph.Edge{
		{From: "project:root", To: "dir:docs", Kind: "contains"},
		{From: "dir:docs", To: "file:docs/sketch.xyz", Kind: "contains"},
	}}
	m := NewMap(g)
	m.Cursor = 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.Current != "dir:docs" {
		t.Fatalf("current = %s", m.Current)
	}
	if !strings.Contains(m.View(), "sketch.xyz") {
		t.Fatal("unknown file is absent from map")
	}
}
