// Package clockify provides a thin, fully-typed HTTP client for the
// Clockify REST API (https://docs.clockify.me).
package clockify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the global Clockify API endpoint. Regional workspaces can
// use a prefixed host instead (e.g. https://euc1.clockify.me/api/v1).
const DefaultBaseURL = "https://api.clockify.me/api/v1"

// maxResponseBody caps how much of a response is buffered (4 MiB).
const maxResponseBody = 4 << 20

// Client talks to the Clockify REST API using an API key (X-Api-Key header).
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient returns a Client for baseURL (or DefaultBaseURL when empty)
// authenticated with apiKey.
func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// APIError is returned for any non-2xx response.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("clockify API error: %s %s -> %d: %s", e.Method, e.Path, e.StatusCode, msg)
}

// IsUnauthorized reports whether err is a 401 (bad or expired API key).
func IsUnauthorized(err error) bool { return statusOf(err) == http.StatusUnauthorized }

// IsForbidden reports whether err is a 403 (missing workspace permission).
func IsForbidden(err error) bool { return statusOf(err) == http.StatusForbidden }

// IsNotFound reports whether err is a 404.
func IsNotFound(err error) bool { return statusOf(err) == http.StatusNotFound }

// IsRateLimited reports whether err is a 429 (too many requests).
func IsRateLimited(err error) bool { return statusOf(err) == http.StatusTooManyRequests }

func statusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// Ptr returns a pointer to v; handy for optional JSON request fields.
func Ptr[T any](v T) *T { return &v }

// do performs an HTTP request and, when out is non-nil, decodes the JSON
// response body into it. Non-2xx responses become *APIError values.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("clockify: encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("clockify: build %s %s: %w", method, path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("clockify: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("clockify: read %s %s: %w", method, path, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &APIError{
			StatusCode: resp.StatusCode,
			Method:     method,
			Path:       path,
			Message:    parseErrorMessage(data),
		}
	}

	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("clockify: decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

// GetUser returns the authenticated user (GET /user).
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	var user User
	if err := c.do(ctx, http.MethodGet, "/user", nil, nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetWorkspaces lists the workspaces the authenticated user belongs to.
func (c *Client) GetWorkspaces(ctx context.Context) ([]Workspace, error) {
	var workspaces []Workspace
	if err := c.do(ctx, http.MethodGet, "/workspaces", nil, nil, &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

// GetProjects lists active projects on a workspace, following pagination
// (100 per page, up to 50 pages as a safety bound).
func (c *Client) GetProjects(ctx context.Context, workspaceID string) ([]Project, error) {
	const pageSize = 100
	var all []Project
	for page := 1; page <= 50; page++ {
		query := url.Values{
			"page":      {strconv.Itoa(page)},
			"page-size": {strconv.Itoa(pageSize)},
		}
		var batch []Project
		path := "/workspaces/" + url.PathEscape(workspaceID) + "/projects"
		if err := c.do(ctx, http.MethodGet, path, query, nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			break
		}
	}
	return all, nil
}

// GetTimeEntries lists a user's time entries (newest first).
func (c *Client) GetTimeEntries(ctx context.Context, workspaceID, userID string, params TimeEntryListParams) ([]TimeEntry, error) {
	query := url.Values{}
	if params.Page > 0 {
		query.Set("page", strconv.Itoa(params.Page))
	}
	if params.PageSize > 0 {
		query.Set("page-size", strconv.Itoa(params.PageSize))
	}
	if params.Hydrated {
		query.Set("hydrated", "true")
	}

	path := fmt.Sprintf("/workspaces/%s/user/%s/time-entries", url.PathEscape(workspaceID), url.PathEscape(userID))
	var entries []TimeEntry
	if err := c.do(ctx, http.MethodGet, path, query, nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// GetInProgress returns the currently running timers on a workspace.
func (c *Client) GetInProgress(ctx context.Context, workspaceID string) ([]TimeEntry, error) {
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/time-entries/status/in-progress"
	var entries []TimeEntry
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// CreateTimeEntry creates a time entry. Omit End to start a running timer
// (Clockify then stops any previously running timer). Timestamps are
// normalized to UTC before sending.
func (c *Client) CreateTimeEntry(ctx context.Context, workspaceID string, req CreateTimeEntryRequest) (*TimeEntry, error) {
	if req.Start != nil {
		req.Start = Ptr(req.Start.UTC())
	}
	if req.End != nil {
		req.End = Ptr(req.End.UTC())
	}
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/time-entries"
	var entry TimeEntry
	if err := c.do(ctx, http.MethodPost, path, nil, req, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// StopTimer stops the user's running timer. The PATCH endpoint requires only
// an end timestamp in the body (the path carries workspace and user IDs).
func (c *Client) StopTimer(ctx context.Context, workspaceID, userID string, end time.Time) (*TimeEntry, error) {
	path := fmt.Sprintf("/workspaces/%s/user/%s/time-entries", url.PathEscape(workspaceID), url.PathEscape(userID))
	req := StopTimeEntryRequest{End: end.UTC()}
	var entry TimeEntry
	if err := c.do(ctx, http.MethodPatch, path, nil, req, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// UpdateTimeEntry edits an existing entry (PUT — note PATCH is reserved for
// stopping a running timer). Start is required by the API. Timestamps are
// normalized to UTC before sending.
func (c *Client) UpdateTimeEntry(ctx context.Context, workspaceID, entryID string, req UpdateTimeEntryRequest) (*TimeEntry, error) {
	if req.Start != nil {
		req.Start = Ptr(req.Start.UTC())
	}
	if req.End != nil {
		req.End = Ptr(req.End.UTC())
	}
	path := fmt.Sprintf("/workspaces/%s/time-entries/%s", url.PathEscape(workspaceID), url.PathEscape(entryID))
	var entry TimeEntry
	if err := c.do(ctx, http.MethodPut, path, nil, req, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// parseErrorMessage extracts Clockify's {"message": "..."} envelope,
// falling back to {"errors": {...}} and finally to the raw body.
func parseErrorMessage(data []byte) string {
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && strings.TrimSpace(envelope.Message) != "" {
		return envelope.Message
	}

	var fieldErrors struct {
		Errors map[string][]string `json:"errors"`
	}
	if err := json.Unmarshal(data, &fieldErrors); err == nil {
		var parts []string
		for _, list := range fieldErrors.Errors {
			parts = append(parts, list...)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}

	msg := strings.TrimSpace(string(data))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}
