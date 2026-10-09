package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
)

var reportFlags struct {
	rangeName string
	from      string
	to        string
}

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Summarize tracked time by day and project",
	Long: `Summarize time tracked in a period — total time, per-day totals, and a
per-project breakdown. Defaults to the current week (Monday–Sunday).

Ranges work exactly like clockify list: today, yesterday, this-week (week),
last-week, this-month (month), last-month, 7d, 30d, 90d, all — or a custom
--from/--to window. Like the list filter, an entry is counted when its *start*
falls inside the window.

Running timers (no end yet) are counted as elapsed-so-far and noted.

Examples:
  clockify report                          # this week
  clockify report --range last-week
  clockify report --range month           # this month
  clockify report --from 2026-10-01 --to 2026-10-07`,
	Args: cobra.NoArgs,
	RunE: runReport,
}

func init() {
	reportCmd.Flags().StringVar(&reportFlags.rangeName, "range", "", "period to summarize: today, yesterday, this-week (week), last-week, this-month (month), last-month, 7d, 30d, 90d, all (default this-week)")
	reportCmd.Flags().StringVar(&reportFlags.from, "from", "", `custom window start ("2026-10-01", "2026-10-01 09:00", or RFC3339)`)
	reportCmd.Flags().StringVar(&reportFlags.to, "to", "", "custom window end (same formats; defaults to now)")
	_ = reportCmd.RegisterFlagCompletionFunc("range", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return canonicalRangeNames(), cobra.ShellCompDirectiveNoFileComp
	})
}

// dayTotal aggregates one local calendar day of tracked time.
type dayTotal struct {
	date    time.Time
	entries int
	total   time.Duration
}

// projectTotal aggregates tracked time per project ("" → "(no project)").
type projectTotal struct {
	name        string
	entries     int
	total       time.Duration
	billable    time.Duration
	notBillable time.Duration
}

// reportData is the aggregated result rendered by renderReport.
type reportData struct {
	total    time.Duration
	entries  int
	running  int // entries counted as elapsed-so-far
	days     []dayTotal
	projects []projectTotal
}

// billableShare is the fraction of the project's time that is billable (0..1).
func (p *projectTotal) billableShare() float64 {
	if p.total <= 0 {
		return 0
	}
	return float64(p.billable) / float64(p.total)
}

// share is the fraction of the report's grand total (0..1).
func (p *projectTotal) share(total time.Duration) float64 {
	if total <= 0 {
		return 0
	}
	return float64(p.total) / float64(total)
}

// aggregateReport rolls time entries into per-day and per-project totals.
// Entries without an end time are counted as elapsed-so-far using now. Days
// are bucketed by the entry's *start* in local time; a multi-day entry lands
// entirely on its start day (that is how Clockify reports it).
func aggregateReport(entries []clockify.TimeEntry, now time.Time) reportData {
	var rep reportData
	dayIndex := map[string]*dayTotal{}
	projIndex := map[string]*projectTotal{}

	for i := range entries {
		e := &entries[i]
		start := entryStart(e)
		if start.IsZero() {
			continue
		}
		dur := entryDuration(e, now)

		rep.total += dur
		rep.entries++
		if e.TimeInterval == nil || e.TimeInterval.End == nil {
			rep.running++
		}

		key := start.Format("2006-01-02")
		day := dayIndex[key]
		if day == nil {
			day = &dayTotal{date: start}
			dayIndex[key] = day
		}
		day.entries++
		day.total += dur

		name := projectDisplayName(e)
		proj := projIndex[name]
		if proj == nil {
			proj = &projectTotal{name: name}
			projIndex[name] = proj
		}
		proj.entries++
		proj.total += dur
		if e.Billable {
			proj.billable += dur
		} else {
			proj.notBillable += dur
		}
	}

	rep.days = make([]dayTotal, 0, len(dayIndex))
	for _, d := range dayIndex {
		rep.days = append(rep.days, *d)
	}
	sort.Slice(rep.days, func(i, j int) bool { return rep.days[i].date.Before(rep.days[j].date) })

	rep.projects = make([]projectTotal, 0, len(projIndex))
	for _, p := range projIndex {
		rep.projects = append(rep.projects, *p)
	}
	sort.Slice(rep.projects, func(i, j int) bool {
		if rep.projects[i].total != rep.projects[j].total {
			return rep.projects[i].total > rep.projects[j].total
		}
		return rep.projects[i].name < rep.projects[j].name
	})
	return rep
}

// entryStart returns the entry's start time in local time (zero if absent).
func entryStart(e *clockify.TimeEntry) time.Time {
	if e.TimeInterval == nil || e.TimeInterval.Start == nil {
		return time.Time{}
	}
	return e.TimeInterval.Start.Local()
}

// entryDuration is end-start, or now-start while the entry is running.
func entryDuration(e *clockify.TimeEntry, now time.Time) time.Duration {
	if e.TimeInterval == nil || e.TimeInterval.Start == nil {
		return 0
	}
	if e.TimeInterval.End == nil {
		if now.Before(*e.TimeInterval.Start) {
			return 0
		}
		return now.Sub(*e.TimeInterval.Start)
	}
	return e.TimeInterval.End.Sub(*e.TimeInterval.Start)
}

// projectDisplayName resolves the hydrated project name, falling back to
// "(no project)" for entries without one.
func projectDisplayName(e *clockify.TimeEntry) string {
	if e.Project != nil && strings.TrimSpace(e.Project.Name) != "" {
		return e.Project.Name
	}
	if e.EffectiveProjectID() == "" {
		return "(no project)"
	}
	return "(unknown project)"
}

// plural renders "1 entry" / "2 entries".
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, pluralForm)
}

// renderReport prints the aggregated report.
func renderReport(rep reportData, label string, win *timeRange) {
	window := strings.TrimSpace(label)
	if win != nil && win.label != "" {
		window = win.label
	}
	fmt.Println(accentStyle.Render(fmt.Sprintf("Report · %s", window)))
	fmt.Println()

	daysWorked := 0
	for _, d := range rep.days {
		if d.total > 0 {
			daysWorked++
		}
	}
	fmt.Printf("Total %s across %s · %s worked\n",
		formatDuration(rep.total),
		plural(rep.entries, "entry", "entries"),
		plural(daysWorked, "day", "days"),
	)
	if rep.running > 0 {
		fmt.Println(mutedStyle.Render(fmt.Sprintf(
			"  includes %s still running, counted up to now",
			plural(rep.running, "timer", "timers"))))
	}
	fmt.Println()

	rows := make([][]string, 0, len(rep.days))
	for _, d := range rep.days {
		rows = append(rows, []string{
			d.date.Format("02 Jan 2006"),
			d.date.Format("Mon"),
			fmt.Sprintf("%d", d.entries),
			formatDuration(d.total),
		})
	}
	fmt.Println(mutedStyle.Render("BY DAY"))
	fmt.Println(renderTable([]string{"DATE", "DAY", "ENTRIES", "DURATION"}, rows))
	fmt.Println()

	rows = make([][]string, 0, len(rep.projects))
	for _, p := range rep.projects {
		rows = append(rows, []string{
			truncate(p.name, 28),
			fmt.Sprintf("%d", p.entries),
			formatDuration(p.total),
			fmt.Sprintf("%.0f%%", p.share(rep.total)*100),
			fmt.Sprintf("%.0f%%", p.billableShare()*100),
		})
	}
	fmt.Println(mutedStyle.Render("BY PROJECT"))
	fmt.Println(renderTable([]string{"PROJECT", "ENTRIES", "DURATION", "SHARE", "BILLABLE"}, rows))
}

// runReport fetches the window's entries and prints the aggregated report.
func runReport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	// Default to the current week when no range flags were given.
	preset := reportFlags.rangeName
	rangeChanged := cmd.Flags().Changed("range")
	fromChanged := strings.TrimSpace(reportFlags.from) != "" || strings.TrimSpace(reportFlags.to) != ""
	if preset == "" && !rangeChanged && !fromChanged {
		preset = "this-week"
	}
	win, err := resolveRange(preset, reportFlags.from, reportFlags.to, time.Now())
	if err != nil {
		return err
	}

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Summarizing time…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		// Follow pagination until a short page (a month can exceed one
		// page); 10 pages of 50 is a generous ceiling.
		for page := 1; page <= 10; page++ {
			params := clockify.TimeEntryListParams{
				Page:     page,
				PageSize: 50,
				Hydrated: true, // embed project names for the breakdown
			}
			if win != nil {
				if win.hasStart() {
					params.Start = clockify.Ptr(win.start)
				}
				if win.hasEnd() {
					params.End = clockify.Ptr(win.end)
				}
			}
			batch, err := client.GetTimeEntries(ctx, workspaceID, user.ID, params)
			if err != nil {
				return err
			}
			entries = append(entries, batch...)
			if len(batch) < 50 {
				break
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if len(entries) == 0 {
		if win != nil && win.label != "" {
			printWarn("No time tracked %s", strings.ToLower(win.label))
		} else {
			printWarn("No time tracked in that window")
		}
		return nil
	}

	rep := aggregateReport(entries, time.Now())
	renderReport(rep, winLabel(win), win)
	return nil
}

// winLabel returns the window's label, defaulting to the preset name.
func winLabel(win *timeRange) string {
	if win == nil {
		return ""
	}
	if win.label != "" {
		return win.label
	}
	return customRangeLabel(win)
}
