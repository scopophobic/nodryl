package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbletea"
	"nodryl/internal/graph"
	"nodryl/internal/scan"
	"nodryl/internal/trace"
	"nodryl/internal/ui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nodryl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		info, err := os.Stdin.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice != 0 {
			args = []string{"explore"}
		} else {
			args = []string{"map"}
		}
	}
	switch args[0] {
	case "explore":
		if len(args) != 1 {
			return errors.New("usage: nodryl explore")
		}
		g, err := scan.Project(".")
		if err != nil {
			return err
		}
		program := tea.NewProgram(ui.NewMap(g), tea.WithAltScreen())
		_, err = program.Run()
		return err
	case "map":
		if len(args) > 2 || len(args) == 2 && args[1] != "--json" {
			return errors.New("usage: nodryl map [--json]")
		}
		g, err := scan.Project(".")
		if err != nil {
			return err
		}
		if len(args) == 2 {
			return json.NewEncoder(os.Stdout).Encode(g)
		}
		printMap(g)
		return nil
	case "dev":
		if len(args) < 3 || args[1] != "--" {
			return errors.New("usage: nodryl dev -- <app command>")
		}
		return runDev(args[2:])
	case "help", "--help", "-h":
		fmt.Println("Nodryl — see how a project fits together\n\n  nodryl                     explore this project in the terminal\n  nodryl explore             open the project map\n  nodryl map [--json]        print the map or export its graph\n  nodryl dev -- <command>    explore with live Node tracing")
		return nil
	default:
		return fmt.Errorf("unknown command %q; run nodryl help", args[0])
	}
}

func printMap(g graph.Graph) {
	routes, modules, packages, manifests := 0, 0, 0, 0
	languages := map[string]int{}
	for _, node := range g.Nodes {
		switch node.Kind {
		case "route":
			routes++
		case "module":
			modules++
		case "package":
			packages++
		case "manifest":
			manifests++
		}
		if node.Kind == "module" && node.Language != "" {
			languages[node.Language]++
		}
	}
	fmt.Printf("NODRYL MAP  %s\n\n%d source files · %d manifests · %d dependencies · %d routes\n", g.Root, modules, manifests, packages, routes)
	if len(languages) > 0 {
		keys := make([]string, 0, len(languages))
		for key := range languages {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Print("Languages: ")
		for i, key := range keys {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s %d", key, languages[key])
		}
		fmt.Println()
	}
	fmt.Println()
	for _, node := range g.Nodes {
		if node.Kind == "route" || node.Kind == "manifest" {
			fmt.Printf("  %-28s %s:%d\n", node.Label, node.File, node.Line)
			for _, call := range g.Targets(node.ID, "contains-call") {
				fmt.Printf("    └─ %-26s %s:%d\n", call.Label, call.File, call.Line)
			}
		}
	}
	if len(g.Warnings) > 0 {
		fmt.Printf("\n%d scan warnings; use --json for details\n", len(g.Warnings))
	}
}

func runDev(command []string) error {
	if runtime.GOOS == "windows" {
		return errors.New("live mode currently supports macOS and Linux")
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return errors.New("live mode needs an interactive terminal")
	}
	g, err := scan.Project(".")
	if err != nil {
		return err
	}
	preload, err := preloadPath()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	store := trace.NewStore()
	server := &http.Server{Handler: trace.Handler(store), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	app := exec.Command(command[0], command[1:]...)
	app.Dir = g.Root
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	previous := os.Getenv("NODE_OPTIONS")
	if previous != "" {
		previous += " "
	}
	app.Env = append(os.Environ(),
		"NODE_OPTIONS="+previous+"--require "+strconv.Quote(preload),
		"NODRYL_OTLP_ENDPOINT=http://"+listener.Addr().String(),
		"OTEL_SERVICE_NAME="+filepath.Base(g.Root),
	)
	output := &lastOutput{}
	app.Stdout, app.Stderr = output, output
	if err := app.Start(); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	exited := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		err := app.Wait()
		close(done)
		if err != nil && output.String() != "" {
			err = fmt.Errorf("%w: %s", err, output.String())
		}
		exited <- err
	}()
	program := tea.NewProgram(ui.New(g, store, exited), tea.WithAltScreen())
	_, uiErr := program.Run()
	_ = syscall.Kill(-app.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-app.Process.Pid, syscall.SIGKILL)
	}
	return uiErr
}

func preloadPath() (string, error) {
	if path := os.Getenv("NODRYL_HELPER_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return filepath.Abs(path)
		}
		return "", fmt.Errorf("tracing helper not found at %s", path)
	}
	path := filepath.Join("bin", "preload.cjs")
	if _, err := os.Stat(path); err == nil {
		return filepath.Abs(path)
	}
	return "", errors.New("tracing helper missing; install the Nodryl package or run from its repository")
}

type lastOutput struct {
	mu   sync.Mutex
	text string
}

func (w *lastOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.text += string(p)
	if len(w.text) > 2000 {
		w.text = w.text[len(w.text)-2000:]
	}
	return len(p), nil
}
func (w *lastOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(w.text)
}

var _ io.Writer = (*lastOutput)(nil)
