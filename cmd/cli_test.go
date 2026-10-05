package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// setupCLIEnv isolates config/cache state and points the CLI at a mock API.
func setupCLIEnv(t *testing.T, apiURL string) {
	t.Helper()
	viper.Reset()
	t.Setenv("CLOCKIFY_API_KEY", "test-key")
	t.Setenv("CLOCKIFY_WORKSPACE_ID", "w1")
	t.Setenv("CLOCKIFY_BASE_URL", apiURL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	resetFlags(rootCmd)
	// Bind CLOCKIFY_* env vars exactly as Execute would, so tests that call
	// helpers directly (newClient, resolveProject, …) see the same config.
	if err := initConfig(); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
}

// resetFlags restores every command flag to its default so tests never leak
// state into each other.
func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	for _, child := range c.Commands() {
		resetFlags(child)
	}
}

// captureStdout captures everything fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	runErr := fn()
	_ = w.Close()
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(data), runErr
}

// runCLI executes the root command with args, returning captured stdout and error.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(args)
	return captureStdout(t, func() error { return rootCmd.Execute() })
}

func TestListEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"Last 2 time entries", "Deep work", "Website Redesign / Acme", "1h 30m", "15m"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLogEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "log", "-d", "E2E task", "--start", "14:00", "--duration", "1h",
		"-p", "Website Redesign", "--billable=false")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(out, "Logged 1h") {
		t.Errorf("missing success line:\n%s", out)
	}

	req, ok := mock.find(http.MethodPost, "/time-entries")
	if !ok {
		t.Fatal("no POST /time-entries recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode recorded body: %v", err)
	}
	if body["description"] != "E2E task" {
		t.Errorf("description = %v", body["description"])
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v, want p1 (resolved from project name)", body["projectId"])
	}
	if body["billable"] != false {
		t.Errorf("billable = %v, want false", body["billable"])
	}
	if start, _ := body["start"].(string); !strings.HasSuffix(start, "Z") {
		t.Errorf("start = %q, want UTC with Z suffix", start)
	}
	if end, _ := body["end"].(string); !strings.HasSuffix(end, "Z") {
		t.Errorf("end = %q, want UTC with Z suffix", end)
	}
}

func TestStopEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Now().Add(-90 * time.Minute).UTC()
	mock.setRunning([]clockify.TimeEntry{{
		ID: "e3", Description: "Review PRs", UserID: "u1",
		TimeInterval: &clockify.TimeInterval{Start: &start},
	}})
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, want := range []string{"Timer stopped", "1h 30m", "Review PRs"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	req, ok := mock.find(http.MethodPatch, "/user/u1/time-entries")
	if !ok {
		t.Fatal("no PATCH .../user/u1/time-entries recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["end"].(string); !ok {
		t.Errorf("stop body must contain end timestamp: %v", body)
	}
}

func TestStopWithoutRunningTimer(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.Contains(out, "No timer running") {
		t.Errorf("expected warning:\n%s", out)
	}
	if _, ok := mock.find(http.MethodPatch, ""); ok {
		t.Error("no PATCH should be sent when no timer is running")
	}
}

func TestStartEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "start", "-d", "New task")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "Timer started") {
		t.Errorf("missing success line:\n%s", out)
	}

	req, ok := mock.find(http.MethodPost, "/time-entries")
	if !ok {
		t.Fatal("no POST /time-entries recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["description"] != "New task" {
		t.Errorf("description = %v", body["description"])
	}
	if _, hasEnd := body["end"]; hasEnd {
		t.Errorf("starting a timer must omit end: %v", body)
	}
	if _, hasStart := body["start"]; !hasStart {
		t.Errorf("starting a timer must include start: %v", body)
	}
}

func TestStartBlockedWhenTimerRunningNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Now().Add(-10 * time.Minute).UTC()
	mock.setRunning([]clockify.TimeEntry{{
		ID: "e3", Description: "Review PRs", UserID: "u1",
		TimeInterval: &clockify.TimeInterval{Start: &start},
	}})
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "start", "-d", "New task")
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v, want 'already running'", err)
	}
	if _, ok := mock.find(http.MethodPost, "/time-entries"); ok {
		t.Error("must not create an entry when the running timer can't be handled")
	}
}

func TestLogRequiresFlagsWhenNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "log")
	if err == nil || !strings.Contains(err.Error(), "missing flags") {
		t.Fatalf("err = %v, want 'missing flags'", err)
	}
}

func TestConfigInitRequiresTerminal(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "config", "init")
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("err = %v, want 'interactive terminal'", err)
	}
}

func TestMissingWorkspaceConfig(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	t.Setenv("CLOCKIFY_WORKSPACE_ID", "")

	_, err := runCLI(t, "list")
	if err == nil || !strings.Contains(err.Error(), "workspace not configured") {
		t.Fatalf("err = %v, want 'workspace not configured'", err)
	}
}

func TestRunningEntryFiltersOtherUsers(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Now().UTC()
	mock.setRunning([]clockify.TimeEntry{
		{ID: "other", UserID: "someone-else", TimeInterval: &clockify.TimeInterval{Start: &start}},
	})
	setupCLIEnv(t, mock.server.URL)

	client, err := newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	got, err := runningEntry(context.Background(), client, "w1", "u1")
	if err != nil {
		t.Fatalf("runningEntry: %v", err)
	}
	if got != nil {
		t.Errorf("another user's timer must be filtered out, got %+v", got)
	}

	mine := clockify.TimeEntry{ID: "mine", UserID: "u1", TimeInterval: &clockify.TimeInterval{Start: &start}}
	mock.setRunning([]clockify.TimeEntry{{ID: "other", UserID: "someone-else"}, mine})
	got, err = runningEntry(context.Background(), client, "w1", "u1")
	if err != nil {
		t.Fatalf("runningEntry: %v", err)
	}
	if got == nil || got.ID != "mine" {
		t.Errorf("got %+v, want the entry with ID 'mine'", got)
	}
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	viper.Reset()
	t.Setenv("CLOCKIFY_API_KEY", "")

	_, err := newClient()
	if err == nil || !strings.Contains(err.Error(), "API key not configured") {
		t.Fatalf("err = %v, want 'API key not configured'", err)
	}
}

func TestUpdateEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "update", "e1", "-d", "Deep work v2")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "Updated entry") {
		t.Errorf("missing success line:\n%s", out)
	}

	if _, ok := mock.find(http.MethodGet, "/time-entries/e1"); !ok {
		t.Error("must fetch the existing entry before updating")
	}
	req, ok := mock.find(http.MethodPut, "/time-entries/e1")
	if !ok {
		t.Fatal("no PUT /workspaces/w1/time-entries/e1 recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["description"] != "Deep work v2" {
		t.Errorf("description = %v, want 'Deep work v2'", body["description"])
	}
	// Unchanged fields must be preserved from the existing entry.
	if body["start"] != "2026-10-05T09:00:00Z" {
		t.Errorf("start = %v, want unchanged 2026-10-05T09:00:00Z", body["start"])
	}
	if body["end"] != "2026-10-05T10:30:00Z" {
		t.Errorf("end = %v, want unchanged 2026-10-05T10:30:00Z", body["end"])
	}
	if body["billable"] != true {
		t.Errorf("billable = %v, want preserved true", body["billable"])
	}
}

func TestUpdateTimesEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "update", "e1", "--start", "2026-10-05 09:15", "--duration", "1h")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "1h") {
		t.Errorf("output should mention new duration:\n%s", out)
	}

	req, ok := mock.find(http.MethodPut, "/time-entries/e1")
	if !ok {
		t.Fatal("no PUT recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	start, err := time.Parse(time.RFC3339, body["start"].(string))
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	wantStart := time.Date(2026, 10, 5, 9, 15, 0, 0, time.Local).UTC()
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	end, err := time.Parse(time.RFC3339, body["end"].(string))
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	if end.Sub(start) != time.Hour {
		t.Errorf("end-start = %v, want 1h", end.Sub(start))
	}
	// Description was not passed → must be preserved.
	if body["description"] != "Deep work" {
		t.Errorf("description = %v, want preserved 'Deep work'", body["description"])
	}
}

func TestUpdateRequiresIDNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "update", "-d", "x")
	if err == nil || !strings.Contains(err.Error(), "entry ID required") {
		t.Fatalf("err = %v, want 'entry ID required'", err)
	}
}

func TestUpdateRequiresEditFlagsNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "update", "e1")
	if err == nil || !strings.Contains(err.Error(), "provide at least one flag") {
		t.Fatalf("err = %v, want 'provide at least one flag'", err)
	}
	if _, ok := mock.find(http.MethodPut, ""); ok {
		t.Error("no PUT should be sent when nothing changed")
	}
}
