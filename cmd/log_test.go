package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/viper"
)

// seedProjectsCache writes a fresh project cache so resolveProject never
// needs the network.
func seedProjectsCache(t *testing.T, apiKey, workspaceID string, projects []clockify.Project) {
	t.Helper()
	path, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	saveCache(path, &cacheFile{
		APIKeyHash: apiKeyHash(apiKey),
		Projects:   map[string]cachedProjects{workspaceID: {FetchedAt: time.Now(), Items: projects}},
	})
}

func TestResolveProject(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	viper.Reset()
	viper.Set("api_key", "test-key")

	seedProjectsCache(t, "test-key", "w1", []clockify.Project{
		{ID: "p1", Name: "Website Redesign"},
		{ID: "p2", Name: "Website Maintenance"},
		{ID: "p3", Name: "Backend API"},
	})
	// Unreachable base URL: every lookup must be satisfied from the cache.
	client := clockify.NewClient("http://127.0.0.1:1", "test-key")
	ctx := context.Background()

	tests := []struct {
		ref     string
		wantID  string
		wantErr string
	}{
		{"", "", ""},
		{"backend api", "p3", ""},    // case-insensitive exact name
		{"Redesign", "p1", ""},       // unique substring
		{"Website", "", "ambiguous"}, // two matches
		{"Does Not Exist", "", "no project matches"},
		{"5a0ab5acb0798751107918ab", "5a0ab5acb0798751107918ab", ""}, // raw ID passthrough
	}
	for _, tt := range tests {
		got, err := resolveProject(ctx, client, "w1", tt.ref)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("resolveProject(%q) err = %v, want containing %q", tt.ref, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveProject(%q): %v", tt.ref, err)
			continue
		}
		if got != tt.wantID {
			t.Errorf("resolveProject(%q) = %q, want %q", tt.ref, got, tt.wantID)
		}
	}
	viper.Reset()
}

func TestLooksLikeID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"5a0ab5acb0798751107918ab", true},
		{"25b687e29ae1f428e7be1234", true},
		{"short", false},
		{"Website Redesign", false}, // contains a space
		{"has-dash-id-123456789012", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeID(tt.in); got != tt.want {
			t.Errorf("looksLikeID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
