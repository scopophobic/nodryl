package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	"nodryl/internal/graph"
)

var verbs = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "options": true, "head": true}
var excluded = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	"coverage": true, ".next": true, "vendor": true, "target": true,
	".venv": true, "venv": true, "__pycache__": true, ".gradle": true,
	".tox": true, "Pods": true, ".build": true,
}

func Project(root string) (graph.Graph, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return graph.Graph{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return graph.Graph{}, err
	}
	if !info.IsDir() {
		return graph.Graph{}, fmt.Errorf("%s is not a directory", abs)
	}
	g := graph.Graph{Root: abs}
	g.AddNode(graph.Node{ID: "project:root", Label: filepath.Base(abs), Kind: "project", Evidence: graph.Static})
	files := 0
	err = filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != abs && excluded[entry.Name()] {
				return filepath.SkipDir
			}
			if path != abs {
				rel, _ := filepath.Rel(abs, path)
				rel = filepath.ToSlash(rel)
				g.AddNode(graph.Node{ID: "dir:" + rel, Label: entry.Name(), Kind: "directory", File: rel, Evidence: graph.Static})
				g.AddEdge(graph.Edge{From: parentID(rel), To: "dir:" + rel, Kind: "contains", Evidence: graph.Static})
			}
			return nil
		}
		if entry.Name() == ".DS_Store" {
			return nil
		}
		files++
		if files > 50000 {
			return fmt.Errorf("project exceeds 50,000 files after exclusions")
		}
		rel, _ := filepath.Rel(abs, path)
		rel = filepath.ToSlash(rel)
		language := languageFor(entry.Name())
		kind := "file"
		id := "file:" + rel
		if language != "" {
			kind, id = "module", "module:"+rel
		}
		if isManifest(entry.Name()) {
			kind, id = "manifest", "manifest:"+rel
		}
		g.AddNode(graph.Node{ID: id, Label: entry.Name(), Kind: kind, Language: language, File: rel, Evidence: graph.Static})
		g.AddEdge(graph.Edge{From: parentID(rel), To: id, Kind: "contains", Evidence: graph.Static})
		if kind == "manifest" {
			if err := parseManifest(path, id, &g); err != nil {
				g.Warnings = append(g.Warnings, rel+": "+err.Error())
			}
		}
		if language == "JavaScript" || language == "TypeScript" {
			if strings.HasSuffix(path, ".d.ts") || strings.HasSuffix(path, ".min.js") {
				return nil
			}
			info, err := entry.Info()
			if err == nil && info.Size() <= 2<<20 {
				if err := parseFile(abs, path, &g); err != nil {
					g.Warnings = append(g.Warnings, rel+": "+err.Error())
				}
			}
		} else if language == "Go" || language == "Python" {
			info, err := entry.Info()
			if err == nil && info.Size() <= 2<<20 {
				if err := parseLanguageFile(path, rel, language, &g); err != nil {
					g.Warnings = append(g.Warnings, rel+": "+err.Error())
				}
			}
		}
		return nil
	})
	return g, err
}

func parentID(rel string) string {
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		return "project:root"
	}
	return "dir:" + parent
}

func languageFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "TypeScript"
	case ".go":
		return "Go"
	case ".py", ".pyi":
		return "Python"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".c", ".h":
		return "C"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "C++"
	case ".cs":
		return "C#"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".swift":
		return "Swift"
	case ".dart":
		return "Dart"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".html", ".htm":
		return "HTML"
	case ".css", ".scss":
		return "CSS"
	case ".vue":
		return "Vue"
	case ".svelte":
		return "Svelte"
	default:
		return ""
	}
}

func parseFile(root, path string, g *graph.Graph) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parser := sitter.NewParser()
	defer parser.Close()
	var language *sitter.Language
	switch filepath.Ext(path) {
	case ".ts", ".mts", ".cts":
		language = sitter.NewLanguage(typescript.LanguageTypescript())
	case ".tsx":
		language = sitter.NewLanguage(typescript.LanguageTSX())
	default:
		language = sitter.NewLanguage(javascript.Language())
	}
	if err := parser.SetLanguage(language); err != nil {
		return err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return fmt.Errorf("cannot parse %s", path)
	}
	defer tree.Close()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	moduleID := "module:" + rel
	g.AddNode(graph.Node{ID: moduleID, Label: rel, Kind: "module", File: rel, Evidence: graph.Static})
	type spanRange struct {
		id         string
		start, end uint
	}
	routes := []spanRange{}
	calls := []spanRange{}
	visit(tree.RootNode(), func(n *sitter.Node) {
		switch n.Kind() {
		case "import_statement":
			for i := uint(0); i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child != nil && child.Kind() == "string" {
					addImport(g, moduleID, rel, unquote(child.Utf8Text(source)), int(child.StartPosition().Row)+1)
				}
			}
		case "call_expression":
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn == nil || args == nil {
				return
			}
			if fn.Kind() == "identifier" && fn.Utf8Text(source) == "require" && args.NamedChildCount() > 0 {
				first := args.NamedChild(0)
				if first.Kind() == "string" {
					addImport(g, moduleID, rel, unquote(first.Utf8Text(source)), int(first.StartPosition().Row)+1)
				}
			}
			if fn.Kind() == "identifier" && fn.Utf8Text(source) == "fetch" {
				calls = append(calls, spanRange{addCallSite(g, moduleID, rel, n, "outbound-http", "fetch"), n.StartByte(), n.EndByte()})
			}
			if fn.Kind() != "member_expression" || args.NamedChildCount() == 0 {
				return
			}
			object := fn.ChildByFieldName("object")
			property := fn.ChildByFieldName("property")
			if object == nil || property == nil {
				return
			}
			verb := property.Utf8Text(source)
			owner := object.Utf8Text(source)
			switch {
			case (owner == "http" || owner == "https" || owner == "axios") && (verb == "get" || verb == "post" || verb == "request"):
				calls = append(calls, spanRange{addCallSite(g, moduleID, rel, n, "outbound-http", owner+"."+verb), n.StartByte(), n.EndByte()})
			case (owner == "pool" || owner == "db") && verb == "query":
				calls = append(calls, spanRange{addCallSite(g, moduleID, rel, n, "database", owner+".query"), n.StartByte(), n.EndByte()})
			case owner == "redis" && (verb == "get" || verb == "set" || verb == "del"):
				calls = append(calls, spanRange{addCallSite(g, moduleID, rel, n, "cache", owner+"."+verb), n.StartByte(), n.EndByte()})
			}
			if !verbs[verb] {
				return
			}
			if owner != "app" && owner != "router" && !strings.HasSuffix(owner, "Router") {
				return
			}
			first := args.NamedChild(0)
			if first.Kind() != "string" {
				return
			}
			route := unquote(first.Utf8Text(source))
			if !strings.HasPrefix(route, "/") {
				return
			}
			label := strings.ToUpper(verb) + " " + route
			id := "route:" + rel + ":" + strconv.Itoa(int(n.StartPosition().Row)+1)
			g.AddNode(graph.Node{ID: id, Label: label, Kind: "route", File: rel, Line: int(n.StartPosition().Row) + 1, Evidence: graph.Static})
			g.AddEdge(graph.Edge{From: moduleID, To: id, Kind: "defines", Evidence: graph.Static})
			routes = append(routes, spanRange{id, n.StartByte(), n.EndByte()})
		}
	})
	for _, route := range routes {
		for _, call := range calls {
			if call.start >= route.start && call.end <= route.end {
				g.AddEdge(graph.Edge{From: route.id, To: call.id, Kind: "contains-call", Evidence: graph.Static})
			}
		}
	}
	return nil
}

func visit(n *sitter.Node, fn func(*sitter.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for i := uint(0); i < n.NamedChildCount(); i++ {
		visit(n.NamedChild(i), fn)
	}
}

func unquote(raw string) string {
	if len(raw) < 2 {
		return raw
	}
	return strings.Trim(raw, "\"'`")
}

func addImport(g *graph.Graph, from, file, target string, line int) {
	if target == "" {
		return
	}
	if strings.HasPrefix(target, ".") {
		base := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(file), target)))
		for _, candidate := range []string{base, base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".cjs", base + ".mjs", base + "/index.ts", base + "/index.js"} {
			if info, err := os.Stat(filepath.Join(g.Root, filepath.FromSlash(candidate))); err == nil && !info.IsDir() {
				base = candidate
				break
			}
		}
		id := "module:" + base
		g.AddNode(graph.Node{ID: id, Label: base, Kind: "module", File: base, Evidence: graph.Static})
		g.AddEdge(graph.Edge{From: from, To: id, Kind: "imports", Evidence: graph.Static})
		return
	}
	parts := strings.Split(target, "/")
	name := parts[0]
	if strings.HasPrefix(name, "@") && len(parts) > 1 {
		name += "/" + parts[1]
	}
	id := "package:npm:" + name
	g.AddNode(graph.Node{ID: id, Label: name, Kind: "package", Language: "npm", File: file, Line: line, Evidence: graph.Static})
	g.AddEdge(graph.Edge{From: from, To: id, Kind: "imports", Evidence: graph.Static})
}

func addCallSite(g *graph.Graph, moduleID, file string, n *sitter.Node, kind, label string) string {
	line := int(n.StartPosition().Row) + 1
	id := "call:" + file + ":" + strconv.Itoa(int(n.StartByte()))
	g.AddNode(graph.Node{ID: id, Label: label, Kind: kind, File: file, Line: line, Evidence: graph.Static})
	g.AddEdge(graph.Edge{From: moduleID, To: id, Kind: "contains", Evidence: graph.Static})
	return id
}
