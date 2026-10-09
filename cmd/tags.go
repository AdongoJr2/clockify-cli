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

var tagFlags struct {
	name     string
	exact    bool
	archived bool
	active   bool
	count    int
	add      string
	delete   string
	yes      bool
}

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "List, create, and delete workspace tags",
	Long: `Manage your workspace's tags.

With no action flag, tags are listed in a colorful table — filter by name,
or narrow to archived/active tags. Attach a tag to an entry with
--tag on clockify log/start/update (by name or ID).

Actions:
  --add "name"        create a tag
  --delete name-or-id delete a tag (asks for confirmation; --yes to skip)

Examples:
  clockify tags                            # all tags
  clockify tags -t "deep"                  # server-side name filter
  clockify tags -t "deep" --exact          # exact name match
  clockify tags --archived                 # archived only
  clockify tags --add "deep-work"          # create a tag
  clockify tags --delete "deep-work"       # delete by name or ID, confirm
  clockify tags --delete t123 --yes        # delete by ID, no prompt`,
	Args: cobra.NoArgs,
	RunE: runTags,
}

func init() {
	tagsCmd.Flags().StringVarP(&tagFlags.name, "tag", "t", "", "server-side filter by tag name (substring)")
	tagsCmd.Flags().BoolVar(&tagFlags.exact, "exact", false, "make --tag an exact-name match (needs --tag)")
	tagsCmd.Flags().BoolVar(&tagFlags.archived, "archived", false, "show archived tags only")
	tagsCmd.Flags().BoolVar(&tagFlags.active, "active", false, "hide archived tags")
	tagsCmd.Flags().IntVarP(&tagFlags.count, "count", "n", 50, "max tags to show")
	tagsCmd.Flags().StringVar(&tagFlags.add, "add", "", "create a tag with the given name")
	tagsCmd.Flags().StringVar(&tagFlags.delete, "delete", "", "delete the tag with the given name or ID")
	tagsCmd.Flags().BoolVarP(&tagFlags.yes, "yes", "y", false, "skip the delete confirmation prompt")
}

func runTags(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	addName := strings.TrimSpace(tagFlags.add)
	delRef := strings.TrimSpace(tagFlags.delete)
	if addName != "" && delRef != "" {
		return errors.New("--add and --delete cannot be combined")
	}
	switch {
	case addName != "":
		return runAddTag(ctx, client, workspaceID, addName)
	case delRef != "":
		return runDeleteTag(ctx, client, workspaceID, delRef, tagFlags.yes)
	}

	if tagFlags.archived && tagFlags.active {
		return errors.New("--archived and --active are mutually exclusive")
	}
	if tagFlags.exact && strings.TrimSpace(tagFlags.name) == "" {
		return errors.New("--exact needs --tag")
	}

	count := tagFlags.count
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	var params clockify.TagListParams
	if name := strings.TrimSpace(tagFlags.name); name != "" {
		params.Name = name
		params.StrictNameSearch = tagFlags.exact
	}
	if tagFlags.archived {
		params.Archived = clockify.Ptr(true)
	}
	params.PageSize = count

	var tags []clockify.Tag
	if err := runWithSpinner("Fetching tags…", func() error {
		var err error
		tags, err = client.GetTags(ctx, workspaceID, params)
		return err
	}); err != nil {
		return err
	}

	// The API has no server-side "active only" filter (mirrors projects).
	if tagFlags.active {
		tags = activeTagsOnly(tags)
	}
	if len(tags) > count {
		tags = tags[:count]
	}

	if len(tags) == 0 {
		printWarn("No tags found")
		return nil
	}

	rows := make([][]string, 0, len(tags))
	for i := range tags {
		tg := &tags[i]
		status := "active"
		if tg.Archived {
			status = mutedStyle.Render("archived")
		}
		rows = append(rows, []string{
			truncate(tg.Name, 32),
			truncate(tg.ID, 16),
			status,
		})
	}

	noun := "tags"
	if len(rows) == 1 {
		noun = "tag"
	}
	fmt.Println(accentStyle.Render(fmt.Sprintf("%d %s", len(rows), noun)))
	fmt.Println()
	fmt.Println(renderTable([]string{"NAME", "ID", "STATUS"}, rows))
	return nil
}

// runAddTag creates a workspace tag.
func runAddTag(ctx context.Context, client *clockify.Client, workspaceID, name string) error {
	if name == "" {
		return errors.New("--add needs a tag name (e.g. --add \"deep-work\")")
	}
	var tag *clockify.Tag
	if err := runWithSpinner("Creating tag…", func() error {
		var err error
		tag, err = client.CreateTag(ctx, workspaceID, clockify.CreateTagRequest{Name: name})
		return err
	}); err != nil {
		return err
	}
	printSuccess("Created tag: %s", tag.Name)
	fmt.Println(mutedStyle.Render("  " + tag.ID))
	return nil
}

// runDeleteTag deletes a tag by name or ID after showing exactly which tag
// will be removed — mirroring the time-entry delete flow.
func runDeleteTag(ctx context.Context, client *clockify.Client, workspaceID, ref string, confirmed bool) error {
	var target *clockify.Tag
	if err := runWithSpinner("Finding tag…", func() error {
		var err error
		target, err = resolveTagObject(ctx, client, workspaceID, ref)
		return err
	}); err != nil {
		return err
	}

	if !confirmed {
		if !isInteractive() {
			return errors.New("refusing to delete without confirmation — pass --yes to proceed non-interactively")
		}
		var ok bool
		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Delete tag %q?", target.Name)).
				Description(truncate(target.ID, 16)).
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

	if err := runWithSpinner("Deleting tag…", func() error {
		return client.DeleteTag(ctx, workspaceID, target.ID)
	}); err != nil {
		return err
	}
	printSuccess("Deleted tag: %s", target.Name)
	return nil
}

// activeTagsOnly drops archived tags from a list.
func activeTagsOnly(tags []clockify.Tag) []clockify.Tag {
	active := make([]clockify.Tag, 0, len(tags))
	for _, t := range tags {
		if !t.Archived {
			active = append(active, t)
		}
	}
	return active
}
