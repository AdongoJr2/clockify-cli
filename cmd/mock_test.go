package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

// mockRequest records one request received by the fake API.
type mockRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// mockClockify is a tiny in-process fake of every Clockify endpoint the CLI
// uses, recording requests so tests can assert on outgoing traffic.
type mockClockify struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []mockRequest
	running  []clockify.TimeEntry
	entries  []clockify.TimeEntry
}

func newMockClockify(t *testing.T) *mockClockify {
	t.Helper()
	m := &mockClockify{
		entries: []clockify.TimeEntry{
			{
				ID: "e1", Description: "Deep work", Billable: true,
				TimeInterval: &clockify.TimeInterval{
					Start: ptrTime(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)),
					End:   ptrTime(time.Date(2026, 10, 5, 10, 30, 0, 0, time.UTC)),
				},
			},
			{
				ID: "e2", Description: "Standup",
				Project: &clockify.ProjectBrief{ID: "p1", Name: "Website Redesign", ClientName: "Acme"},
				TimeInterval: &clockify.TimeInterval{
					Start: ptrTime(time.Date(2026, 10, 5, 8, 30, 0, 0, time.UTC)),
					End:   ptrTime(time.Date(2026, 10, 5, 8, 45, 0, 0, time.UTC)),
				},
			},
		},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

func ptrTime(t time.Time) *time.Time { return &t }

func (m *mockClockify) setRunning(entries []clockify.TimeEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = entries
}

func (m *mockClockify) setEntries(entries []clockify.TimeEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = entries
}

// find returns the first recorded request matching method and path suffix.
func (m *mockClockify) find(method, pathSuffix string) (mockRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.requests {
		if req.Method == method && strings.HasSuffix(req.Path, pathSuffix) {
			return req, true
		}
	}
	return mockRequest{}, false
}

// countRequests returns how many requests of the given method were recorded.
func (m *mockClockify) countRequests(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, req := range m.requests {
		if req.Method == method {
			n++
		}
	}
	return n
}

// findByDescription returns the first recorded request whose JSON body carries
// the given description — needed because batch entries are posted concurrently.
func (m *mockClockify) findByDescription(method, description string) (mockRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.requests {
		if req.Method != method || req.Body == "" {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			continue
		}
		if body["description"] == description {
			return req, true
		}
	}
	return mockRequest{}, false
}

func (m *mockClockify) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)

	m.mu.Lock()
	m.requests = append(m.requests, mockRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(raw)})
	running, entries := m.running, m.entries
	m.mu.Unlock()

	writeJSON := func(v any, status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}

	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/user":
		writeJSON(map[string]any{"id": "u1", "name": "Test User", "email": "t@example.com"}, 200)
	case r.Method == http.MethodGet && path == "/workspaces":
		writeJSON([]map[string]any{{"id": "w1", "name": "My Workspace"}}, 200)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/projects"):
		items := []map[string]any{
			{"id": "p1", "name": "Website Redesign", "billable": true},
			{"id": "p2", "name": "Backend API", "billable": true},
			{"id": "p3", "name": "Old Website", "billable": false, "archived": true},
		}
		// Mirror the real API: ?name= is a substring filter, ?archived=true
		// returns archived projects only.
		q := r.URL.Query()
		var out []map[string]any
		for _, it := range items {
			if name := q.Get("name"); name != "" &&
				!strings.Contains(strings.ToLower(it["name"].(string)), strings.ToLower(name)) {
				continue
			}
			isArch, _ := it["archived"].(bool)
			if q.Get("archived") == "true" && !isArch {
				continue
			}
			out = append(out, it)
		}
		writeJSON(out, 200)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/time-entries/status/in-progress"):
		writeJSON(running, 200)
	case r.Method == http.MethodGet && strings.Contains(path, "/time-entries/"):
		// Single entry: /workspaces/{ws}/time-entries/{id}
		id := path[strings.LastIndex(path, "/")+1:]
		for i := range entries {
			if entries[i].ID == id {
				writeJSON(entries[i], 200)
				return
			}
		}
		writeJSON(map[string]any{"message": "time entry not found"}, 404)
	case r.Method == http.MethodPut && strings.Contains(path, "/time-entries/"):
		writeJSON(map[string]any{"id": "e1"}, 200)
	case r.Method == http.MethodDelete && strings.Contains(path, "/time-entries/"):
		w.WriteHeader(http.StatusNoContent) // DELETE → 204, empty body
	case r.Method == http.MethodGet && strings.Contains(path, "/time-entries"):
		writeJSON(entries, 200)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/time-entries"):
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		// Failure injection: descriptions containing "fail" are rejected, so
		// tests can exercise partial-failure reporting.
		if desc, _ := body["description"].(string); strings.Contains(strings.ToLower(desc), "fail") {
			writeJSON(map[string]any{"message": "entry rejected by mock"}, 400)
			return
		}
		writeJSON(map[string]any{"id": "e4"}, 201)
	case r.Method == http.MethodPatch && strings.Contains(path, "/user/"):
		writeJSON(map[string]any{"id": "e3"}, 200)
	default:
		writeJSON(map[string]any{"message": "not found"}, 404)
	}
}
