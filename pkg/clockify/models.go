// Package clockify provides a thin, fully-typed HTTP client for the
// Clockify REST API (https://docs.clockify.me).
package clockify

import "time"

// User is the authenticated account returned by GET /v1/user.
type User struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	ActiveWorkspace  string `json:"activeWorkspace"`
	DefaultWorkspace string `json:"defaultWorkspace"`
}

// Workspace is a Clockify workspace returned by GET /v1/workspaces.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Project is a workspace project returned by GET /v1/workspaces/{id}/projects.
type Project struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Billable bool   `json:"billable"`
	Color    string `json:"color"`
}

// ProjectBrief is the hydrated project snapshot embedded in time entries
// when a list request sets hydrated=true.
type ProjectBrief struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ClientID   string `json:"clientId"`
	ClientName string `json:"clientName"`
	Color      string `json:"color"`
}

// TaskBrief is a hydrated task embedded in a time entry.
type TaskBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TagBrief is a hydrated tag embedded in a time entry.
type TagBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TimeInterval holds the start/end/duration of a time entry.
// End is nil while a timer is still running.
type TimeInterval struct {
	Start    *time.Time `json:"start"`
	End      *time.Time `json:"end"`
	Duration string     `json:"duration"` // ISO-8601, e.g. "PT1H30M"
}

// TimeEntry is a single tracked entry. One struct covers both plain and
// hydrated responses: hydrated-only fields (Project, Task, Tags) are simply
// absent otherwise, and TimeEntryInfoDto omits the flat ProjectID/TagIDs.
type TimeEntry struct {
	ID           string        `json:"id"`
	Description  string        `json:"description"`
	Billable     bool          `json:"billable"`
	ProjectID    string        `json:"projectId"`
	TaskID       string        `json:"taskId"`
	TagIDs       []string      `json:"tagIds"`
	UserID       string        `json:"userId"`
	WorkspaceID  string        `json:"workspaceId"`
	Type         string        `json:"type"`
	IsLocked     bool          `json:"isLocked"`
	TimeInterval *TimeInterval `json:"timeInterval"`

	// Populated when the request sets hydrated=true.
	Project *ProjectBrief `json:"project"`
	Task    *TaskBrief    `json:"task"`
	Tags    []TagBrief    `json:"tags"`
}

// EffectiveProjectID returns the project ID from either the flat or the
// hydrated representation.
func (t *TimeEntry) EffectiveProjectID() string {
	if t.ProjectID != "" {
		return t.ProjectID
	}
	if t.Project != nil {
		return t.Project.ID
	}
	return ""
}

// IsRunning reports whether the entry has no end time yet.
func (t *TimeEntry) IsRunning() bool {
	return t.TimeInterval != nil && t.TimeInterval.Start != nil && t.TimeInterval.End == nil
}

// CreateTimeEntryRequest is the body for
// POST /v1/workspaces/{workspaceId}/time-entries.
// Omit End to start a running timer.
type CreateTimeEntryRequest struct {
	Description string     `json:"description,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	ProjectID   string     `json:"projectId,omitempty"`
	Billable    *bool      `json:"billable,omitempty"`
	TaskID      string     `json:"taskId,omitempty"`
	TagIDs      []string   `json:"tagIds,omitempty"`
}

// UpdateTimeEntryRequest is the body for
// PUT /v1/workspaces/{workspaceId}/time-entries/{id}.
// Start is required by the API; omit optional fields to leave them unchanged.
// TaskID and TagIDs are pointers so an explicit empty value can clear them
// (an empty string/slice is otherwise indistinguishable from "omit").
type UpdateTimeEntryRequest struct {
	Start       *time.Time `json:"start"`
	Description string     `json:"description,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	ProjectID   string     `json:"projectId,omitempty"`
	Billable    *bool      `json:"billable,omitempty"`
	TaskID      *string    `json:"taskId,omitempty"`
	TagIDs      *[]string  `json:"tagIds,omitempty"`
}

// StopTimeEntryRequest is the body for
// PATCH /v1/workspaces/{workspaceId}/user/{userId}/time-entries
// (stop the running timer). End is the only required field.
type StopTimeEntryRequest struct {
	End time.Time `json:"end"`
}

// Tag is a workspace tag returned by GET /v1/workspaces/{workspaceId}/tags.
type Tag struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Archived    bool   `json:"archived"`
	WorkspaceID string `json:"workspaceId"`
}

// CreateTagRequest is the body for POST /v1/workspaces/{workspaceId}/tags.
type CreateTagRequest struct {
	Name string `json:"name"`
}

// UpdateTagRequest is the body for PUT /v1/workspaces/{workspaceId}/tags/{id}.
// Both fields are optional; omit one to leave it unchanged.
type UpdateTagRequest struct {
	Name     *string `json:"name,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

// TagListParams are query options for GetTags.
type TagListParams struct {
	Name             string // server-side substring filter; "" = no filter
	StrictNameSearch bool   // make Name an exact-match filter
	Archived         *bool  // nil = both; true = archived only
	PageSize         int    // per page; default 100
}

// TaskStatus is the lifecycle state of a project task.
type TaskStatus string

// Task status values accepted by the API.
const (
	TaskStatusActive TaskStatus = "ACTIVE"
	TaskStatusDone   TaskStatus = "DONE"
)

// Task is a project task returned by
// GET /v1/workspaces/{workspaceId}/projects/{projectId}/tasks.
type Task struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	ProjectID string     `json:"projectId"`
	Status    TaskStatus `json:"status"`
	Estimate  string     `json:"estimate,omitempty"` // ISO-8601 duration, e.g. "PT1H30M"
	Billable  bool       `json:"billable"`
	Duration  string     `json:"duration,omitempty"` // tracked time so far, e.g. "PT1H30M"
}

// CreateTaskRequest is the body for
// POST /v1/workspaces/{workspaceId}/projects/{projectId}/tasks.
// Name is required by the API.
type CreateTaskRequest struct {
	Name     string     `json:"name"`
	Status   TaskStatus `json:"status,omitempty"`
	Estimate string     `json:"estimate,omitempty"` // ISO-8601, e.g. "PT1H30M"
}

// UpdateTaskRequest is the body for
// PUT /v1/workspaces/{workspaceId}/projects/{projectId}/tasks/{id}.
// Name is required by the API.
type UpdateTaskRequest struct {
	Name     string     `json:"name"`
	Status   TaskStatus `json:"status,omitempty"`
	Estimate string     `json:"estimate,omitempty"`
	Billable *bool      `json:"billable,omitempty"`
}

// TaskListParams are query options for GetTasks.
type TaskListParams struct {
	Name             string // server-side substring filter; "" = no filter
	StrictNameSearch bool   // make Name an exact-match filter
	IsActive         *bool  // nil = both; true = active only
	PageSize         int    // per page; default 100
}

// TimeEntryListParams are query options for GetTimeEntries.
type TimeEntryListParams struct {
	Page     int        // 1-indexed; omitted from the query when <= 0
	PageSize int        // omitted from the query when <= 0
	Hydrated bool       // embed project/task/tag objects instead of bare IDs
	Start    *time.Time // only entries starting at/after this time (UTC on the wire)
	End      *time.Time // only entries starting at/before this time (UTC on the wire)
}

// ProjectListParams are query options for GetProjects.
type ProjectListParams struct {
	Name string // server-side substring filter on the project name; "" = no filter
	// StrictNameSearch makes Name an exact-match filter (ignored without Name).
	StrictNameSearch bool
	// Archived, when set to true, returns only archived projects. There is no
	// server-side "active only" filter: leave it nil (both are returned) and
	// drop archived entries client-side if you need active ones.
	Archived *bool
	PageSize int // per page; default 100
}
