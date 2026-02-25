package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitLabPipeline represents a GitLab CI pipeline.
type GitLabPipeline struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	WebURL    string `json:"web_url"`
	CreatedAt string `json:"created_at"`
}

// GitLabMergeRequest represents a GitLab merge request.
type GitLabMergeRequest struct {
	ID           int    `json:"id"`
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	State        string `json:"state"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	WebURL       string `json:"web_url"`
}

// MROptions holds options for creating a merge request.
type MROptions struct {
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	Labels       []string
}

// GitLabClient is a lightweight GitLab REST API v4 client.
// When baseURL is "mock://", all methods return canned responses for testing.
type GitLabClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewGitLabClient creates a new GitLabClient.
func NewGitLabClient(baseURL, token string) *GitLabClient {
	return &GitLabClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *GitLabClient) isMock() bool {
	return strings.HasPrefix(c.baseURL, "mock:") // TrimRight removes trailing slashes
}

// TriggerPipeline triggers a pipeline for the given project ref.
func (c *GitLabClient) TriggerPipeline(projectID, ref string, variables map[string]string) (*GitLabPipeline, error) {
	if c.isMock() {
		return &GitLabPipeline{
			ID:        42,
			Status:    "created",
			Ref:       ref,
			SHA:       "abc123def456",
			WebURL:    "https://gitlab.example.com/" + projectID + "/-/pipelines/42",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	body := map[string]any{"ref": ref}
	if len(variables) > 0 {
		vars := make([]map[string]string, 0, len(variables))
		for k, v := range variables {
			vars = append(vars, map[string]string{"key": k, "value": v})
		}
		body["variables"] = vars
	}

	encoded := encodeProjectID(projectID)
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v4/projects/%s/pipeline", encoded), body)
	if err != nil {
		return nil, err
	}

	var pipeline GitLabPipeline
	if err := json.Unmarshal(resp, &pipeline); err != nil {
		return nil, fmt.Errorf("gitlab: failed to decode pipeline response: %w", err)
	}
	return &pipeline, nil
}

// GetPipeline retrieves the status of a pipeline by ID.
func (c *GitLabClient) GetPipeline(projectID string, pipelineID int) (*GitLabPipeline, error) {
	if c.isMock() {
		return &GitLabPipeline{
			ID:        pipelineID,
			Status:    "success",
			Ref:       "main",
			SHA:       "abc123def456",
			WebURL:    fmt.Sprintf("https://gitlab.example.com/%s/-/pipelines/%d", projectID, pipelineID),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	encoded := encodeProjectID(projectID)
	resp, err := c.doRequest("GET", fmt.Sprintf("/api/v4/projects/%s/pipelines/%d", encoded, pipelineID), nil)
	if err != nil {
		return nil, err
	}

	var pipeline GitLabPipeline
	if err := json.Unmarshal(resp, &pipeline); err != nil {
		return nil, fmt.Errorf("gitlab: failed to decode pipeline response: %w", err)
	}
	return &pipeline, nil
}

// CreateMergeRequest creates a merge request in the given project.
func (c *GitLabClient) CreateMergeRequest(projectID string, opts MROptions) (*GitLabMergeRequest, error) {
	if c.isMock() {
		return &GitLabMergeRequest{
			ID:           100,
			IID:          1,
			Title:        opts.Title,
			State:        "opened",
			SourceBranch: opts.SourceBranch,
			TargetBranch: opts.TargetBranch,
			WebURL:       "https://gitlab.example.com/" + projectID + "/-/merge_requests/1",
		}, nil
	}

	body := map[string]any{
		"source_branch": opts.SourceBranch,
		"target_branch": opts.TargetBranch,
		"title":         opts.Title,
	}
	if opts.Description != "" {
		body["description"] = opts.Description
	}
	if len(opts.Labels) > 0 {
		body["labels"] = strings.Join(opts.Labels, ",")
	}

	encoded := encodeProjectID(projectID)
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v4/projects/%s/merge_requests", encoded), body)
	if err != nil {
		return nil, err
	}

	var mr GitLabMergeRequest
	if err := json.Unmarshal(resp, &mr); err != nil {
		return nil, fmt.Errorf("gitlab: failed to decode MR response: %w", err)
	}
	return &mr, nil
}

// CommentOnMR posts a note (comment) on a merge request.
func (c *GitLabClient) CommentOnMR(projectID string, mrIID int, body string) error {
	if c.isMock() {
		return nil
	}

	encoded := encodeProjectID(projectID)
	_, err := c.doRequest("POST", fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", encoded, mrIID),
		map[string]any{"body": body})
	return err
}

// doRequest performs an authenticated HTTP request to the GitLab API.
func (c *GitLabClient) doRequest(method, path string, payload any) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("gitlab: failed to encode request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gitlab: API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// encodeProjectID URL-encodes a project path (e.g. "group/project" → "group%2Fproject").
func encodeProjectID(id string) string {
	return strings.ReplaceAll(id, "/", "%2F")
}
