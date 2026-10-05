package clockify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
