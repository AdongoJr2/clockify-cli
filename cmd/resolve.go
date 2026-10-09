package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

// tagAmbiguousError describes competing substring matches for ref.
func tagAmbiguousError(ref string, matches []clockify.Tag) error {
	names := make([]string, 0, len(matches))
	for _, t := range matches {
		names = append(names, t.Name)
	}
	return fmt.Errorf("tag %q is ambiguous — matches: %s", ref, strings.Join(names, ", "))
}

// matchTags is matchTag's ambiguity-aware form: it returns either the
// single match or an error when several tags contain ref.
func matchTags(tags []clockify.Tag, ref string) (*clockify.Tag, error) {
	var exact *clockify.Tag
	var partial []clockify.Tag
	for i := range tags {
		t := &tags[i]
		if strings.EqualFold(t.Name, ref) {
			exact = t
			break
		}
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(ref)) {
			partial = append(partial, *t)
		}
	}
	switch {
	case exact != nil:
		return exact, nil
	case len(partial) == 1:
		return &partial[0], nil
	case len(partial) > 1:
		return nil, tagAmbiguousError(ref, partial)
	default:
		return nil, nil
	}
}

// resolveTagObject resolves a tag name or ID to the full tag. Raw IDs are
// verified with GET so the user sees the real name (and a 404) before any
// destructive action.
func resolveTagObject(ctx context.Context, client *clockify.Client, workspaceID, ref string) (*clockify.Tag, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("tag name or ID is required")
	}
	if looksLikeID(ref) {
		return client.GetTag(ctx, workspaceID, ref)
	}

	tags, err := client.GetTags(ctx, workspaceID, clockify.TagListParams{})
	if err != nil {
		return nil, err
	}
	tag, err := matchTags(tags, ref)
	if err != nil {
		return nil, err
	}
	if tag == nil {
		return nil, fmt.Errorf("no tag matches %q (run `clockify tags` to list them)", ref)
	}
	return tag, nil
}

// matchTagRef resolves one tag ref against a pre-fetched tag list to an ID:
// exact/unique-substring name match first, then raw-ID passthrough.
func matchTagRef(tags []clockify.Tag, ref string) (string, error) {
	tag, err := matchTags(tags, ref)
	if err != nil {
		return "", err
	}
	if tag != nil {
		return tag.ID, nil
	}
	if looksLikeID(ref) {
		return ref, nil // raw ID of an unknown or archived tag
	}
	return "", fmt.Errorf("no tag matches %q (run `clockify tags` to list them)", ref)
}

// matchTaskRef is matchTagRef for project tasks.
func matchTaskRef(tasks []clockify.Task, ref string) (string, error) {
	task, err := matchTasks(tasks, ref)
	if err != nil {
		return "", err
	}
	if task != nil {
		return task.ID, nil
	}
	if looksLikeID(ref) {
		return ref, nil // raw ID of an unknown task
	}
	return "", fmt.Errorf("no task matches %q (run `clockify tasks -p <project>` to list them)", ref)
}

// resolveTagRefs maps tag names or IDs to IDs, preserving order and
// dropping duplicates. Empty entries are skipped (an explicit empty value
// clears tags in `clockify update`). The tag list is fetched once.
func resolveTagRefs(ctx context.Context, client *clockify.Client, workspaceID string, refs []string) ([]string, error) {
	trimmed := make([]string, 0, len(refs))
	for _, r := range refs {
		if s := strings.TrimSpace(r); s != "" {
			trimmed = append(trimmed, s)
		}
	}
	if len(trimmed) == 0 {
		return nil, nil
	}

	tags, err := client.GetTags(ctx, workspaceID, clockify.TagListParams{})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(trimmed))
	seen := map[string]bool{}
	for _, ref := range trimmed {
		id, err := matchTagRef(tags, ref)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// resolveTaskObject resolves a task name or ID within a project to the full
// task. Raw IDs are verified with GET (same rationale as resolveTagObject).
func resolveTaskObject(ctx context.Context, client *clockify.Client, workspaceID, projectID, ref string) (*clockify.Task, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("task name or ID is required")
	}
	if looksLikeID(ref) {
		return client.GetTask(ctx, workspaceID, projectID, ref)
	}

	tasks, err := client.GetTasks(ctx, workspaceID, projectID, clockify.TaskListParams{})
	if err != nil {
		return nil, err
	}
	task, err := matchTasks(tasks, ref)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("no task matches %q (run `clockify tasks -p <project>` to list them)", ref)
	}
	return task, nil
}

// matchTasks picks the task matching ref with resolveProject's semantics:
// exact name first, then a unique substring; multiple matches are an error.
func matchTasks(tasks []clockify.Task, ref string) (*clockify.Task, error) {
	var exact *clockify.Task
	var partial []clockify.Task
	for i := range tasks {
		t := &tasks[i]
		if strings.EqualFold(t.Name, ref) {
			exact = t
			break
		}
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(ref)) {
			partial = append(partial, *t)
		}
	}
	switch {
	case exact != nil:
		return exact, nil
	case len(partial) == 1:
		return &partial[0], nil
	case len(partial) > 1:
		names := make([]string, 0, len(partial))
		for _, t := range partial {
			names = append(names, t.Name)
		}
		return nil, fmt.Errorf("task %q is ambiguous — matches: %s", ref, strings.Join(names, ", "))
	default:
		return nil, nil
	}
}

// resolveTaskRef resolves a task name or ID within a project to an ID.
func resolveTaskRef(ctx context.Context, client *clockify.Client, workspaceID, projectID, ref string) (string, error) {
	task, err := resolveTaskObject(ctx, client, workspaceID, projectID, ref)
	if err != nil {
		return "", err
	}
	return task.ID, nil
}
