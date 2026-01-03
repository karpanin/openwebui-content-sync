package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v56/github"
	"github.com/openwebui-content-sync/internal/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// GitHubAdapter implements the Adapter interface for GitHub repositories
type GitHubAdapter struct {
	client       *github.Client
	config       config.GitHubConfig
	lastSync     time.Time
	repositories []string
	mappings     map[string]string // repository -> knowledge_id mapping
	lastCommits  map[string]string // repository -> last commit hash
	storagePath  string            // path to storage directory
}

// GitHubState represents the persisted state of the GitHub adapter
type GitHubState struct {
	LastCommits map[string]string `json:"last_commits"`
	LastSync    time.Time         `json:"last_sync"`
}

// NewGitHubAdapter creates a new GitHub adapter
func NewGitHubAdapter(cfg config.GitHubConfig, storagePath string) (*GitHubAdapter, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("GitHub token is required")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.Token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	// Build repository mappings
	mappings := make(map[string]string)
	repos := []string{}

	// Process mappings
	for _, mapping := range cfg.Mappings {
		if mapping.Repository != "" && mapping.KnowledgeID != "" {
			mappings[mapping.Repository] = mapping.KnowledgeID
			repos = append(repos, mapping.Repository)
		}
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("at least one repository mapping must be configured")
	}

	adapter := &GitHubAdapter{
		client:       client,
		config:       cfg,
		repositories: repos,
		mappings:     mappings,
		lastSync:     time.Now().Add(-24 * time.Hour), // Default to 24 hours ago
		lastCommits:  make(map[string]string),
		storagePath:  storagePath,
	}

	// Load state from disk
	if err := adapter.loadState(); err != nil {
		logrus.Warnf("Failed to load GitHub adapter state: %v", err)
	}

	return adapter, nil
}

// Name returns the adapter name
func (g *GitHubAdapter) Name() string {
	return "github"
}

// FetchFiles retrieves files from GitHub repositories
func (g *GitHubAdapter) FetchFiles(ctx context.Context) ([]*File, error) {
	var files []*File

	for _, repo := range g.repositories {
		logrus.Debugf("Fetching files from repository: %s", repo)
		knowledgeID := g.mappings[repo]
		repoFiles, err := g.fetchRepositoryFiles(ctx, repo, knowledgeID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch files from repository %s: %w", repo, err)
		}
		logrus.Debugf("Found %d files in repository %s (knowledge_id: %s)", len(repoFiles), repo, knowledgeID)
		files = append(files, repoFiles...)
	}

	logrus.Debugf("Total files fetched: %d", len(files))
	return files, nil
}

// fetchRepositoryFiles fetches files from a specific repository
func (g *GitHubAdapter) fetchRepositoryFiles(ctx context.Context, repo string, knowledgeID string) ([]*File, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository format, expected 'owner/repo'")
	}

	owner, repoName := parts[0], parts[1]

	// Check for updates via latest commit
	latestCommit, err := g.getLatestCommit(ctx, owner, repoName)
	if err != nil {
		logrus.Warnf("Failed to get latest commit for %s: %v. Proceeding with full fetch.", repo, err)
	} else {
		// If we have a stored commit and it matches the latest, skip fetch
		if lastCommit, ok := g.lastCommits[repo]; ok && lastCommit == latestCommit {
			logrus.Debugf("Repository %s is up to date (commit: %s). Skipping fetch.", repo, latestCommit)
			return []*File{}, nil
		}
		logrus.Debugf("Repository %s has updates (old: %s, new: %s). Fetching files.", repo, g.lastCommits[repo], latestCommit)
	}

	// Get repository contents
	_, contents, _, err := g.client.Repositories.GetContents(ctx, owner, repoName, "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository contents: %w", err)
	}

	var files []*File
	for _, content := range contents {
		fileList, err := g.processContent(ctx, owner, repoName, content, "", knowledgeID)
		if err != nil {
			continue // Skip files that can't be processed
		}
		if fileList != nil {
			files = append(files, fileList...)
		}
	}

	// Update the last commit hash after successful fetch (will be persisted via SetLastSync)
	if latestCommit != "" {
		g.lastCommits[repo] = latestCommit
	}

	return files, nil
}

// loadState loads the adapter state from disk
func (g *GitHubAdapter) loadState() error {
	if g.storagePath == "" {
		return nil
	}

	statePath := filepath.Join(g.storagePath, "github_state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}

	var state GitHubState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	g.lastCommits = state.LastCommits
	if !state.LastSync.IsZero() {
		g.lastSync = state.LastSync
	}
	// Verify lastCommits map is not nil
	if g.lastCommits == nil {
		g.lastCommits = make(map[string]string)
	}

	logrus.Debugf("Loaded GitHub state: %d stored commits, last sync %v", len(g.lastCommits), g.lastSync)
	return nil
}

// saveState saves the adapter state to disk
func (g *GitHubAdapter) saveState() error {
	if g.storagePath == "" {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(g.storagePath, 0755); err != nil {
		return err
	}

	state := GitHubState{
		LastCommits: g.lastCommits,
		LastSync:    g.lastSync,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	statePath := filepath.Join(g.storagePath, "github_state.json")
	return os.WriteFile(statePath, data, 0644)
}

// getLatestCommit retrieves the SHA of the latest commit on the default branch
func (g *GitHubAdapter) getLatestCommit(ctx context.Context, owner, repo string) (string, error) {
	opts := &github.CommitsListOptions{
		ListOptions: github.ListOptions{PerPage: 1},
	}
	commits, _, err := g.client.Repositories.ListCommits(ctx, owner, repo, opts)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found")
	}
	return commits[0].GetSHA(), nil
}

// processContent processes a GitHub content item recursively
func (g *GitHubAdapter) processContent(ctx context.Context, owner, repo string, content *github.RepositoryContent, path string, knowledgeID string) ([]*File, error) {
	if content == nil {
		return nil, nil
	}

	currentPath := filepath.Join(path, content.GetName())

	// Skip binary files and non-text files
	if content.GetType() == "file" {
		// Check if it's a text file
		if !isTextFile(content.GetName()) {
			return nil, nil
		}

		// Get file content
		fileContent, err := g.getFileContent(ctx, owner, repo, content)
		if err != nil {
			return nil, fmt.Errorf("failed to get file content: %w", err)
		}

		// Calculate hash
		hash := fmt.Sprintf("%x", sha256.Sum256(fileContent))

		return []*File{{
			Path:        currentPath,
			Content:     fileContent,
			Hash:        hash,
			Modified:    time.Now(), // GitHub API doesn't provide modification time for content
			Size:        int64(len(fileContent)),
			Source:      fmt.Sprintf("%s/%s", owner, repo),
			KnowledgeID: knowledgeID,
		}}, nil
	}

	// If it's a directory, recurse
	if content.GetType() == "dir" {
		_, contents, _, err := g.client.Repositories.GetContents(ctx, owner, repo, content.GetPath(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get directory contents: %w", err)
		}

		var allFiles []*File
		for _, subContent := range contents {
			files, err := g.processContent(ctx, owner, repo, subContent, currentPath, knowledgeID)
			if err != nil {
				continue
			}
			if files != nil {
				allFiles = append(allFiles, files...)
			}
		}

		return allFiles, nil
	}

	return nil, nil
}

// getFileContent retrieves the actual content of a file
func (g *GitHubAdapter) getFileContent(ctx context.Context, owner, repo string, content *github.RepositoryContent) ([]byte, error) {
	fileContent, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get content: %w", err)
	}

	if fileContent != "" {
		// Content is already available (for small files)
		return []byte(fileContent), nil
	}

	// For larger files, we need to download them
	url := content.GetDownloadURL()
	if url == "" {
		return nil, fmt.Errorf("no download URL available for file")
	}

	resp, err := g.client.Client().Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// isTextFile checks if a file is likely to be a text file
func isTextFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))

	// Common text file extensions
	textExts := map[string]bool{
		".md":              true,
		".txt":             true,
		".json":            true,
		".yaml":            true,
		".yml":             true,
		".go":              true,
		".py":              true,
		".js":              true,
		".ts":              true,
		".java":            true,
		".cpp":             true,
		".c":               true,
		".h":               true,
		".hpp":             true,
		".cs":              true,
		".php":             true,
		".rb":              true,
		".rs":              true,
		".swift":           true,
		".kt":              true,
		".scala":           true,
		".sh":              true,
		".bash":            true,
		".zsh":             true,
		".fish":            true,
		".ps1":             true,
		".sql":             true,
		".xml":             true,
		".html":            true,
		".css":             true,
		".scss":            true,
		".sass":            true,
		".less":            true,
		".dockerfile":      true,
		".gitignore":       true,
		".gitattributes":   true,
		".editorconfig":    true,
		".env":             true,
		".env.example":     true,
		".env.local":       true,
		".env.production":  true,
		".env.development": true,
		".env.test":        true,
	}

	return textExts[ext] || ext == ""
}

// GetLastSync returns the last sync timestamp
func (g *GitHubAdapter) GetLastSync() time.Time {
	return g.lastSync
}

// SetLastSync updates the last sync timestamp and persists state to disk
// This is called by the sync manager after files are successfully synced to OpenWebUI
func (g *GitHubAdapter) SetLastSync(t time.Time) {
	g.lastSync = t
	if err := g.saveState(); err != nil {
		logrus.Warnf("Failed to save GitHub state: %v", err)
	}
}
