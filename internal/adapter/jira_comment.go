package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

// CommentData holds the extracted comment data we want
type CommentData struct {
	RenderedBody string `json:"renderedBody"`
	AuthorName   string `json:"authorName"`
	Created      string `json:"created"`
}

// fetchComment fetches a single comment by URL and returns only the renderedBody and author displayName
func (j *JiraAdapter) fetchComment(ctx context.Context, commentURL string) (*CommentData, error) {
	// Build URL for individual comment fetch
	url := commentURL
	url += "?expand=renderedBody&name&fields=summary,description,parent,issuetype,reporter,status"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	j.setAuth(req)
	req.Header.Set("Accept", "application/json")

	logrus.Debugf("Jira comment API URL: %s", url)

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("API request failed with status %d: response body omitted", resp.StatusCode)
	}

	var comment struct {
		Self         string   `json:"self"`
		ID           string   `json:"id"`
		Author       JiraUser `json:"author"`
		RenderedBody string   `json:"renderedBody"`
		UpdateAuthor JiraUser `json:"updateAuthor"`
		Created      string   `json:"created"`
		Updated      string   `json:"updated"`
		JsdPublic    bool     `json:"jsdPublic"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&comment); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	resp.Body.Close()

	return &CommentData{
		RenderedBody: comment.RenderedBody,
		AuthorName:   comment.Author.DisplayName,
	}, nil
}

// fetchCommentsForIssue fetches all comments for a specific issue and returns only the renderedBody and author displayName
func (j *JiraAdapter) fetchCommentsForIssue(ctx context.Context, issueID string) ([]CommentData, error) {
	var comments []CommentData

	// First fetch the issue to get the comments
	issue, err := j.fetchIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue %s: %w", issueID, err)
	}

	// Extract comments from the issue
	for _, comment := range issue.Fields.Comment.Comments {
		// Extract rendered body from the comment's body field
		fetchedComment, err2 := j.fetchComment(ctx, comment.Self)
		if err2 != nil {
			return comments, fmt.Errorf("failed to Fetch Comment %w", err)
		}

		renderedBody := j.HtmlToMarkdown(fetchedComment.RenderedBody)
		logrus.Debugf("FetchedComment: %s,renderedBody %s ", fetchedComment, renderedBody)
		comments = append(comments, CommentData{
			RenderedBody: j.HtmlToMarkdown(renderedBody),
			AuthorName:   comment.Author.DisplayName,
			Created:      comment.Created,
		})
	}

	return comments, nil
}
