package trace

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	traces "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestIngestKeepsOnlyDisplayFields(t *testing.T) {
	s := NewStore()
	now := uint64(time.Now().UnixNano())
	span := &traces.Span{
		TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8),
		Name: "GET /checkout", StartTimeUnixNano: now, EndTimeUnixNano: now + 1000000,
		Attributes: []*common.KeyValue{
			{Key: "http.route", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "/checkout"}}},
			{Key: "http.request.method", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "GET"}}},
			{Key: "db.statement", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "secret SQL"}}},
		},
	}
	unsafeSpan := &traces.Span{
		TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{3}, 8), ParentSpanId: span.SpanId,
		Name: "SELECT * FROM users WHERE token='secret'", StartTimeUnixNano: now, EndTimeUnixNano: now + 1000000,
		Attributes: []*common.KeyValue{
			{Key: "db.system.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "postgresql"}}},
			{Key: "db.statement", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "secret SQL"}}},
		},
	}
	batch := &collector.ExportTraceServiceRequest{ResourceSpans: []*traces.ResourceSpans{
		{ScopeSpans: []*traces.ScopeSpans{{Spans: []*traces.Span{span, unsafeSpan}}}},
	}}
	data, err := proto.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(data); err != nil {
		t.Fatal(err)
	}
	requests := s.Snapshot()
	if len(requests) != 1 || requests[0].Name != "GET /checkout" || len(requests[0].Spans) != 2 {
		t.Fatalf("unexpected requests: %+v", requests)
	}
	for _, got := range requests[0].Spans {
		if strings.Contains(got.Name, "secret") || strings.Contains(got.Name, "SELECT") {
			t.Fatal("unsafe span text leaked")
		}
	}
}

func TestExpressRequestProducesLiveSpans(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is unavailable")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	serverFile := filepath.Join(root, "test", "fixture", "dist", "server.js")
	if _, err := os.Stat(serverFile); err != nil {
		t.Skip("run npm run build:fixture first")
	}
	store := NewStore()
	otlp := httptest.NewServer(Handler(store))
	defer otlp.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cmd := exec.Command("node", serverFile)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PORT="+strconv.Itoa(port),
		"NODE_OPTIONS=--require "+strconv.Quote(filepath.Join(root, "bin", "preload.cjs")),
		"NODRYL_OTLP_ENDPOINT="+otlp.URL,
		"OTEL_SERVICE_NAME=nodryl-fixture",
	)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/checkout"
	var response *http.Response
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("fixture did not start: %v; output: %s", err, output.String())
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("checkout returned %d", response.StatusCode)
	}
	deadline = time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		for _, request := range store.Snapshot() {
			if request.Name == "GET /checkout" && len(request.Spans) >= 2 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no correlated checkout spans received; requests: %+v; output: %s", store.Snapshot(), output.String())
}
