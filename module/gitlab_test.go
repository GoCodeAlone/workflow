package module_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ---- Webhook event parsing ----

func TestGitLabWebhookParseEvent_Push(t *testing.T) {
	payload := `{
		"ref": "refs/heads/main",
		"checkout_sha": "abc123",
		"user_name": "alice",
		"commits": [{"author": {"name": "Alice"}}],
		"project": {"path_with_namespace": "group/repo", "web_url": "https://gitlab.com/group/repo"}
	}`

	m := newTestWebhookModule()
	event, err := m.ParseEvent("Push Hook", []byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEq(t, "provider", "gitlab", event.Provider)
	assertEq(t, "event", "push", event.Event)
	assertEq(t, "ref", "refs/heads/main", event.Ref)
	assertEq(t, "commit", "abc123", event.Commit)
	assertEq(t, "author", "Alice", event.Author)
	assertEq(t, "repository", "group/repo", event.Repository)
	assertEq(t, "url", "https://gitlab.com/group/repo", event.URL)
}

func TestGitLabWebhookParseEvent_TagPush(t *testing.T) {
	payload := `{
		"ref": "refs/tags/v1.0",
		"checkout_sha": "deadbeef",
		"user_name": "bob",
		"commits": [],
		"project": {"path_with_namespace": "org/service", "web_url": "https://gitlab.com/org/service"}
	}`

	m := newTestWebhookModule()
	event, err := m.ParseEvent("Tag Push Hook", []byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEq(t, "event", "tag_push", event.Event)
	assertEq(t, "ref", "refs/tags/v1.0", event.Ref)
	assertEq(t, "commit", "deadbeef", event.Commit)
}

func TestGitLabWebhookParseEvent_MergeRequest(t *testing.T) {
	payload := `{
		"user": {"name": "carol"},
		"object_attributes": {
			"iid": 7,
			"title": "Add feature",
			"action": "opened",
			"source_branch": "feature-x",
			"last_commit": {"id": "fff000"}
		},
		"project": {"path_with_namespace": "ns/proj", "web_url": "https://gitlab.com/ns/proj"}
	}`

	m := newTestWebhookModule()
	event, err := m.ParseEvent("Merge Request Hook", []byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEq(t, "event", "merge_request", event.Event)
	assertEq(t, "mr_action", "open", event.MRAction)
	if event.MRNumber != 7 {
		t.Errorf("mr_number: expected 7, got %d", event.MRNumber)
	}
	assertEq(t, "mr_title", "Add feature", event.MRTitle)
	assertEq(t, "author", "carol", event.Author)
}

func TestGitLabWebhookParseEvent_Pipeline(t *testing.T) {
	payload := `{
		"object_attributes": {"ref": "main", "sha": "beefdead"},
		"commit": {"author": {"name": "dave"}},
		"project": {"path_with_namespace": "a/b", "web_url": "https://gitlab.com/a/b"}
	}`

	m := newTestWebhookModule()
	event, err := m.ParseEvent("Pipeline Hook", []byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEq(t, "event", "pipeline", event.Event)
	assertEq(t, "ref", "main", event.Ref)
	assertEq(t, "commit", "beefdead", event.Commit)
	assertEq(t, "author", "dave", event.Author)
}

func TestGitLabWebhookParseEvent_InvalidJSON(t *testing.T) {
	m := newTestWebhookModule()
	_, err := m.ParseEvent("Push Hook", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---- Webhook secret validation ----

func TestGitLabWebhookHTTP_ValidSecret(t *testing.T) {
	payload := `{"ref":"refs/heads/main","checkout_sha":"abc","user_name":"x","commits":[],"project":{"path_with_namespace":"g/r","web_url":"https://g.com"}}`

	req := httptest.NewRequest("POST", "/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "mysecret")
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	rw := httptest.NewRecorder()
	handler := newTestWebhookHandler("mysecret")
	handler.Handle(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGitLabWebhookHTTP_InvalidSecret(t *testing.T) {
	req := httptest.NewRequest("POST", "/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "wrong")
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	rw := httptest.NewRecorder()
	handler := newTestWebhookHandler("mysecret")
	handler.Handle(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rw.Code)
	}
}

func TestGitLabWebhookHTTP_MissingEventHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "mysecret")
	// No X-Gitlab-Event header

	rw := httptest.NewRecorder()
	handler := newTestWebhookHandler("mysecret")
	handler.Handle(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rw.Code)
	}
}

// ---- GitLab client mock mode ----

func TestGitLabClientMock_TriggerPipeline(t *testing.T) {
	client := module.NewGitLabClient("mock://", "token")
	pipeline, err := client.TriggerPipeline("group/project", "main", map[string]string{"ENV": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pipeline.ID == 0 {
		t.Error("expected non-zero pipeline ID")
	}
	if pipeline.Status == "" {
		t.Error("expected non-empty status")
	}
	assertEq(t, "ref", "main", pipeline.Ref)
}

func TestGitLabClientMock_GetPipeline(t *testing.T) {
	client := module.NewGitLabClient("mock://", "token")
	pipeline, err := client.GetPipeline("group/project", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pipeline.ID != 42 {
		t.Errorf("expected pipeline ID 42, got %d", pipeline.ID)
	}
	assertEq(t, "status", "success", pipeline.Status)
}

func TestGitLabClientMock_CreateMergeRequest(t *testing.T) {
	client := module.NewGitLabClient("mock://", "token")
	mr, err := client.CreateMergeRequest("group/project", module.MROptions{
		SourceBranch: "feature-y",
		TargetBranch: "main",
		Title:        "My MR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEq(t, "title", "My MR", mr.Title)
	assertEq(t, "state", "opened", mr.State)
	assertEq(t, "source_branch", "feature-y", mr.SourceBranch)
}

func TestGitLabClientMock_CommentOnMR(t *testing.T) {
	client := module.NewGitLabClient("mock://", "token")
	err := client.CommentOnMR("group/project", 1, "LGTM!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- Event normalization ----

func TestGitLabEventNormalization_MRActions(t *testing.T) {
	cases := []struct {
		action   string
		expected string
	}{
		{"opened", "open"},
		{"updated", "update"},
		{"merged", "merge"},
		{"closed", "close"},
	}

	for _, tc := range cases {
		payload, _ := json.Marshal(map[string]any{
			"user":              map[string]any{"name": "test"},
			"object_attributes": map[string]any{"iid": 1, "title": "t", "action": tc.action, "source_branch": "x", "last_commit": map[string]any{"id": "abc"}},
			"project":           map[string]any{"path_with_namespace": "a/b", "web_url": "https://x.com"},
		})

		m := newTestWebhookModule()
		event, err := m.ParseEvent("Merge Request Hook", payload)
		if err != nil {
			t.Fatalf("action=%s: unexpected error: %v", tc.action, err)
		}
		if event.MRAction != tc.expected {
			t.Errorf("action=%s: expected normalized %q, got %q", tc.action, tc.expected, event.MRAction)
		}
	}
}

// ---- Helpers ----

func newTestWebhookModule() *module.GitLabWebhookModule {
	return module.NewGitLabWebhookModule("test-webhook", map[string]any{})
}

type testWebhookHTTPHandler struct {
	secret string
}

func newTestWebhookHandler(secret string) *testWebhookHTTPHandler {
	return &testWebhookHTTPHandler{secret: secret}
}

func (h *testWebhookHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Validate secret
	if h.secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != h.secret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}
	eventType := r.Header.Get("X-Gitlab-Event")
	if eventType == "" {
		http.Error(w, `{"error":"missing event header"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func assertEq(t *testing.T, field, expected, got string) {
	t.Helper()
	if got != expected {
		t.Errorf("%s: expected %q, got %q", field, expected, got)
	}
}
