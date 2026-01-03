package adapter

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/openwebui-content-sync/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
)

// GitLabAdapter implements the Adapter interface for GitLab repositories
type GitLabAdapter struct {
	client       *gitlab.Client
	config       config.GitLabConfig
	lastSync     time.Time
	repositories []string
	mappings     map[string]string // repository -> knowledge_id mapping
	lastCommits  map[string]string // repository -> last commit hash
}

// NewGitLabAdapter creates a new GitLab adapter
func NewGitLabAdapter(cfg config.GitLabConfig) (*GitLabAdapter, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("GitLab token is required")
	}

	var client *gitlab.Client
	var err error

	if cfg.BaseURL != "" {
		// On-premise GitLab
		client, err = gitlab.NewClient(cfg.Token, gitlab.WithBaseURL(cfg.BaseURL))
	} else {
		// Cloud GitLab
		client, err = gitlab.NewClient(cfg.Token)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab client: %w", err)
	}

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

	return &GitLabAdapter{
		client:       client,
		config:       cfg,
		repositories: repos,
		mappings:     mappings,
		lastSync:     time.Now().Add(-24 * time.Hour), // Default to 24 hours ago
		lastCommits:  make(map[string]string),
	}, nil
}

// Name returns the adapter name
func (g *GitLabAdapter) Name() string {
	return "gitlab"
}

// FetchFiles retrieves files from GitLab repositories
func (g *GitLabAdapter) FetchFiles(ctx context.Context) ([]*File, error) {
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

	logrus.Debugf("Total files fetched from GitLab: %d", len(files))
	return files, nil
}

// fetchRepositoryFiles fetches files from a specific repository
func (g *GitLabAdapter) fetchRepositoryFiles(ctx context.Context, repo string, knowledgeID string) ([]*File, error) {
	// GitLab uses ID or URL-encoded path for project
	// repo is expected to be "group/project"

	// Check for updates via latest commit
	latestCommit, err := g.getLatestCommit(ctx, repo)
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

	files, err := g.processDirectory(ctx, repo, "", knowledgeID)
	if err != nil {
		return nil, err
	}

	// Update the last commit hash after successful fetch
	if latestCommit != "" {
		g.lastCommits[repo] = latestCommit
	}

	return files, nil
}

// getLatestCommit retrieves the SHA of the latest commit on the default branch
func (g *GitLabAdapter) getLatestCommit(ctx context.Context, projectID string) (string, error) {
	opts := &gitlab.ListCommitsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 1},
	}
	commits, _, err := g.client.Commits.ListCommits(projectID, opts)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found")
	}
	return commits[0].ID, nil
}

// processDirectory processes a directory in the repository recursively
func (g *GitLabAdapter) processDirectory(ctx context.Context, projectID, path string, knowledgeID string) ([]*File, error) {
	var allFiles []*File

	opts := &gitlab.ListTreeOptions{
		Path:      &path,
		Recursive: gitlab.Ptr(false),
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	for {
		nodes, resp, err := g.client.Repositories.ListTree(projectID, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list tree for project %s path %s: %w", projectID, path, err)
		}

		for _, node := range nodes {
			// Skip binary files and non-text files if it's a file
			if node.Type == "blob" {
				if !isGitLabTextFile(node.Name) {
					continue
				}

				// Get file content
				fileContent, err := g.getFileContent(ctx, projectID, node.Path)
				if err != nil {
					logrus.Warnf("Failed to get content for file %s: %v", node.Path, err)
					continue
				}

				// Calculate hash
				hash := fmt.Sprintf("%x", sha256.Sum256(fileContent))

				allFiles = append(allFiles, &File{
					Path:        node.Path,
					Content:     fileContent,
					Hash:        hash,
					Modified:    time.Now(), // GitLab ListTree doesn't provide modified time, would need Commit info
					Size:        int64(len(fileContent)),
					Source:      fmt.Sprintf("gitlab/%s", projectID),
					KnowledgeID: knowledgeID,
				})
			} else if node.Type == "tree" {
				// Recurse into directory
				subFiles, err := g.processDirectory(ctx, projectID, node.Path, knowledgeID)
				if err != nil {
					logrus.Warnf("Failed to process directory %s: %v", node.Path, err)
					continue
				}
				allFiles = append(allFiles, subFiles...)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// getFileContent retrieves the actual content of a file
func (g *GitLabAdapter) getFileContent(ctx context.Context, projectID, filePath string) ([]byte, error) {
	file, _, err := g.client.RepositoryFiles.GetRawFile(projectID, filePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get raw file content: %w", err)
	}

	return file, nil
}

// GetLastSync returns the last sync timestamp
func (g *GitLabAdapter) GetLastSync() time.Time {
	return g.lastSync
}

// SetLastSync updates the last sync timestamp
func (g *GitLabAdapter) SetLastSync(t time.Time) {
	g.lastSync = t
}

// isGitLabTextFile checks if a file is likely to be a text file
// Duplicated from github.go to avoid dependency
func isGitLabTextFile(filename string) bool {
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
