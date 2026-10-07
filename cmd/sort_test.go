package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

func TestResolveSort(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		asc       bool
		desc      bool
		wantField string
		wantDir   string
		wantErr   string
	}{
		{"no sort", "", false, false, "", "", ""},
		{"default direction for duration", "duration", false, false, "duration", "desc", ""},
		{"default direction for description", "description", false, false, "description", "asc", ""},
		{"case-insensitive field", "START", false, false, "start", "desc", ""},
		{"asc override", "duration", true, false, "duration", "asc", ""},
		{"desc override", "project", false, true, "project", "desc", ""},
		{"unknown field", "priority", false, false, "", "", "unknown sort field"},
		{"asc and desc", "start", true, true, "", "", "mutually exclusive"},
		{"asc without sort", "", true, false, "", "", "need --sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, dir, err := resolveSort(tt.field, tt.asc, tt.desc)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if field != tt.wantField || dir != tt.wantDir {
				t.Errorf("got (%q, %q), want (%q, %q)", field, dir, tt.wantField, tt.wantDir)
			}
		})
	}
}

// ts builds a UTC timestamp pointer for compact fixtures.
func ts(h, m int) *time.Time {
	t := time.Date(2026, 10, 5, h, m, 0, 0, time.UTC)
	return &t
}

func TestSortEntriesDuration(t *testing.T) {
	entries := []clockify.TimeEntry{
		{Description: "short", TimeInterval: &clockify.TimeInterval{Start: ts(9, 0), End: ts(9, 10)}},    // 10m
		{Description: "long", TimeInterval: &clockify.TimeInterval{Start: ts(8, 0), End: ts(9, 30)}},     // 1h30m
		{Description: "medium", TimeInterval: &clockify.TimeInterval{Start: ts(10, 0), End: ts(10, 45)}}, // 45m
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	sortEntries(entries, "duration", "desc", now)
	if got := entries[0].Description + "," + entries[1].Description + "," + entries[2].Description; got != "long,medium,short" {
		t.Errorf("desc order = %s, want long,medium,short", got)
	}

	sortEntries(entries, "duration", "asc", now)
	if got := entries[0].Description + "," + entries[1].Description + "," + entries[2].Description; got != "short,medium,long" {
		t.Errorf("asc order = %s, want short,medium,long", got)
	}
}

func TestSortEntriesStartAndRunning(t *testing.T) {
	entries := []clockify.TimeEntry{
		{Description: "middle", TimeInterval: &clockify.TimeInterval{Start: ts(9, 0), End: ts(9, 30)}},
		{Description: "running", TimeInterval: &clockify.TimeInterval{Start: ts(11, 0), End: nil}},
		{Description: "no-time"}, // zero start
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	// start desc: zero start sorts last.
	sortEntries(entries, "start", "desc", now)
	if got := entries[0].Description + "," + entries[1].Description + "," + entries[2].Description; got != "running,middle,no-time" {
		t.Errorf("start desc order = %s, want running,middle,no-time", got)
	}

	// end desc: the running entry counts as ending "now" → first.
	sortEntries(entries, "end", "desc", now)
	if entries[0].Description != "running" {
		t.Errorf("end desc first = %s, want running (end treated as now)", entries[0].Description)
	}
}

func TestSortEntriesText(t *testing.T) {
	entries := []clockify.TimeEntry{
		{Description: "banana", Project: &clockify.ProjectBrief{Name: "Zeta"}},
		{Description: "Apple", Project: &clockify.ProjectBrief{Name: "alpha"}},
		{Description: "cherry", Project: &clockify.ProjectBrief{Name: "Midway"}},
	}
	now := time.Now()

	sortEntries(entries, "description", "asc", now)
	if entries[0].Description != "Apple" || entries[1].Description != "banana" || entries[2].Description != "cherry" {
		t.Errorf("description asc = %s,%s,%s", entries[0].Description, entries[1].Description, entries[2].Description)
	}

	sortEntries(entries, "project", "asc", now)
	if entries[0].Project.Name != "alpha" || entries[1].Project.Name != "Midway" || entries[2].Project.Name != "Zeta" {
		t.Errorf("project asc = %s,%s,%s", entries[0].Project.Name, entries[1].Project.Name, entries[2].Project.Name)
	}
}

func TestSortEntriesNoFieldKeepsOrder(t *testing.T) {
	entries := []clockify.TimeEntry{
		{Description: "b"},
		{Description: "a"},
	}
	sortEntries(entries, "", "asc", time.Now())
	if entries[0].Description != "b" || entries[1].Description != "a" {
		t.Errorf("empty sort field must not reorder: %s,%s", entries[0].Description, entries[1].Description)
	}
}
