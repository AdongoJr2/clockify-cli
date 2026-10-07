package cmd

import (
	"fmt"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
)

var duplicateCmd = &cobra.Command{
	Use:   "duplicate [entry-id]",
	Short: "Duplicate a time entry",
	Long: `Copy an existing time entry into a new one ("do it again").

Pass an entry ID to copy it directly, or omit the ID in a terminal to pick
one from a list of your recent entries. The copy keeps the original's
description, times, project, and billable status — edit the result
afterwards with "clockify update".

Examples:
  clockify duplicate                       # pick from recent entries
  clockify duplicate 5a0ab5acb0798751107918ab`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDuplicate,
}

func runDuplicate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	// 1. Resolve which entry to copy.
	entryID := ""
	if len(args) == 1 {
		entryID = strings.TrimSpace(args[0])
	} else {
		var picked *clockify.TimeEntry
		picked, err = pickRecentEntry(ctx, client, workspaceID, "Entry to duplicate", "Your 10 most recent entries, newest first")
		if err != nil {
			return err
		}
		entryID = picked.ID
	}

	// 2. Duplicate it (the response is the newly created entry).
	var duplicated *clockify.TimeEntry
	if err := runWithSpinner("Duplicating entry…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		duplicated, err = client.DuplicateTimeEntry(ctx, workspaceID, user.ID, entryID)
		return err
	}); err != nil {
		if clockify.IsNotFound(err) {
			return fmt.Errorf("entry %q not found", entryID)
		}
		return err
	}

	if ti := duplicated.TimeInterval; ti != nil && ti.Start != nil && ti.End != nil {
		printSuccess("Duplicated entry (%s, %s → %s)", formatDuration(ti.End.Sub(*ti.Start)), formatClock(*ti.Start), formatClock(*ti.End))
	} else {
		printSuccess("Duplicated entry")
	}
	if desc := strings.TrimSpace(duplicated.Description); desc != "" {
		fmt.Println(mutedStyle.Render("  " + desc))
	}
	return nil
}
