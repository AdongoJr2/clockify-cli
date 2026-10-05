package cmd

import (
	"fmt"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
)

var listFlags struct {
	count int
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List your most recent time entries",
	Long: `Display your most recent time entries in a colorful table.

Examples:
  clockify list
  clockify list -n 10`,
	Args: cobra.NoArgs,
	RunE: runList,
}

func init() {
	listCmd.Flags().IntVarP(&listFlags.count, "count", "n", 5, "how many entries to show")
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
	count := listFlags.count
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Fetching time entries…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		entries, err = client.GetTimeEntries(ctx, workspaceID, user.ID, clockify.TimeEntryListParams{
			Page:     1,
			PageSize: count,
			Hydrated: true, // embed project names instead of bare IDs
		})
		return err
	}); err != nil {
		return err
	}
	if len(entries) == 0 {
		printWarn("No time entries yet")
		return nil
	}

	now := time.Now()
	rows := make([][]string, 0, len(entries))
	for i := range entries {
		e := &entries[i]

		desc := truncate(e.Description, 40)
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
			project = truncate(id, 14)
		}
		project = truncate(project, 30)

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

	fmt.Println(accentStyle.Render(fmt.Sprintf("Last %d time entries", len(rows))))
	fmt.Println()
	fmt.Println(renderTable([]string{"DESCRIPTION", "PROJECT", "START", "END", "DURATION"}, rows))
	return nil
}
