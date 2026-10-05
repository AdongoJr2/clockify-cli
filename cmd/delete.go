package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var deleteFlags struct {
	description string
	yes         bool
}

var deleteCmd = &cobra.Command{
	Use:   "delete [entry-id]",
	Short: "Delete a time entry (cannot be undone)",
	Long: `Permanently delete a time entry.

Target the entry by ID, by description (-d), or omit both in a terminal to
pick from your recent entries. Deletion always shows which entry will be
removed and asks for confirmation, unless --yes is passed.

Examples:
  clockify delete -d "Testing..."             # find by description, confirm
  clockify delete -d "Testing..." --yes       # same, without the prompt
  clockify delete 5a0ab5acb0798751107918ab    # by ID, with confirmation
  clockify delete                              # pick from recent entries`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVarP(&deleteFlags.description, "description", "d", "", "find the entry by description instead of ID")
	deleteCmd.Flags().BoolVarP(&deleteFlags.yes, "yes", "y", false, "skip the confirmation prompt")
}

func runDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	if deleteFlags.description != "" && len(args) == 1 {
		return errors.New("pass either an entry ID or --description, not both")
	}

	// 1. Find the target entry.
	var target *clockify.TimeEntry
	switch {
	case len(args) == 1:
		id := strings.TrimSpace(args[0])
		if err := runWithSpinner("Loading entry…", func() error {
			var err error
			target, err = client.GetTimeEntry(ctx, workspaceID, id)
			return err
		}); err != nil {
			return err
		}
	case strings.TrimSpace(deleteFlags.description) != "":
		target, err = findEntryByDescription(ctx, client, workspaceID, strings.TrimSpace(deleteFlags.description))
		if err != nil {
			return err
		}
	default:
		if !isInteractive() {
			return errors.New("specify an entry ID, use -d/--description, or run in a terminal to pick from a list")
		}
		target, err = pickRecentEntry(ctx, client, workspaceID, "Entry to delete", "Your 10 most recent entries, newest first")
		if err != nil {
			return err
		}
	}

	// 2. Show exactly what will be removed and confirm.
	label := entryOptionLabel(target)
	if !deleteFlags.yes {
		if !isInteractive() {
			return errors.New("refusing to delete without confirmation — pass --yes to proceed non-interactively")
		}
		var ok bool
		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Delete %q?", strings.TrimSpace(target.Description))).
				Description(label).
				Affirmative("Delete").
				Negative("Cancel").
				Value(&ok),
		))
		if err := form.Run(); err != nil {
			return fmt.Errorf("cancelled: %w", err)
		}
		if !ok {
			fmt.Println(mutedStyle.Render("Cancelled — nothing deleted"))
			return nil
		}
	}

	// 3. Delete it.
	if err := runWithSpinner("Deleting entry…", func() error {
		return client.DeleteTimeEntry(ctx, workspaceID, target.ID)
	}); err != nil {
		return err
	}

	printSuccess("Deleted entry: %s", label)
	return nil
}

// findEntryByDescription searches recent entries for a description match.
// Exact (case-insensitive) matches win over substring matches; multiple
// matches open an interactive picker, or list candidate IDs when non-TTY.
func findEntryByDescription(ctx context.Context, client *clockify.Client, workspaceID, want string) (*clockify.TimeEntry, error) {
	want = strings.ToLower(strings.TrimSpace(want))

	var entries []clockify.TimeEntry
	if err := runWithSpinner("Searching entries…", func() error {
		user, err := currentUser(ctx, client)
		if err != nil {
			return err
		}
		for page := 1; page <= 3; page++ {
			batch, err := client.GetTimeEntries(ctx, workspaceID, user.ID, clockify.TimeEntryListParams{
				Page:     page,
				PageSize: 50,
				Hydrated: true,
			})
			if err != nil {
				return err
			}
			entries = append(entries, batch...)
			if len(batch) < 50 || hasExactDescription(batch, want) {
				break
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var exact, partial []*clockify.TimeEntry
	for i := range entries {
		e := &entries[i]
		switch desc := strings.ToLower(strings.TrimSpace(e.Description)); {
		case desc == want:
			exact = append(exact, e)
		case strings.Contains(desc, want):
			partial = append(partial, e)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no entry with a description containing %q (checked %d recent entries)", want, len(entries))
	case 1:
		return matches[0], nil
	default:
		if !isInteractive() {
			lines := make([]string, 0, len(matches))
			for _, e := range matches {
				lines = append(lines, fmt.Sprintf("  %s  %s", e.ID, e.Description))
			}
			return nil, fmt.Errorf("%d entries match %q:\n%s\nrefine the description or pass an entry ID", len(matches), want, strings.Join(lines, "\n"))
		}
		return pickFromMatches(matches)
	}
}

// hasExactDescription reports whether any entry's trimmed lowercase
// description equals want.
func hasExactDescription(entries []clockify.TimeEntry, want string) bool {
	for i := range entries {
		if strings.ToLower(strings.TrimSpace(entries[i].Description)) == want {
			return true
		}
	}
	return false
}

// pickFromMatches opens a picker restricted to the matched entries.
func pickFromMatches(matches []*clockify.TimeEntry) (*clockify.TimeEntry, error) {
	type choice struct {
		entry *clockify.TimeEntry
	}
	options := make([]huh.Option[choice], 0, len(matches))
	for _, e := range matches {
		options = append(options, huh.NewOption(entryOptionLabel(e), choice{entry: e}))
	}
	var selected choice
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[choice]().
			Title("Which entry?").
			Description(fmt.Sprintf("%d matches — pick the one to delete", len(matches))).
			Options(options...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("cancelled: %w", err)
	}
	return selected.entry, nil
}
