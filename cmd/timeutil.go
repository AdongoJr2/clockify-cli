package cmd

import (
	"fmt"
	"strings"
	"time"
)

// parseUserTime parses the absolute time formats accepted on the command line,
// interpreted in the user's local timezone unless an offset is given:
//
//	RFC3339       2026-10-05T14:00:00+02:00
//	"2006-01-02 15:04[:05]"   2026-10-05 14:00
//	"2006-01-02T15:04[:05]"   2026-10-05T14:00
//	"2006-01-02"              2026-10-05 (midnight)
//	"15:04"                   today at 14:00 local
func parseUserTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("time is required")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	loc := now.Location()
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("15:04", s, loc); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q (try \"14:00\", \"2026-10-05 14:00\", or RFC3339)", s)
}

// parseDuration parses Go durations such as "1h30m" or "45m".
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("unrecognized duration %q (try \"1h30m\" or \"45m\")", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}

// resolveEnd computes the entry's end time from either an explicit end string
// or a duration string. An end time that is not after the start rolls forward
// one day to support overnight entries.
func resolveEnd(endStr, durStr string, start, now time.Time) (time.Time, error) {
	switch {
	case strings.TrimSpace(durStr) != "":
		d, err := parseDuration(durStr)
		if err != nil {
			return time.Time{}, err
		}
		return start.Add(d), nil
	case strings.TrimSpace(endStr) != "":
		end, err := parseUserTime(endStr, now)
		if err != nil {
			return time.Time{}, err
		}
		if !end.After(start) {
			end = end.AddDate(0, 0, 1)
		}
		return end, nil
	default:
		return time.Time{}, fmt.Errorf("either an end time or a duration is required")
	}
}

// classifyEndInput decides whether a combined "end or duration" input is a
// clock/date (end string) or a Go duration ("1h30m").
func classifyEndInput(s string, now time.Time) (endStr, durStr string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("end time or duration is required")
	}
	if _, err := parseUserTime(s, now); err == nil {
		return s, "", nil
	}
	if _, err := parseDuration(s); err == nil {
		return "", s, nil
	}
	return "", "", fmt.Errorf("unrecognized %q (use \"17:00\", \"2026-10-05 17:00\", or a duration like \"1h30m\")", s)
}

// formatDuration renders a duration rounded to the minute, e.g. "1h 30m".
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// formatClock renders a timestamp as local "02 Jan 15:04".
func formatClock(t time.Time) string {
	return t.Local().Format("02 Jan 15:04")
}

// toISODuration renders d as the ISO-8601 duration Clockify expects for task
// estimates (e.g. "PT4H", "PT1H30M"). Returns "" for non-positive durations.
func toISODuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("PT%dH%dM", h, m)
	case h > 0:
		return fmt.Sprintf("PT%dH", h)
	default:
		return fmt.Sprintf("PT%dM", m)
	}
}

// parseISODuration parses the ISO-8601 durations Clockify returns for task
// estimates and tracked time, e.g. "PT45M", "PT1H30M", "P1DT2H".
func parseISODuration(s string) (time.Duration, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) < 2 || s[0] != 'P' {
		return 0, fmt.Errorf("unrecognized ISO-8601 duration %q", s)
	}
	s = s[1:]

	var total time.Duration
	var num int
	seen := false
	inTime := false
	consumed := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			num = num*10 + int(c-'0')
			seen = true
		case c == 'T':
			if inTime || seen {
				return 0, fmt.Errorf("unrecognized ISO-8601 duration")
			}
			inTime = true
		default:
			if !seen {
				return 0, fmt.Errorf("unrecognized ISO-8601 duration")
			}
			var unit time.Duration
			switch c {
			case 'W':
				unit = 7 * 24 * time.Hour
			case 'D':
				if inTime {
					return 0, fmt.Errorf("unrecognized ISO-8601 duration")
				}
				unit = 24 * time.Hour
			case 'H', 'M', 'S':
				if !inTime {
					return 0, fmt.Errorf("unrecognized ISO-8601 duration")
				}
				switch c {
				case 'H':
					unit = time.Hour
				case 'M':
					unit = time.Minute
				default:
					unit = time.Second
				}
			default:
				return 0, fmt.Errorf("unrecognized ISO-8601 duration")
			}
			total += time.Duration(num) * unit
			num, seen = 0, false
			consumed = true
		}
	}
	if seen || !consumed {
		return 0, fmt.Errorf("unrecognized ISO-8601 duration")
	}
	return total, nil
}

// formatISODuration renders an ISO-8601 duration for display ("PT1H30M" →
// "1h 30m"), falling back to the raw string when it cannot be parsed.
func formatISODuration(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	d, err := parseISODuration(s)
	if err != nil {
		return s
	}
	return formatDuration(d)
}
