package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/openwebui-content-sync/internal/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

// ConfluenceAdapter implements the Adapter interface for Confluence spaces
type ConfluenceAdapter struct {
	client             *http.Client
	config             config.ConfluenceConfig
	lastSync           time.Time
	spaces             []string
	parentPageIDs      []string
	spaceMappings      map[string]string // space_key -> knowledge_id mapping
	parentPageMappings map[string]string // parent_page_id -> knowledge_id mapping
	spaceLabels        map[string][]string // space_key -> labels mapping
	storagePath        string              // path to storage directory
}

// ConfluenceState represents the persisted state of the Confluence adapter
type ConfluenceState struct {
	LastSync time.Time `json:"last_sync"`
}

// ConfluenceSpace represents a space from Confluence API
type ConfluenceSpace struct {
	ID                 string                 `json:"id"`
	Key                string                 `json:"key"`
	Name               string                 `json:"name"`
	Type               string                 `json:"type"`
	Status             string                 `json:"status"`
	Description        string                 `json:"description"`
	HomepageID         string                 `json:"homepageId"`
	Icon               interface{}            `json:"icon"`
	SpaceOwnerID       string                 `json:"spaceOwnerId"`
	AuthorID           string                 `json:"authorId"`
	CreatedAt          string                 `json:"createdAt"`
	CurrentActiveAlias string                 `json:"currentActiveAlias"`
	Links              map[string]interface{} `json:"_links"`
}

// ConfluenceSpaceList represents the response from listing spaces
type ConfluenceSpaceList struct {
	Results []ConfluenceSpace      `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluencePage represents a page from Confluence API
type ConfluencePage struct {
	ID                string                 `json:"id"`
	Status            string                 `json:"status"`
	Title             string                 `json:"title"`
	SpaceID           string                 `json:"spaceId"`
	ParentID          string                 `json:"parentId"`
	ParentType        string                 `json:"parentType"`
	Position          int                    `json:"position"`
	AuthorID          string                 `json:"authorId"`
	AuthorDisplayName string                 `json:"authorDisplayName"`
	OwnerID           string                 `json:"ownerId"`
	LastOwnerID       string                 `json:"lastOwnerId"`
	CreatedAt         string                 `json:"createdAt"`
	Version           ConfluenceVersion      `json:"version"`
	Body              ConfluenceBody         `json:"body"`
	Links             map[string]interface{} `json:"_links"`
}

// ConfluenceVersion represents version information
type ConfluenceVersion struct {
	CreatedAt string `json:"createdAt"`
	Message   string `json:"message"`
	Number    int    `json:"number"`
	MinorEdit bool   `json:"minorEdit"`
	AuthorID  string `json:"authorId"`
}

// ConfluenceBody represents the body content
type ConfluenceBody struct {
	View       ConfluenceBodyView `json:"view"`
	ExportView ConfluenceBodyView `json:"export_view"`
}

// ConfluenceBodyView represents the view content
type ConfluenceBodyView struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

// ConfluencePageList represents the response from listing pages
type ConfluencePageList struct {
	Results []ConfluencePage       `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceChildPage represents a child page from the children API
type ConfluenceChildPage struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Title         string `json:"title"`
	SpaceID       string `json:"spaceId"`
	ChildPosition int    `json:"childPosition"`
}

// ConfluenceChildPageList represents the response from listing child pages
type ConfluenceChildPageList struct {
	Results []ConfluenceChildPage  `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceAttachment represents an attachment
type ConfluenceAttachment struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	MediaType string                 `json:"mediaType"`
	FileSize  int                    `json:"fileSize"`
	Comment   string                 `json:"comment"`
	PageID    string                 `json:"pageId"`
	SpaceID   string                 `json:"spaceId"`
	Version   ConfluenceVersion      `json:"version"`
	CreatedAt string                 `json:"createdAt"`
	AuthorID  string                 `json:"authorId"`
	Links     map[string]interface{} `json:"_links"`
}

// ConfluenceAttachmentList represents the response from listing attachments
type ConfluenceAttachmentList struct {
	Results []ConfluenceAttachment `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceBlogPost represents a blog post from Confluence API
type ConfluenceBlogPost struct {
	ID                string                 `json:"id"`
	Status            string                 `json:"status"`
	Title             string                 `json:"title"`
	SpaceID           string                 `json:"spaceId"`
	ParentID          string                 `json:"parentId"`
	ParentType        string                 `json:"parentType"`
	Position          int                    `json:"position"`
	AuthorID          string                 `json:"authorId"`
	AuthorDisplayName string                 `json:"authorDisplayName"`
	OwnerID           string                 `json:"ownerId"`
	LastOwnerID       string                 `json:"lastOwnerId"`
	CreatedAt         string                 `json:"createdAt"`
	Version           ConfluenceVersion      `json:"version"`
	Body              ConfluenceBody         `json:"body"`
	Links             map[string]interface{} `json:"_links"`
}

// ConfluenceBlogPostList represents the response from listing blog posts
type ConfluenceBlogPostList struct {
	Results []ConfluenceBlogPost   `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceUser represents a user from Confluence API
type ConfluenceUser struct {
	AccountID        string                     `json:"accountId"`
	AccountType      string                     `json:"accountType"`
	Active           bool                       `json:"active"`
	ApplicationRoles ConfluenceApplicationRoles `json:"applicationRoles"`
	AvatarURLs       map[string]string          `json:"avatarUrls"`
	DisplayName      string                     `json:"displayName"`
	EmailAddress     string                     `json:"emailAddress"`
	Groups           ConfluenceGroups           `json:"groups"`
	Key              string                     `json:"key"`
	Name             string                     `json:"name"`
	Self             string                     `json:"self"`
	TimeZone         string                     `json:"timeZone"`
}

// ConfluenceUserList represents the response from listing users
type ConfluenceUserList struct {
	Results []ConfluenceUser       `json:"results"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceApplicationRoles represents application roles for a user
type ConfluenceApplicationRoles struct {
	Items []interface{} `json:"items"`
	Size  int           `json:"size"`
}

// ConfluenceGroups represents groups for a user
type ConfluenceGroups struct {
	Items []interface{} `json:"items"`
	Size  int           `json:"size"`
}

// NewConfluenceAdapter creates a new Confluence adapter
func NewConfluenceAdapter(cfg config.ConfluenceConfig, storagePath string) (*ConfluenceAdapter, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("confluence base URL is required")
	}
	if cfg.Username == "" && cfg.Type != "datacenter" {
		return nil, fmt.Errorf("confluence username is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("confluence API key is required")
	}

	// Build space and parent page mappings
	spaceMappings := make(map[string]string)
	spaceLabels := make(map[string][]string)
	parentPageMappings := make(map[string]string)
	spaces := []string{}
	parentPageIDs := []string{}

	// Process space mappings
	for _, mapping := range cfg.SpaceMappings {
		if mapping.SpaceKey != "" && mapping.KnowledgeID != "" {
			spaceMappings[mapping.SpaceKey] = mapping.KnowledgeID
			spaces = append(spaces, mapping.SpaceKey)
			if len(mapping.Labels) > 0 {
				spaceLabels[mapping.SpaceKey] = mapping.Labels
			}
		}
	}

	// Process parent page mappings
	for _, mapping := range cfg.ParentPageMappings {
		if mapping.ParentPageID != "" && mapping.KnowledgeID != "" {
			parentPageMappings[mapping.ParentPageID] = mapping.KnowledgeID
			parentPageIDs = append(parentPageIDs, mapping.ParentPageID)
		}
	}

	// If no mappings are configured, return error
	if len(spaces) == 0 && len(parentPageIDs) == 0 {
		return nil, fmt.Errorf("at least one confluence space or parent page mapping must be configured")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	adapter := &ConfluenceAdapter{
		client:             client,
		config:             cfg,
		spaces:             spaces,
		parentPageIDs:      parentPageIDs,
		spaceMappings:      spaceMappings,
		parentPageMappings: parentPageMappings,
		spaceLabels:        spaceLabels,
		lastSync:           time.Time{}, // Zero time initially
		storagePath:        storagePath,
	}

	// Load state from disk
	if err := adapter.loadState(); err != nil {
		logrus.Warnf("Failed to load Confluence adapter state: %v", err)
	}

	return adapter, nil
}

// Name returns the adapter name
func (c *ConfluenceAdapter) Name() string {
	return "confluence"
}

// setAuth sets the authentication header based on the configuration type
func (c *ConfluenceAdapter) setAuth(req *http.Request) {
	if c.config.Type == "datacenter" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	} else {
		// Set authentication
		c.setAuth(req)
	}
}

// FetchFiles fetches files from all configured Confluence spaces and parent pages
func (c *ConfluenceAdapter) FetchFiles(ctx context.Context) ([]*File, error) {
	var allFiles []*File

	logrus.Debugf("Confluence adapter config - ParentPageIDs: %v, Spaces: %v, BaseURL: %s, Username: %s",
		c.parentPageIDs, c.spaces, c.config.BaseURL, c.config.Username)

	// Process parent pages if configured
	if len(c.parentPageIDs) > 0 {
		logrus.Debugf("Using PARENT PAGE mode - Processing %d parent pages", len(c.parentPageIDs))
		for _, parentPageID := range c.parentPageIDs {
			logrus.Debugf("Fetching files from Confluence parent page: %s", parentPageID)

			var pages []ConfluencePage
			// Check for incremental update
			if !c.lastSync.IsZero() {
				logrus.Debugf("Checking for incremental updates for parent page %s since %v", parentPageID, c.lastSync)
				// Format: "2006-01-02 15:04"
				lastMod := c.lastSync.Format("2006-01-02 15:04")
				// Query: (id = "ID" OR parent = "ID") AND type = "page" AND lastModified > "TIME"
				cql := fmt.Sprintf("(id = \"%s\" OR parent = \"%s\") AND type = \"page\" AND lastModified > \"%s\"", parentPageID, parentPageID, lastMod)

				var err error
				pages, err = c.fetchPagesByCQL(ctx, cql)
				if err != nil {
					logrus.Errorf("Failed to fetch incremental updates for parent page %s: %v", parentPageID, err)
					continue
				}
				logrus.Debugf("Found %d updated pages under parent page %s", len(pages), parentPageID)
			} else {
				// Full sync logic
				// Step 1: Get the parent page details
				parentPage, err := c.fetchPageByID(ctx, parentPageID)
				if err != nil {
					logrus.Errorf("Failed to fetch parent page %s: %v", parentPageID, err)
					continue
				}

				logrus.Debugf("Parent page: %s (Space: %s)", parentPage.Title, parentPage.SpaceID)

				// Step 2: Fetch all sub-pages under this parent
				subPages, err := c.fetchSubPages(ctx, parentPageID)
				if err != nil {
					logrus.Errorf("Failed to fetch sub-pages for parent %s: %v", parentPageID, err)
					continue
				}

				// Include the parent page itself in the results
				pages = append([]ConfluencePage{parentPage}, subPages...)
				logrus.Debugf("Found %d pages under parent page %s", len(pages), parentPage.Title)
			}

			// Step 3: Process each page
			knowledgeID := c.parentPageMappings[parentPageID]
			for _, page := range pages {
				file, err := c.processPage(ctx, page, knowledgeID)
				if err != nil {
					logrus.Errorf("Failed to process page %s: %v", page.Title, err)
					continue
				}
				allFiles = append(allFiles, file)
			}
		}
	}

	// Process spaces if configured
	if len(c.spaces) > 0 {
		logrus.Debugf("Using SPACE mode - Processing %d spaces", len(c.spaces))
		for _, spaceKey := range c.spaces {
			logrus.Debugf("Fetching files from Confluence space: %s", spaceKey)

			// Step 1: Get space ID from space key
			spaceID, err := c.getSpaceID(ctx, spaceKey)
			if err != nil {
				logrus.Errorf("Failed to get space ID for %s: %v", spaceKey, err)
				continue
			}

			logrus.Debugf("Space %s has ID: %s", spaceKey, spaceID)

			// Step 2: Fetch pages from the space
			var pages []ConfluencePage


			useCQL := false
			cqlParts := []string{fmt.Sprintf("space = \"%s\"", spaceKey), "type = \"page\""}

			// Check labels
			if labels, ok := c.spaceLabels[spaceKey]; ok && len(labels) > 0 {
				useCQL = true
				labelList := ""
				for i, label := range labels {
					if i > 0 {
						labelList += ", "
					}
					labelList += fmt.Sprintf("\"%s\"", label)
				}
				cqlParts = append(cqlParts, fmt.Sprintf("label in (%s)", labelList))
			}

			// Check incremental update
			if !c.lastSync.IsZero() {
				useCQL = true
				lastMod := c.lastSync.Format("2006-01-02 15:04")
				cqlParts = append(cqlParts, fmt.Sprintf("lastModified > \"%s\"", lastMod))
				logrus.Debugf("Using incremental update for space %s since %s", spaceKey, lastMod)
			}

			if useCQL {
				cql := strings.Join(cqlParts, " AND ")
				logrus.Debugf("Fetching pages with CQL: %s", cql)
				pages, err = c.fetchPagesByCQL(ctx, cql)
			} else {
				pages, err = c.fetchSpacePages(ctx, spaceID, spaceKey)
			}

			if err != nil {
				logrus.Errorf("Failed to fetch pages from space %s: %v", spaceKey, err)
				continue
			}

			logrus.Debugf("Found %d pages in space %s", len(pages), spaceKey)

			// Step 3: Process each page
			knowledgeID := c.spaceMappings[spaceKey]
			for _, page := range pages {
				file, err := c.processPage(ctx, page, knowledgeID)
				if err != nil {
					logrus.Errorf("Failed to process page %s: %v", page.Title, err)
					continue
				}
				allFiles = append(allFiles, file)
			}

			// Step 4: Fetch blog posts from the space
			if c.config.IncludeBlogPosts {
				blogposts, err := c.fetchSpaceBlogposts(ctx, spaceID, spaceKey)
				if err != nil {
					logrus.Errorf("Failed to fetch blog posts from space %s: %v", spaceKey, err)
					continue
				}

				logrus.Debugf("Found %d blog posts in space %s", len(blogposts), spaceKey)

				// Step 5: Process each blog post
				for _, blogpost := range blogposts {
					file, err := c.processBlogpost(ctx, blogpost, knowledgeID)
					if err != nil {
						logrus.Errorf("Failed to process blog post %s: %v", blogpost.Title, err)
						continue
					}
					allFiles = append(allFiles, file)
				}
			}
		}
	}

	c.lastSync = time.Now()
	// State is NOT saved here - it will be saved by SetLastSync when sync manager confirms successful sync
	return allFiles, nil
}

// loadState loads the adapter state from disk
func (c *ConfluenceAdapter) loadState() error {
	if c.storagePath == "" {
		return nil
	}

	statePath := filepath.Join(c.storagePath, "confluence_state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}

	var state ConfluenceState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	if !state.LastSync.IsZero() {
		c.lastSync = state.LastSync
	}

	logrus.Debugf("Loaded Confluence state: last sync %v", c.lastSync)
	return nil
}

// saveState saves the adapter state to disk
func (c *ConfluenceAdapter) saveState() error {
	if c.storagePath == "" {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(c.storagePath, 0755); err != nil {
		return err
	}

	state := ConfluenceState{
		LastSync: c.lastSync,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	statePath := filepath.Join(c.storagePath, "confluence_state.json")
	return os.WriteFile(statePath, data, 0644)
}

// getSpaceID retrieves the space ID from the space key
func (c *ConfluenceAdapter) getSpaceID(ctx context.Context, spaceKey string) (string, error) {
	if c.config.Type == "datacenter" {
		return c.getSpaceIDV1(ctx, spaceKey)
	}

	// URL encode the space key
	encodedSpaceKey := url.QueryEscape(spaceKey)
	url := fmt.Sprintf("%s/wiki/api/v2/spaces?keys=%s", c.config.BaseURL, encodedSpaceKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OpenWebUI-Content-Sync/1.0")

	logrus.Debugf("Confluence space API URL: %s", url)
	logrus.Debugf("Confluence space key - Original: %s, Encoded: %s", spaceKey, encodedSpaceKey)
	logrus.Debugf("Confluence auth - Username: %s, APIKey length: %d", c.config.Username, len(c.config.APIKey))
	logrus.Debugf("Request headers: %+v", req.Header)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // Consume body for proper connection reuse
		logrus.Errorf("Confluence space API failed - Status: %d, URL: %s, Response: %s", resp.StatusCode, url, string(body))
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var spaceList ConfluenceSpaceList
	if err := json.NewDecoder(resp.Body).Decode(&spaceList); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(spaceList.Results) == 0 {
		return "", fmt.Errorf("space %s not found", spaceKey)
	}

	return spaceList.Results[0].ID, nil
}

// fetchSpacePages fetches all pages from a space using space ID
func (c *ConfluenceAdapter) fetchSpacePages(ctx context.Context, spaceID, spaceKey string) ([]ConfluencePage, error) {
	if c.config.Type == "datacenter" {
		return c.fetchSpacePagesV1(ctx, spaceKey)
	}

	var allPages []ConfluencePage
	limit := c.config.PageLimit
	if limit <= 0 {
		limit = 100 // Default limit
	}

	url := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/pages?limit=%d", c.config.BaseURL, spaceID, limit)

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set authentication
		c.setAuth(req)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Confluence pages API URL: %s", url)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var pageList ConfluencePageList
		if err := json.NewDecoder(resp.Body).Decode(&pageList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		allPages = append(allPages, pageList.Results...)

		// Check for next page
		nextLink, hasNext := pageList.Links["next"]
		if !hasNext {
			break
		}

		nextURL, ok := nextLink.(string)
		if !ok {
			break
		}
		// Check if nextURL doesn't start with https
		if nextURL != "" && !strings.HasPrefix(nextURL, "https") {
			// Prepend the base URL
			nextURL = c.config.BaseURL + nextURL
		}

		url = nextURL
		url = nextURL
	}

	// Extract all unique AuthorIDs from pages
	authorIDs := make(map[string]bool)
	for _, page := range allPages {
		if page.AuthorID != "" {
			authorIDs[page.AuthorID] = true
		}
	}

	// If we have author IDs and additional data is enabled, fetch user information
	if c.config.AddAdditionalData && len(authorIDs) > 0 {
		// Convert map keys to slice
		accountIDs := make([]string, 0, len(authorIDs))
		for accountID := range authorIDs {
			accountIDs = append(accountIDs, accountID)
		}

		// Fetch users by IDs
		users, err := c.fetchUsersByIds(ctx, accountIDs)
		if err != nil {
			logrus.Errorf("Failed to fetch users for pages: %v", err)
			// Continue without user information if fetch fails
		} else {
			// Update pages with user display names
			for i := range allPages {
				if user, exists := users[allPages[i].AuthorID]; exists {
					allPages[i].AuthorDisplayName = user.DisplayName
				}
			}
		}
	}

	return allPages, nil
}

// fetchPageByID fetches a specific page by its ID
func (c *ConfluenceAdapter) fetchPageByID(ctx context.Context, pageID string) (ConfluencePage, error) {
	url := fmt.Sprintf("%s/wiki/api/v2/pages/%s", c.config.BaseURL, pageID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConfluencePage{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Confluence page API URL: %s", url)
	resp, err := c.client.Do(req)
	if err != nil {
		return ConfluencePage{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // Consume body for proper connection reuse
		logrus.Errorf("Confluence page API failed - Status: %d, URL: %s, Response: %s", resp.StatusCode, url, string(body))
		return ConfluencePage{}, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var page ConfluencePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return ConfluencePage{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return page, nil
}

// fetchSubPages fetches all sub-pages under a specific parent page
func (c *ConfluenceAdapter) fetchSubPages(ctx context.Context, parentPageID string) ([]ConfluencePage, error) {
	var allPages []ConfluencePage
	limit := c.config.PageLimit
	if limit <= 0 {
		limit = 100 // Default limit
	}

	url := fmt.Sprintf("%s/wiki/api/v2/pages/%s/children?limit=%d", c.config.BaseURL, parentPageID, limit)

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set authentication
		c.setAuth(req)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Confluence sub-pages API URL: %s", url)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var childPageList ConfluenceChildPageList
		if err := json.NewDecoder(resp.Body).Decode(&childPageList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		// Convert child pages to full pages by fetching each one
		for _, childPage := range childPageList.Results {
			fullPage, err := c.fetchPageByID(ctx, childPage.ID)
			if err != nil {
				logrus.Errorf("Failed to fetch full page details for %s: %v", childPage.ID, err)
				continue
			}
			allPages = append(allPages, fullPage)
		}

		// Check for next page
		nextLink, hasNext := childPageList.Links["next"]
		if !hasNext {
			break
		}

		nextURL, ok := nextLink.(string)
		if !ok {
			break
		}

		url = nextURL
	}

	return allPages, nil
}

// processPage processes a single page and returns a File
func (c *ConfluenceAdapter) processPage(ctx context.Context, page ConfluencePage, knowledgeID string) (*File, error) {
	// Get the page body with content
	pageBody, err := c.fetchPageBody(ctx, page.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page body: %w", err)
	}

	// Create filename from title
	filename := c.SanitizeFilename(page.Title)
	if c.config.UseMarkdownParser {
		filename += ".md"
	} else {
		filename += ".txt"
	}

	// Format content as webui link + body content
	webuiLink := ""
	if webui, exists := page.Links["webui"]; exists {
		if webuiStr, ok := webui.(string); ok {
			webuiLink = webuiStr
		}
	}
	// Determine link prefix based on configuration type
	linkPrefix := c.config.BaseURL + "/wiki"
	if c.config.Type == "datacenter" {
		linkPrefix = c.config.BaseURL
	}
	metaData := fmt.Sprintf("---\nAuthor: %s\nCreatedAt: %s\nLinkToPage: %s\nTitle: %s\n---", page.AuthorDisplayName, page.CreatedAt, linkPrefix+webuiLink, page.Title)
	content := fmt.Sprintf("%s\n\n%s", metaData, pageBody)

	// Create file content
	fileContent := []byte(content)

	// Generate content hash for change detection
	hash := sha256.Sum256(fileContent)
	contentHash := base64.StdEncoding.EncodeToString(hash[:])

	return &File{
		Path:        filename,
		Content:     fileContent,
		Hash:        contentHash,
		Modified:    c.lastSync,
		Size:        int64(len(fileContent)),
		Source:      "confluence",
		KnowledgeID: knowledgeID,
	}, nil
}

// fetchPageBody fetches the body content of a specific page
func (c *ConfluenceAdapter) fetchPageBody(ctx context.Context, pageID string) (string, error) {
	if c.config.Type == "datacenter" {
		return c.fetchPageBodyV1(ctx, pageID)
	}

	url := fmt.Sprintf("%s/wiki/api/v2/pages/%s?body-format=export_view", c.config.BaseURL, pageID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Confluence page body API URL: %s", url)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	var page ConfluencePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	// Extract content from body.view.value
	if page.Body.ExportView.Value != "" {
		// Convert HTML to plain text or markdown based on configuration
		if c.config.UseMarkdownParser {
			return c.HtmlToMarkdown(page.Body.ExportView.Value), nil
		}
		return c.HtmlToText(page.Body.ExportView.Value), nil
	}

	return "", fmt.Errorf("no content found in page body")
}

// fetchSpaceBlogposts fetches all blog posts from a space using space ID
func (c *ConfluenceAdapter) fetchSpaceBlogposts(ctx context.Context, spaceID, spaceKey string) ([]ConfluenceBlogPost, error) {
	if c.config.Type == "datacenter" {
		return c.fetchSpaceBlogpostsV1(ctx, spaceKey)
	}

	var allBlogposts []ConfluenceBlogPost
	limit := c.config.PageLimit
	if limit <= 0 {
		limit = 100 // Default limit
	}

	url := fmt.Sprintf("%s/wiki/api/v2/spaces/%s/blogposts?limit=%d", c.config.BaseURL, spaceID, limit)

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set authentication
		c.setAuth(req)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Confluence blogposts API URL: %s", url)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var blogpostList ConfluenceBlogPostList
		if err := json.NewDecoder(resp.Body).Decode(&blogpostList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		allBlogposts = append(allBlogposts, blogpostList.Results...)

		// Check for next page
		nextLink, hasNext := blogpostList.Links["next"]
		if !hasNext {
			break
		}

		nextURL, ok := nextLink.(string)
		if !ok {
			break
		}
		// Check if nextURL doesn't start with https
		if nextURL != "" && !strings.HasPrefix(nextURL, "https") {
			// Prepend the base URL
			nextURL = c.config.BaseURL + nextURL
		}

		url = nextURL
	}

	// Extract all unique AuthorIDs from blogposts
	authorIDs := make(map[string]bool)
	for _, blogpost := range allBlogposts {
		if blogpost.AuthorID != "" {
			authorIDs[blogpost.AuthorID] = true
		}
	}

	// If we have author IDs and additional data is enabled, fetch user information
	if c.config.AddAdditionalData && len(authorIDs) > 0 {
		// Convert map keys to slice
		accountIDs := make([]string, 0, len(authorIDs))
		for accountID := range authorIDs {
			accountIDs = append(accountIDs, accountID)
		}

		// Fetch users by IDs
		users, err := c.fetchUsersByIds(ctx, accountIDs)
		if err != nil {
			logrus.Errorf("Failed to fetch users for blogposts: %v", err)
			// Continue without user information if fetch fails
		} else {
			// Update blogposts with user display names
			for i := range allBlogposts {
				if user, exists := users[allBlogposts[i].AuthorID]; exists {
					allBlogposts[i].AuthorDisplayName = user.DisplayName
				}
			}
		}
	}

	return allBlogposts, nil
}

// fetchBlogpostByID fetches a specific blog post by its ID
func (c *ConfluenceAdapter) fetchBlogpostByID(ctx context.Context, blogpostID string) (ConfluenceBlogPost, error) {
	url := fmt.Sprintf("%s/wiki/api/v2/blogposts/%s", c.config.BaseURL, blogpostID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConfluenceBlogPost{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Confluence blogpost API URL: %s", url)
	resp, err := c.client.Do(req)
	if err != nil {
		return ConfluenceBlogPost{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // Consume body for proper connection reuse
		logrus.Errorf("Confluence blogpost API failed - Status: %d, URL: %s, Response: %s", resp.StatusCode, url, string(body))
		return ConfluenceBlogPost{}, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var blogpost ConfluenceBlogPost
	if err := json.NewDecoder(resp.Body).Decode(&blogpost); err != nil {
		return ConfluenceBlogPost{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return blogpost, nil
}

// processBlogpost processes a single blog post and returns a File
func (c *ConfluenceAdapter) processBlogpost(ctx context.Context, blogpost ConfluenceBlogPost, knowledgeID string) (*File, error) {
	// Get the blog post body with content
	blogpostBody, err := c.fetchBlogpostBody(ctx, blogpost.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blogpost body: %w", err)
	}

	// Create filename from title
	filename := c.SanitizeFilename(blogpost.Title)
	if c.config.UseMarkdownParser {
		filename += ".md"
	} else {
		filename += ".txt"
	}

	// Format content as webui link + body content
	webuiLink := ""
	if webui, exists := blogpost.Links["webui"]; exists {
		if webuiStr, ok := webui.(string); ok {
			webuiLink = webuiStr
		}
	}
	// Determine link prefix based on configuration type
	linkPrefix := c.config.BaseURL + "/wiki"
	if c.config.Type == "datacenter" {
		linkPrefix = c.config.BaseURL
	}
	metaData := fmt.Sprintf("Author: %s\nCreatedAt: %s\nLinkToPage: %s", blogpost.AuthorDisplayName, blogpost.CreatedAt, linkPrefix+webuiLink)

	content := fmt.Sprintf("%s\n\n%s", metaData, blogpostBody)

	// Create file content
	fileContent := []byte(content)

	// Generate content hash for change detection
	hash := sha256.Sum256(fileContent)
	contentHash := base64.StdEncoding.EncodeToString(hash[:])

	return &File{
		Path:        filename,
		Content:     fileContent,
		Hash:        contentHash,
		Modified:    c.lastSync,
		Size:        int64(len(fileContent)),
		Source:      "confluence",
		KnowledgeID: knowledgeID,
	}, nil
}

// fetchBlogpostBody fetches the body content of a specific blog post
func (c *ConfluenceAdapter) fetchBlogpostBody(ctx context.Context, blogpostID string) (string, error) {
	url := fmt.Sprintf("%s/wiki/api/v2/blogposts/%s?body-format=export_view", c.config.BaseURL, blogpostID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Confluence blogpost body API URL: %s", url)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	var blogpost ConfluenceBlogPost
	if err := json.NewDecoder(resp.Body).Decode(&blogpost); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	// Extract content from body.view.value
	if blogpost.Body.ExportView.Value != "" {
		// Convert HTML to plain text or markdown based on configuration
		if c.config.UseMarkdownParser {
			return c.HtmlToMarkdown(blogpost.Body.ExportView.Value), nil
		}
		return c.HtmlToText(blogpost.Body.ExportView.Value), nil
	}

	return "", fmt.Errorf("no content found in blogpost body")
}

// HtmlToMarkdown converts HTML content to markdown
func (c *ConfluenceAdapter) HtmlToMarkdown(htmlContent string) string {
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
	markdown, err := conv.ConvertString(htmlContent)
	if err != nil {
		logrus.Warnf("Failed to convert HTML to markdown: %v", err)
		return htmlContent
	}
	return markdown
}

// htmlToText converts HTML content to plain text
func (c *ConfluenceAdapter) HtmlToText(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		logrus.Warnf("Failed to parse HTML: %v", err)
		return htmlContent
	}

	var text strings.Builder
	c.extractText(doc, &text)
	return strings.TrimSpace(text.String())
}

// extractText recursively extracts text from HTML nodes
func (c *ConfluenceAdapter) extractText(n *html.Node, text *strings.Builder) {
	if n.Type == html.TextNode {
		text.WriteString(n.Data)
		return
	}

	// Handle special elements
	if n.Type == html.ElementNode {
		switch n.Data {
		case "br":
			text.WriteString("\n")
			return
		case "p":
			// Add line break before paragraph (except if it's the first element)
			if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
				text.WriteString("\n")
			}
			// Process children
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				c.extractText(child, text)
			}
			// Add double line break after paragraph
			text.WriteString("\n\n")
			return
		case "div", "h1", "h2", "h3", "h4", "h5", "h6":
			// Add line break before other block elements (except if it's the first element)
			if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
				text.WriteString("\n")
			}
			// Process children
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				c.extractText(child, text)
			}
			// Add single line break after other block elements
			text.WriteString("\n")
			return
		}
	}

	// Process children for other elements
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.extractText(child, text)
	}
}

// sanitizeFilename converts a title to a safe filename
func (c *ConfluenceAdapter) SanitizeFilename(title string) string {
	// Convert to lowercase and replace spaces with underscores
	filename := strings.ToLower(title)

	// Replace invalid characters with underscores
	// Keep letters, numbers, spaces, underscores, dots, and hyphens
	// This supports unicode characters (including Cyrillic)
	reg := regexp.MustCompile(`[^\p{L}\p{N}\s_.-]`)
	filename = reg.ReplaceAllString(filename, "_")

	// Replace spaces and multiple underscores with single underscore
	reg = regexp.MustCompile(`[\s_]+`)
	filename = reg.ReplaceAllString(filename, "_")

	// Remove leading/trailing underscores
	filename = strings.Trim(filename, "_")

	// Limit length to 100 characters (runes, not bytes to respect unicode)
	runes := []rune(filename)
	if len(runes) > 100 {
		filename = string(runes[:100])
	}

	// Ensure it's not empty
	if filename == "" {
		filename = "untitled"
	}

	return filename
}

// GetLastSync returns the last sync time
func (c *ConfluenceAdapter) GetLastSync() time.Time {
	return c.lastSync
}

// SetLastSync sets the last sync time and persists state to disk
// This is called by the sync manager after files are successfully synced to OpenWebUI
func (c *ConfluenceAdapter) SetLastSync(t time.Time) {
	c.lastSync = t
	if err := c.saveState(); err != nil {
		logrus.Warnf("Failed to save Confluence state: %v", err)
	}
}

// fetchUsersByIds fetches user information for multiple account IDs using the bulk API
func (c *ConfluenceAdapter) fetchUsersByIds(ctx context.Context, accountIds []string) (map[string]*ConfluenceUser, error) {
	// Create the request body
	requestBody := map[string]interface{}{
		"accountIds": accountIds,
	}

	// Marshal the request body
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Prepare the URL for the bulk user lookup endpoint
	url := fmt.Sprintf("%s/wiki/api/v2/users-bulk", c.config.BaseURL)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication and headers
	// Set authentication
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	logrus.Debugf("Confluence bulk user API URL: %s", url)
	logrus.Debugf("Confluence bulk user request body: %s", string(body))

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // Consume body for proper connection reuse
		logrus.Errorf("Confluence bulk user API failed - Status: %d, URL: %s, Response: %s", resp.StatusCode, url, string(body))
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var userResponse struct {
		Results []ConfluenceUser `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Create a map of account IDs to users for easy lookup
	userMap := make(map[string]*ConfluenceUser)
	for i := range userResponse.Results {
		user := &userResponse.Results[i]
		userMap[user.AccountID] = user
	}

	return userMap, nil
}
