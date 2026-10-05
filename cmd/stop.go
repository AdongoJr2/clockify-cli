package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the currently running timer",
	Long: `Stop the currently running timer and print how long the last stretch ran.

Example:
  clockify stop`,
	Args: cobra.NoArgs,
	RunE: runStop,
}

func runStop(cmd *cobra.Command, _ []string) error {
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

	running, err := runningEntry(ctx, client, workspaceID, user.ID)
	if err != nil {
		return err
	}
	if running == nil {
		printWarn("No timer running")
		return nil
	}

	now := time.Now()
	if _, err := client.StopTimer(ctx, workspaceID, user.ID, now); err != nil {
		return err
	}

	var elapsed time.Duration
	if running.TimeInterval != nil && running.TimeInterval.Start != nil {
		elapsed = now.Sub(*running.TimeInterval.Start)
	}
	printSuccess("Timer stopped (%s)", formatDuration(elapsed))
	if desc := running.Description; desc != "" {
		fmt.Println(mutedStyle.Render("  " + desc))
	}
	return nil
}
