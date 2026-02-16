package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// ReporterConfig configures the built-in observability reporter.
type ReporterConfig struct {
	// AdminURL is the base URL of the admin server (e.g., "http://admin-server:8081").
	AdminURL string `yaml:"admin_url" json:"admin_url"`
	// FlushInterval is how often buffered data is sent to the admin server.
	FlushInterval time.Duration `yaml:"flush_interval" json:"flush_interval"`
	// BatchSize is the maximum number of items per flush batch.
	BatchSize int `yaml:"batch_size" json:"batch_size"`
	// InstanceName identifies this worker instance.
	InstanceName string `yaml:"instance_name" json:"instance_name"`
	// HeartbeatInterval is how often to send heartbeats to the admin server.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" json:"heartbeat_interval"`
}

// DefaultReporterConfig returns a config with sensible defaults.
func DefaultReporterConfig() ReporterConfig {
	hostname, _ := os.Hostname()
	return ReporterConfig{
		FlushInterval:     5 * time.Second,
		BatchSize:         100,
		InstanceName:      hostname,
		HeartbeatInterval: 30 * time.Second,
	}
}

// ExecutionReport represents an execution record to report to the admin.
type ExecutionReport struct {
	ID           string    `json:"id"`
	WorkflowID   string    `json:"workflow_id"`
	TriggerType  string    `json:"trigger_type"`
	Status       string    `json:"status"`
	TriggeredBy  string    `json:"triggered_by,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DurationMs   int64     `json:"duration_ms"`
}

// LogReport represents a log entry to report to the admin.
type LogReport struct {
	WorkflowID  string `json:"workflow_id"`
	ExecutionID string `json:"execution_id,omitempty"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	ModuleName  string `json:"module_name,omitempty"`
	Fields      string `json:"fields,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// EventReport represents an event to report to the admin.
type EventReport struct {
	ExecutionID string         `json:"execution_id"`
	EventType   string         `json:"event_type"`
	EventData   map[string]any `json:"event_data"`
	CreatedAt   string         `json:"created_at"`
}

// Reporter buffers observability data and periodically flushes it to the admin server.
type Reporter struct {
	config ReporterConfig
	client *http.Client
	logger *slog.Logger

	mu         sync.Mutex
	executions []ExecutionReport
	logs       []LogReport
	events     []EventReport
	registered bool
	cancel     context.CancelFunc
}

// NewReporter creates a new observability reporter.
func NewReporter(config ReporterConfig, logger *slog.Logger) *Reporter {
	if config.FlushInterval == 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	return &Reporter{
		config:     config,
		client:     &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
		executions: make([]ExecutionReport, 0),
		logs:       make([]LogReport, 0),
		events:     make([]EventReport, 0),
	}
}

// Start begins the background flush and heartbeat loops.
func (r *Reporter) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)

	// Register this instance with the admin
	go r.register(ctx)

	// Flush loop
	go func() {
		ticker := time.NewTicker(r.config.FlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				r.flush(context.Background()) // Final flush
				return
			case <-ticker.C:
				r.flush(ctx)
			}
		}
	}()

	// Heartbeat loop
	go func() {
		ticker := time.NewTicker(r.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.heartbeat(ctx)
			}
		}
	}()
}

// Stop shuts down the reporter, performing a final flush.
func (r *Reporter) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// ReportExecution buffers an execution record for the next flush.
func (r *Reporter) ReportExecution(exec ExecutionReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executions = append(r.executions, exec)
}

// ReportLog buffers a log entry for the next flush.
func (r *Reporter) ReportLog(log LogReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, log)
}

// ReportEvent buffers an event for the next flush.
func (r *Reporter) ReportEvent(event EventReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// flush sends all buffered data to the admin server.
func (r *Reporter) flush(ctx context.Context) {
	r.mu.Lock()
	execs := r.executions
	logs := r.logs
	events := r.events
	r.executions = make([]ExecutionReport, 0)
	r.logs = make([]LogReport, 0)
	r.events = make([]EventReport, 0)
	r.mu.Unlock()

	if len(execs) > 0 {
		r.sendBatch(ctx, "/api/v1/admin/ingest/executions", execs)
	}
	if len(logs) > 0 {
		r.sendBatch(ctx, "/api/v1/admin/ingest/logs", logs)
	}
	if len(events) > 0 {
		r.sendBatch(ctx, "/api/v1/admin/ingest/events", events)
	}
}

// sendBatch POSTs a batch of data to the admin server.
func (r *Reporter) sendBatch(ctx context.Context, path string, data any) {
	body, err := json.Marshal(map[string]any{
		"instance": r.config.InstanceName,
		"items":    data,
	})
	if err != nil {
		r.logger.Warn("Failed to marshal batch", "path", path, "error", err)
		return
	}

	url := r.config.AdminURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		r.logger.Warn("Failed to create request", "url", url, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Debug("Failed to send batch to admin", "url", url, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		r.logger.Warn("Admin rejected batch", "url", url, "status", resp.StatusCode)
	}
}

// register sends an initial registration to the admin server.
func (r *Reporter) register(ctx context.Context) {
	body, _ := json.Marshal(map[string]any{
		"instance_name": r.config.InstanceName,
		"registered_at": time.Now().UTC().Format(time.RFC3339),
	})

	url := r.config.AdminURL + "/api/v1/admin/instances/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Debug("Failed to register with admin", "url", url, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 {
		r.mu.Lock()
		r.registered = true
		r.mu.Unlock()
		r.logger.Info("Registered with admin server", "url", r.config.AdminURL)
	}
}

// heartbeat sends a periodic health check to the admin server.
func (r *Reporter) heartbeat(ctx context.Context) {
	body, _ := json.Marshal(map[string]any{
		"instance_name": r.config.InstanceName,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"status":        "healthy",
	})

	url := r.config.AdminURL + "/api/v1/admin/instances/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// ReporterFromEnv creates a Reporter from environment variables.
// Returns nil if WORKFLOW_ADMIN_URL is not set.
func ReporterFromEnv(logger *slog.Logger) *Reporter {
	adminURL := os.Getenv("WORKFLOW_ADMIN_URL")
	if adminURL == "" {
		return nil
	}

	cfg := DefaultReporterConfig()
	cfg.AdminURL = adminURL

	if v := os.Getenv("WORKFLOW_REPORTER_FLUSH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.FlushInterval = d
		}
	}

	return NewReporter(cfg, logger)
}

// IngestHandler handles incoming observability data from worker instances.
// It is registered on the admin server to receive batches from reporters.
type IngestHandler struct {
	store  IngestStore
	logger *slog.Logger
}

// IngestStore defines the storage interface for ingested observability data.
type IngestStore interface {
	IngestExecutions(ctx context.Context, instance string, items []ExecutionReport) error
	IngestLogs(ctx context.Context, instance string, items []LogReport) error
	IngestEvents(ctx context.Context, instance string, items []EventReport) error
	RegisterInstance(ctx context.Context, name string, registeredAt time.Time) error
	Heartbeat(ctx context.Context, name string, timestamp time.Time) error
}

// NewIngestHandler creates a new handler for admin ingest endpoints.
func NewIngestHandler(store IngestStore, logger *slog.Logger) *IngestHandler {
	return &IngestHandler{store: store, logger: logger}
}

// RegisterRoutes registers the ingest API routes on the given mux.
func (h *IngestHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/ingest/executions", h.handleIngestExecutions)
	mux.HandleFunc("POST /api/v1/admin/ingest/logs", h.handleIngestLogs)
	mux.HandleFunc("POST /api/v1/admin/ingest/events", h.handleIngestEvents)
	mux.HandleFunc("GET /api/v1/admin/ingest/health", h.handleHealth)
	mux.HandleFunc("POST /api/v1/admin/instances/register", h.handleRegister)
	mux.HandleFunc("POST /api/v1/admin/instances/heartbeat", h.handleHeartbeat)
}

func (h *IngestHandler) handleIngestExecutions(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Instance string            `json:"instance"`
		Items    []ExecutionReport `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := h.store.IngestExecutions(r.Context(), payload.Instance, payload.Items); err != nil {
		h.logger.Warn("Failed to ingest executions", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(payload.Items)})
}

func (h *IngestHandler) handleIngestLogs(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Instance string      `json:"instance"`
		Items    []LogReport `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := h.store.IngestLogs(r.Context(), payload.Instance, payload.Items); err != nil {
		h.logger.Warn("Failed to ingest logs", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(payload.Items)})
}

func (h *IngestHandler) handleIngestEvents(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Instance string        `json:"instance"`
		Items    []EventReport `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := h.store.IngestEvents(r.Context(), payload.Instance, payload.Items); err != nil {
		h.logger.Warn("Failed to ingest events", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(payload.Items)})
}

func (h *IngestHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *IngestHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		InstanceName string `json:"instance_name"`
		RegisteredAt string `json:"registered_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	t, _ := time.Parse(time.RFC3339, payload.RegisteredAt)
	if t.IsZero() {
		t = time.Now()
	}
	if err := h.store.RegisterInstance(r.Context(), payload.InstanceName, t); err != nil {
		h.logger.Warn("Failed to register instance", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

func (h *IngestHandler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		InstanceName string `json:"instance_name"`
		Timestamp    string `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	t, _ := time.Parse(time.RFC3339, payload.Timestamp)
	if t.IsZero() {
		t = time.Now()
	}
	if err := h.store.Heartbeat(r.Context(), payload.InstanceName, t); err != nil {
		h.logger.Warn("Failed to heartbeat", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
