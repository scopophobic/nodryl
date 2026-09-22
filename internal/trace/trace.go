package trace

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

type Span struct {
	ID       string
	ParentID string
	Name     string
	Start    time.Time
	Duration time.Duration
	Error    bool
	System   string
	Method   string
	Route    string
}

type Request struct {
	ID    string
	Name  string
	Start time.Time
	Spans []Span
}

type Store struct {
	mu       sync.RWMutex
	requests map[string]*Request
	order    []string
	changed  chan struct{}
}

func NewStore() *Store {
	return &Store{requests: make(map[string]*Request), changed: make(chan struct{}, 1)}
}
func (s *Store) Changed() <-chan struct{} { return s.changed }

func (s *Store) Snapshot() []Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Request, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if item := s.requests[s.order[i]]; item != nil {
			copyItem := *item
			copyItem.Spans = append([]Span(nil), item.Spans...)
			sort.Slice(copyItem.Spans, func(i, j int) bool { return copyItem.Spans[i].Start.Before(copyItem.Spans[j].Start) })
			out = append(out, copyItem)
		}
	}
	return out
}

func (s *Store) Ingest(payload []byte) error {
	var batch collector.ExportTraceServiceRequest
	if err := proto.Unmarshal(payload, &batch); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, resource := range batch.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			for _, raw := range scope.Spans {
				id := hex.EncodeToString(raw.TraceId)
				if id == "" {
					continue
				}
				attrs := attributes(raw.Attributes)
				end := raw.EndTimeUnixNano
				if end < raw.StartTimeUnixNano {
					end = raw.StartTimeUnixNano
				}
				statusCode := first(attrs, "http.response.status_code", "http.status_code")
				span := Span{
					ID: hex.EncodeToString(raw.SpanId), ParentID: hex.EncodeToString(raw.ParentSpanId),
					Name: "operation", Start: time.Unix(0, int64(raw.StartTimeUnixNano)),
					Duration: time.Duration(end - raw.StartTimeUnixNano),
					Error:    raw.Status != nil && raw.Status.Code == 2 || strings.HasPrefix(statusCode, "5"),
					System:   first(attrs, "db.system.name", "db.system", "messaging.system"),
					Method:   first(attrs, "http.request.method", "http.method"),
					Route:    first(attrs, "http.route"),
				}
				span.Method = safeLabel(span.Method, 12)
				span.Route = safeLabel(span.Route, 120)
				span.System = safeLabel(span.System, 40)
				switch {
				case span.Route != "" && span.Method != "":
					span.Name = span.Method + " " + span.Route
				case span.Route != "":
					span.Name = "handler " + span.Route
				case span.System != "":
					span.Name = "database/cache call"
				case span.Method != "":
					span.Name = span.Method + " HTTP"
				}
				item := s.requests[id]
				if item == nil {
					item = &Request{ID: id, Name: span.Name, Start: span.Start}
					s.requests[id] = item
					s.order = append(s.order, id)
				}
				if span.ParentID == "" {
					item.Name = span.Name
				}
				if item.Start.IsZero() || span.Start.Before(item.Start) {
					item.Start = span.Start
				}
				found := false
				for i := range item.Spans {
					if item.Spans[i].ID == span.ID {
						item.Spans[i] = span
						found = true
						break
					}
				}
				if !found {
					item.Spans = append(item.Spans, span)
				}
			}
		}
	}
	for len(s.order) > 100 {
		delete(s.requests, s.order[0])
		s.order = s.order[1:]
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return nil
}

func Handler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-protobuf") {
			http.Error(w, "OTLP protobuf required", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			http.Error(w, "trace batch too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := store.Ingest(body); err != nil {
			http.Error(w, fmt.Sprintf("invalid trace: %v", err), http.StatusBadRequest)
			return
		}
		response, _ := proto.Marshal(&collector.ExportTraceServiceResponse{})
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(response)
	})
	return mux
}

func attributes(kvs []*common.KeyValue) map[string]string {
	out := make(map[string]string)
	for _, kv := range kvs {
		if kv.Value != nil {
			out[kv.Key] = kv.Value.GetStringValue()
		}
	}
	return out
}

func first(m map[string]string, keys ...string) string {
	for _, key := range keys {
		if m[key] != "" {
			return m[key]
		}
	}
	return ""
}

func safeLabel(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit]) + "…"
	}
	return value
}
