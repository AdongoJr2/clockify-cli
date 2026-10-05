package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestResolveRangePresets(t *testing.T) {
	// 2026-10-07 is a Wednesday; its week starts Monday 2026-10-05.
	now := time.Date(2026, 10, 7, 15, 30, 0, 0, time.Local)
	midnight := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}

	tests := []struct {
		preset   string
		wantLbl  string
		wantFrom time.Time
		wantTo   time.Time // zero = unbounded
	}{
		{"today", "Today", midnight(2026, 10, 7), midnight(2026, 10, 8)},
		{"yesterday", "Yesterday", midnight(2026, 10, 6), midnight(2026, 10, 7)},
		{"this-week", "This week", midnight(2026, 10, 5), time.Time{}},
		{"week", "This week", midnight(2026, 10, 5), time.Time{}},
		{"last-week", "Last week", midnight(2026, 9, 28), midnight(2026, 10, 5)},
		{"previous-week", "Last week", midnight(2026, 9, 28), midnight(2026, 10, 5)},
		{"this-month", "This month", midnight(2026, 10, 1), time.Time{}},
		{"month", "This month", midnight(2026, 10, 1), time.Time{}},
		{"last-month", "Last month", midnight(2026, 9, 1), midnight(2026, 10, 1)},
		{"7d", "Last 7 days", midnight(2026, 10, 1), time.Time{}},
		{"30d", "Last 30 days", midnight(2026, 9, 8), time.Time{}},
		{"all", "All time", time.Time{}, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			got, err := resolveRange(tt.preset, "", "", now)
			if err != nil {
				t.Fatalf("resolveRange(%q): %v", tt.preset, err)
			}
			if got.label != tt.wantLbl {
				t.Errorf("label = %q, want %q", got.label, tt.wantLbl)
			}
			if !got.start.Equal(tt.wantFrom) {
				t.Errorf("start = %v, want %v", got.start, tt.wantFrom)
			}
			if !got.end.Equal(tt.wantTo) {
				t.Errorf("end = %v, want %v", got.end, tt.wantTo)
			}
		})
	}
}

func TestResolveRangeCustomWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 30, 0, 0, time.Local)

	got, err := resolveRange("", "2026-10-01", "2026-10-07", now)
	if err != nil {
		t.Fatalf("resolveRange: %v", err)
	}
	wantFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	wantTo := time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local)
	if !got.start.Equal(wantFrom) || !got.end.Equal(wantTo) {
		t.Errorf("window = %v → %v, want %v → %v", got.start, got.end, wantFrom, wantTo)
	}
	if !strings.Contains(got.label, "01 Oct 2026") {
		t.Errorf("label %q should mention the from date", got.label)
	}

	// Only --from → end stays unbounded.
	got, err = resolveRange("", "2026-10-01", "", now)
	if err != nil {
		t.Fatalf("resolveRange: %v", err)
	}
	if got.hasEnd() {
		t.Errorf("end should be unbounded, got %v", got.end)
	}

	// Only --to → start stays unbounded.
	got, err = resolveRange("", "", "2026-10-07", now)
	if err != nil {
		t.Fatalf("resolveRange: %v", err)
	}
	if got.hasStart() {
		t.Errorf("start should be unbounded, got %v", got.start)
	}
}

func TestResolveRangeErrors(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 30, 0, 0, time.Local)

	if _, err := resolveRange("last-fortnight", "", "", now); err == nil || !strings.Contains(err.Error(), "unknown range") {
		t.Errorf("err = %v, want 'unknown range'", err)
	}
	if _, err := resolveRange("last-week", "2026-10-01", "", now); err == nil {
		t.Error("--range combined with --from/--to must be rejected")
	}
	if _, err := resolveRange("", "2026-10-07", "2026-10-01", now); err == nil || !strings.Contains(err.Error(), "--to must be after") {
		t.Errorf("err = %v, want '--to must be after --from'", err)
	}
	if _, err := resolveRange("", "not-a-date", "", now); err == nil {
		t.Error("invalid --from must be rejected")
	}
}

func TestResolveRangeNoFilter(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 30, 0, 0, time.Local)

	got, err := resolveRange("", "", "", now)
	if err != nil {
		t.Fatalf("resolveRange: %v", err)
	}
	if got != nil {
		t.Errorf("no flags should mean no filter, got %+v", got)
	}
}

func TestCanonicalRangeNames(t *testing.T) {
	names := canonicalRangeNames()
	joined := strings.Join(names, ",")
	for _, want := range []string{"today", "yesterday", "this-week", "last-week", "this-month", "last-month", "7d", "30d", "90d", "all"} {
		if !strings.Contains(joined, want) {
			t.Errorf("canonical names %v missing %q", names, want)
		}
	}
}
