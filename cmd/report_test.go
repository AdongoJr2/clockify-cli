package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

// weekMonday returns the Monday-based start of the week containing now.
func weekMonday(now time.Time) time.Time {
	m := now.Local()
	offset := (int(m.Weekday()) + 6) % 7
	return time.Date(m.Year(), m.Month(), m.Day(), 0, 0, 0, 0, m.Location()).AddDate(0, 0, -offset)
}

func mkEntry(id string, start, end time.Time, project string, billable bool) clockify.TimeEntry {
	e := clockify.TimeEntry{
		ID:       id,
		Billable: billable,
		TimeInterval: &clockify.TimeInterval{
			Start: clockify.Ptr(start),
		},
	}
	if !end.IsZero() {
		e.TimeInterval.End = clockify.Ptr(end)
	}
	if project != "" {
		e.Project = &clockify.ProjectBrief{ID: "p-" + project, Name: project}
		e.ProjectID = "p-" + project
	}
	return e
}

func TestAggregateReport(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local) // Wednesday
	entries := []clockify.TimeEntry{
		mkEntry("e1", time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local), time.Date(2026, 10, 5, 10, 30, 0, 0, time.Local), "Website", true),
		mkEntry("e2", time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local), time.Date(2026, 10, 6, 9, 45, 0, 0, time.Local), "Backend", false),
		mkEntry("e3", time.Date(2026, 10, 6, 13, 0, 0, 0, time.Local), time.Date(2026, 10, 6, 13, 30, 0, 0, time.Local), "Website", true),
		mkEntry("e4", time.Date(2026, 10, 7, 11, 45, 0, 0, time.Local), time.Time{}, "Website", true), // running
		// running → 15m of elapsed time at now=12:00, no end set
	}

	rep := aggregateReport(entries, now)
	if rep.entries != 4 {
		t.Errorf("entries = %d, want 4", rep.entries)
	}
	if rep.running != 1 {
		t.Errorf("running = %d, want 1", rep.running)
	}
	// 1h30 + 45m + 30m + 15m = 3h
	if rep.total != 3*time.Hour {
		t.Errorf("total = %v, want 3h", rep.total)
	}

	// Days are ordered Mon→Wed with correct per-day totals.
	if len(rep.days) != 3 {
		t.Fatalf("days = %d, want 3 (Mon, Tue, Wed)", len(rep.days))
	}
	wantDays := []struct {
		date   time.Time
		total  time.Duration
		number int
	}{
		{time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local), 90 * time.Minute, 1},
		{time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local), 75 * time.Minute, 2},
		{time.Date(2026, 10, 7, 11, 45, 0, 0, time.Local), 15 * time.Minute, 1},
	}
	for i, w := range wantDays {
		if !rep.days[i].date.Equal(w.date) || rep.days[i].total != w.total || rep.days[i].entries != w.number {
			t.Errorf("day %d = %+v, want %+v (%d entries)", i, rep.days[i], w.total, w.number)
		}
	}

	// Projects are ordered by total descending.
	if len(rep.projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(rep.projects))
	}
	site := rep.projects[0]
	if site.name != "Website" || site.total != 135*time.Minute || site.entries != 3 {
		t.Errorf("first project = %+v, want Website 2h15m/3 entries", site)
	}
	if site.billable != 135*time.Minute {
		t.Errorf("Website billable = %v, want 2h15m", site.billable)
	}
	back := rep.projects[1]
	if back.name != "Backend" || back.total != 45*time.Minute || back.billable != 0 {
		t.Errorf("second project = %+v, want Backend 45m, none billable", back)
	}
	if share := back.share(rep.total); share < 0.24 || share > 0.26 {
		t.Errorf("Backend share = %.2f, want ~0.25", share)
	}
	if back.billableShare() != 0 {
		t.Errorf("Backend billableShare = %v, want 0", back.billableShare())
	}
}

func TestAggregateReportBucketsAndOvernight(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.Local)
	entries := []clockify.TimeEntry{
		mkEntry("e1", time.Date(2026, 10, 5, 23, 0, 0, 0, time.Local), time.Date(2026, 10, 6, 1, 0, 0, 0, time.Local), "", true), // overnight
		mkEntry("e2", time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local), time.Date(2026, 10, 5, 12, 30, 0, 0, time.Local), "", false),
		{ID: "broken"}, // no time data — skipped
	}

	rep := aggregateReport(entries, now)
	if rep.entries != 2 {
		t.Fatalf("entries = %d, want 2 (entry without time data is skipped)", rep.entries)
	}
	// A multi-day entry lands entirely on its start day.
	if len(rep.days) != 1 {
		t.Fatalf("days = %d, want 1 (overnight entry buckets on its start day)", len(rep.days))
	}
	if rep.days[0].total != 150*time.Minute {
		t.Errorf("Monday total = %v, want 2h30m", rep.days[0].total)
	}
	// No project → "(no project)" bucket.
	if len(rep.projects) != 1 || rep.projects[0].name != "(no project)" {
		t.Fatalf("projects = %+v, want a single (no project)", rep.projects)
	}
	if rep.projects[0].billable != 2*time.Hour || rep.projects[0].notBillable != 30*time.Minute {
		t.Errorf("billable split = %v/%v, want 2h/30m", rep.projects[0].billable, rep.projects[0].notBillable)
	}
}

func TestAggregateReportRunningEntryCountedSoFar(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.Local)
	start := now.Add(-90 * time.Minute) // running for 90 minutes
	// An entry that starts in the future (clock skew) must not go negative.
	future := now.Add(30 * time.Minute)

	rep := aggregateReport([]clockify.TimeEntry{
		mkEntry("running", start, time.Time{}, "Website", true),
		mkEntry("future", future, time.Time{}, "Website", true),
	}, now)
	if rep.total != 90*time.Minute {
		t.Errorf("total = %v, want 1h30m (running counted so far, future clamped to 0)", rep.total)
	}
	if rep.running != 2 {
		t.Errorf("running = %d, want 2", rep.running)
	}
}

func TestReportEndToEnd(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	monday := weekMonday(time.Now())
	mock.setEntries([]clockify.TimeEntry{
		mkEntry("e1", monday.Add(9*time.Hour), monday.Add(10*time.Hour+30*time.Minute), "Website Redesign", true),
		mkEntry("e2", monday.AddDate(0, 0, 1).Add(11*time.Hour), monday.AddDate(0, 0, 1).Add(11*time.Hour+45*time.Minute), "Backend API", false),
	})

	out, err := runCLI(t, "report") // defaults to this week
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	for _, want := range []string{
		"This week", "BY DAY", "BY PROJECT",
		"Website Redesign", "Backend API", "1h 30m", "45m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// The window must reach the API as start/end params.
	req, ok := mock.find("GET", "/time-entries")
	if !ok {
		t.Fatal("no GET time-entries recorded")
	}
	if !strings.Contains(req.Query, "start=") {
		t.Errorf("query = %q, want a start= window parameter", req.Query)
	}
}

func TestReportEmptyWindow(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)
	mock.setEntries(nil)

	out, err := runCLI(t, "report", "--range", "last-week")
	if err != nil {
		t.Fatalf("empty report: %v", err)
	}
	if !strings.Contains(out, "No time tracked last week") {
		t.Errorf("empty-window message missing:\n%s", out)
	}
}

func TestReportUnknownRange(t *testing.T) {
	mock := newMockClockify(t)
	setupCLIEnv(t, mock.server.URL)

	_, err := runCLI(t, "report", "--range", "next-century")
	if err == nil || !strings.Contains(err.Error(), "unknown range") {
		t.Fatalf("err = %v, want 'unknown range'", err)
	}
}
