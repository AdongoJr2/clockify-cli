package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
)

var listFlags struct {
	count     int
	rangeName string
	from      string
	to        string
	noTrunc   bool
	sortField string
	asc       bool
	desc      bool
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List your time entries, optionally filtered by date range",
	Long: `Display your time entries in a colorful table, optionally filtered by a
predefined period or a custom date window.

Ranges: today, yesterday, this-week (week), last-week, this-month (month),
last-month, 7d, 30d, 90d, all.

Filtering matches an entry's *start* time, so an entry that began before the
window but ran into it will not be listed.

Long descriptions and project names are truncated to fit the table —
pass --no-trunc to show them in full.

The API returns entries newest-first; use --sort to order client-side by
start (default), end, duration, description, or project. Times and durations
default to longest/most recent first, text to A→Z; flip with --asc/--desc.

Examples:
  clockify list                                # recent 5
  clockify list --range last-week
  clockify list --range today -n 20
  clockify list --sort duration                # longest first
  clockify list --sort start --asc             # oldest first
  clockify list --sort description --asc
  clockify list --no-trunc                     # full text, no truncation
  clockify list --from 2026-10-01 --to 2026-10-07
  clockify list --from "2026-10-01 09:00"
  clockify list --range 30d`,
	Args: cobra.NoArgs,
	RunE: runList,
}

func init() {
	listCmd.Flags().IntVarP(&listFlags.count, "count", "n", 5, "how many entries to show")
	listCmd.Flags().StringVar(&listFlags.rangeName, "range", "", "filter by period: today, yesterday, this-week, last-week, this-month, last-month, 7d, 30d, 90d, all")
	listCmd.Flags().StringVar(&listFlags.from, "from", "", `custom window start ("2026-10-01", "2026-10-01 09:00", or RFC3339)`)
	listCmd.Flags().StringVar(&listFlags.to, "to", "", "custom window end (same formats; defaults to now)")
	listCmd.Flags().BoolVar(&listFlags.noTrunc, "no-trunc", false, "show full descriptions and project names instead of truncating")
	listCmd.Flags().StringVar(&listFlags.sortField, "sort", "", "order entries by: start, end, duration, description, or project")
	listCmd.Flags().BoolVar(&listFlags.asc, "asc", false, "sort ascending (with --sort)")
	listCmd.Flags().BoolVar(&listFlags.desc, "desc", false, "sort descending (with --sort)")

	_ = listCmd.RegisterFlagCompletionFunc("range", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return canonicalRangeNames(), cobra.ShellCompDirectiveNoFileComp
	})
}

func runList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	// Optional date filtering (--range or --from/--to).
	timeRange, err := resolveRange(listFlags.rangeName, listFlags.from, listFlags.to, time.Now())
	if err != nil {
		return err
	}

	// Optional client-side ordering (--sort/--asc/--desc).
	sortField, sortDir, err := resolveSort(listFlags.sortField, listFlags.asc, listFlags.desc)
	if err != nil {
		return err
	}

	count := listFlags.count
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	// A filtered or sorted view is usually a report rather than a quick peek,
	// so show a full page unless the user asked for a specific count.
	if (timeRange != nil || sortField != "") && !cmd.Flags().Changed("count") {
		count = 50
	}

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Fetching time entries…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		params := clockify.TimeEntryListParams{
			Page:     1,
			PageSize: count,
			Hydrated: true, // embed project names instead of bare IDs
		}
		if timeRange != nil {
			if timeRange.hasStart() {
				params.Start = clockify.Ptr(timeRange.start)
			}
			if timeRange.hasEnd() {
				params.End = clockify.Ptr(timeRange.end)
			}
		}
		entries, err = client.GetTimeEntries(ctx, workspaceID, user.ID, params)
		return err
	}); err != nil {
		return err
	}
	if len(entries) == 0 {
		if timeRange != nil && timeRange.label != "" {
			printWarn("No time entries %s", strings.ToLower(timeRange.label))
		} else {
			printWarn("No time entries yet")
		}
		return nil
	}

	now := time.Now()
	// Order client-side: the API has no sort parameters.
	sortEntries(entries, sortField, sortDir, now)

	rows := make([][]string, 0, len(entries))
	for i := range entries {
		e := &entries[i]

		// sanitize collapses internal whitespace (incl. newlines) into single
		// spaces so descriptions always render as one table row.
		desc := sanitize(e.Description)
		if !listFlags.noTrunc {
			desc = truncate(desc, 40)
		}
		if desc == "" {
			desc = mutedStyle.Render("—")
		}

		project := "(no project)"
		if e.Project != nil && e.Project.Name != "" {
			project = e.Project.Name
			if e.Project.ClientName != "" {
				project += " / " + e.Project.ClientName
			}
		} else if id := e.EffectiveProjectID(); id != "" {
			project = truncate(id, 14) // IDs stay short even with --no-trunc
		}
		if !listFlags.noTrunc {
			project = truncate(project, 30)
		}

		startStr, endStr, durStr := "—", "—", "—"
		if ti := e.TimeInterval; ti != nil {
			if ti.Start != nil {
				startStr = formatClock(*ti.Start)
				if ti.End != nil {
					endStr = formatClock(*ti.End)
					durStr = formatDuration(ti.End.Sub(*ti.Start))
				} else {
					endStr = mutedStyle.Render("running")
					durStr = formatDuration(now.Sub(*ti.Start))
				}
			}
		}

		rows = append(rows, []string{desc, project, startStr, endStr, durStr})
	}

	header := ""
	if timeRange != nil && timeRange.label != "" {
		noun := "entries"
		if len(rows) == 1 {
			noun = "entry"
		}
		header = fmt.Sprintf("%s · %d %s", timeRange.label, len(rows), noun)
	} else {
		header = fmt.Sprintf("Last %d time entries", len(rows))
	}
	fmt.Println(accentStyle.Render(header))
	if sortField != "" {
		arrow := "↓"
		if sortDir == "asc" {
			arrow = "↑"
		}
		fmt.Println(mutedStyle.Render(fmt.Sprintf("sorted by %s %s", sortField, arrow)))
	}
	fmt.Println()
	fmt.Println(renderTable([]string{"DESCRIPTION", "PROJECT", "START", "END", "DURATION"}, rows))
	return nil
}
