package scan

import (
	"path/filepath"
	"testing"
)

func TestExpressTypeScriptRoutes(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g, err := Project(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"GET /checkout": false, "GET /payment": false}
	callFound := false
	callID, checkoutID := "", ""
	for _, n := range g.Nodes {
		if n.Kind == "outbound-http" && n.Label == "http.get" {
			callFound = true
			callID = n.ID
		}
		if n.Label == "GET /checkout" {
			checkoutID = n.ID
		}
		if _, ok := want[n.Label]; ok {
			if n.Kind != "route" || n.File != "server.ts" || n.Line < 1 {
				t.Fatalf("invalid route evidence: %+v", n)
			}
			want[n.Label] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing %s", route)
		}
	}
	if !callFound {
		t.Error("missing outbound HTTP call site")
	}
	linked := false
	for _, e := range g.Edges {
		if e.From == checkoutID && e.To == callID && e.Kind == "contains-call" {
			linked = true
		}
	}
	if !linked {
		t.Error("checkout route is not linked to its call site")
	}
}
