package cmd

import (
	"fmt"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
)

var projectFlags struct {
	name     string
	exact    bool
	archived bool
	active   bool
	count    int
}

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List projects in the workspace",
	Long: `Display your workspace's projects in a colorful table.

By default both active and archived projects are shown. Combine with -n to
limit, or narrow by name — mirroring your time entries back to the project
that owns them.

Examples:
  clockify projects                            # active + archived
  clockify projects --active                   # hide archived
  clockify projects --archived                 # archived only
  clockify projects -p "Website"               # server-side name filter
  clockify projects -p "Website" --exact       # exact name match
  clockify projects --active -n 5`,
	Args: cobra.NoArgs,
	RunE: runProjects,
}

func init() {
	projectsCmd.Flags().StringVarP(&projectFlags.name, "project", "p", "", "server-side filter by project name (substring)")
	projectsCmd.Flags().BoolVar(&projectFlags.exact, "exact", false, "make --project an exact-name match (needs --project)")
	projectsCmd.Flags().BoolVar(&projectFlags.archived, "archived", false, "show archived projects only")
	projectsCmd.Flags().BoolVar(&projectFlags.active, "active", false, "hide archived projects")
	projectsCmd.Flags().IntVarP(&projectFlags.count, "count", "n", 50, "max projects to show")
}

func runProjects(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	if projectFlags.archived && projectFlags.active {
		return fmt.Errorf("--archived and --active are mutually exclusive")
	}
	if projectFlags.exact && strings.TrimSpace(projectFlags.name) == "" {
		return fmt.Errorf("--exact needs --project")
	}

	count := projectFlags.count
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	// One filtered page from the server; the client stops early when the page
	// comes back short, so most calls cost a single request.
	var params clockify.ProjectListParams
	if name := strings.TrimSpace(projectFlags.name); name != "" {
		params.Name = name
		params.StrictNameSearch = projectFlags.exact
	}
	if projectFlags.archived {
		params.Archived = clockify.Ptr(true)
	}
	params.PageSize = count

	var projects []clockify.Project
	if err := runWithSpinner("Fetching projects…", func() error {
		var err error
		projects, err = client.GetProjects(ctx, workspaceID, params)
		return err
	}); err != nil {
		return err
	}

	// Clockify has no server-side "active only" filter, so screen archived
	// projects out here and let the API do the rest.
	if projectFlags.active {
		projects = activeOnly(projects)
	}
	// Safety cap: with archived-only server filter or post-filtering, don't
	// render more than asked for.
	if len(projects) > count {
		projects = projects[:count]
	}

	if len(projects) == 0 {
		printWarn("No projects found")
		return nil
	}

	rows := make([][]string, 0, len(projects))
	for i := range projects {
		p := &projects[i]
		status := "active"
		if p.Archived {
			status = mutedStyle.Render("archived")
		}
		billable := "—"
		if p.Billable {
			billable = "yes"
		}
		rows = append(rows, []string{
			truncate(p.Name, 32),
			truncate(p.ID, 16),
			status,
			billable,
		})
	}

	noun := "projects"
	if len(rows) == 1 {
		noun = "project"
	}
	fmt.Println(accentStyle.Render(fmt.Sprintf("%d %s", len(rows), noun)))
	fmt.Println()
	fmt.Println(renderTable([]string{"NAME", "ID", "STATUS", "BILLABLE"}, rows))
	return nil
}
