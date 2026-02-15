package workflow

import "time"

// Workflow represents a configured workflow definition.
type Workflow struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Version     int            `json:"version"`
	Status      string         `json:"status"` // "active", "inactive", "draft", "error"
	Config      map[string]any `json:"config,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// StepExecution represents a single step within a workflow execution.
type StepExecution struct {
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Input       map[string]any `json:"input,omitempty"`
	Output      map[string]any `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	DurationMs  *int64         `json:"duration_ms,omitempty"`
}

// Execution represents a running or completed workflow execution.
type Execution struct {
	ID          string          `json:"id"`
	WorkflowID  string          `json:"workflow_id"`
	Status      string          `json:"status"` // "pending", "running", "completed", "failed", "cancelled", "timeout"
	Input       map[string]any  `json:"input"`
	Output      map[string]any  `json:"output,omitempty"`
	Error       string          `json:"error,omitempty"`
	Steps       []StepExecution `json:"steps,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	DurationMs  *int64          `json:"duration_ms,omitempty"`
}

// SSEEvent represents a Server-Sent Event from execution streaming.
type SSEEvent struct {
	ID    string `json:"id"`
	Event string `json:"event"`
	Data  string `json:"data"`
}

// DLQEntry represents a dead-letter queue entry for failed events.
type DLQEntry struct {
	ID          string         `json:"id"`
	WorkflowID  string         `json:"workflow_id"`
	ExecutionID string         `json:"execution_id"`
	Error       string         `json:"error"`
	Payload     map[string]any `json:"payload"`
	RetryCount  int            `json:"retry_count"`
	MaxRetries  int            `json:"max_retries"`
	CreatedAt   time.Time      `json:"created_at"`
	LastRetryAt *time.Time     `json:"last_retry_at,omitempty"`
}

// HealthCheck represents the result of a single health check.
type HealthCheck struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthStatus represents the overall system health.
type HealthStatus struct {
	Status string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Checks map[string]HealthCheck `json:"checks"`
}

// ExecutionFilter provides parameters for filtering execution listings.
type ExecutionFilter struct {
	WorkflowID string `url:"workflow_id,omitempty"`
	Status     string `url:"status,omitempty"`
	Since      string `url:"since,omitempty"`
	Until      string `url:"until,omitempty"`
	Limit      int    `url:"limit,omitempty"`
	Offset     int    `url:"offset,omitempty"`
}

// DLQFilter provides parameters for filtering DLQ entry listings.
type DLQFilter struct {
	WorkflowID string `url:"workflow_id,omitempty"`
	Since      string `url:"since,omitempty"`
	Limit      int    `url:"limit,omitempty"`
	Offset     int    `url:"offset,omitempty"`
}

// WorkflowError represents an API error response.
type WorkflowError struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Body       string `json:"body,omitempty"`
}

// Error implements the error interface.
func (e *WorkflowError) Error() string {
	if e.Body != "" {
		return "workflow API error " + e.Message + ": " + e.Body
	}
	return "workflow API error: " + e.Message
}
