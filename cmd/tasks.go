package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var taskFlags struct {
	project  string
	name     string
	exact    bool
	active   bool
	count    int
	add      string
	estimate string
	status   string
	done     string
	delete   string
	yes      bool
}

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List, create, and manage a project's tasks",
	Long: `Manage the tasks of a project (Clockify tasks live inside a project).

--project picks the project (by name or ID); omit it in a terminal to pick
from a list. Attach a task to an entry with --task on clockify
log/start/update, or set task: on batch entries.

Actions:
  --add "name"              create a task (--estimate 2h, --status done)
  --done name-or-id         mark a task as done
  --delete name-or-id       delete a task (asks for confirmation; --yes to skip)

Examples:
  clockify tasks -p "Website Redesign"            # list the project's tasks
  clockify tasks -p p1 -t "Bug"                   # server-side name filter
  clockify tasks -p p1 --active                   # active only
  clockify tasks -p p1 --add "Fix login" --estimate 2h
  clockify tasks -p p1 --done "Fix login"         # mark done
  clockify tasks -p p1 --delete "Fix login" --yes # delete, no prompt`,
	Args: cobra.NoArgs,
	RunE: runTasks,
}

func init() {
	tasksCmd.Flags().StringVarP(&taskFlags.project, "project", "p", "", "project name or ID (omit to pick interactively)")
	tasksCmd.Flags().StringVarP(&taskFlags.name, "task", "t", "", "server-side filter by task name (substring)")
	tasksCmd.Flags().BoolVar(&taskFlags.exact, "exact", false, "make --task an exact-name match (needs --task)")
	tasksCmd.Flags().BoolVar(&taskFlags.active, "active", false, "show active tasks only")
	tasksCmd.Flags().IntVarP(&taskFlags.count, "count", "n", 50, "max tasks to show")
	tasksCmd.Flags().StringVar(&taskFlags.add, "add", "", "create a task with the given name")
	tasksCmd.Flags().StringVar(&taskFlags.estimate, "estimate", "", `with --add: time estimate (e.g. "2h", "45m")`)
	tasksCmd.Flags().StringVar(&taskFlags.status, "status", "", `with --add: initial status (active or done)`)
	tasksCmd.Flags().StringVar(&taskFlags.done, "done", "", "mark the task with the given name or ID as done")
	tasksCmd.Flags().StringVar(&taskFlags.delete, "delete", "", "delete the task with the given name or ID")
	tasksCmd.Flags().BoolVarP(&taskFlags.yes, "yes", "y", false, "skip the delete confirmation prompt")
}

func runTasks(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	workspaceID, err := requireWorkspaceID()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	// Tasks are scoped to a project: resolve --project (or pick one).
	projectID, err := resolveProjectForTasks(ctx, client, workspaceID)
	if err != nil {
		return err
	}

	addName := strings.TrimSpace(taskFlags.add)
	delRef := strings.TrimSpace(taskFlags.delete)
	doneRef := strings.TrimSpace(taskFlags.done)
	if addName != "" && delRef != "" {
		return errors.New("--add and --delete cannot be combined")
	}
	if doneRef != "" && (addName != "" || delRef != "") {
		return errors.New("--done cannot be combined with --add or --delete")
	}
	if cmd.Flags().Changed("estimate") && addName == "" {
		return errors.New("--estimate only applies to --add")
	}
	if cmd.Flags().Changed("status") && addName == "" {
		return errors.New("--status only applies to --add")
	}
	switch {
	case addName != "":
		return runAddTask(ctx, client, workspaceID, projectID, addName)
	case doneRef != "":
		return runDoneTask(ctx, client, workspaceID, projectID, doneRef)
	case delRef != "":
		return runDeleteTask(ctx, client, workspaceID, projectID, delRef, taskFlags.yes)
	}

	if taskFlags.exact && strings.TrimSpace(taskFlags.name) == "" {
		return errors.New("--exact needs --task")
	}

	count := taskFlags.count
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	var params clockify.TaskListParams
	if name := strings.TrimSpace(taskFlags.name); name != "" {
		params.Name = name
		params.StrictNameSearch = taskFlags.exact
	}
	if taskFlags.active {
		// is-active=true is the server's active-only filter; --done is
		// filtered client-side (the semantics of is-active=false are
		// not documented, so don't rely on them).
		params.IsActive = clockify.Ptr(true)
	}
	params.PageSize = count

	var tasks []clockify.Task
	if err := runWithSpinner("Fetching tasks…", func() error {
		var err error
		tasks, err = client.GetTasks(ctx, workspaceID, projectID, params)
		return err
	}); err != nil {
		return err
	}

	if len(tasks) > count {
		tasks = tasks[:count]
	}

	if len(tasks) == 0 {
		printWarn("No tasks found")
		return nil
	}

	rows := make([][]string, 0, len(tasks))
	for i := range tasks {
		t := &tasks[i]
		status := "active"
		if t.Status == clockify.TaskStatusDone {
			status = mutedStyle.Render("done")
		}
		estimate := formatISODuration(t.Estimate)
		if estimate == "" {
			estimate = "—"
		}
		rows = append(rows, []string{
			truncate(t.Name, 32),
			status,
			estimate,
			truncate(t.ID, 16),
		})
	}

	noun := "tasks"
	if len(rows) == 1 {
		noun = "task"
	}
	fmt.Println(accentStyle.Render(fmt.Sprintf("%d %s", len(rows), noun)))
	fmt.Println()
	fmt.Println(renderTable([]string{"NAME", "STATUS", "ESTIMATE", "ID"}, rows))
	return nil
}

// resolveProjectForTasks resolves --project to an ID, falling back to an
// interactive picker when the flag is omitted in a terminal.
func resolveProjectForTasks(ctx context.Context, client *clockify.Client, workspaceID string) (string, error) {
	ref := strings.TrimSpace(taskFlags.project)
	if ref != "" {
		return resolveProject(ctx, client, workspaceID, ref)
	}
	if !isInteractive() {
		return "", errors.New("--project is required outside a terminal (e.g. -p \"Website Redesign\")")
	}
	return pickProjectForTasks(ctx, client, workspaceID)
}

// pickProjectForTasks opens a project picker (cached active projects).
func pickProjectForTasks(ctx context.Context, client *clockify.Client, workspaceID string) (string, error) {
	type choice struct{ id string }
	var selected choice
	if err := runWithSpinner("Fetching projects…", func() error {
		projects, err := getCachedProjects(ctx, client, viper.GetString("api_key"), workspaceID)
		if err != nil {
			return fmt.Errorf("load projects: %w", err)
		}
		if len(projects) == 0 {
			return errors.New("no active projects — create a project first")
		}
		options := make([]huh.Option[choice], 0, len(projects))
		for _, p := range projects {
			options = append(options, huh.NewOption(p.Name, choice{id: p.ID}))
		}
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[choice]().
				Title("Project").
				Description("Which project's tasks?").
				Options(options...).
				Value(&selected),
		))
		return form.Run()
	}); err != nil {
		return "", fmt.Errorf("cancelled: %w", err)
	}
	return selected.id, nil
}

// runAddTask creates a task on the project.
func runAddTask(ctx context.Context, client *clockify.Client, workspaceID, projectID, name string) error {
	req := clockify.CreateTaskRequest{Name: name}

	if est := strings.TrimSpace(taskFlags.estimate); est != "" {
		d, err := parseDuration(est)
		if err != nil {
			return fmt.Errorf("estimate: %w", err)
		}
		req.Estimate = toISODuration(d)
	}
	if status := strings.TrimSpace(taskFlags.status); status != "" {
		switch strings.ToLower(status) {
		case "active":
			req.Status = clockify.TaskStatusActive
		case "done":
			req.Status = clockify.TaskStatusDone
		default:
			return errors.New(`--status must be "active" or "done"`)
		}
	}

	var task *clockify.Task
	if err := runWithSpinner("Creating task…", func() error {
		var err error
		task, err = client.CreateTask(ctx, workspaceID, projectID, req)
		return err
	}); err != nil {
		return err
	}
	printSuccess("Created task: %s", task.Name)
	fmt.Println(mutedStyle.Render("  " + task.ID))
	return nil
}

// runDeleteTask deletes a task by name or ID after showing which task will
// be removed — mirroring the time-entry delete flow.
func runDeleteTask(ctx context.Context, client *clockify.Client, workspaceID, projectID, ref string, confirmed bool) error {
	var target *clockify.Task
	if err := runWithSpinner("Finding task…", func() error {
		var err error
		target, err = resolveTaskObject(ctx, client, workspaceID, projectID, ref)
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
				Title(fmt.Sprintf("Delete task %q?", target.Name)).
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

	if err := runWithSpinner("Deleting task…", func() error {
		return client.DeleteTask(ctx, workspaceID, projectID, target.ID)
	}); err != nil {
		return err
	}
	printSuccess("Deleted task: %s", target.Name)
	return nil
}

// runDoneTask marks a task as done (PUT requires the current name, so the
// task is resolved first).
func runDoneTask(ctx context.Context, client *clockify.Client, workspaceID, projectID, ref string) error {
	var task *clockify.Task
	if err := runWithSpinner("Finding task…", func() error {
		var err error
		task, err = resolveTaskObject(ctx, client, workspaceID, projectID, ref)
		return err
	}); err != nil {
		return err
	}
	if task.Status == clockify.TaskStatusDone {
		fmt.Println(mutedStyle.Render(fmt.Sprintf("Task is already done: %s", task.Name)))
		return nil
	}

	if err := runWithSpinner("Marking done…", func() error {
		_, err := client.UpdateTask(ctx, workspaceID, projectID, task.ID, clockify.UpdateTaskRequest{
			Name:   task.Name,
			Status: clockify.TaskStatusDone,
		})
		return err
	}); err != nil {
		return err
	}
	printSuccess("Task done: %s", task.Name)
	return nil
}
