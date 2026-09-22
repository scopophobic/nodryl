package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMixedRepositoryKeepsUnknownFilesAndExtractsKnownEvidence(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"Cargo.toml":        "[package]\nname = 'sample'\n[dependencies]\nserde = '1'\n",
		"src/main.rs":       "fn main() {}\n",
		"app/main.py":       "import requests\ndef start():\n    pass\n",
		"service/main.go":   "package main\nimport \"fmt\"\nfunc main() { fmt.Println(1) }\n",
		"notes/diagram.xyz": "opaque project file\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	g, err := Project(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"project:root", "dir:src", "module:src/main.rs", "module:app/main.py",
		"module:service/main.go", "file:notes/diagram.xyz", "manifest:Cargo.toml",
		"package:cargo:serde", "package:pypi:requests",
	} {
		if _, ok := g.NodeByID(id); !ok {
			t.Errorf("missing %s", id)
		}
	}
	goEntry := false
	for _, edge := range g.Edges {
		if edge.Kind == "entrypoint" && edge.From == "project:root" {
			goEntry = true
		}
	}
	if !goEntry {
		t.Error("Go main entrypoint not detected")
	}
	if len(g.Warnings) != 0 {
		t.Fatalf("scan warnings: %v", g.Warnings)
	}
}
