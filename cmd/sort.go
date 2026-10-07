package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

// sortFields maps each accepted --sort value to its default direction:
// time-based fields show the most recent/longest first, text fields A→Z.
var sortFields = map[string]string{
	"start":       "desc",
	"end":         "desc",
	"duration":    "desc",
	"description": "asc",
	"project":     "asc",
}

// resolveSort validates --sort/--asc/--desc and returns the field and final
// direction ("" field means no sorting). An empty field returns ("", "").
func resolveSort(field string, asc, desc bool) (string, string, error) {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" {
		if asc || desc {
			return "", "", fmt.Errorf("--asc/--desc need --sort")
		}
		return "", "", nil
	}
	dir, ok := sortFields[field]
	if !ok {
		names := make([]string, 0, len(sortFields))
		for name := range sortFields {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", "", fmt.Errorf("unknown sort field %q — valid: %s", field, strings.Join(names, ", "))
	}
	if asc && desc {
		return "", "", fmt.Errorf("--asc and --desc are mutually exclusive")
	}
	if asc {
		dir = "asc"
	}
	if desc {
		dir = "desc"
	}
	return field, dir, nil
}

// sortEntries orders entries in place (stable) by field and direction. The
// Clockify API has no ordering parameters, so this runs client-side; "now"
// anchors running entries (their end/duration is treated as ongoing).
func sortEntries(entries []clockify.TimeEntry, field, dir string, now time.Time) {
	if field == "" {
		return
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if dir == "desc" {
			return entryLess(&entries[j], &entries[i], field, now)
		}
		return entryLess(&entries[i], &entries[j], field, now)
	})
}

// entryLess reports whether a sorts before b for the given field.
func entryLess(a, b *clockify.TimeEntry, field string, now time.Time) bool {
	switch field {
	case "start":
		return startOf(a).Before(startOf(b))
	case "end":
		return endOrNow(a, now).Before(endOrNow(b, now))
	case "duration":
		return durationOf(a, now) < durationOf(b, now)
	case "description":
		return strings.ToLower(sanitize(a.Description)) < strings.ToLower(sanitize(b.Description))
	case "project":
		return strings.ToLower(projectLabel(a)) < strings.ToLower(projectLabel(b))
	default:
		return false
	}
}

// startOf returns the entry's start (zero time when missing).
func startOf(e *clockify.TimeEntry) time.Time {
	if e.TimeInterval != nil && e.TimeInterval.Start != nil {
		return *e.TimeInterval.Start
	}
	return time.Time{}
}

// endOrNow returns the entry's end, or now while it is still running.
func endOrNow(e *clockify.TimeEntry, now time.Time) time.Time {
	if e.TimeInterval != nil && e.TimeInterval.End != nil {
		return *e.TimeInterval.End
	}
	if e.TimeInterval != nil && e.TimeInterval.Start != nil {
		return now // running: treat as ongoing
	}
	return time.Time{}
}

// durationOf returns the entry's length (zero when it has no start).
func durationOf(e *clockify.TimeEntry, now time.Time) time.Duration {
	start := startOf(e)
	if start.IsZero() {
		return 0
	}
	return endOrNow(e, now).Sub(start)
}

// projectLabel returns the display name of the entry's project ("" if none).
func projectLabel(e *clockify.TimeEntry) string {
	if e.Project != nil && e.Project.Name != "" {
		name := e.Project.Name
		if e.Project.ClientName != "" {
			name += " / " + e.Project.ClientName
		}
		return name
	}
	return e.EffectiveProjectID()
}
