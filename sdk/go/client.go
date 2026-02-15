// Package workflow provides a Go client SDK for the GoCodeAlone Workflow engine.
//
// Example usage:
//
//	client := workflow.NewClient("http://localhost:8080",
//	    workflow.WithAPIKey("my-api-key"),
//	)
//
//	// List workflows
//	workflows, err := client.ListWorkflows(ctx)
//
//	// Execute a workflow
//	execution, err := client.ExecuteWorkflow(ctx, "my-workflow", map[string]any{
//	    "order_id": "12345",
//	})
//
//	// Stream execution events via SSE
//	events, err := client.StreamExecution(ctx, execution.ID)
//	for event := range events {
//	    fmt.Printf("[%s] %s\n", event.Event, event.Data)
//	}
package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client communicates with the Workflow engine REST API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithAPIKey sets the API key for authentication.
func WithAPIKey(apiKey string) ClientOption {
	return func(c *Client) {
		c.apiKey = apiKey
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithTimeout sets the default request timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// NewClient creates a new Workflow API client.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ---- Internal helpers ----

func (c *Client) buildRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &WorkflowError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
			Body:       string(bodyBytes),
		}
	}

	if resp.StatusCode == http.StatusNoContent || target == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = strings.NewReader(string(b))
	}

	req, err := c.buildRequest(ctx, method, path, reader)
	if err != nil {
		return err
	}

	return c.do(req, target)
}

func buildQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	v := url.Values{}
	for key, val := range params {
		if val != "" {
			v.Set(key, val)
		}
	}
	if encoded := v.Encode(); encoded != "" {
		return "?" + encoded
	}
	return ""
}

// ---- Workflows ----

// ListWorkflows returns all workflows.
func (c *Client) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	var result []Workflow
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/workflows", nil, &result)
	return result, err
}

// GetWorkflow returns a single workflow by ID.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var result Workflow
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/workflows/"+url.PathEscape(id), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateWorkflow creates a new workflow from a configuration.
func (c *Client) CreateWorkflow(ctx context.Context, config map[string]any) (*Workflow, error) {
	var result Workflow
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/workflows", config, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteWorkflow deletes a workflow by ID.
func (c *Client) DeleteWorkflow(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/workflows/"+url.PathEscape(id), nil, nil)
}

// ---- Executions ----

// ExecuteWorkflow triggers a workflow execution with the given input data.
func (c *Client) ExecuteWorkflow(ctx context.Context, id string, data map[string]any) (*Execution, error) {
	var result Execution
	path := "/api/v1/workflows/" + url.PathEscape(id) + "/execute"
	err := c.doJSON(ctx, http.MethodPost, path, data, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetExecution returns a single execution by ID.
func (c *Client) GetExecution(ctx context.Context, id string) (*Execution, error) {
	var result Execution
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/executions/"+url.PathEscape(id), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListExecutions returns executions matching the optional filter.
func (c *Client) ListExecutions(ctx context.Context, filter *ExecutionFilter) ([]Execution, error) {
	params := map[string]string{}
	if filter != nil {
		if filter.WorkflowID != "" {
			params["workflow_id"] = filter.WorkflowID
		}
		if filter.Status != "" {
			params["status"] = filter.Status
		}
		if filter.Since != "" {
			params["since"] = filter.Since
		}
		if filter.Until != "" {
			params["until"] = filter.Until
		}
		if filter.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", filter.Limit)
		}
		if filter.Offset > 0 {
			params["offset"] = fmt.Sprintf("%d", filter.Offset)
		}
	}

	var result []Execution
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/executions"+buildQuery(params), nil, &result)
	return result, err
}

// StreamExecution opens an SSE connection and streams execution events.
// The returned channel is closed when the server closes the connection or
// the context is cancelled. Callers should range over the channel.
func (c *Client) StreamExecution(ctx context.Context, id string) (<-chan SSEEvent, error) {
	u := c.baseURL + "/api/v1/executions/" + url.PathEscape(id) + "/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Use a client without timeout for streaming
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to SSE stream: %w", err)
	}

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &WorkflowError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
			Body:       string(bodyBytes),
		}
	}

	ch := make(chan SSEEvent, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var currentID, currentEvent, currentData string

		for scanner.Scan() {
			line := scanner.Text()

			switch {
			case line == "":
				// Empty line = end of event
				if currentData != "" || currentEvent != "" || currentID != "" {
					select {
					case ch <- SSEEvent{
						ID:    currentID,
						Event: currentEvent,
						Data:  currentData,
					}:
					case <-ctx.Done():
						return
					}
					currentID = ""
					currentEvent = ""
					currentData = ""
				}
			case strings.HasPrefix(line, "id: "):
				currentID = line[4:]
			case strings.HasPrefix(line, "event: "):
				currentEvent = line[7:]
			case strings.HasPrefix(line, "data: "):
				currentData = line[6:]
			case strings.HasPrefix(line, ":"):
				// Comment, ignore
			}
		}

		// Flush any remaining event
		if currentData != "" || currentEvent != "" || currentID != "" {
			select {
			case ch <- SSEEvent{
				ID:    currentID,
				Event: currentEvent,
				Data:  currentData,
			}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// ---- DLQ ----

// ListDLQEntries returns dead-letter queue entries matching the optional filter.
func (c *Client) ListDLQEntries(ctx context.Context, filter *DLQFilter) ([]DLQEntry, error) {
	params := map[string]string{}
	if filter != nil {
		if filter.WorkflowID != "" {
			params["workflow_id"] = filter.WorkflowID
		}
		if filter.Since != "" {
			params["since"] = filter.Since
		}
		if filter.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", filter.Limit)
		}
		if filter.Offset > 0 {
			params["offset"] = fmt.Sprintf("%d", filter.Offset)
		}
	}

	var result []DLQEntry
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/dlq"+buildQuery(params), nil, &result)
	return result, err
}

// RetryDLQEntry retries a failed DLQ entry.
func (c *Client) RetryDLQEntry(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/dlq/"+url.PathEscape(id)+"/retry", nil, nil)
}

// ---- Health ----

// Health checks the system health status.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	var result HealthStatus
	err := c.doJSON(ctx, http.MethodGet, "/healthz", nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
