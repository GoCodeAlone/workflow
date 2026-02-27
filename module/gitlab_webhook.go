package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitEvent is the normalized representation of a git provider webhook event.
// Both the GitLab and GitHub webhook modules emit this structure.
type GitEvent struct {
	Provider   string `json:"provider"`
	Event      string `json:"event"`
	Ref        string `json:"ref"`
	Commit     string `json:"commit"`
	Author     string `json:"author"`
	Repository string `json:"repository"`
	URL        string `json:"url"`
	// MR / PR fields
	MRNumber int    `json:"mr_number,omitempty"`
	MRTitle  string `json:"mr_title,omitempty"`
	MRAction string `json:"mr_action,omitempty"`
}

// GitLabWebhookModule registers an HTTP route that receives GitLab webhook events,
// validates the X-Gitlab-Token secret, normalizes the event, and makes the
// resulting GitEvent available in the pipeline context.
//
// Config:
//
//   - name: gitlab-hooks
//     type: gitlab.webhook
//     config:
//     secret: "${GITLAB_WEBHOOK_SECRET}"
//     path: /webhooks/gitlab         # optional, default: /webhooks/gitlab
//     events: [push, merge_request, tag_push, pipeline]
type GitLabWebhookModule struct {
	name   string
	config map[string]any
	secret string
	path   string
	events map[string]bool
}

// NewGitLabWebhookModule creates a new gitlab.webhook module.
func NewGitLabWebhookModule(name string, cfg map[string]any) *GitLabWebhookModule {
	return &GitLabWebhookModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *GitLabWebhookModule) Name() string { return m.name }

// Init configures the module and registers it as a service.
func (m *GitLabWebhookModule) Init(app modular.Application) error {
	secret, _ := m.config["secret"].(string)
	m.secret = os.ExpandEnv(secret)

	m.path = "/webhooks/gitlab"
	if p, ok := m.config["path"].(string); ok && p != "" {
		m.path = p
	}

	m.events = make(map[string]bool)
	if evtsRaw, ok := m.config["events"].([]any); ok {
		for _, e := range evtsRaw {
			if s, ok := e.(string); ok {
				m.events[s] = true
			}
		}
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service provided by this module.
func (m *GitLabWebhookModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "GitLab webhook receiver: " + m.name,
			Instance:    m,
		},
	}
}

// RegisterRoutes registers the webhook HTTP route with the router.
// This is called by the engine bridge after Init.
func (m *GitLabWebhookModule) RegisterRoutes(router HTTPRouter) {
	router.AddRoute("POST", m.path, &gitLabWebhookHandler{module: m})
}

// gitLabWebhookHandler handles incoming GitLab webhook HTTP requests.
type gitLabWebhookHandler struct {
	module *GitLabWebhookModule
}

func (h *gitLabWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Validate secret token
	if h.module.secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != h.module.secret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	eventType := r.Header.Get("X-Gitlab-Event")
	if eventType == "" {
		http.Error(w, `{"error":"missing X-Gitlab-Event header"}`, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	event, err := h.module.parseEvent(eventType, body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to parse event: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Check if event type is in allowed list
	if len(h.module.events) > 0 && !h.module.events[event.Event] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored","reason":"event type not in filter list"}`))
		return
	}

	out, _ := json.Marshal(event)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out) //nolint:gosec // response is JSON-marshaled from trusted internal data
}

func (h *gitLabWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, params map[string]string) {
	h.Handle(w, r)
}

// ParseEvent normalizes a GitLab webhook payload into a GitEvent.
// This is exported for testing.
func (m *GitLabWebhookModule) ParseEvent(eventType string, body []byte) (*GitEvent, error) {
	return m.parseEvent(eventType, body)
}

// parseEvent normalizes a GitLab webhook payload.
func (m *GitLabWebhookModule) parseEvent(eventType string, body []byte) (*GitEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Normalize event type from the header value (e.g. "Push Hook" → "push")
	normalized := normalizeGitLabEventType(eventType)

	event := &GitEvent{
		Provider: "gitlab",
		Event:    normalized,
	}

	// Common fields
	if repo, ok := raw["project"].(map[string]any); ok {
		event.Repository, _ = repo["path_with_namespace"].(string)
		event.URL, _ = repo["web_url"].(string)
	}

	switch normalized {
	case "push", "tag_push":
		event.Ref, _ = raw["ref"].(string)
		event.Commit, _ = raw["checkout_sha"].(string)
		if commits, ok := raw["commits"].([]any); ok && len(commits) > 0 {
			if last, ok := commits[len(commits)-1].(map[string]any); ok {
				if author, ok := last["author"].(map[string]any); ok {
					event.Author, _ = author["name"].(string)
				}
			}
		}
		if ua, ok := raw["user_name"].(string); ok && event.Author == "" {
			event.Author = ua
		}

	case "merge_request":
		if oa, ok := raw["object_attributes"].(map[string]any); ok {
			event.Ref, _ = oa["source_branch"].(string)
			event.Commit, _ = oa["last_commit"].(map[string]any)["id"].(string)
			mrIID, _ := oa["iid"].(float64)
			event.MRNumber = int(mrIID)
			event.MRTitle, _ = oa["title"].(string)
			event.MRAction, _ = oa["action"].(string)
			// Normalize GitLab action to common vocabulary
			switch event.MRAction {
			case "opened":
				event.MRAction = "open"
			case "updated":
				event.MRAction = "update"
			case "merged":
				event.MRAction = "merge"
			case "closed":
				event.MRAction = "close"
			}
		}
		if ua, ok := raw["user"].(map[string]any); ok {
			event.Author, _ = ua["name"].(string)
		}

	case "pipeline":
		if oa, ok := raw["object_attributes"].(map[string]any); ok {
			event.Ref, _ = oa["ref"].(string)
			event.Commit, _ = oa["sha"].(string)
		}
		if commit, ok := raw["commit"].(map[string]any); ok {
			if author, ok := commit["author"].(map[string]any); ok {
				event.Author, _ = author["name"].(string)
			}
		}
	}

	return event, nil
}

// normalizeGitLabEventType converts the X-Gitlab-Event header value to a
// lower-case canonical event name.
func normalizeGitLabEventType(header string) string {
	lower := strings.ToLower(header)
	switch lower {
	case "push hook":
		return "push"
	case "tag push hook":
		return "tag_push"
	case "merge request hook":
		return "merge_request"
	case "pipeline hook":
		return "pipeline"
	case "note hook":
		return "note"
	case "job hook":
		return "job"
	default:
		// Strip " hook" suffix if present, replace spaces with underscores
		s := strings.TrimSuffix(lower, " hook")
		return strings.ReplaceAll(s, " ", "_")
	}
}

// GitLabWebhookParseStep is a pipeline step that parses a GitLab webhook from
// the HTTP request in the pipeline context.
//
//   - name: parse-webhook
//     type: step.gitlab_parse_webhook
//     config:
//     secret: "${GITLAB_WEBHOOK_SECRET}"   # optional; skips validation if empty
type GitLabWebhookParseStep struct {
	name   string
	secret string
}

// NewGitLabWebhookParseStepFactory returns a StepFactory for step.gitlab_parse_webhook.
func NewGitLabWebhookParseStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		secret, _ := config["secret"].(string)
		return &GitLabWebhookParseStep{
			name:   name,
			secret: os.ExpandEnv(secret),
		}, nil
	}
}

// Name returns the step name.
func (s *GitLabWebhookParseStep) Name() string { return s.name }

// Execute reads the HTTP request from pipeline context and parses the GitLab webhook.
func (s *GitLabWebhookParseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	req, _ := pc.Metadata["_http_request"].(*http.Request)
	if req == nil {
		return nil, fmt.Errorf("gitlab_parse_webhook step %q: no HTTP request in pipeline context", s.name)
	}

	// Validate token if secret is configured
	if s.secret != "" {
		token := req.Header.Get("X-Gitlab-Token")
		if token != s.secret {
			if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			}
			return &StepResult{Stop: true, Output: map[string]any{"error": "unauthorized"}}, nil
		}
	}

	eventType := req.Header.Get("X-Gitlab-Event")
	if eventType == "" {
		return nil, fmt.Errorf("gitlab_parse_webhook step %q: missing X-Gitlab-Event header", s.name)
	}

	// Read body (use cached if available)
	var body []byte
	if raw, ok := pc.Metadata["_raw_body"].([]byte); ok {
		body = raw
	} else if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("gitlab_parse_webhook step %q: failed to read body: %w", s.name, err)
		}
		pc.Metadata["_raw_body"] = body
	}

	m := &GitLabWebhookModule{}
	event, err := m.parseEvent(eventType, body)
	if err != nil {
		return nil, fmt.Errorf("gitlab_parse_webhook step %q: %w", s.name, err)
	}

	// Convert to map for pipeline context
	eventMap, err := toMap(event)
	if err != nil {
		return nil, err
	}

	return &StepResult{Output: eventMap}, nil
}

// toMap serializes a value to map[string]any via JSON round-trip.
func toMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
