package module

import (
	"strings"
	"testing"
)

func newMockAppWithGitLabClient(clientName string) *MockApplication {
	app := NewMockApplication()
	client := NewGitLabClient("mock://", "test-token")
	app.Services[clientName] = client
	return app
}

// ── gitlab_trigger_pipeline ────────────────────────────────────────────────

func TestGitLabTriggerPipeline_MissingClient(t *testing.T) {
	_, err := NewGitLabTriggerPipelineStepFactory()("trig", map[string]any{
		"project": "group/proj",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
	if !strings.Contains(err.Error(), "'client' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabTriggerPipeline_MissingProject(t *testing.T) {
	_, err := NewGitLabTriggerPipelineStepFactory()("trig", map[string]any{
		"client": "gl",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "'project' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabTriggerPipeline_ValidConfig(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	step, err := NewGitLabTriggerPipelineStepFactory()("trig", map[string]any{
		"client":  "gl",
		"project": "group/proj",
		"ref":     "main",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "trig" {
		t.Errorf("expected name 'trig', got %q", step.Name())
	}
}

// ── gitlab_pipeline_status ─────────────────────────────────────────────────

func TestGitLabPipelineStatus_MissingClient(t *testing.T) {
	_, err := NewGitLabPipelineStatusStepFactory()("status", map[string]any{
		"project": "group/proj",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
	if !strings.Contains(err.Error(), "'client' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabPipelineStatus_MissingProject(t *testing.T) {
	_, err := NewGitLabPipelineStatusStepFactory()("status", map[string]any{
		"client": "gl",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "'project' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabPipelineStatus_InvalidPipelineID(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	_, err := NewGitLabPipelineStatusStepFactory()("status", map[string]any{
		"client":      "gl",
		"project":     "group/proj",
		"pipeline_id": "not-a-number",
	}, app)
	if err == nil {
		t.Fatal("expected error for invalid pipeline_id")
	}
}

func TestGitLabPipelineStatus_ValidConfig(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	step, err := NewGitLabPipelineStatusStepFactory()("status", map[string]any{
		"client":      "gl",
		"project":     "group/proj",
		"pipeline_id": 42,
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "status" {
		t.Errorf("expected name 'status', got %q", step.Name())
	}
}

// ── gitlab_create_mr ───────────────────────────────────────────────────────

func TestGitLabCreateMR_MissingClient(t *testing.T) {
	_, err := NewGitLabCreateMRStepFactory()("mr", map[string]any{
		"project":       "group/proj",
		"source_branch": "feature-x",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
	if !strings.Contains(err.Error(), "'client' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabCreateMR_MissingProject(t *testing.T) {
	_, err := NewGitLabCreateMRStepFactory()("mr", map[string]any{
		"client":        "gl",
		"source_branch": "feature-x",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "'project' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabCreateMR_MissingSourceBranch(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	_, err := NewGitLabCreateMRStepFactory()("mr", map[string]any{
		"client":  "gl",
		"project": "group/proj",
	}, app)
	if err == nil {
		t.Fatal("expected error for missing source_branch")
	}
	if !strings.Contains(err.Error(), "'source_branch' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabCreateMR_ValidConfig(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	step, err := NewGitLabCreateMRStepFactory()("mr", map[string]any{
		"client":        "gl",
		"project":       "group/proj",
		"source_branch": "feature-x",
		"target_branch": "main",
		"title":         "Feature X",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "mr" {
		t.Errorf("expected name 'mr', got %q", step.Name())
	}
}

// ── gitlab_mr_comment ──────────────────────────────────────────────────────

func TestGitLabMRComment_MissingClient(t *testing.T) {
	_, err := NewGitLabMRCommentStepFactory()("comment", map[string]any{
		"project": "group/proj",
		"body":    "LGTM",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
	if !strings.Contains(err.Error(), "'client' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabMRComment_MissingBody(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	_, err := NewGitLabMRCommentStepFactory()("comment", map[string]any{
		"client":  "gl",
		"project": "group/proj",
		"mr_iid":  1,
	}, app)
	if err == nil {
		t.Fatal("expected error for missing body")
	}
	if !strings.Contains(err.Error(), "'body' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGitLabMRComment_InvalidMrIID(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	_, err := NewGitLabMRCommentStepFactory()("comment", map[string]any{
		"client":  "gl",
		"project": "group/proj",
		"mr_iid":  "not-a-number",
		"body":    "LGTM",
	}, app)
	if err == nil {
		t.Fatal("expected error for invalid mr_iid")
	}
}

func TestGitLabMRComment_ValidConfig(t *testing.T) {
	app := newMockAppWithGitLabClient("gl")
	step, err := NewGitLabMRCommentStepFactory()("comment", map[string]any{
		"client":  "gl",
		"project": "group/proj",
		"mr_iid":  42,
		"body":    "Pipeline passed!",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "comment" {
		t.Errorf("expected name 'comment', got %q", step.Name())
	}
}
