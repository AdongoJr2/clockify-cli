package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// updateFlags holds optional overrides for `clockify update`. Only values the
// user actually passed (or that the interactive form collected) are applied.
var updateFlags struct {
	description string
	start       string
	end         string
	duration    string
	project     string
	billable    bool
}

var updateCmd = &cobra.Command{
	Use:   "update [entry-id]",
	Short: "Update an existing time entry",
	Long: `Edit an existing time entry: its description, start/end time, project,
or billable status.

Pass an entry ID to target it directly, or omit the ID in a terminal to pick
one from a list of your recent entries. Run with no edit flags in a terminal
to fill in an interactive form preloaded with the entry's current values.
Only what you change is updated — everything else stays as-is.

Examples:
  clockify update 5a0ab5acb0798751107918ab -d "Renamed task"
  clockify update 5a0ab5acb0798751107918ab --start "09:00" --end "11:00"
  clockify update 5a0ab5acb0798751107918ab --start "14:00" --duration 2h -p "Backend API"
  clockify update                              # pick an entry, edit via form`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVarP(&updateFlags.description, "description", "d", "", "new description")
	updateCmd.Flags().StringVarP(&updateFlags.start, "start", "s", "", `new start time ("14:00", "2026-10-05 14:00", RFC3339)`)
	updateCmd.Flags().StringVar(&updateFlags.end, "end", "", "new end time")
	updateCmd.Flags().StringVar(&updateFlags.duration, "duration", "", `new duration instead of --end (e.g. "1h30m")`)
	updateCmd.Flags().StringVarP(&updateFlags.project, "project", "p", "", "project name or ID")
	updateCmd.Flags().BoolVar(&updateFlags.billable, "billable", true, "mark the entry as billable")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	now := time.Now()

	changed := map[string]bool{
		"description": cmd.Flags().Changed("description"),
		"start":       cmd.Flags().Changed("start"),
		"end":         cmd.Flags().Changed("end"),
		"duration":    cmd.Flags().Changed("duration"),
		"project":     cmd.Flags().Changed("project"),
		"billable":    cmd.Flags().Changed("billable"),
	}
	endStr := strings.TrimSpace(updateFlags.end)
	durStr := strings.TrimSpace(updateFlags.duration)
	if endStr != "" && durStr != "" {
		return errors.New("--end and --duration are mutually exclusive")
	}

	// 1. Resolve which entry to edit.
	entryID := ""
	if len(args) == 1 {
		entryID = strings.TrimSpace(args[0])
	} else {
		entryID, err = pickEntryToEdit(ctx, client, workspaceID)
		if err != nil {
			return err
		}
	}

	// 2. Load current state — this is the merge base for unchanged fields.
	var existing *clockify.TimeEntry
	if err := runWithSpinner("Loading entry…", func() error {
		var err error
		existing, err = client.GetTimeEntry(ctx, workspaceID, entryID)
		return err
	}); err != nil {
		return err
	}
	if existing.TimeInterval == nil || existing.TimeInterval.Start == nil {
		return errors.New("entry has no start time — it cannot be updated")
	}

	// 3. No flags in a terminal → interactive form preloaded with current values.
	if !anyChanged(changed) {
		if !isInteractive() {
			return errors.New("provide at least one flag (--description, --start, --end, --duration, --project, --billable) or run in a terminal to edit interactively")
		}
		if err := runUpdateForm(ctx, client, workspaceID, existing, changed); err != nil {
			return err
		}
		endStr = strings.TrimSpace(updateFlags.end)
		durStr = strings.TrimSpace(updateFlags.duration)
		if endStr != "" && durStr != "" {
			return errors.New("--end and --duration are mutually exclusive")
		}
	}

	// 4. Merge: explicit edits override the entry's current values.
	description := existing.Description
	if changed["description"] {
		description = updateFlags.description
	}

	startT := *existing.TimeInterval.Start
	if s := strings.TrimSpace(updateFlags.start); s != "" {
		startT, err = parseUserTime(s, now)
		if err != nil {
			return fmt.Errorf("start time: %w", err)
		}
	}

	endT := existing.TimeInterval.End // may be nil (running entry)
	switch {
	case durStr != "":
		d, err := parseDuration(durStr)
		if err != nil {
			return fmt.Errorf("duration: %w", err)
		}
		endT = clockify.Ptr(startT.Add(d))
	case endStr != "":
		e, err := parseUserTime(endStr, now)
		if err != nil {
			return fmt.Errorf("end time: %w", err)
		}
		if !e.After(startT) {
			e = e.AddDate(0, 0, 1) // overnight entry
		}
		endT = &e
	}
	if endT != nil && !endT.After(startT) {
		return errors.New("end must be after start")
	}

	projectID := existing.EffectiveProjectID()
	if changed["project"] {
		projectID, err = resolveProject(ctx, client, workspaceID, updateFlags.project)
		if err != nil {
			return err
		}
	}

	billable := existing.Billable
	if changed["billable"] {
		billable = updateFlags.billable
	}

	// 5. Persist the full merged state (PUT requires start; sending every
	// field makes the result deterministic regardless of server semantics).
	err = runWithSpinner("Saving changes…", func() error {
		_, err := client.UpdateTimeEntry(ctx, workspaceID, entryID, clockify.UpdateTimeEntryRequest{
			Start:       clockify.Ptr(startT),
			Description: description,
			End:         endT,
			ProjectID:   projectID,
			Billable:    clockify.Ptr(billable),
		})
		return err
	})
	if err != nil {
		return err
	}

	if endT != nil {
		printSuccess("Updated entry (%s, %s → %s)", formatDuration(endT.Sub(startT)), formatClock(startT), formatClock(*endT))
	} else {
		printSuccess("Updated entry (still running since %s)", formatClock(startT))
	}
	if description != "" {
		fmt.Println(mutedStyle.Render("  " + description))
	}
	return nil
}

// anyChanged reports whether at least one edit flag was supplied.
func anyChanged(changed map[string]bool) bool {
	for _, v := range changed {
		if v {
			return true
		}
	}
	return false
}

// runUpdateForm edits an entry interactively, preloading every field with the
// entry's current values and marking all fields as changed on submit.
func runUpdateForm(ctx context.Context, client *clockify.Client, workspaceID string, e *clockify.TimeEntry, changed map[string]bool) error {
	now := time.Now()

	description := e.Description
	startStr := e.TimeInterval.Start.Local().Format("2006-01-02 15:04")
	endOrDur := ""
	if e.TimeInterval.End != nil {
		endOrDur = e.TimeInterval.End.Local().Format("2006-01-02 15:04")
	}
	billable := e.Billable

	fields := []huh.Field{
		huh.NewInput().
			Title("Description").
			Value(&description),
		huh.NewInput().
			Title("Start").
			Description(`Local time: "14:00", "2026-10-05 14:00", or RFC3339`).
			Value(&startStr).
			Validate(func(s string) error {
				_, err := parseUserTime(s, now)
				return err
			}),
		huh.NewInput().
			Title("End or duration").
			Description(`e.g. "17:00" or "1h30m" — keep the current value to leave it unchanged`).
			Value(&endOrDur).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				_, _, err := classifyEndInput(s, now)
				return err
			}),
	}

	type projectChoice struct{ id string }
	projects, err := getCachedProjects(ctx, client, viper.GetString("api_key"), workspaceID)
	if err != nil {
		return fmt.Errorf("load projects: %w", err)
	}
	options := []huh.Option[projectChoice]{huh.NewOption("(no project)", projectChoice{})}
	selected := projectChoice{}
	currentProject := e.EffectiveProjectID()
	found := currentProject == ""
	for _, p := range projects {
		options = append(options, huh.NewOption(p.Name, projectChoice{id: p.ID}))
		if p.ID == currentProject {
			selected = projectChoice{id: p.ID}
			found = true
		}
	}
	if !found {
		// The entry's project isn't in the active list (archived/removed) —
		// offer it so submitting doesn't silently clear the project.
		options = append(options, huh.NewOption("current project (unavailable)", projectChoice{id: currentProject}))
		selected = projectChoice{id: currentProject}
	}
	fields = append(fields,
		huh.NewSelect[projectChoice]().
			Title("Project").
			Options(options...).
			Value(&selected),
		huh.NewConfirm().
			Title("Billable?").
			Value(&billable),
	)

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return fmt.Errorf("cancelled: %w", err)
	}

	updateFlags.description = description
	updateFlags.start = strings.TrimSpace(startStr)
	updateFlags.end = ""
	updateFlags.duration = ""
	if s := strings.TrimSpace(endOrDur); s != "" {
		end, dur, err := classifyEndInput(s, now)
		if err != nil {
			return err
		}
		updateFlags.end, updateFlags.duration = end, dur
	}
	updateFlags.project = selected.id
	updateFlags.billable = billable
	for k := range changed {
		changed[k] = true
	}
	return nil
}

// pickEntryToEdit lets the user choose an entry from their recent history.
func pickEntryToEdit(ctx context.Context, client *clockify.Client, workspaceID string) (string, error) {
	if !isInteractive() {
		return "", errors.New("entry ID required: clockify update <entry-id> [flags] (or run in a terminal to pick from a list)")
	}

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Fetching recent entries…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		entries, err = client.GetTimeEntries(ctx, workspaceID, user.ID, clockify.TimeEntryListParams{
			Page:     1,
			PageSize: 10,
			Hydrated: true,
		})
		return err
	}); err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", errors.New("no time entries to update")
	}

	type choice struct{ id string }
	options := make([]huh.Option[choice], 0, len(entries))
	for i := range entries {
		options = append(options, huh.NewOption(entryOptionLabel(&entries[i]), choice{id: entries[i].ID}))
	}

	var selected choice
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[choice]().
			Title("Entry to update").
			Description("Your 10 most recent entries, newest first").
			Options(options...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("cancelled: %w", err)
	}
	return selected.id, nil
}

// entryOptionLabel formats an entry for pickers and confirmation text:
// "05 Oct 09:00 → 10:30 (1h 30m) · Deep work".
func entryOptionLabel(e *clockify.TimeEntry) string {
	label := e.Description
	if label == "" {
		label = "(no description)"
	}
	timing := "no time data"
	if ti := e.TimeInterval; ti != nil && ti.Start != nil {
		if ti.End != nil {
			timing = fmt.Sprintf("%s → %s (%s)", formatClock(*ti.Start), formatClock(*ti.End), formatDuration(ti.End.Sub(*ti.Start)))
		} else {
			timing = fmt.Sprintf("%s → running", formatClock(*ti.Start))
		}
	}
	return fmt.Sprintf("%s · %s", timing, label)
}

// pickRecentEntry lets the user choose one of their 10 most recent entries.
func pickRecentEntry(ctx context.Context, client *clockify.Client, workspaceID, title, description string) (*clockify.TimeEntry, error) {
	if !isInteractive() {
		return nil, errors.New("entry ID required — run in a terminal to pick from a list")
	}

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Fetching recent entries…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		entries, err = client.GetTimeEntries(ctx, workspaceID, user.ID, clockify.TimeEntryListParams{
			Page:     1,
			PageSize: 10,
			Hydrated: true,
		})
		return err
	}); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("no time entries")
	}

	type choice struct {
		entry *clockify.TimeEntry
	}
	options := make([]huh.Option[choice], 0, len(entries))
	for i := range entries {
		options = append(options, huh.NewOption(entryOptionLabel(&entries[i]), choice{entry: &entries[i]}))
	}

	var selected choice
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[choice]().
			Title(title).
			Description(description).
			Options(options...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("cancelled: %w", err)
	}
	return selected.entry, nil
}
