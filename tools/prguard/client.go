package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubClient handles API interactions with GitHub.
type GitHubClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient(baseURL, token string) *GitHubClient {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &GitHubClient{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchPRFiles fetches all files in a PR, handling pagination and the 3000-file GitHub API limit.
func (c *GitHubClient) FetchPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]PRFile, error) {
	var allFiles []PRFile
	page := 1
	const perPage = 100
	const maxLimit = 3000

	for {
		reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=%d&page=%d", c.BaseURL, owner, repo, prNumber, perPage, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create files request: %w", err)
		}

		c.setHeaders(req)
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("execute files request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("fetch files failed (status %d): %s", resp.StatusCode, string(body))
		}

		var pageFiles []PRFile
		err = json.NewDecoder(resp.Body).Decode(&pageFiles)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode files JSON on page %d: %w", page, err)
		}

		if len(pageFiles) == 0 {
			break
		}

		allFiles = append(allFiles, pageFiles...)

		// Enforce or detect the 3000-file limit
		if len(allFiles) >= maxLimit {
			break
		}

		if len(pageFiles) < perPage {
			break
		}

		page++
	}

	return allFiles, nil
}

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// PostOrUpdateComment searches for an existing PR Guard comment and updates it, or creates a new one.
func (c *GitHubClient) PostOrUpdateComment(ctx context.Context, owner, repo string, prNumber int, commentBody string) error {
	const marker = "<!-- fairway-pr-guard -->"

	// List existing comments
	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=100", c.BaseURL, owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("create list comments request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute list comments request: %w", err)
	}

	var existingID int64
	if resp.StatusCode == http.StatusOK {
		var comments []issueComment
		if err := json.NewDecoder(resp.Body).Decode(&comments); err == nil {
			for _, comment := range comments {
				if strings.Contains(comment.Body, marker) {
					existingID = comment.ID
					break
				}
			}
		}
	}
	resp.Body.Close()

	payload := map[string]string{"body": commentBody}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal comment payload: %w", err)
	}

	if existingID > 0 {
		// Update existing comment
		updateURL := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", c.BaseURL, owner, repo, existingID)
		patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, updateURL, bytes.NewReader(payloadBytes))
		if err != nil {
			return fmt.Errorf("create update comment request: %w", err)
		}
		c.setHeaders(patchReq)
		updateResp, err := c.HTTPClient.Do(patchReq)
		if err != nil {
			return fmt.Errorf("execute update comment request: %w", err)
		}
		defer updateResp.Body.Close()
		if updateResp.StatusCode < 200 || updateResp.StatusCode >= 300 {
			body, _ := io.ReadAll(updateResp.Body)
			return fmt.Errorf("update comment failed (status %d): %s", updateResp.StatusCode, string(body))
		}
	} else {
		// Create new comment
		createURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.BaseURL, owner, repo, prNumber)
		postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(payloadBytes))
		if err != nil {
			return fmt.Errorf("create comment request: %w", err)
		}
		c.setHeaders(postReq)
		postResp, err := c.HTTPClient.Do(postReq)
		if err != nil {
			return fmt.Errorf("execute comment request: %w", err)
		}
		defer postResp.Body.Close()
		if postResp.StatusCode < 200 || postResp.StatusCode >= 300 {
			body, _ := io.ReadAll(postResp.Body)
			return fmt.Errorf("create comment failed (status %d): %s", postResp.StatusCode, string(body))
		}
	}

	return nil
}

// ApplyLabel adds the specified label to the PR issue.
func (c *GitHubClient) ApplyLabel(ctx context.Context, owner, repo string, prNumber int, label string) error {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels", c.BaseURL, owner, repo, prNumber)
	payload := map[string][]string{"labels": {label}}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal label payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create label request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute label request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apply label failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ClosePR closes the pull request.
func (c *GitHubClient) ClosePR(ctx context.Context, owner, repo string, prNumber int) error {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.BaseURL, owner, repo, prNumber)
	payload := map[string]string{"state": "closed"}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal close PR payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create close PR request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute close PR request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("close PR failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *GitHubClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
