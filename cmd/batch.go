package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// batchFile is the on-disk YAML schema for `clockify log --batch`:
//
//	default-project: Website Redesign   # optional; applied to entries without one
//	entries:
//	  - description: Standup
//	    start: "09:30"                 # same formats as the single-entry flags
//	    duration: 15m                   # ... or end: "09:45" (never both)
//	    project: Website Redesign       # optional; project name or ID
//	    billable: true                  # optional; omit to use the project default
type batchFile struct {
	DefaultProject string       `yaml:"default-project"`
	Entries        []batchEntry `yaml:"entries"`
}

// batchEntry is one entry as written in the YAML file.
type batchEntry struct {
	Description string `yaml:"description"`
	Start       string `yaml:"start"`
	End         string `yaml:"end"`
	Duration    string `yaml:"duration"`
	Project     string `yaml:"project"`
	Billable    *bool  `yaml:"billable"`
}

// resolvedEntry is a validated entry, ready to send.
type resolvedEntry struct {
	Description string
	Start       time.Time
	End         time.Time
	Project     string // name or ID as written; "" means no project
	Billable    *bool
}

// batchValidationError aggregates every problem found in a batch file so the
// user can fix them all in one pass. Nothing is sent to the API when it is
// returned.
type batchValidationError struct {
	problems []string
}

func (e *batchValidationError) Error() string {
	if len(e.problems) == 1 {
		return "invalid batch file: " + e.problems[0]
	}
	return fmt.Sprintf("invalid batch file — %d problems:\n  - %s", len(e.problems), strings.Join(e.problems, "\n  - "))
}

// readBatchSource reads the batch YAML from a file, or from stdin for "-".
func readBatchSource(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read batch from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read batch file: %w", err)
	}
	return data, nil
}

// parseBatchFile parses and fully validates a batch YAML document. It makes no
// network calls: every structural, field and time problem is reported together,
// and a partially-valid file is rejected outright.
func parseBatchFile(data []byte, now time.Time) ([]resolvedEntry, error) {
	var problems []string
	fail := func(format string, a ...any) {
		problems = append(problems, fmt.Sprintf(format, a...))
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown keys / typos

	var file batchFile
	if err := dec.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &batchValidationError{problems: []string{"file is empty — expected a YAML document with an `entries:` list"}}
		}
		return nil, &batchValidationError{problems: []string{fmt.Sprintf("cannot parse YAML: %v", err)}}
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, &batchValidationError{problems: []string{"file must contain exactly one YAML document"}}
	}

	if len(file.Entries) == 0 {
		return nil, &batchValidationError{problems: []string{"no entries found — add an `entries:` list with at least one entry"}}
	}
	defaultProject := strings.TrimSpace(file.DefaultProject)

	resolved := make([]resolvedEntry, 0, len(file.Entries))
	for i, raw := range file.Entries {
		label := fmt.Sprintf("entry %d", i+1)
		if desc := strings.TrimSpace(raw.Description); desc != "" {
			label = fmt.Sprintf("entry %d (%q)", i+1, truncate(desc, 30))
		}
		before := len(problems)

		desc := strings.TrimSpace(raw.Description)
		if desc == "" {
			fail("%s: description is required", label)
		}

		startStr := strings.TrimSpace(raw.Start)
		var start time.Time
		switch {
		case startStr == "":
			fail("%s: start is required", label)
		default:
			t, err := parseUserTime(startStr, now)
			if err != nil {
				fail("%s: start — %v", label, err)
			} else {
				start = t
			}
		}

		endStr := strings.TrimSpace(raw.End)
		durStr := strings.TrimSpace(raw.Duration)
		switch {
		case endStr != "" && durStr != "":
			fail("%s: set either end or duration, not both", label)
		case endStr == "" && durStr == "":
			fail("%s: end or duration is required", label)
		}

		var end time.Time
		if !start.IsZero() && (endStr != "" || durStr != "") {
			t, err := resolveEnd(endStr, durStr, start, now)
			switch {
			case err != nil:
				fail("%s: %v", label, err)
			case !t.After(start):
				fail("%s: end must be after start", label)
			default:
				end = t
			}
		}

		project := strings.TrimSpace(raw.Project)
		if project == "" {
			project = defaultProject
		}

		if len(problems) > before {
			continue // entry is invalid — keep collecting problems from others
		}
		resolved = append(resolved, resolvedEntry{
			Description: desc,
			Start:       start,
			End:         end,
			Project:     project,
			Billable:    raw.Billable,
		})
	}

	if len(problems) > 0 {
		return nil, &batchValidationError{problems: problems}
	}
	return resolved, nil
}

// runBatch implements `clockify log --batch <file>`. The file is validated in
// full before any request is made; --dry-run stops after validation.
func runBatch(cmd *cobra.Command, path string) error {
	ctx := cmd.Context()

	data, err := readBatchSource(path)
	if err != nil {
		return err
	}
	entries, err := parseBatchFile(data, time.Now())
	if err != nil {
		return err // nothing was sent
	}
	fmt.Println(mutedStyle.Render(fmt.Sprintf("✓ %d entries validated", len(entries))))

	if logFlags.dryRun {
		rows := make([][]string, 0, len(entries))
		for i, e := range entries {
			project := e.Project
			if project == "" {
				project = mutedStyle.Render("(no project)")
			}
			billable := "—"
			if e.Billable != nil {
				billable = fmt.Sprintf("%v", *e.Billable)
			}
			rows = append(rows, []string{
				strconv.Itoa(i + 1),
				truncate(e.Description, 30),
				project,
				formatClock(e.Start),
				formatClock(e.End),
				formatDuration(e.End.Sub(e.Start)),
				billable,
			})
		}
		fmt.Println(accentStyle.Render(fmt.Sprintf("Dry run · %d entries (no requests sent)", len(rows))))
		fmt.Println()
		fmt.Println(renderTable([]string{"#", "DESCRIPTION", "PROJECT", "START", "END", "DURATION", "BILLABLE"}, rows))
		return nil
	}

	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	// Resolve every distinct project name to an ID once (served from the
	// 15-minute project cache) — still before the first write request.
	projectIDs, err := resolveProjectRefs(ctx, client, workspaceID, entries)
	if err != nil {
		return fmt.Errorf("no entries were logged: %w", err)
	}

	concurrency := logFlags.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 10 {
		concurrency = 10
	}

	var failed []batchFailure
	if err := runWithSpinner(fmt.Sprintf("Logging %d entries…", len(entries)), func() error {
		// Failures are collected per entry rather than aborting the batch.
		failed = executeBatch(ctx, client, workspaceID, entries, projectIDs, concurrency)
		return nil
	}); err != nil {
		return err
	}
	if len(failed) == 0 {
		printSuccess("Logged %d entries", len(entries))
		return nil
	}

	printWarn("%d of %d entries failed", len(failed), len(entries))
	for _, f := range failed {
		fmt.Println(errorStyle.Render("  ✗ ") + fmt.Sprintf("%q — %v", f.description, f.err))
	}
	return fmt.Errorf("batch logging incomplete")
}

// batchFailure records a single entry that could not be logged.
type batchFailure struct {
	index       int
	description string
	err         error
}

// resolveProjectRefs maps each distinct project name/ID to an ID once.
func resolveProjectRefs(ctx context.Context, client *clockify.Client, workspaceID string, entries []resolvedEntry) (map[string]string, error) {
	ids := map[string]string{}
	for _, e := range entries {
		if e.Project == "" {
			continue
		}
		if _, done := ids[e.Project]; done {
			continue
		}
		id, err := resolveProject(ctx, client, workspaceID, e.Project)
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", truncate(e.Description, 30), err)
		}
		ids[e.Project] = id
	}
	return ids, nil
}

// executeBatch posts the entries with bounded concurrency and returns the
// failures (continuing past errors so one bad entry doesn't sink the batch).
func executeBatch(
	ctx context.Context,
	client *clockify.Client,
	workspaceID string,
	entries []resolvedEntry,
	projectIDs map[string]string,
	concurrency int,
) []batchFailure {
	failures := make([]batchFailure, len(entries))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range entries {
		wg.Add(1)
		go func(idx int, e resolvedEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			_, err := client.CreateTimeEntry(ctx, workspaceID, clockify.CreateTimeEntryRequest{
				Description: e.Description,
				Start:       clockify.Ptr(e.Start),
				End:         clockify.Ptr(e.End),
				ProjectID:   projectIDs[e.Project],
				Billable:    e.Billable,
			})
			if err != nil {
				failures[idx] = batchFailure{index: idx, description: e.Description, err: err}
			}
		}(i, entries[i])
	}
	wg.Wait()

	var failed []batchFailure
	for _, f := range failures {
		if f.err != nil {
			failed = append(failed, f)
		}
	}
	return failed
}
