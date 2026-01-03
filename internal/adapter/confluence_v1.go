package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/sirupsen/logrus"
)

// --- Data Center (V1 API) Implementations ---

// ConfluenceSpaceListV1 represents the response from listing spaces in V1
type ConfluenceSpaceListV1 struct {
	Results []ConfluenceSpaceV1 `json:"results"`
}

// ConfluenceSpaceV1 represents a space in V1
type ConfluenceSpaceV1 struct {
	ID   int64  `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ConfluenceContentListV1 represents the response from listing content in V1
type ConfluenceContentListV1 struct {
	Results []ConfluenceContentV1  `json:"results"`
	Size    int                    `json:"size"`
	Start   int                    `json:"start"`
	Limit   int                    `json:"limit"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceContentV1 represents content (page/blogpost) in V1
type ConfluenceContentV1 struct {
	ID      string              `json:"id"`
	Type    string              `json:"type"`
	Status  string              `json:"status"`
	Title   string              `json:"title"`
	Body    ConfluenceBodyV1    `json:"body"`
	History ConfluenceHistoryV1 `json:"history"`
	Space   ConfluenceSpaceV1   `json:"space"`
	Version ConfluenceVersionV1 `json:"version"`
	Links   map[string]interface{} `json:"_links"`
}

// ConfluenceBodyV1 represents body content in V1
type ConfluenceBodyV1 struct {
	Storage ConfluenceBodyView `json:"storage"`
	View    ConfluenceBodyView `json:"view"`
}

// ConfluenceHistoryV1 represents history/author info in V1
type ConfluenceHistoryV1 struct {
	CreatedBy   ConfluenceUserV1 `json:"createdBy"`
	CreatedDate string           `json:"createdDate"`
}

// ConfluenceUserV1 represents user info in V1
type ConfluenceUserV1 struct {
	Username    string `json:"username"`
	UserKey     string `json:"userKey"`
	DisplayName string `json:"displayName"`
}

// ConfluenceVersionV1 represents version info in V1
type ConfluenceVersionV1 struct {
	When   string           `json:"when"`
	By     ConfluenceUserV1 `json:"by"`
	Number int              `json:"number"`
}

// getSpaceIDV1 retrieves the space ID (as string) from the space key using V1 API
func (c *ConfluenceAdapter) getSpaceIDV1(ctx context.Context, spaceKey string) (string, error) {
	url := fmt.Sprintf("%s/rest/api/space?spaceKey=%s", c.config.BaseURL, url.QueryEscape(spaceKey))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var spaceList ConfluenceSpaceListV1
	if err := json.NewDecoder(resp.Body).Decode(&spaceList); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(spaceList.Results) == 0 {
		return "", fmt.Errorf("space %s not found", spaceKey)
	}

	// Helper to check precise match if API returns partial matches
	for _, space := range spaceList.Results {
		if space.Key == spaceKey {
			return fmt.Sprintf("%d", space.ID), nil
		}
	}

	// Fallback if exact key not found in list (unlikely with spaceKey param)
	return fmt.Sprintf("%d", spaceList.Results[0].ID), nil
}

// fetchSpacePagesV1 fetches pages using V1 API
func (c *ConfluenceAdapter) fetchSpacePagesV1(ctx context.Context, spaceKey string) ([]ConfluencePage, error) {
	var allPages []ConfluencePage
	limit := c.config.PageLimit
	if limit <= 0 {
		limit = 100 // Default limit (though V1 default is often 25)
	}
	start := 0

	for {
		// V1 API: /rest/api/content?spaceKey=KEY&type=page&limit=LIMIT&start=START&expand=...
		// expand: version so we get date/author if needed? or history.
		// We need authorDisplayName and createdDate.
		// history.createdBy gives author. history.createdDate gives creation date.
		qs := url.Values{}
		qs.Set("spaceKey", spaceKey)
		qs.Set("type", "page")
		qs.Set("limit", fmt.Sprintf("%d", limit))
		qs.Set("start", fmt.Sprintf("%d", start))
		qs.Set("expand", "history.createdBy,version,space")

		fullURL := fmt.Sprintf("%s/rest/api/content?%s", c.config.BaseURL, qs.Encode())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuth(req)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Confluence V1 pages API URL: %s", fullURL)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var contentList ConfluenceContentListV1
		if err := json.NewDecoder(resp.Body).Decode(&contentList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		for _, item := range contentList.Results {
			allPages = append(allPages, c.mapContentV1ToPage(item))
		}

		if len(contentList.Results) < limit {
			break
		}
		start += len(contentList.Results)
	}

	return allPages, nil
}

// fetchSpaceBlogpostsV1 fetches blog posts using V1 API
func (c *ConfluenceAdapter) fetchSpaceBlogpostsV1(ctx context.Context, spaceKey string) ([]ConfluenceBlogPost, error) {
	var allBlogposts []ConfluenceBlogPost
	limit := c.config.PageLimit
	if limit <= 0 {
		limit = 100
	}
	start := 0

	for {
		qs := url.Values{}
		qs.Set("spaceKey", spaceKey)
		qs.Set("type", "blogpost")
		qs.Set("limit", fmt.Sprintf("%d", limit))
		qs.Set("start", fmt.Sprintf("%d", start))
		qs.Set("expand", "history.createdBy,version,space")

		fullURL := fmt.Sprintf("%s/rest/api/content?%s", c.config.BaseURL, qs.Encode())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuth(req)
		req.Header.Set("Accept", "application/json")

		logrus.Debugf("Confluence V1 blogposts API URL: %s", fullURL)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
		}

		var contentList ConfluenceContentListV1
		if err := json.NewDecoder(resp.Body).Decode(&contentList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		for _, item := range contentList.Results {
			allBlogposts = append(allBlogposts, c.mapContentV1ToBlogPost(item))
		}

		if len(contentList.Results) < limit {
			break
		}
		start += len(contentList.Results)
	}

	return allBlogposts, nil
}

// fetchPageBodyV1 fetches body content using V1 API
func (c *ConfluenceAdapter) fetchPageBodyV1(ctx context.Context, pageID string) (string, error) {
	// expand=body.view for HTML content
	// or body.storage for raw
	// Existing V2 adapter uses export_view which is likely HTML.
	// So we use body.view for V1 which is also HTML.

	url := fmt.Sprintf("%s/rest/api/content/%s?expand=body.view", c.config.BaseURL, pageID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Confluence V1 page body API URL: %s", url)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	var content ConfluenceContentV1
	if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if content.Body.View.Value != "" {
		if c.config.UseMarkdownParser {
			return c.HtmlToMarkdown(content.Body.View.Value), nil
		}
		return c.HtmlToText(content.Body.View.Value), nil
	}

	return "", fmt.Errorf("no content found in page body (V1)")
}

// mapContentV1ToPage maps V1 Content to common ConfluencePage struct
func (c *ConfluenceAdapter) mapContentV1ToPage(content ConfluenceContentV1) ConfluencePage {
	return ConfluencePage{
		ID:                content.ID,
		Status:            content.Status,
		Title:             content.Title,
		SpaceID:           fmt.Sprintf("%d", content.Space.ID),
		AuthorID:          content.History.CreatedBy.UserKey,
		AuthorDisplayName: content.History.CreatedBy.DisplayName,
		CreatedAt:         content.History.CreatedDate,
		Version: ConfluenceVersion{
			Number:    content.Version.Number,
			CreatedAt: content.Version.When,
			AuthorID:  content.Version.By.UserKey,
		},
		Links: content.Links,
	}
}

// mapContentV1ToBlogPost maps V1 Content to common ConfluenceBlogPost struct
func (c *ConfluenceAdapter) mapContentV1ToBlogPost(content ConfluenceContentV1) ConfluenceBlogPost {
	return ConfluenceBlogPost{
		ID:                content.ID,
		Status:            content.Status,
		Title:             content.Title,
		SpaceID:           fmt.Sprintf("%d", content.Space.ID),
		AuthorID:          content.History.CreatedBy.UserKey,
		AuthorDisplayName: content.History.CreatedBy.DisplayName,
		CreatedAt:         content.History.CreatedDate,
		Version: ConfluenceVersion{
			Number:    content.Version.Number,
			CreatedAt: content.Version.When,
			AuthorID:  content.Version.By.UserKey,
		},
		Links: content.Links,
	}
}
