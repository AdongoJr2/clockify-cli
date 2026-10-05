package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// timeRange is a local-time window used to filter time entries. A zero start
// or end means "unbounded on that side" (i.e. that query param is omitted).
type timeRange struct {
	label string
	start time.Time
	end   time.Time
}

func (r *timeRange) hasStart() bool { return !r.start.IsZero() }
func (r *timeRange) hasEnd() bool   { return !r.end.IsZero() }

// rangeAliases maps every accepted --range value to its canonical preset name.
var rangeAliases = map[string]string{
	"today":          "today",
	"yesterday":      "yesterday",
	"week":           "this-week",
	"this-week":      "this-week",
	"last-week":      "last-week",
	"previous-week":  "last-week",
	"month":          "this-month",
	"this-month":     "this-month",
	"last-month":     "last-month",
	"previous-month": "last-month",
	"7d":             "7d",
	"last-7-days":    "7d",
	"30d":            "30d",
	"last-30-days":   "30d",
	"90d":            "90d",
	"last-90-days":   "90d",
	"all":            "all",
	"any":            "all",
}

// canonicalRangeNames returns the sorted canonical preset names (for help,
// error messages, and shell completion).
func canonicalRangeNames() []string {
	seen := map[string]bool{}
	for _, name := range rangeAliases {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveRange converts --range / --from / --to into a filter window relative
// to now. It returns (nil, nil) when no filtering was requested.
//
// Weeks are Monday-based (ISO). Bounds are computed in the user's local
// timezone and converted to UTC by the client.
func resolveRange(preset, fromStr, toStr string, now time.Time) (*timeRange, error) {
	preset = strings.ToLower(strings.TrimSpace(preset))
	fromStr = strings.TrimSpace(fromStr)
	toStr = strings.TrimSpace(toStr)

	if preset != "" && (fromStr != "" || toStr != "") {
		return nil, errors.New("use either --range or --from/--to, not both")
	}

	// Custom window.
	if fromStr != "" || toStr != "" {
		r := &timeRange{}
		if fromStr != "" {
			start, err := parseUserTime(fromStr, now)
			if err != nil {
				return nil, fmt.Errorf("--from: %w", err)
			}
			r.start = start
		}
		if toStr != "" {
			end, err := parseUserTime(toStr, now)
			if err != nil {
				return nil, fmt.Errorf("--to: %w", err)
			}
			r.end = end
		}
		if r.hasStart() && r.hasEnd() && !r.end.After(r.start) {
			return nil, errors.New("--to must be after --from")
		}
		r.label = customRangeLabel(r)
		return r, nil
	}

	if preset == "" {
		return nil, nil
	}
	canonical, ok := rangeAliases[preset]
	if !ok {
		return nil, fmt.Errorf("unknown range %q — valid values: %s", preset, strings.Join(canonicalRangeNames(), ", "))
	}

	loc := now.Location()
	midnight := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	}
	// ISO weeks start on Monday.
	weekStart := func(d time.Time) time.Time {
		offset := (int(d.Weekday()) + 6) % 7 // Monday = 0 … Sunday = 6
		return midnight(d.AddDate(0, 0, -offset))
	}
	monthStart := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, loc)
	}
	today := midnight(now)

	switch canonical {
	case "today":
		return &timeRange{label: "Today", start: today, end: today.AddDate(0, 0, 1)}, nil
	case "yesterday":
		return &timeRange{label: "Yesterday", start: today.AddDate(0, 0, -1), end: today}, nil
	case "this-week":
		return &timeRange{label: "This week", start: weekStart(now)}, nil
	case "last-week":
		thisWeek := weekStart(now)
		return &timeRange{label: "Last week", start: thisWeek.AddDate(0, 0, -7), end: thisWeek}, nil
	case "this-month":
		return &timeRange{label: "This month", start: monthStart(now)}, nil
	case "last-month":
		thisMonth := monthStart(now)
		return &timeRange{label: "Last month", start: thisMonth.AddDate(0, -1, 0), end: thisMonth}, nil
	case "7d", "30d", "90d":
		days, _ := strconv.Atoi(strings.TrimSuffix(canonical, "d"))
		return &timeRange{
			label: fmt.Sprintf("Last %d days", days),
			start: today.AddDate(0, 0, -(days - 1)),
		}, nil
	case "all":
		return &timeRange{label: "All time"}, nil
	default:
		return nil, fmt.Errorf("unsupported range %q", preset)
	}
}

// customRangeLabel renders a human label for a --from/--to window.
func customRangeLabel(r *timeRange) string {
	const day = "02 Jan 2006"
	const stamp = "02 Jan 2006 15:04"
	switch {
	case r.hasStart() && r.hasEnd():
		return r.start.Local().Format(day) + " → " + r.end.Local().Format(stamp)
	case r.hasStart():
		return "from " + r.start.Local().Format(stamp)
	case r.hasEnd():
		return "until " + r.end.Local().Format(stamp)
	default:
		return "Custom range"
	}
}
