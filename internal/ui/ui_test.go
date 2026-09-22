package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
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
