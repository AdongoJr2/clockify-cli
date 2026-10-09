package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseBatchFileValid(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)
	doc := `
default-project: Website Redesign
entries:
  - description: Standup
    start: "09:30"
    duration: 15m
  - description: Deep work
    start: "2026-09-30 10:00"
    end: "12:30"
    project: Backend API
    billable: false
`
	entries, err := parseBatchFile([]byte(doc), now)
	if err != nil {
		t.Fatalf("parseBatchFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	wantStart := time.Date(2026, 10, 5, 9, 30, 0, 0, time.Local)
	if !entries[0].Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", entries[0].Start, wantStart)
	}
	if want := wantStart.Add(15 * time.Minute); !entries[0].End.Equal(want) {
		t.Errorf("end = %v, want %v", entries[0].End, want)
	}
	if entries[0].Project != "Website Redesign" {
		t.Errorf("default-project not applied: %q", entries[0].Project)
	}
	if entries[0].Billable != nil {
		t.Errorf("omitted billable must stay nil so the project default applies")
	}

	if entries[1].Project != "Backend API" {
		t.Errorf("project = %q, want 'Backend API'", entries[1].Project)
	}
	if entries[1].Billable == nil || *entries[1].Billable {
		t.Errorf("billable = %v, want explicit false", entries[1].Billable)
	}
	if want := time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local); !entries[1].Start.Equal(want) {
		t.Errorf("start = %v, want %v", entries[1].Start, want)
	}
}

func TestParseBatchFileAggregatesAllProblems(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)
	doc := `
entries:
  - start: "09:00"
    duration: 1h
  - description: Both ends
    start: "09:00"
    end: "10:00"
    duration: 1h
  - description: Bad time
    start: "not-a-time"
    duration: 1h
`
	_, err := parseBatchFile([]byte(doc), now)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var verr *batchValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err type = %T, want *batchValidationError", err)
	}
	if len(verr.problems) != 3 {
		t.Fatalf("problems = %v, want 3", verr.problems)
	}
	joined := ""
	for _, p := range verr.problems {
		joined += p + "\n"
	}
	for _, want := range []string{"description is required", "either end or duration", "start"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems should mention %q:\n%s", want, joined)
		}
	}
}

func TestParseBatchFileRejections(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{"empty file", ``, "empty"},
		{"no entries", "default-project: Web\n", "no entries found"},
		{"empty entries list", "entries: []\n", "no entries found"},
		{"unknown field", "entries:\n  - description: x\n    start: \"09:00\"\n    duration: 1h\n    foo: bar\n", "cannot parse YAML"},
		{"two documents", "entries:\n  - description: x\n    start: \"09:00\"\n    duration: 1h\n---\nentries: []\n", "exactly one YAML document"},
		{"missing start", "entries:\n  - description: x\n    duration: 1h\n", "start is required"},
		{"missing end and duration", "entries:\n  - description: x\n    start: \"09:00\"\n", "end or duration is required"},
		{"bad duration", "entries:\n  - description: x\n    start: \"09:00\"\n    duration: banana\n", "duration"},
		{"not a list", "entries: nope\n", "cannot parse YAML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBatchFile([]byte(tt.doc), now)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// writeBatchFile writes a batch YAML file in a temp dir and returns its path.
func writeBatchFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "entries.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write batch file: %v", err)
	}
	return path
}

func TestBatchLogEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - description: Standup
    start: "09:30"
    duration: 15m
    project: Website Redesign
  - description: Deep work
    start: "2026-09-30 10:00"
    end: "12:30"
    billable: false
`)

	out, err := runCLI(t, "log", "--batch", path, "--concurrency", "2")
	if err != nil {
		t.Fatalf("batch log: %v", err)
	}
	for _, want := range []string{"2 entries validated", "Logged 2 entries"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if got := mock.countRequests(http.MethodPost); got != 2 {
		t.Fatalf("POST requests = %d, want 2", got)
	}

	req, ok := mock.findByDescription(http.MethodPost, "Standup")
	if !ok {
		t.Fatal("no POST for the 'Standup' entry recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["description"] != "Standup" {
		t.Errorf("description = %v", body["description"])
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v, want p1 (resolved from project name)", body["projectId"])
	}
	start, err := time.Parse(time.RFC3339, body["start"].(string))
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	// "09:30" is time-only, so it means "today" — never hardcode the date.
	now := time.Now()
	wantStart := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, now.Location())
	if !start.Equal(wantStart.UTC()) {
		t.Errorf("start = %v, want %v (UTC)", start, wantStart.UTC())
	}
	if end, err := time.Parse(time.RFC3339, body["end"].(string)); err != nil || end.Sub(start) != 15*time.Minute {
		t.Errorf("end-start should be 15m, got start=%v end=%v err=%v", start, end, err)
	}
}

func TestBatchDryRunSendsNothing(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, "") // no API base URL: a dry run must not need credentials
	path := writeBatchFile(t, `
entries:
  - description: Standup
    start: "09:30"
    duration: 15m
`)

	out, err := runCLI(t, "log", "--batch", path, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "Dry run · 1 entries") {
		t.Errorf("dry-run header missing:\n%s", out)
	}
	if !strings.Contains(out, "no requests sent") {
		t.Errorf("dry-run should say nothing was sent:\n%s", out)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("dry run sent %d POSTs, want 0", got)
	}
}

func TestBatchValidationFailureSendsNothing(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - start: "09:00"
    duration: 1h
`)

	_, err := runCLI(t, "log", "--batch", path)
	if err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("err = %v, want 'description is required'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("invalid batch sent %d POSTs, want 0", got)
	}
}

func TestBatchPartialFailureIsReported(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - description: Good entry
    start: "09:00"
    duration: 1h
  - description: FAIL please
    start: "10:00"
    duration: 1h
`)

	out, err := runCLI(t, "log", "--batch", path)
	if err == nil {
		t.Fatal("expected a non-zero exit when an entry fails")
	}
	if !strings.Contains(out, "1 of 2 entries failed") {
		t.Errorf("summary missing:\n%s", out)
	}
	if !strings.Contains(out, "entry rejected by mock") {
		t.Errorf("per-entry failure detail missing:\n%s", out)
	}
	if got := mock.countRequests(http.MethodPost); got != 2 {
		t.Errorf("both entries should have been attempted, got %d POSTs", got)
	}
}

func TestBatchConflictsWithEntryFlags(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, "entries:\n  - description: x\n    start: \"09:00\"\n    duration: 1h\n")

	_, err := runCLI(t, "log", "--batch", path, "-d", "something")
	if err == nil || !strings.Contains(err.Error(), "--batch cannot be combined with --description") {
		t.Fatalf("err = %v, want conflict error", err)
	}
}

func TestDryRunWithoutBatchRejected(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "log", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "only applies to --batch") {
		t.Fatalf("err = %v, want 'only applies to --batch'", err)
	}
}

func TestBatchUnknownProjectAbortsBeforeSending(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - description: Standup
    start: "09:00"
    duration: 15m
    project: No Such Project
`)

	_, err := runCLI(t, "log", "--batch", path)
	if err == nil || !strings.Contains(err.Error(), "no entries were logged") {
		t.Fatalf("err = %v, want 'no entries were logged'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("unresolved project must abort before any POST, got %d", got)
	}
}

func TestBatchWithTagsAndTask(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
default-project: Website Redesign
entries:
  - description: Login fix
    start: "09:00"
    duration: 1h
    task: Fix login
    tags: [deep-work, review]
  - description: Plain work
    start: "10:00"
    duration: 30m
`)

	out, err := runCLI(t, "log", "--batch", path)
	if err != nil {
		t.Fatalf("batch log: %v", err)
	}
	if !strings.Contains(out, "Logged 2 entries") {
		t.Errorf("output missing success line:\n%s", out)
	}

	req, ok := mock.findByDescription(http.MethodPost, "Login fix")
	if !ok {
		t.Fatal("no POST for the 'Login fix' entry recorded")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["taskId"] != "k1" {
		t.Errorf("taskId = %v, want k1 (resolved from task name)", body["taskId"])
	}
	if body["projectId"] != "p1" {
		t.Errorf("projectId = %v, want p1 (from default-project)", body["projectId"])
	}
	tagIDs, _ := body["tagIds"].([]any)
	if len(tagIDs) != 2 || tagIDs[0] != "t1" || tagIDs[1] != "t2" {
		t.Errorf("tagIds = %v, want [t1 t2] (resolved from tag names)", tagIDs)
	}

	// The entry without tags/task must not gain any.
	plain, ok := mock.findByDescription(http.MethodPost, "Plain work")
	if !ok {
		t.Fatal("no POST for the 'Plain work' entry recorded")
	}
	var plainBody map[string]any
	if err := json.Unmarshal([]byte(plain.Body), &plainBody); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, present := plainBody["tagIds"]; present {
		t.Errorf("entry without tags should omit tagIds, got %v", plainBody["tagIds"])
	}
	if _, present := plainBody["taskId"]; present {
		t.Errorf("entry without a task should omit taskId, got %v", plainBody["taskId"])
	}
}

func TestBatchTaskRequiresProjectAtValidation(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - description: Orphan task
    start: "09:00"
    duration: 1h
    task: Fix login
`)

	_, err := runCLI(t, "log", "--batch", path)
	if err == nil || !strings.Contains(err.Error(), "task requires a project") {
		t.Fatalf("err = %v, want 'task requires a project'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("invalid batch sent %d POSTs, want 0", got)
	}
}

func TestBatchUnknownTagAbortsBeforeSending(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	path := writeBatchFile(t, `
entries:
  - description: Tagged
    start: "09:00"
    duration: 1h
    project: Website Redesign
    tags: [nope]
`)

	_, err := runCLI(t, "log", "--batch", path)
	if err == nil || !strings.Contains(err.Error(), "no tag matches") {
		t.Fatalf("err = %v, want 'no tag matches'", err)
	}
	if got := mock.countRequests(http.MethodPost); got != 0 {
		t.Errorf("unresolved tag must abort before any POST, got %d", got)
	}
}
