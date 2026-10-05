package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
)

// Cache TTLs keep the CLI within the Free plan's 30 requests/hour/workspace.
const (
	userCacheTTL    = 24 * time.Hour
	projectCacheTTL = 15 * time.Minute
)

// cacheFile is the on-disk cache (0600) under the OS cache directory.
type cacheFile struct {
	APIKeyHash string                    `json:"api_key_hash"`
	User       *cachedUser               `json:"user,omitempty"`
	Projects   map[string]cachedProjects `json:"projects,omitempty"`
}

type cachedUser struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	FetchedAt time.Time `json:"fetched_at"`
}

type cachedProjects struct {
	FetchedAt time.Time          `json:"fetched_at"`
	Items     []clockify.Project `json:"items"`
}

// cachePath returns $XDG_CACHE_HOME/clockify-cli/cache.json.
func cachePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "clockify-cli", "cache.json"), nil
}

func apiKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}

// loadCache reads the cache, returning an empty one when missing, corrupt,
// or written with a different API key (so key rotation invalidates it).
func loadCache(path, apiKey string) *cacheFile {
	cf := &cacheFile{Projects: map[string]cachedProjects{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return cf
	}
	if err := json.Unmarshal(data, cf); err != nil {
		return &cacheFile{Projects: map[string]cachedProjects{}}
	}
	if cf.APIKeyHash != apiKeyHash(apiKey) {
		return &cacheFile{Projects: map[string]cachedProjects{}}
	}
	if cf.Projects == nil {
		cf.Projects = map[string]cachedProjects{}
	}
	return cf
}

// saveCache persists the cache best-effort; failures are never fatal.
func saveCache(path string, cf *cacheFile) {
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// getCachedUser returns the authenticated user, using a 24h disk cache and
// falling back to GET /user.
func getCachedUser(ctx context.Context, client *clockify.Client, apiKey string) (*clockify.User, error) {
	path, err := cachePath()
	if err != nil {
		return client.GetUser(ctx)
	}
	cf := loadCache(path, apiKey)
	if cf.User != nil && time.Since(cf.User.FetchedAt) < userCacheTTL {
		return &clockify.User{ID: cf.User.ID, Name: cf.User.Name, Email: cf.User.Email}, nil
	}

	user, err := client.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	cf.User = &cachedUser{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		FetchedAt: time.Now(),
	}
	saveCache(path, cf)
	return user, nil
}

// getCachedProjects returns a workspace's active projects, using a 15m disk
// cache and falling back to GET /workspaces/{id}/projects.
func getCachedProjects(ctx context.Context, client *clockify.Client, apiKey, workspaceID string) ([]clockify.Project, error) {
	path, err := cachePath()
	if err != nil {
		return client.GetProjects(ctx, workspaceID)
	}
	cf := loadCache(path, apiKey)
	if entry, ok := cf.Projects[workspaceID]; ok && time.Since(entry.FetchedAt) < projectCacheTTL {
		return entry.Items, nil
	}

	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	cf.Projects[workspaceID] = cachedProjects{FetchedAt: time.Now(), Items: projects}
	saveCache(path, cf)
	return projects, nil
}
