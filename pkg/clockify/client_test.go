package clockify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGetUserSendsAPIKeyHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path = %q, want /user", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","name":"Ada","email":"ada@example.com"}`))
	}))
	defer server.Close()

	user, err := NewClient(server.URL, "test-key").GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.ID != "u1" || user.Name != "Ada" || user.Email != "ada@example.com" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestAPIErrorUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid API key"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "bad-key").GetUser(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsUnauthorized(err) {
		t.Errorf("IsUnauthorized = false, want true (err: %v)", err)
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("error should carry the API message, got: %v", err)
	}
}

func TestRateLimitedClassification(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many requests"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "k").GetUser(context.Background())
	if !IsRateLimited(err) {
		t.Errorf("IsRateLimited = false, want true (err: %v)", err)
	}
}

func TestStopTimer(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"e1"}`))
	}))
	defer server.Close()

	end := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entry, err := NewClient(server.URL, "k").StopTimer(context.Background(), "w1", "u1", end)
	if err != nil {
		t.Fatalf("StopTimer: %v", err)
	}
	if entry.ID != "e1" {
		t.Errorf("entry.ID = %q, want e1", entry.ID)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if want := "/workspaces/w1/user/u1/time-entries"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if got := body["end"]; got != "2026-10-05T14:00:00Z" {
		t.Errorf("body end = %v, want 2026-10-05T14:00:00Z", got)
	}
}

func TestCreateTimeEntry(t *testing.T) {
	t.Parallel()

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/workspaces/w1/time-entries" {
			t.Errorf("path = %q, want /workspaces/w1/time-entries", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"e2"}`))
	}))
	defer server.Close()

	start := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	req := CreateTimeEntryRequest{
		Description: "Deep work",
		Start:       Ptr(start),
		End:         Ptr(end),
		ProjectID:   "p1",
		Billable:    Ptr(true),
	}
	if _, err := NewClient(server.URL, "k").CreateTimeEntry(context.Background(), "w1", req); err != nil {
		t.Fatalf("CreateTimeEntry: %v", err)
	}

	if body["description"] != "Deep work" {
		t.Errorf("description = %v", body["description"])
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v", body["projectId"])
	}
	if body["billable"] != true {
		t.Errorf("billable = %v, want true", body["billable"])
	}
	if body["start"] != "2026-10-05T14:00:00Z" {
		t.Errorf("start = %v, want UTC RFC3339", body["start"])
	}
	if body["end"] != "2026-10-05T15:30:00Z" {
		t.Errorf("end = %v, want UTC RFC3339", body["end"])
	}
}

func TestCreateNormalizesOffsetTimesToUTC(t *testing.T) {
	t.Parallel()

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"e5"}`))
	}))
	defer server.Close()

	// 14:00 at +02:00 is 12:00 UTC.
	loc := time.FixedZone("CEST", 2*3600)
	start := time.Date(2026, 10, 5, 14, 0, 0, 0, loc)
	req := CreateTimeEntryRequest{Start: Ptr(start)}
	if _, err := NewClient(server.URL, "k").CreateTimeEntry(context.Background(), "w1", req); err != nil {
		t.Fatalf("CreateTimeEntry: %v", err)
	}
	if body["start"] != "2026-10-05T12:00:00Z" {
		t.Errorf("start = %v, want 2026-10-05T12:00:00Z", body["start"])
	}
}

func TestUpdateTimeEntryUsesPut(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"e9"}`))
	}))
	defer server.Close()

	start := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	entry, err := NewClient(server.URL, "k").UpdateTimeEntry(context.Background(), "w1", "e9", UpdateTimeEntryRequest{
		Start:       Ptr(start),
		Description: "updated",
	})
	if err != nil {
		t.Fatalf("UpdateTimeEntry: %v", err)
	}
	if entry.ID != "e9" {
		t.Errorf("entry.ID = %q, want e9", entry.ID)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT (PATCH is reserved for stopping)", gotMethod)
	}
	if want := "/workspaces/w1/time-entries/e9"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if body["start"] != "2026-10-05T09:00:00Z" || body["description"] != "updated" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestGetWorkspaces(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces" {
			t.Errorf("path = %q, want /workspaces", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"w1","name":"Personal"},{"id":"w2","name":"Team"}]`))
	}))
	defer server.Close()

	workspaces, err := NewClient(server.URL, "k").GetWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("GetWorkspaces: %v", err)
	}
	if len(workspaces) != 2 || workspaces[0].Name != "Personal" {
		t.Errorf("unexpected workspaces: %+v", workspaces)
	}
}

func TestGetProjectsFollowsPagination(t *testing.T) {
	t.Parallel()

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		count := 100
		if r.URL.Query().Get("page") == "2" {
			count = 3 // a short page ends pagination
		}
		items := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			items = append(items, map[string]any{"id": "p", "name": "Project"})
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	projects, err := NewClient(server.URL, "k").GetProjects(context.Background(), "w1", ProjectListParams{})
	if err != nil {
		t.Fatalf("GetProjects: %v", err)
	}
	if len(projects) != 103 {
		t.Errorf("len(projects) = %d, want 103", len(projects))
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Errorf("pages = %v, want [1 2]", pages)
	}
}

func TestGetProjectsSendsFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/w1/projects" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("name") != "Web" {
			t.Errorf("name = %q, want Web", q.Get("name"))
		}
		if q.Get("strict-name-search") != "true" {
			t.Errorf("strict-name-search = %q, want true", q.Get("strict-name-search"))
		}
		if q.Get("archived") != "true" {
			t.Errorf("archived = %q, want true", q.Get("archived"))
		}
		if q.Get("page-size") != "25" {
			t.Errorf("page-size = %q, want 25", q.Get("page-size"))
		}
		_, _ = w.Write([]byte(`[{"id":"p3","name":"Old Website","archived":true}]`))
	}))
	defer server.Close()

	projects, err := NewClient(server.URL, "k").GetProjects(context.Background(), "w1", ProjectListParams{
		Name:             "Web",
		StrictNameSearch: true,
		Archived:         Ptr(true),
		PageSize:         25,
	})
	if err != nil {
		t.Fatalf("GetProjects: %v", err)
	}
	if len(projects) != 1 || !projects[0].Archived || projects[0].ID != "p3" {
		t.Errorf("unexpected projects: %+v", projects)
	}
}
func TestGetTimeEntriesQueryParams(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/w1/user/u1/time-entries" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("page") != "1" || q.Get("page-size") != "5" || q.Get("hydrated") != "true" {
			t.Errorf("query = %v, want page=1&page-size=5&hydrated=true", q)
		}
		_, _ = w.Write([]byte(`[
			{"id":"e1","description":"Deep work",
			 "timeInterval":{"start":"2026-10-05T09:00:00Z","end":"2026-10-05T10:30:00Z"}},
			{"id":"e2","description":"Review",
			 "project":{"id":"p1","name":"Website Redesign","clientName":"Acme"},
			 "timeInterval":{"start":"2026-10-05T11:00:00Z","end":null}}
		]`))
	}))
	defer server.Close()

	entries, err := NewClient(server.URL, "k").GetTimeEntries(context.Background(), "w1", "u1", TimeEntryListParams{
		Page: 1, PageSize: 5, Hydrated: true,
	})
	if err != nil {
		t.Fatalf("GetTimeEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].IsRunning() {
		t.Error("first entry should not be running")
	}
	if !entries[1].IsRunning() {
		t.Error("second entry should be running")
	}
	if entries[1].Project == nil || entries[1].Project.Name != "Website Redesign" {
		t.Errorf("hydrated project not decoded: %+v", entries[1].Project)
	}
	if entries[1].EffectiveProjectID() != "p1" {
		t.Errorf("EffectiveProjectID = %q, want p1", entries[1].EffectiveProjectID())
	}
}

func TestGetInProgress(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/w1/time-entries/status/in-progress" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"e7","description":"Pairing","userId":"u1",
			"timeInterval":{"start":"2026-10-05T11:00:00Z","end":null}}]`))
	}))
	defer server.Close()

	entries, err := NewClient(server.URL, "k").GetInProgress(context.Background(), "w1")
	if err != nil {
		t.Fatalf("GetInProgress: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsRunning() {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestErrorEnvelopeVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantStatus int
	}{
		{"message envelope", http.StatusBadRequest, `{"message":"Bad request"}`, "Bad request", 400},
		{"errors map", http.StatusForbidden, `{"errors":{"workspace":["no access"]}}`, "no access", 403},
		{"raw text fallback", http.StatusTeapot, `I'm a teapot`, "teapot", 418},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := NewClient(server.URL, "k").GetUser(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantMsg)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.wantStatus {
				t.Errorf("status classification failed: %v", err)
			}
		})
	}
}

func TestGetTimeEntriesDateRange(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("start") != "2026-10-01T00:00:00Z" {
			t.Errorf("start = %q, want 2026-10-01T00:00:00Z", q.Get("start"))
		}
		if q.Get("end") != "2026-10-08T00:00:00Z" {
			t.Errorf("end = %q, want 2026-10-08T00:00:00Z", q.Get("end"))
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	// Local 02:00 at +02:00 normalizes to 00:00 UTC on the wire.
	loc := time.FixedZone("CEST", 2*3600)
	start := time.Date(2026, 10, 1, 2, 0, 0, 0, loc)
	end := time.Date(2026, 10, 8, 2, 0, 0, 0, loc)

	_, err := NewClient(server.URL, "k").GetTimeEntries(context.Background(), "w1", "u1", TimeEntryListParams{
		Page: 1, PageSize: 50, Start: Ptr(start), End: Ptr(end),
	})
	if err != nil {
		t.Fatalf("GetTimeEntries: %v", err)
	}
}

func TestDeleteTimeEntry(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent) // Clockify returns 204 with no body
	}))
	defer server.Close()

	if err := NewClient(server.URL, "k").DeleteTimeEntry(context.Background(), "w1", "e1"); err != nil {
		t.Fatalf("DeleteTimeEntry: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if want := "/workspaces/w1/time-entries/e1"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDuplicateTimeEntry(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"e-dup","description":"Deep work",
			"timeInterval":{"start":"2026-10-05T09:00:00Z","end":"2026-10-05T10:30:00Z"}}`))
	}))
	defer server.Close()

	entry, err := NewClient(server.URL, "k").DuplicateTimeEntry(context.Background(), "w1", "u1", "e1")
	if err != nil {
		t.Fatalf("DuplicateTimeEntry: %v", err)
	}
	if entry.ID != "e-dup" {
		t.Errorf("entry.ID = %q, want e-dup (the server assigns the new ID)", entry.ID)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/workspaces/w1/user/u1/time-entries/e1/duplicate"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if strings.TrimSpace(gotBody) != "" {
		t.Errorf("duplicate takes no body, sent %q", gotBody)
	}
}

func TestGetTags(t *testing.T) {
	t.Parallel()

	var gotMethod, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":"t1","name":"deep-work","archived":false},
			{"id":"t3","name":"legacy","archived":true}]`))
	}))
	defer server.Close()

	tags, err := NewClient(server.URL, "k").GetTags(context.Background(), "w1", TagListParams{
		Name: "deep", StrictNameSearch: true, Archived: Ptr(true), PageSize: 50,
	})
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if len(tags) != 2 || tags[0].ID != "t1" || !tags[1].Archived {
		t.Errorf("unexpected tags: %+v", tags)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	for _, want := range []string{"name=deep", "strict-name-search=true", "archived=true", "page=1", "page-size=50"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestCreateTag(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"t9","name":"deep-work","workspaceId":"w1"}`))
	}))
	defer server.Close()

	tag, err := NewClient(server.URL, "k").CreateTag(context.Background(), "w1", CreateTagRequest{Name: "deep-work"})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tag.ID != "t9" || tag.Name != "deep-work" {
		t.Errorf("unexpected tag: %+v", tag)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/workspaces/w1/tags"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotBody, `"name":"deep-work"`) {
		t.Errorf("body = %q, want the tag name", gotBody)
	}
}

func TestDeleteTag(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"id":"t1","name":"renamed","archived":true,"workspaceId":"w1"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "k")
	tag, err := client.GetTag(context.Background(), "w1", "t1")
	if err != nil {
		t.Fatalf("GetTag: %v", err)
	}
	if tag.ID != "t1" {
		t.Errorf("tag.ID = %q, want t1", tag.ID)
	}
	renamed, err := client.UpdateTag(context.Background(), "w1", "t1", UpdateTagRequest{
		Name: Ptr("renamed"), Archived: Ptr(true),
	})
	if err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if renamed.Name != "renamed" || !renamed.Archived {
		t.Errorf("unexpected updated tag: %+v", renamed)
	}
	if err := client.DeleteTag(context.Background(), "w1", "t1"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"GET /workspaces/w1/tags/t1",
		"PUT /workspaces/w1/tags/t1",
		"DELETE /workspaces/w1/tags/t1",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestGetTasks(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":"k1","name":"Fix login","projectId":"p1","status":"ACTIVE"}]`))
	}))
	defer server.Close()

	tasks, err := NewClient(server.URL, "k").GetTasks(context.Background(), "w1", "p1", TaskListParams{
		Name: "login", IsActive: Ptr(true), PageSize: 25,
	})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "k1" || tasks[0].Status != TaskStatusActive {
		t.Errorf("unexpected tasks: %+v", tasks)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if want := "/workspaces/w1/projects/p1/tasks"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	for _, want := range []string{"name=login", "is-active=true", "page-size=25"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestCreateTask(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"k9","name":"Fix login","projectId":"p1","status":"ACTIVE","estimate":"PT2H"}`))
	}))
	defer server.Close()

	task, err := NewClient(server.URL, "k").CreateTask(context.Background(), "w1", "p1", CreateTaskRequest{
		Name: "Fix login", Estimate: "PT2H",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != "k9" || task.Estimate != "PT2H" {
		t.Errorf("unexpected task: %+v", task)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/workspaces/w1/projects/p1/tasks"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	for _, want := range []string{`"name":"Fix login"`, `"estimate":"PT2H"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body %q missing %q", gotBody, want)
		}
	}
}

func TestUpdateAndDeleteTask(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls []string
	var lastBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPut {
			lastBody = string(raw)
		}
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"id":"k1","name":"Fix login","projectId":"p1","status":"DONE"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "k")
	task, err := client.UpdateTask(context.Background(), "w1", "p1", "k1", UpdateTaskRequest{
		Name: "Fix login", Status: TaskStatusDone,
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if task.Status != TaskStatusDone {
		t.Errorf("task.Status = %q, want DONE", task.Status)
	}
	if err := client.DeleteTask(context.Background(), "w1", "p1", "k1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want PUT then DELETE", calls)
	}
	if want := "PUT /workspaces/w1/projects/p1/tasks/k1"; calls[0] != want {
		t.Errorf("first call = %q, want %q", calls[0], want)
	}
	if want := "DELETE /workspaces/w1/projects/p1/tasks/k1"; calls[1] != want {
		t.Errorf("second call = %q, want %q", calls[1], want)
	}
	// Name is required by the API's update contract.
	if !strings.Contains(lastBody, `"name":"Fix login"`) || !strings.Contains(lastBody, `"status":"DONE"`) {
		t.Errorf("update body = %q", lastBody)
	}
}
