package scan

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	"nodryl/internal/graph"
)

func parseLanguageFile(path, rel, language string, g *graph.Graph) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parser := sitter.NewParser()
	defer parser.Close()
	var grammar *sitter.Language
	if language == "Go" {
		grammar = sitter.NewLanguage(golang.Language())
	} else {
		grammar = sitter.NewLanguage(python.Language())
	}
	if err := parser.SetLanguage(grammar); err != nil {
		return err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return fmt.Errorf("cannot parse %s", rel)
	}
	defer tree.Close()
	moduleID := "module:" + rel
	visit(tree.RootNode(), func(n *sitter.Node) {
		switch language {
		case "Go":
			switch n.Kind() {
			case "import_spec":
				if value := n.ChildByFieldName("path"); value != nil {
					addLanguageImport(g, moduleID, "go", unquote(value.Utf8Text(source)))
				}
			case "function_declaration", "method_declaration":
				if value := n.ChildByFieldName("name"); value != nil {
					name := value.Utf8Text(source)
					id := addSymbol(g, moduleID, rel, language, name, int(n.StartPosition().Row)+1)
					if name == "main" && strings.HasSuffix(rel, "main.go") {
						g.AddEdge(graph.Edge{From: "project:root", To: id, Kind: "entrypoint", Evidence: graph.Static})
					}
				}
			}
		case "Python":
			switch n.Kind() {
			case "import_statement":
				visit(n, func(child *sitter.Node) {
					if child.Kind() == "dotted_name" {
						addLanguageImport(g, moduleID, "pypi", child.Utf8Text(source))
					}
				})
			case "import_from_statement":
				if value := n.ChildByFieldName("module_name"); value != nil {
					addLanguageImport(g, moduleID, "pypi", value.Utf8Text(source))
				}
			case "function_definition", "class_definition":
				if value := n.ChildByFieldName("name"); value != nil {
					addSymbol(g, moduleID, rel, language, value.Utf8Text(source), int(n.StartPosition().Row)+1)
				}
			}
		}
	})
	return nil
}

func addLanguageImport(g *graph.Graph, from, ecosystem, target string) {
	if target == "" || strings.HasPrefix(target, ".") {
		return
	}
	name := target
	if ecosystem == "pypi" {
		name = strings.Split(target, ".")[0]
	}
	id := "package:" + ecosystem + ":" + name
	g.AddNode(graph.Node{ID: id, Label: name, Kind: "package", Language: ecosystem, Evidence: graph.Static})
	g.AddEdge(graph.Edge{From: from, To: id, Kind: "imports", Evidence: graph.Static})
}

func addSymbol(g *graph.Graph, from, file, language, name string, line int) string {
	id := "symbol:" + file + ":" + strconv.Itoa(line) + ":" + name
	g.AddNode(graph.Node{ID: id, Label: name, Kind: "symbol", Language: language, File: file, Line: line, Evidence: graph.Static})
	g.AddEdge(graph.Edge{From: from, To: id, Kind: "defines", Evidence: graph.Static})
	return id
}
