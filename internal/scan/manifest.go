package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nodryl/internal/graph"
)

var manifestNames = map[string]bool{
	"package.json": true, "go.mod": true, "Cargo.toml": true,
	"pyproject.toml": true, "requirements.txt": true, "Pipfile": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"Gemfile": true, "composer.json": true, "pubspec.yaml": true,
	"CMakeLists.txt": true, "Makefile": true, "Dockerfile": true,
	"mix.exs": true, "Package.swift": true, "project.clj": true,
}

func isManifest(name string) bool {
	return manifestNames[name] || strings.HasSuffix(name, ".csproj") || strings.HasSuffix(name, ".fsproj")
}

func parseManifest(path, id string, g *graph.Graph) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return fmt.Errorf("manifest exceeds 2 MB")
	}
	name := filepath.Base(path)
	add := func(ecosystem, dependency string) {
		dependency = strings.TrimSpace(dependency)
		if dependency == "" || strings.ContainsAny(dependency, "$*{}") {
			return
		}
		depID := "package:" + ecosystem + ":" + dependency
		g.AddNode(graph.Node{ID: depID, Label: dependency, Kind: "package", Language: ecosystem, Evidence: graph.Static})
		g.AddEdge(graph.Edge{From: id, To: depID, Kind: "depends", Evidence: graph.Static})
	}
	switch name {
	case "package.json", "composer.json":
		var doc struct {
			Dependencies map[string]json.RawMessage `json:"dependencies"`
			Require      map[string]json.RawMessage `json:"require"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		deps := doc.Dependencies
		ecosystem := "npm"
		if name == "composer.json" {
			deps, ecosystem = doc.Require, "composer"
		}
		keys := make([]string, 0, len(deps))
		for key := range deps {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			add(ecosystem, key)
		}
	case "go.mod":
		inBlock := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
			if line == "require (" {
				inBlock = true
				continue
			}
			if line == ")" {
				inBlock = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if fields[0] == "require" && len(fields) >= 3 {
				add("go", fields[1])
			}
			if inBlock {
				add("go", fields[0])
			}
		}
	case "requirements.txt":
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			if line == "" || strings.HasPrefix(line, "-") {
				continue
			}
			parts := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune("<>=!~;[ ", r) })
			if len(parts) > 0 {
				add("pypi", parts[0])
			}
		}
	case "Cargo.toml":
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			if strings.HasPrefix(line, "[") {
				section = line
				continue
			}
			if section != "[dependencies]" && section != "[dev-dependencies]" && section != "[build-dependencies]" {
				continue
			}
			if before, _, ok := strings.Cut(line, "="); ok {
				add("cargo", strings.TrimSpace(before))
			}
		}
	case "pyproject.toml":
		section := ""
		inDependencies := false
		quoted := regexp.MustCompile(`['"]([^'"]+)['"]`)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			if strings.HasPrefix(line, "[") {
				section, inDependencies = line, false
				continue
			}
			if section == "[tool.poetry.dependencies]" {
				if before, _, ok := strings.Cut(line, "="); ok && strings.TrimSpace(before) != "python" {
					add("pypi", strings.TrimSpace(before))
				}
			}
			if section == "[project]" && strings.HasPrefix(line, "dependencies") {
				inDependencies = true
			}
			if inDependencies {
				for _, match := range quoted.FindAllStringSubmatch(line, -1) {
					value := strings.FieldsFunc(match[1], func(r rune) bool { return strings.ContainsRune("<>=!~;[ ", r) })
					if len(value) > 0 {
						add("pypi", value[0])
					}
				}
				if strings.Contains(line, "]") {
					inDependencies = false
				}
			}
		}
	case "Gemfile":
		gem := regexp.MustCompile(`^gem\s+['"]([^'"]+)['"]`)
		for _, line := range strings.Split(string(data), "\n") {
			if match := gem.FindStringSubmatch(strings.TrimSpace(line)); len(match) > 1 {
				add("rubygems", match[1])
			}
		}
	case "pubspec.yaml":
		inDeps := false
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "dependencies:" {
				inDeps = true
				continue
			}
			if inDeps && line != "" && line[0] != ' ' {
				inDeps = false
			}
			if inDeps {
				trimmed := strings.TrimSpace(line)
				if before, _, ok := strings.Cut(trimmed, ":"); ok {
					add("pub", before)
				}
			}
		}
	}
	return nil
}
