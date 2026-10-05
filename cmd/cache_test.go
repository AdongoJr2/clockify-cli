package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

func TestLoadCacheRoundTripAndInvalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	saveCache(path, &cacheFile{
		APIKeyHash: apiKeyHash("key-1"),
		User:       &cachedUser{ID: "u1", Name: "Ada", Email: "a@b.c", FetchedAt: time.Now()},
		Projects:   map[string]cachedProjects{"w1": {FetchedAt: time.Now(), Items: []clockify.Project{{ID: "p1", Name: "Web"}}}},
	})

	same := loadCache(path, "key-1")
	if same.User == nil || same.User.ID != "u1" {
		t.Fatalf("cache not restored: %+v", same.User)
	}
	if got := same.Projects["w1"]; len(got.Items) != 1 {
		t.Errorf("projects not restored: %+v", got)
	}

	rotated := loadCache(path, "key-2")
	if rotated.User != nil || len(rotated.Projects) != 0 {
		t.Errorf("rotated API key must invalidate the cache: %+v", rotated)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := loadCache(path, "key-1")
	if corrupt.User != nil || len(corrupt.Projects) != 0 {
		t.Errorf("corrupt cache must fall back to empty: %+v", corrupt)
	}
}

func TestGetCachedUserHitsAPOnce(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"u1","name":"Ada","email":"a@b.c"}`))
	}))
	defer server.Close()

	client := clockify.NewClient(server.URL, "k")
	first, err := getCachedUser(context.Background(), client, "k")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := getCachedUser(context.Background(), client, "k")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.ID != "u1" || second.ID != "u1" {
		t.Errorf("unexpected users: %v / %v", first.ID, second.ID)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("GET /user called %d times, want 1 (second must come from cache)", got)
	}

	// Expire the cached entry → next call must hit the API again.
	path, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	cf := loadCache(path, "k")
	cf.User.FetchedAt = time.Now().Add(-userCacheTTL - time.Minute)
	saveCache(path, cf)

	if _, err := getCachedUser(context.Background(), client, "k"); err != nil {
		t.Fatalf("refresh call: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("GET /user called %d times after expiry, want 2", got)
	}
}

func TestGetCachedProjectsHitsAPIOnce(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"id":"p1","name":"Web"}]`))
	}))
	defer server.Close()

	client := clockify.NewClient(server.URL, "k")
	for i := 0; i < 3; i++ {
		projects, err := getCachedProjects(context.Background(), client, "k", "w1")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if len(projects) != 1 {
			t.Fatalf("len(projects) = %d, want 1", len(projects))
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("GET /projects called %d times, want 1 (must be cached)", got)
	}
}
