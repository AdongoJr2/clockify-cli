package cmd

import (
	"testing"
	"time"
)

func TestParseUserTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		in      string
		want    time.Time
		wantErr bool
	}{
		{"time of day uses today", "09:30", time.Date(2026, 10, 5, 9, 30, 0, 0, time.Local), false},
		{"date and space time", "2026-10-05 14:00", time.Date(2026, 10, 5, 14, 0, 0, 0, time.Local), false},
		{"date and T time", "2026-10-05T14:00", time.Date(2026, 10, 5, 14, 0, 0, 0, time.Local), false},
		{"date only is midnight", "2026-10-06", time.Date(2026, 10, 6, 0, 0, 0, 0, time.Local), false},
		{"RFC3339 keeps offset", "2026-10-05T14:00:00Z", time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC), false},
		{"empty is an error", "", time.Time{}, true},
		{"garbage is an error", "not a time", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserTime(tt.in, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseUserTime(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUserTime(%q): %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseUserTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveEnd(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.Local)
	start := time.Date(2026, 10, 5, 14, 0, 0, 0, time.Local)

	t.Run("duration", func(t *testing.T) {
		got, err := resolveEnd("", "1h30m", start, now)
		if err != nil {
			t.Fatal(err)
		}
		if want := start.Add(90 * time.Minute); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("end time same day", func(t *testing.T) {
		got, err := resolveEnd("16:00", "", start, now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 10, 5, 16, 0, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("end before start rolls to next day", func(t *testing.T) {
		nightStart := time.Date(2026, 10, 5, 22, 0, 0, 0, time.Local)
		got, err := resolveEnd("02:00", "", nightStart, now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 10, 6, 2, 0, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("missing end and duration", func(t *testing.T) {
		if _, err := resolveEnd("", "", start, now); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		if _, err := resolveEnd("", "banana", start, now); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestClassifyEndInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.Local)

	end, dur, err := classifyEndInput("17:00", now)
	if err != nil || end != "17:00" || dur != "" {
		t.Errorf("got (%q, %q, %v), want (17:00, \"\", nil)", end, dur, err)
	}

	end, dur, err = classifyEndInput("1h30m", now)
	if err != nil || end != "" || dur != "1h30m" {
		t.Errorf("got (%q, %q, %v), want (\"\", 1h30m, nil)", end, dur, err)
	}

	if _, _, err = classifyEndInput("banana", now); err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   time.Duration
		want string
	}{
		{90 * time.Minute, "1h 30m"},
		{45 * time.Minute, "45m"},
		{2 * time.Hour, "2h"},
		{-5 * time.Minute, "0m"},
		{90 * time.Second, "2m"}, // rounds to nearest minute
	}
	for _, tt := range tests {
		if got := formatDuration(tt.in); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
