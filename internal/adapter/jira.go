package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/openwebui-content-sync/internal/config"
	"github.com/sirupsen/logrus"
)

// JiraAdapter implements the Adapter interface for Jira projects
type JiraAdapter struct {
	client   *http.Client
	config   config.JiraConfig
	lastSync time.Time
	projects []string
	mappings map[string]string // project_key -> knowledge_id mapping
	storagePath string
}

// JiraState represents the persisted state of the Jira adapter
type JiraState struct {
	LastSync time.Time `json:"last_sync"`
}

// JiraIssue represents a Jira issue from the API
type JiraIssue struct {
	ID              string                  `json:"id"`
	Self            string                  `json:"self"`
	Key             string                  `json:"key"`
	Fields          JiraIssueFields         `json:"fields"`
	Changelog       JiraIssueChangelog      `json:"changelog,omitempty"`
	Transitions     []JiraIssueTransition   `json:"transitions,omitempty"`
	Operations      []JiraIssueOperation    `json:"operations,omitempty"`
	RenderedFields  JiraIssueRenderedFields `json:"renderedFields,omitempty"`
	FetchedComments []CommentData           `json:"fetchedComments,omitempty"`
}
type JiraIssueRenderedFields struct {
	Description string `json:"description"`
}

// JiraIssueFields represents the fields of a Jira issue
type JiraIssueFields struct {
	Project JiraProject `json:"project"`
	Summary string      `json:"summary"`
	// Description string           `json:"description"`
	Comment     JiraComments     `json:"comment,omitempty"`
	IssueType   JiraIssueType    `json:"issuetype,omitempty"`
	Reporter    JiraUser         `json:"reporter,omitempty"`
	Assignee    *JiraUser        `json:"assignee,omitempty"`
	Status      JiraStatus       `json:"status,omitempty"`
	Priority    JiraPriority     `json:"priority,omitempty"`
	Created     string           `json:"created,omitempty"`
	Updated     string           `json:"updated,omitempty"`
	Resolution  *JiraResolution  `json:"resolution,omitempty"`
	Labels      []string         `json:"labels,omitempty"`
	Components  []JiraComponent  `json:"components,omitempty"`
	Environment string           `json:"environment,omitempty"`
	Attachments []JiraAttachment `json:"attachment,omitempty"`
	Worklog     JiraWorklog      `json:"worklog,omitempty"`
	Parent      *JiraParent      `json:"parent,omitempty"`
}
type JiraComments struct {
	Comments []JiraComment `json:"comments,omitempty"`
}
type JiraComment struct {
	Self         string   `json:"self"`
	ID           string   `json:"id"`
	Author       JiraUser `json:"author"`
	Body         JiraBody `json:"body"`
	UpdateAuthor JiraUser `json:"updateAuthor"`
	Created      string   `json:"created"`
	Updated      string   `json:"updated"`
	JsdPublic    bool     `json:"jsdPublic"`
}
type JiraBody struct {
	Type    string      `json:"type"`
	Version int         `json:"version"`
	Content []JiraBlock `json:"content"`
}
type JiraBlock struct {
	Type    string     `json:"type"`
	Content []JiraText `json:"content"`
}
type JiraText struct {
	Type  string     `json:"type"`
	Text  string     `json:"text"`
	Marks []JiraMark `json:"marks,omitempty"`
}
type JiraMark struct {
	Type  string    `json:"type"`
	Attrs JiraAttrs `json:"attrs,omitempty"`
}
type JiraAttrs struct {
	Href string `json:"href"`
}

// JiraProject represents a Jira project
type JiraProject struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	ProjectType string `json:"projectTypeKey"`
}

// JiraIssueType represents the type of a Jira issue
type JiraIssueType struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Description string `json:"description"`
	IconURL     string `json:"iconUrl"`
	Name        string `json:"name"`
	Subtask     bool   `json:"subtask"`
}

// JiraUser represents a Jira user
type JiraUser struct {
	Self         string `json:"self"`
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType"`
	Name         string `json:"name"`
	Key          string `json:"key"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone"`
}

// JiraStatus represents the status of a Jira issue
type JiraStatus struct {
	Self             string `json:"self"`
	ID               string `json:"id"`
	Description      string `json:"description"`
	IconURL          string `json:"iconUrl"`
	Name             string `json:"name"`
	UntranslatedName string `json:"untranslatedName"`
	StatusCode       string `json:"statusCode"`
	Resolved         bool   `json:"resolved"`
}

// JiraPriority represents the priority of a Jira issue
type JiraPriority struct {
	Self    string `json:"self"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
}

// JiraResolution represents the resolution of a Jira issue
type JiraResolution struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// JiraComponent represents a component of a Jira issue
type JiraComponent struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// JiraAttachment represents an attachment of a Jira issue
type JiraAttachment struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Author    *JiraUser `json:"author"`
	Created   string    `json:"created"`
	Size      int       `json:"size"`
	MimeType  string    `json:"mimeType"`
	Content   string    `json:"content"`
	Thumbnail string    `json:"thumbnail"`
}

// JiraWorklog represents the worklog of a Jira issue
type JiraWorklog struct {
	MaxResults int                `json:"maxResults"`
	StartAt    int                `json:"startAt"`
	Total      int                `json:"total"`
	Worklogs   []JiraWorklogEntry `json:"worklogs"`
}

// JiraWorklogEntry represents a worklog entry
type JiraWorklogEntry struct {
	ID               string   `json:"id"`
	Self             string   `json:"self"`
	Author           JiraUser `json:"author"`
	Comment          string   `json:"comment"`
	Created          string   `json:"created"`
	Updated          string   `json:"updated"`
	Started          string   `json:"started"`
	TimeSpent        string   `json:"timeSpent"`
	TimeSpentSeconds int      `json:"timeSpentSeconds"`
}

// JiraParent represents a parent issue
type JiraParent struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
	Name string `json:"name"`
}

// JiraIssueChangelog represents the changelog of a Jira issue
type JiraIssueChangelog struct {
	MaxResults int                `json:"maxResults"`
	StartAt    int                `json:"startAt"`
	Total      int                `json:"total"`
	Histories  []JiraIssueHistory `json:"histories"`
}

// JiraIssueHistory represents a history entry in the changelog
type JiraIssueHistory struct {
	ID      string                 `json:"id"`
	Author  JiraUser               `json:"author"`
	Created string                 `json:"created"`
	Items   []JiraIssueHistoryItem `json:"items"`
}

// JiraIssueHistoryItem represents an item in a history entry
type JiraIssueHistoryItem struct {
	Field      string `json:"field"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

// JiraIssueTransition represents a transition in a Jira issue
type JiraIssueTransition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// JiraIssueOperation represents an operation on a Jira issue
type JiraIssueOperation struct {
	IconURL      string `json:"iconUrl"`
	Name         string `json:"name"`
	OperationKey string `json:"operationKey"`
}

// NewJiraAdapter creates a new Jira adapter
func NewJiraAdapter(cfg config.JiraConfig, storagePath string) (*JiraAdapter, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("jira base URL is required")
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("jira username is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("jira API key is required")
	}

	// Build project mappings
	mappings := make(map[string]string)
	projects := []string{}

	// Process mappings
	for _, mapping := range cfg.ProjectMappings {
		if mapping.ProjectKey != "" && mapping.KnowledgeID != "" {
			mappings[mapping.ProjectKey] = mapping.KnowledgeID
			projects = append(projects, mapping.ProjectKey)
		}
	}

	if len(projects) == 0 {
		return nil, fmt.Errorf("at least one jira project mapping must be configured")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	adapter := &JiraAdapter{
		client:      client,
		config:      cfg,
		projects:    projects,
		mappings:    mappings,
		lastSync:    time.Time{}, // Zero time initially
		storagePath: storagePath,
	}

	// Load state from disk
	if err := adapter.loadState(); err != nil {
		logrus.Warnf("Failed to load Jira adapter state: %v", err)
	}

	return adapter, nil
}

// Name returns the adapter name
func (j *JiraAdapter) Name() string {
	return "jira"
}

// getAPIBase returns the API base path based on configuration
func (j *JiraAdapter) getAPIBase() string {
	if j.config.Type == "datacenter" {
		return "/rest/api/2"
	}
	return "/rest/api/3"
}

// FetchFiles fetches all issues from the configured Jira projects
func (j *JiraAdapter) FetchFiles(ctx context.Context) ([]*File, error) {
	var allFiles []*File

	for _, projectKey := range j.projects {
		logrus.Debugf("Fetching files from Jira project: %s", projectKey)
		knowledgeID := j.mappings[projectKey]

		// Fetch all issues from the project
		issues, err := j.fetchIssues(ctx, projectKey)
		if err != nil {
			logrus.Errorf("Failed to fetch issues from Jira project %s: %v", projectKey, err)
			continue
		}

		logrus.Debugf("Found %d issues in Jira project %s", len(issues), projectKey)

		// Process each issue
		for _, issue := range issues {
			file, err := j.processIssue(ctx, issue, knowledgeID)
			if err != nil {
				logrus.Errorf("Failed to process issue %s: %v", issue.Key, err)
				continue
			}
			allFiles = append(allFiles, file)
		}
	}

	j.lastSync = time.Now()
	if err := j.saveState(); err != nil {
		logrus.Warnf("Failed to save Jira state: %v", err)
	}
	return allFiles, nil
}

// loadState loads the adapter state from disk
func (j *JiraAdapter) loadState() error {
	if j.storagePath == "" {
		return nil
	}

	statePath := filepath.Join(j.storagePath, "jira_state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}

	var state JiraState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	if !state.LastSync.IsZero() {
		j.lastSync = state.LastSync
	}

	logrus.Debugf("Loaded Jira state: last sync %v", j.lastSync)
	return nil
}

// saveState saves the adapter state to disk
func (j *JiraAdapter) saveState() error {
	if j.storagePath == "" {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(j.storagePath, 0755); err != nil {
		return err
	}

	state := JiraState{
		LastSync: j.lastSync,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	statePath := filepath.Join(j.storagePath, "jira_state.json")
	return os.WriteFile(statePath, data, 0644)
}

// fetchIssues fetches all issues from a Jira project using search endpoint and individual issue fetching
func (j *JiraAdapter) fetchIssues(ctx context.Context, projectKey string) ([]JiraIssue, error) {
	var allIssues []JiraIssue

	// Use search endpoint to get issue IDs first
	issueIDs, err := j.fetchAllIssueIDs(ctx, projectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue IDs for project %s: %w", projectKey, err)
	}

	// Then fetch complete details for each issue
	for _, issueID := range issueIDs {
		issue, err := j.fetchIssue(ctx, issueID)
		if err != nil {
			logrus.Errorf("Failed to fetch issue %s: %v", issueID, err)
			continue
		}
		allIssues = append(allIssues, issue)
	}

	return allIssues, nil
}

// fetchAllIssueIDs fetches all issue IDs for a project using the search endpoint with pagination
func (j *JiraAdapter) fetchAllIssueIDs(ctx context.Context, projectKey string) ([]string, error) {
	var issueIDs []string
	startAt := 0
	maxResults := 100
	nextPageToken := ""
	limit := j.config.PageLimit
	if limit <= 0 {
		limit = 100 // Default limit
	}
	if maxResults > limit {
		maxResults = limit
	}
	for {
		logrus.Debugf("Limit: %d, MaxResults: %d", limit, maxResults)
		// Build JQL query to fetch issues from the project
		jqlQuery := fmt.Sprintf("project = '%s'", projectKey)

		// Add incremental update filter if lastSync is set
		if !j.lastSync.IsZero() {
			// Jira expects "yyyy/MM/dd HH:mm" or "yyyy-MM-dd HH:mm"
			lastMod := j.lastSync.Format("2006-01-02 15:04")
			jqlQuery += fmt.Sprintf(" AND updated >= '%s'", lastMod)
			logrus.Debugf("Using incremental update for project %s since %s", projectKey, lastMod)
		}

		// Build URL for search endpoint with pagination - following the exact API specification
		url := fmt.Sprintf("%s%s/search/jql?jql=%s&maxResults=%d&fields=id%s",
			j.config.BaseURL, j.getAPIBase(), url.QueryEscape(jqlQuery), maxResults, nextPageToken)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set authentication
		req.SetBasicAuth(j.config.Username, j.config.APIKey)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Jira search API URL: %s", url)

		resp, err := j.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var response struct {
			Issues []struct {
				ID string `json:"id"`
			} `json:"issues"`

			IsLast        bool   `json:"isLast"`
			NextPageToken string `json:"nextPageToken,omitempty"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		// Extract issue IDs
		for _, issue := range response.Issues {
			issueIDs = append(issueIDs, issue.ID)
		}

		if response.IsLast || len(issueIDs) >= limit {
			break
		} else {
			nextPageToken = fmt.Sprintf(`&nextPageToken=%s`, response.NextPageToken)
		}
		// Check if there are more results
		startAt += maxResults
	}

	return issueIDs, nil
}

// fetchIssue fetches a single issue by ID using the issue endpoint
func (j *JiraAdapter) fetchIssue(ctx context.Context, issueID string) (JiraIssue, error) {
	var issue JiraIssue

	// Build URL for individual issue fetch
	// Build URL for individual issue fetch
	url := fmt.Sprintf("%s%s/issue/%s?expand=renderedFields&name&fields=summary,description,parent,issuetype,reporter,status,comment", j.config.BaseURL, j.getAPIBase(), issueID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return issue, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	req.SetBasicAuth(j.config.Username, j.config.APIKey)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Jira issue API URL: %s", url)

	resp, err := j.client.Do(req)
	if err != nil {
		return issue, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return issue, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		resp.Body.Close()
		return issue, fmt.Errorf("failed to decode response: %w", err)
	}
	resp.Body.Close()

	return issue, nil
}

// fetchProject fetches project details
func (j *JiraAdapter) fetchProject(ctx context.Context, projectKey string) (JiraProject, error) {
	var project JiraProject

	// Build URL for project fetch
	// Build URL for project fetch
	url := fmt.Sprintf("%s%s/project/%s", j.config.BaseURL, j.getAPIBase(), projectKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return project, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	req.SetBasicAuth(j.config.Username, j.config.APIKey)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Jira project API URL: %s", url)

	resp, err := j.client.Do(req)
	if err != nil {
		return project, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return project, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		resp.Body.Close()
		return project, fmt.Errorf("failed to decode response: %w", err)
	}
	resp.Body.Close()

	return project, nil
}

// HtmlToMarkdown converts HTML content to markdown
func (j *JiraAdapter) HtmlToMarkdown(htmlContent string) string {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithStrongDelimiter("__"),
				// ...additional configurations for the plugin
			),
			table.NewTablePlugin(),
			// ...additional plugins (e.g. table)
		),
	)
	markdown, err := conv.ConvertString(htmlContent, converter.WithDomain(j.config.BaseURL))
	if err != nil {
		logrus.Warnf("Failed to convert HTML to markdown: %v", err)
		return htmlContent
	}
	return markdown
}

// processIssue processes a single Jira issue and returns a File
func (j *JiraAdapter) processIssue(ctx context.Context, issue JiraIssue, knowledgeID string) (*File, error) {
	// Fetch comments for this issue
	comments, err := j.fetchCommentsForIssue(ctx, issue.ID)
	if err != nil {
		logrus.Warnf("Failed to fetch comments for issue %s: %v", issue.Key, err)
		// Continue processing without comments
	}

	// Add comments to the issue
	issue.FetchedComments = comments

	// Convert issue to JSON

	description := j.HtmlToMarkdown(issue.RenderedFields.Description)
	metaData := fmt.Sprintf("# Jira Issue\n---\n## Issue Metadata:\nTicket-ID: %s\nReporter: %s\nIssueType: %s\nStatus: %s\nResolved: %t\n---\n ", issue.Key, issue.Fields.Reporter.DisplayName, issue.Fields.IssueType.Name, issue.Fields.Status.Name, issue.Fields.Status.Resolved)

	// Format comments in markdown
	var commentsMarkdown string
	if len(comments) > 0 {
		commentsMarkdown = "\n## Comments\n"
		for _, comment := range comments {
			// Format the created timestamp to YYYY-MM-DD HH:MM
			formattedDate := comment.Created
			if len(comment.Created) >= 16 {
				// Extract date and time part (e.g., "2025-02-19T17:07:41.093+0100" -> "2025-02-19 17:07")
				formattedDate = fmt.Sprintf("%s %s", comment.Created[:10], comment.Created[11:16])
			}
			commentsMarkdown += fmt.Sprintf("%s (%s): %s\n\n", comment.AuthorName, formattedDate, comment.RenderedBody)
		}
	}

	// Generate content hash for change detection
	// issueJSON, err := json.MarshalIndent(issue, "", "  ")
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to marshal issue to JSON: %w", err)
	// }
	content := fmt.Sprintf("%s\n\n## %s\n%s%s\n\n\n", metaData, issue.Fields.Summary, description, commentsMarkdown)

	// // Create file content
	fileContent := []byte(content)

	// Create filename from issue key
	filename := fmt.Sprintf("%s.md", issue.Key)

	// Create file content
	// fileContent := issueJSON
	hash := sha256.Sum256(fileContent)
	contentHash := base64.StdEncoding.EncodeToString(hash[:])

	return &File{
		Path:        filename,
		Content:     fileContent,
		Hash:        contentHash,
		Modified:    time.Now(),
		Size:        int64(len(fileContent)),
		Source:      "jira",
		KnowledgeID: knowledgeID,
	}, nil
}

// GetLastSync returns the last sync time
func (j *JiraAdapter) GetLastSync() time.Time {
	return j.lastSync
}

// SetLastSync sets the last sync time
func (j *JiraAdapter) SetLastSync(t time.Time) {
	j.lastSync = t
}
