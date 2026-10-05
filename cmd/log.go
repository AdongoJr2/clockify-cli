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

// logInput collects the values needed to create a past time entry.
type logInput struct {
	description string
	start       string
	end         string
	duration    string
	project     string
	billable    bool
}

var logFlags logInput

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Log a past time entry",
	Long: `Log a completed (past) time entry.

Provide every value via flags for scripting, or omit flags to fill in an
interactive form. Times are interpreted in your local timezone and stored in UTC.

Examples:
  clockify log -d "Standup" --start "09:30" --duration 15m
  clockify log -d "Deep work" --start "2026-10-05 14:00" --end "16:00" -p "Website Redesign"
  clockify log                                 # interactive form`,
	Args: cobra.NoArgs,
	RunE: runLog,
}

func init() {
	logCmd.Flags().StringVarP(&logFlags.description, "description", "d", "", "what you worked on")
	logCmd.Flags().StringVarP(&logFlags.start, "start", "s", "", `start time ("14:00", "2026-10-05 14:00", or RFC3339)`)
	logCmd.Flags().StringVar(&logFlags.end, "end", "", "end time (alternative to --duration)")
	logCmd.Flags().StringVar(&logFlags.duration, "duration", "", `duration instead of end time (e.g. "1h30m")`)
	logCmd.Flags().StringVarP(&logFlags.project, "project", "p", "", "project name or ID (optional)")
	logCmd.Flags().BoolVar(&logFlags.billable, "billable", true, "mark the entry as billable")
}

func runLog(cmd *cobra.Command, _ []string) error {
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
	in := logFlags
	in.description = strings.TrimSpace(in.description)
	in.start = strings.TrimSpace(in.start)
	in.end = strings.TrimSpace(in.end)
	in.duration = strings.TrimSpace(in.duration)
	in.project = strings.TrimSpace(in.project)

	if in.description == "" || in.start == "" || (in.end == "" && in.duration == "") {
		if !isInteractive() {
			return errors.New("missing flags: --description, --start, and either --end or --duration (run without flags in a terminal to use the interactive form)")
		}
		if err := runLogForm(ctx, client, workspaceID, &in, cmd.Flags().Changed("project")); err != nil {
			return err
		}
	}

	start, err := parseUserTime(in.start, now)
	if err != nil {
		return fmt.Errorf("start time: %w", err)
	}
	end, err := resolveEnd(in.end, in.duration, start, now)
	if err != nil {
		return fmt.Errorf("end: %w", err)
	}
	if !end.After(start) {
		return errors.New("end must be after start")
	}

	projectID, err := resolveProject(ctx, client, workspaceID, in.project)
	if err != nil {
		return err
	}

	err = runWithSpinner("Saving time entry…", func() error {
		_, err := client.CreateTimeEntry(ctx, workspaceID, clockify.CreateTimeEntryRequest{
			Description: in.description,
			Start:       clockify.Ptr(start),
			End:         clockify.Ptr(end),
			ProjectID:   projectID,
			Billable:    clockify.Ptr(in.billable),
		})
		return err
	})
	if err != nil {
		return err
	}

	printSuccess("Logged %s (%s → %s)", formatDuration(end.Sub(start)), formatClock(start), formatClock(end))
	if in.description != "" {
		fmt.Println(mutedStyle.Render("  " + in.description))
	}
	return nil
}

// runLogForm fills any missing values through an interactive Huh form.
// Project is only shown when not already supplied via --project.
func runLogForm(ctx context.Context, client *clockify.Client, workspaceID string, in *logInput, projectProvided bool) error {
	now := time.Now()

	endOrDur := in.end
	if in.duration != "" {
		endOrDur = in.duration
	}

	fields := []huh.Field{
		huh.NewInput().
			Title("Description").
			Description("What did you work on?").
			Validate(nonEmpty("description")).
			Value(&in.description),
		huh.NewInput().
			Title("Start").
			Description(`Local time: "14:00", "2026-10-05 14:00", or RFC3339`).
			Validate(func(s string) error {
				_, err := parseUserTime(s, now)
				return err
			}).
			Value(&in.start),
		huh.NewInput().
			Title("End or duration").
			Description(`e.g. "17:00" or "1h30m" — end may be a time or a duration`).
			Validate(func(s string) error {
				_, _, err := classifyEndInput(s, now)
				return err
			}).
			Value(&endOrDur),
	}

	type projectChoice struct {
		id    string
		label string
	}
	if !projectProvided {
		projects, err := getCachedProjects(ctx, client, viper.GetString("api_key"), workspaceID)
		if err != nil {
			return fmt.Errorf("load projects: %w", err)
		}
		options := []huh.Option[projectChoice]{huh.NewOption("(no project)", projectChoice{})}
		for _, p := range projects {
			options = append(options, huh.NewOption(p.Name, projectChoice{id: p.ID, label: p.Name}))
		}
		var selected projectChoice
		fields = append(fields,
			huh.NewSelect[projectChoice]().
				Title("Project").
				Options(options...).
				Value(&selected),
		)
		defer func() { in.project = selected.id }()
	}

	billable := in.billable
	fields = append(fields, huh.NewConfirm().Title("Billable?").Value(&billable))

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return fmt.Errorf("cancelled: %w", err)
	}

	end, dur, err := classifyEndInput(endOrDur, now)
	if err != nil {
		return err
	}
	in.end, in.duration = end, dur
	in.description = strings.TrimSpace(in.description)
	in.start = strings.TrimSpace(in.start)
	in.billable = billable
	return nil
}

// resolveProject turns a project name or ID from a flag into a project ID.
// Names are matched exactly first, then by unique substring; raw IDs pass
// through when they cannot be (or fail to) match the fetched project list.
func resolveProject(ctx context.Context, client *clockify.Client, workspaceID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}

	projects, err := getCachedProjects(ctx, client, viper.GetString("api_key"), workspaceID)
	if err != nil {
		if looksLikeID(ref) {
			return ref, nil // trust a raw ID when the lookup fails
		}
		return "", err
	}

	var exact *clockify.Project
	var partial []clockify.Project
	for i := range projects {
		p := &projects[i]
		if strings.EqualFold(p.Name, ref) {
			exact = p
			break
		}
		if strings.Contains(strings.ToLower(p.Name), strings.ToLower(ref)) {
			partial = append(partial, *p)
		}
	}
	switch {
	case exact != nil:
		return exact.ID, nil
	case len(partial) == 1:
		return partial[0].ID, nil
	case len(partial) > 1:
		names := make([]string, 0, len(partial))
		for _, p := range partial {
			names = append(names, p.Name)
		}
		return "", fmt.Errorf("project %q is ambiguous — matches: %s", ref, strings.Join(names, ", "))
	case looksLikeID(ref):
		return ref, nil // raw ID of an unknown or archived project
	default:
		return "", fmt.Errorf("no project matches %q (run without --project in a terminal to pick from a list)", ref)
	}
}

// looksLikeID reports whether s resembles a Clockify object ID
// (a long, space-free alphanumeric token).
func looksLikeID(s string) bool {
	if len(s) < 18 || len(s) > 40 || strings.ContainsAny(s, " \t") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}
