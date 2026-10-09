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

var startFlags struct {
	description string
	project     string
	tags        []string
	task        string
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a live timer",
	Long: `Start a live timer for the given description and project.

If a timer is already running you'll be asked whether to stop it first
(non-interactive runs must stop it explicitly with ` + "`clockify stop`" + `).

Examples:
  clockify start -d "Writing docs" -p "Website Redesign"
  clockify start -d "Fix login" -p "Website" --tag dev --task "Fix login"
  clockify start                              # interactive form`,
	Args: cobra.NoArgs,
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVarP(&startFlags.description, "description", "d", "", "what you're working on")
	startCmd.Flags().StringVarP(&startFlags.project, "project", "p", "", "project name or ID (optional)")
	startCmd.Flags().StringSliceVar(&startFlags.tags, "tag", nil, "attach tags by name or ID (repeatable or comma-separated)")
	startCmd.Flags().StringVar(&startFlags.task, "task", "", "attach a task by name or ID (needs --project)")
}

func runStart(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	user, err := currentUser(ctx, client)
	if err != nil {
		return err
	}

	description := strings.TrimSpace(startFlags.description)
	projectRef := strings.TrimSpace(startFlags.project)
	projectProvided := cmd.Flags().Changed("project")

	if description == "" {
		if !isInteractive() {
			return errors.New("missing --description (run in a terminal to use the interactive form)")
		}
		if description, projectRef, err = runStartForm(ctx, client, workspaceID, description, projectRef, projectProvided); err != nil {
			return err
		}
	}

	// A running timer must be handled before starting the new one.
	var running *clockify.TimeEntry
	if err := runWithSpinner("Checking for a running timer…", func() error {
		var err error
		running, err = runningEntry(ctx, client, workspaceID, user.ID)
		return err
	}); err != nil {
		return err
	}
	if running != nil {
		stopFirst, err := confirmStopRunning(running)
		if err != nil {
			return err
		}
		if !stopFirst {
			return nil // user cancelled
		}
	}

	err = runWithSpinner("Starting timer…", func() error {
		if running != nil {
			if _, err := client.StopTimer(ctx, workspaceID, user.ID, time.Now()); err != nil {
				return fmt.Errorf("stop previous timer: %w", err)
			}
		}
		projectID, err := resolveProject(ctx, client, workspaceID, projectRef)
		if err != nil {
			return err
		}
		var tagIDs []string
		if len(startFlags.tags) > 0 {
			tagIDs, err = resolveTagRefs(ctx, client, workspaceID, startFlags.tags)
			if err != nil {
				return err
			}
		}
		taskID := ""
		if ref := strings.TrimSpace(startFlags.task); ref != "" {
			if projectID == "" {
				return errors.New("--task requires a project (add -p/--project)")
			}
			taskID, err = resolveTaskRef(ctx, client, workspaceID, projectID, ref)
			if err != nil {
				return err
			}
		}
		_, err = client.CreateTimeEntry(ctx, workspaceID, clockify.CreateTimeEntryRequest{
			Description: description,
			Start:       clockify.Ptr(time.Now()),
			ProjectID:   projectID,
			TagIDs:      tagIDs,
			TaskID:      taskID,
		})
		return err
	})
	if err != nil {
		return err
	}

	printSuccess("Timer started")
	fmt.Println(mutedStyle.Render("  " + description))
	return nil
}

// runStartForm collects description (and project, unless --project was given).
func runStartForm(ctx context.Context, client *clockify.Client, workspaceID, description, projectRef string, projectProvided bool) (string, string, error) {
	fields := []huh.Field{
		huh.NewInput().
			Title("Description").
			Description("What are you working on?").
			Validate(nonEmpty("description")).
			Value(&description),
	}

	type projectChoice struct {
		id string
	}
	if !projectProvided {
		projects, err := getCachedProjects(ctx, client, viper.GetString("api_key"), workspaceID)
		if err != nil {
			return "", "", fmt.Errorf("load projects: %w", err)
		}
		options := []huh.Option[projectChoice]{huh.NewOption("(no project)", projectChoice{})}
		for _, p := range projects {
			options = append(options, huh.NewOption(p.Name, projectChoice{id: p.ID}))
		}
		var selected projectChoice
		fields = append(fields,
			huh.NewSelect[projectChoice]().
				Title("Project").
				Options(options...).
				Value(&selected),
		)
		if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
			return "", "", fmt.Errorf("cancelled: %w", err)
		}
		return strings.TrimSpace(description), selected.id, nil
	}

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return "", "", fmt.Errorf("cancelled: %w", err)
	}
	return strings.TrimSpace(description), projectRef, nil
}

// confirmStopRunning asks (interactively) whether to stop the current timer.
// Non-interactive runs get an instructive error instead of a prompt.
func confirmStopRunning(running *clockify.TimeEntry) (bool, error) {
	desc := strings.TrimSpace(running.Description)
	if desc == "" {
		desc = "(no description)"
	}
	if !isInteractive() {
		return false, fmt.Errorf("a timer is already running (%s) — run `clockify stop` first", desc)
	}
	var ok bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("A timer is already running: %s", desc)).
			Description("Stop it and start the new one?").
			Affirmative("Stop & start").
			Negative("Cancel").
			Value(&ok),
	))
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("cancelled: %w", err)
	}
	return ok, nil
}

// runningEntry returns the user's currently running timer, or nil when none.
// The in-progress endpoint is workspace-wide, so entries belonging to other
// users are filtered out.
func runningEntry(ctx context.Context, client *clockify.Client, workspaceID, userID string) (*clockify.TimeEntry, error) {
	entries, err := client.GetInProgress(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].UserID == "" || entries[i].UserID == userID {
			return &entries[i], nil
		}
	}
	return nil, nil
}
