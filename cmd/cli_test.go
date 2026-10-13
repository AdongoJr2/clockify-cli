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
	reset := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok && f.Value.Type() == "stringSlice" {
			// pflag's stringSliceValue replaces its contents only on the
			// first Set call and appends afterwards, so Set(DefValue) can
			// never shrink it back — Replace resets it in place (keeping
			// the binding to the command's backing variable).
			_ = sv.Replace(nil)
			f.Changed = false
			return
		}
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
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

// runCLI executes the root command with args, returning captured stdout and
// error. Flags are reset first so repeated invocations (like consecutive runs
// in one test) never leak state into each other — in production each process
// executes exactly once.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(rootCmd)
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

func TestDeleteByDescriptionEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	mock.setEntries([]clockify.TimeEntry{
		{ID: "e7", Description: "Testing...", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
		{ID: "e8", Description: "Other work", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
	})
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "delete", "-d", "Testing...", "--yes")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Deleted entry") || !strings.Contains(out, "Testing...") {
		t.Errorf("output should name the deleted entry:\n%s", out)
	}
	if _, ok := mock.find(http.MethodDelete, "/time-entries/e7"); !ok {
		t.Error("no DELETE .../time-entries/e7 recorded")
	}
}

func TestDeletePrefersExactDescriptionMatch(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	mock.setEntries([]clockify.TimeEntry{
		{ID: "e9", Description: "Testing more", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
		{ID: "e7", Description: "Testing...", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
	})
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "delete", "-d", "Testing...", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := mock.find(http.MethodDelete, "/time-entries/e7"); !ok {
		t.Error("exact match e7 must win over substring match e9")
	}
	if _, ok := mock.find(http.MethodDelete, "/time-entries/e9"); ok {
		t.Error("substring match e9 must not be deleted")
	}
}

func TestDeleteAmbiguousDescriptionNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	start := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	mock.setEntries([]clockify.TimeEntry{
		{ID: "e7", Description: "Testing round 1", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
		{ID: "e9", Description: "Testing round 2", TimeInterval: &clockify.TimeInterval{Start: &start, End: &end}},
	})
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "delete", "-d", "Testing", "--yes")
	if err == nil || !strings.Contains(err.Error(), "2 entries match") {
		t.Fatalf("err = %v, want '2 entries match'", err)
	}
	if _, ok := mock.find(http.MethodDelete, ""); ok {
		t.Error("ambiguous match must not delete anything")
	}
}

func TestDeleteRequiresYesWhenNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "delete", "e1")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want '--yes'", err)
	}
	if _, ok := mock.find(http.MethodDelete, ""); ok {
		t.Error("no DELETE should be sent without confirmation")
	}
}

func TestDeleteRequiresTargetWhenNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "delete")
	if err == nil || !strings.Contains(err.Error(), "entry ID") {
		t.Fatalf("err = %v, want 'entry ID'", err)
	}
}

func TestDeleteNoMatchingDescription(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "delete", "-d", "Nothing matches this", "--yes")
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err = %v, want 'no entry'", err)
	}
	if _, ok := mock.find(http.MethodDelete, ""); ok {
		t.Error("no DELETE should be sent when nothing matches")
	}
}

func TestProjectsEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "projects")
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	for _, want := range []string{"3 projects", "Website Redesign", "Backend API", "Old Website", "archived"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestProjectsActiveFlag(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "projects", "--active")
	if err != nil {
		t.Fatalf("projects --active: %v", err)
	}
	if !strings.Contains(out, "2 projects") {
		t.Errorf("expected 2 active projects:\n%s", out)
	}
	if strings.Contains(out, "Old Website") {
		t.Errorf("archived project must be hidden:\n%s", out)
	}
}

func TestProjectsArchivedFlag(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "projects", "--archived")
	if err != nil {
		t.Fatalf("projects --archived: %v", err)
	}
	if !strings.Contains(out, "Old Website") || !strings.Contains(out, "1 project") {
		t.Errorf("expected only the archived project:\n%s", out)
	}
	if strings.Contains(out, "Backend API") {
		t.Errorf("active projects must be excluded:\n%s", out)
	}

	req, ok := mock.find(http.MethodGet, "/projects")
	if !ok {
		t.Fatal("no GET /projects recorded")
	}
	if !strings.Contains(req.Query, "archived=true") {
		t.Errorf("query should carry archived=true: %q", req.Query)
	}
}

func TestProjectsNameFilter(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "projects", "-p", "Backend", "--exact")
	if err != nil {
		t.Fatalf("projects -p: %v", err)
	}
	if !strings.Contains(out, "Backend API") {
		t.Errorf("expected the filtered project:\n%s", out)
	}
	if strings.Contains(out, "Website Redesign") {
		t.Errorf("server-side filter should exclude others:\n%s", out)
	}

	req, ok := mock.find(http.MethodGet, "/projects")
	if !ok {
		t.Fatal("no GET /projects recorded")
	}
	if !strings.Contains(req.Query, "name=Backend") || !strings.Contains(req.Query, "strict-name-search=true") {
		t.Errorf("query should carry name filter + strict flag: %q", req.Query)
	}
}

func TestProjectsFlagConflicts(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "projects", "--archived", "--active"); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want 'mutually exclusive'", err)
	}
	if _, err := runCLI(t, "projects", "--exact"); err == nil ||
		!strings.Contains(err.Error(), "needs --project") {
		t.Errorf("err = %v, want 'needs --project'", err)
	}
}

func TestProjectsEmptyWarning(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "projects", "-p", "Nothing matches this name")
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	if !strings.Contains(out, "No projects found") {
		t.Errorf("expected empty warning:\n%s", out)
	}
}

func TestDuplicateEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "duplicate", "e1")
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if !strings.Contains(out, "Duplicated entry") {
		t.Errorf("missing success line:\n%s", out)
	}
	if !strings.Contains(out, "Deep work") {
		t.Errorf("copied entry's description should be shown:\n%s", out)
	}

	req, ok := mock.find(http.MethodPost, "/time-entries/e1/duplicate")
	if !ok {
		t.Fatal("no POST .../time-entries/e1/duplicate recorded")
	}
	_ = req
}

func TestDuplicateEntryNotFound(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "duplicate", "no-such-entry")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
}

func TestDuplicateRequiresTargetNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "duplicate")
	if err == nil || !strings.Contains(err.Error(), "entry ID") {
		t.Fatalf("err = %v, want 'entry ID'", err)
	}
	if _, ok := mock.find(http.MethodPost, "/duplicate"); ok {
		t.Error("nothing should be duplicated without a target")
	}
}

func TestTagsListEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tags")
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	for _, want := range []string{"3 tags", "deep-work", "review", "legacy", "archived"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTagsAddEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tags", "--add", "ship-it")
	if err != nil {
		t.Fatalf("tags --add: %v", err)
	}
	if !strings.Contains(out, "Created tag: ship-it") {
		t.Errorf("missing success line:\n%s", out)
	}
	req, ok := mock.find(http.MethodPost, "/tags")
	if !ok {
		t.Fatal("no POST .../tags recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["name"] != "ship-it" {
		t.Errorf("body name = %v, want ship-it", body["name"])
	}
}

func TestTagsDeleteNonInteractiveNeedsYes(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "tags", "--delete", "legacy")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want a --yes refusal", err)
	}
	if _, ok := mock.find(http.MethodDelete, "/tags/"); ok {
		t.Error("nothing should be deleted without confirmation")
	}
}

func TestTagsDeleteEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tags", "--delete", "legacy", "--yes")
	if err != nil {
		t.Fatalf("tags --delete: %v", err)
	}
	if !strings.Contains(out, "Deleted tag: legacy") {
		t.Errorf("missing success line:\n%s", out)
	}
	if _, ok := mock.find(http.MethodDelete, "/tags/t3"); !ok {
		t.Error("no DELETE .../tags/t3 recorded (resolved from name)")
	}
}

func TestTasksListEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tasks", "-p", "Website Redesign")
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	for _, want := range []string{"3 tasks", "Fix login", "Design review", "done", "2h"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTasksRequiresProjectNonInteractive(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "tasks")
	if err == nil || !strings.Contains(err.Error(), "--project is required") {
		t.Fatalf("err = %v, want '--project is required'", err)
	}
}

func TestTasksAddEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tasks", "-p", "Website Redesign", "--add", "Ship it", "--estimate", "90m")
	if err != nil {
		t.Fatalf("tasks --add: %v", err)
	}
	if !strings.Contains(out, "Created task: Ship it") {
		t.Errorf("missing success line:\n%s", out)
	}
	req, ok := mock.find(http.MethodPost, "/tasks")
	if !ok {
		t.Fatal("no POST .../tasks recorded")
	}
	if !strings.HasSuffix(req.Path, "/projects/p1/tasks") {
		t.Errorf("path = %q, want it under /projects/p1/tasks", req.Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["name"] != "Ship it" {
		t.Errorf("body name = %v, want Ship it", body["name"])
	}
	if body["estimate"] != "PT1H30M" {
		t.Errorf("body estimate = %v, want PT1H30M (90m converted)", body["estimate"])
	}
}

func TestTasksMarkDone(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "tasks", "-p", "Website Redesign", "--done", "Write docs")
	if err != nil {
		t.Fatalf("tasks --done: %v", err)
	}
	if !strings.Contains(out, "Task done: Write docs") {
		t.Errorf("missing success line:\n%s", out)
	}
	req, ok := mock.find(http.MethodPut, "/tasks/k2")
	if !ok {
		t.Fatal("no PUT .../tasks/k2 recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "DONE" {
		t.Errorf("body status = %v, want DONE", body["status"])
	}
	if body["name"] != "Write docs" {
		t.Errorf("body name = %v, want 'Write docs' (required by the API)", body["name"])
	}
}

func TestLogWithTagsAndTask(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "log",
		"-d", "Tagged work", "--start", "09:00", "--duration", "1h",
		"-p", "Website Redesign", "--tag", "deep-work,review", "--task", "Fix login")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(out, "Logged 1h") {
		t.Errorf("missing success line:\n%s", out)
	}

	req, ok := mock.findByDescription(http.MethodPost, "Tagged work")
	if !ok {
		t.Fatal("no POST for 'Tagged work' recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v, want p1", body["projectId"])
	}
	if body["taskId"] != "k1" {
		t.Errorf("taskId = %v, want k1 (resolved from task name)", body["taskId"])
	}
	tagIDs, _ := body["tagIds"].([]any)
	if len(tagIDs) != 2 || tagIDs[0] != "t1" || tagIDs[1] != "t2" {
		t.Errorf("tagIds = %v, want [t1 t2]", tagIDs)
	}
}

func TestLogTaskRequiresProject(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "log", "-d", "x", "--start", "09:00", "--duration", "1h",
		"--task", "Fix login")
	if err == nil || !strings.Contains(err.Error(), "--task requires a project") {
		t.Fatalf("err = %v, want '--task requires a project'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("unresolved task must abort before any POST, got %d", got)
	}
}

func TestLogUnknownTagFails(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "log", "-d", "x", "--start", "09:00", "--duration", "1h",
		"--tag", "nope")
	if err == nil || !strings.Contains(err.Error(), "no tag matches") {
		t.Fatalf("err = %v, want 'no tag matches'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("unknown tag must abort before any POST, got %d", got)
	}
}

func TestStartWithTagAndTask(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "start",
		"-d", "Timer work", "-p", "Website Redesign",
		"--tag", "deep-work", "--task", "Fix login")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "Timer started") {
		t.Errorf("missing success line:\n%s", out)
	}

	req, ok := mock.find(http.MethodPost, "/time-entries")
	if !ok {
		t.Fatal("no POST .../time-entries recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["taskId"] != "k1" {
		t.Errorf("taskId = %v, want k1", body["taskId"])
	}
	tagIDs, _ := body["tagIds"].([]any)
	if len(tagIDs) != 1 || tagIDs[0] != "t1" {
		t.Errorf("tagIds = %v, want [t1]", tagIDs)
	}
}

func TestUpdateTagsAndTask(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "update", "e1",
		"-p", "Website Redesign", "--tag", "review", "--task", "Fix login")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "Updated entry") {
		t.Errorf("missing success line:\n%s", out)
	}

	req, ok := mock.find(http.MethodPut, "/time-entries/e1")
	if !ok {
		t.Fatal("no PUT .../time-entries/e1 recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v, want p1", body["projectId"])
	}
	if body["taskId"] != "k1" {
		t.Errorf("taskId = %v, want k1", body["taskId"])
	}
	tagIDs, _ := body["tagIds"].([]any)
	if len(tagIDs) != 1 || tagIDs[0] != "t2" {
		t.Errorf("tagIds = %v, want [t2]", tagIDs)
	}
}

func TestUpdateClearsTagsWithEmptyValue(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "update", "e1", "--tag", ""); err != nil {
		t.Fatalf("update --tag \"\": %v", err)
	}
	req, ok := mock.find(http.MethodPut, "/time-entries/e1")
	if !ok {
		t.Fatal("no PUT .../time-entries/e1 recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	raw, present := body["tagIds"]
	if !present {
		t.Fatal("clearing tags must send an explicit empty tagIds")
	}
	if ids, ok := raw.([]any); !ok || len(ids) != 0 {
		t.Errorf("tagIds = %v, want an empty array", raw)
	}
	if _, present := body["taskId"]; present {
		t.Errorf("untouched task must not be sent, got %v", body["taskId"])
	}
}

func TestActiveOnlyHelper(t *testing.T) {
	in := []clockify.Project{
		{ID: "p1", Name: "A"},
		{ID: "p2", Name: "B", Archived: true},
		{ID: "p3", Name: "C"},
	}
	got := activeOnly(in)
	if len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p3" {
		t.Errorf("activeOnly = %v, want [p1 p3]", got)
	}
}

func sortFixture() []clockify.TimeEntry {
	return []clockify.TimeEntry{
		{
			ID: "e-short", Description: "Zebra task",
			TimeInterval: &clockify.TimeInterval{Start: ts(9, 0), End: ts(9, 10)}, // 10m
		},
		{
			ID: "e-long", Description: "alpha task",
			TimeInterval: &clockify.TimeInterval{Start: ts(8, 0), End: ts(9, 30)}, // 1h30m
		},
		{
			ID: "e-mid", Description: "Middle",
			TimeInterval: &clockify.TimeInterval{Start: ts(10, 0), End: ts(10, 45)}, // 45m
		},
	}
}

// rowOrder returns the positions of needles in s; fails the test when any is missing.
func rowOrder(t *testing.T, out string, needles ...string) []int {
	t.Helper()
	pos := make([]int, len(needles))
	for i, n := range needles {
		pos[i] = strings.Index(out, n)
		if pos[i] < 0 {
			t.Fatalf("output missing %q:\n%s", n, out)
		}
	}
	return pos
}

func TestListSortDurationEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	mock.setEntries(sortFixture())

	out, err := runCLI(t, "list", "--sort", "duration")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "sorted by duration ↓") {
		t.Errorf("header should announce the sort:\n%s", out)
	}
	// Default duration direction: longest first.
	p := rowOrder(t, out, "alpha task", "Middle", "Zebra task")
	if p[0] >= p[1] || p[1] >= p[2] {
		t.Errorf("duration desc order wrong (positions %v):\n%s", p, out)
	}

	out, err = runCLI(t, "list", "--sort", "duration", "--asc")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "sorted by duration ↑") {
		t.Errorf("asc header missing:\n%s", out)
	}
	p = rowOrder(t, out, "Zebra task", "Middle", "alpha task")
	if p[0] >= p[1] || p[1] >= p[2] {
		t.Errorf("duration asc order wrong (positions %v):\n%s", p, out)
	}
}

func TestListSortStartDefaultDirection(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	mock.setEntries(sortFixture())

	out, err := runCLI(t, "list", "--sort", "start")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// start desc → most recent start (10:00 Middle) first, earliest (08:00) last.
	p := rowOrder(t, out, "Middle", "Zebra task", "alpha task")
	if p[0] >= p[1] || p[1] >= p[2] {
		t.Errorf("start desc order wrong (positions %v):\n%s", p, out)
	}

	// Sorting fetches a full page unless -n is given.
	req, ok := mock.find(http.MethodGet, "/time-entries")
	if !ok {
		t.Fatal("no GET /time-entries recorded")
	}
	if !strings.Contains(req.Query, "page-size=50") {
		t.Errorf("sorted list should default to page-size=50: %q", req.Query)
	}
}

func TestListSortErrors(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "list", "--sort", "priority"); err == nil ||
		!strings.Contains(err.Error(), "unknown sort field") {
		t.Errorf("err = %v, want 'unknown sort field'", err)
	}
	if _, err := runCLI(t, "list", "--sort", "start", "--asc", "--desc"); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want 'mutually exclusive'", err)
	}
	if _, err := runCLI(t, "list", "--asc"); err == nil ||
		!strings.Contains(err.Error(), "need --sort") {
		t.Errorf("err = %v, want 'need --sort'", err)
	}
}

func TestListRangePresetEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "list", "--range", "last-week")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "Last week") {
		t.Errorf("header should name the range:\n%s", out)
	}
	if !strings.Contains(out, "Deep work") {
		t.Errorf("entries missing from output:\n%s", out)
	}

	req, ok := mock.find(http.MethodGet, "/time-entries")
	if !ok {
		t.Fatal("no GET /time-entries recorded")
	}
	if !strings.Contains(req.Query, "start=") {
		t.Errorf("query should carry a start filter: %q", req.Query)
	}
	// Filtered views default to a full page unless -n is given.
	if !strings.Contains(req.Query, "page-size=50") {
		t.Errorf("filtered list should default to page-size=50: %q", req.Query)
	}
}

func TestListCustomWindowEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	out, err := runCLI(t, "list", "--from", "2026-10-01", "--to", "2026-10-07")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "01 Oct 2026") {
		t.Errorf("header should show the custom window:\n%s", out)
	}

	req, ok := mock.find(http.MethodGet, "/time-entries")
	if !ok {
		t.Fatal("no GET /time-entries recorded")
	}
	if !strings.Contains(req.Query, "start=") || !strings.Contains(req.Query, "end=") {
		t.Errorf("query should carry both bounds: %q", req.Query)
	}
}

func TestListNoTruncEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	mock.setEntries([]clockify.TimeEntry{
		{
			ID:          "e-long",
			Description: "Addressing PR review comments on my pull request in painstaking detail",
			Project:     &clockify.ProjectBrief{ID: "p9", Name: "Website Scaffold", ClientName: "Technopreneurs International Enterprises"},
			TimeInterval: &clockify.TimeInterval{
				Start: ptrTime(time.Date(2026, 10, 5, 16, 30, 0, 0, time.UTC)),
				End:   ptrTime(time.Date(2026, 10, 5, 19, 0, 0, 0, time.UTC)),
			},
		},
	})

	out, err := runCLI(t, "list", "--no-trunc")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	const desc = "Addressing PR review comments on my pull request in painstaking detail"
	const proj = "Website Scaffold / Technopreneurs International Enterprises"
	if !strings.Contains(out, desc) {
		t.Errorf("full description missing:\n%s", out)
	}
	if !strings.Contains(out, proj) {
		t.Errorf("full project missing:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("no ellipsis should appear with --no-trunc:\n%s", out)
	}

	out, err = runCLI(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("default view should still truncate:\n%s", out)
	}
}

func TestListRespectsExplicitCountWithRange(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "list", "--range", "today", "-n", "3"); err != nil {
		t.Fatalf("list: %v", err)
	}
	req, _ := mock.find(http.MethodGet, "/time-entries")
	if !strings.Contains(req.Query, "page-size=3") {
		t.Errorf("explicit -n must win over the filtered default: %q", req.Query)
	}
}

func TestListRangeErrors(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	if _, err := runCLI(t, "list", "--range", "yesteryear"); err == nil || !strings.Contains(err.Error(), "unknown range") {
		t.Errorf("err = %v, want 'unknown range'", err)
	}
	if _, err := runCLI(t, "list", "--range", "today", "--from", "2026-10-01"); err == nil {
		t.Error("--range with --from/--to must be rejected")
	}
	if _, err := runCLI(t, "list", "--from", "2026-10-07", "--to", "2026-10-01"); err == nil {
		t.Error("reversed custom window must be rejected")
	}
}
