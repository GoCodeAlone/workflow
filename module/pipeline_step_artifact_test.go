package module

import (
	"strings"
	"testing"
)

// ── artifact_push ──────────────────────────────────────────────────────────

func TestArtifactPushStep_MissingSourcePath(t *testing.T) {
	_, err := NewArtifactPushStepFactory()("push", map[string]any{
		"key": "build-output",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing source_path")
	}
	if !strings.Contains(err.Error(), "'source_path' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArtifactPushStep_MissingKey(t *testing.T) {
	_, err := NewArtifactPushStepFactory()("push", map[string]any{
		"source_path": "/tmp/output.bin",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "'key' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArtifactPushStep_ValidConfig(t *testing.T) {
	step, err := NewArtifactPushStepFactory()("push", map[string]any{
		"source_path": "/tmp/output.bin",
		"key":         "build-output",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "push" {
		t.Errorf("expected name 'push', got %q", step.Name())
	}
}

// ── artifact_pull ──────────────────────────────────────────────────────────

func TestArtifactPullStep_MissingSource(t *testing.T) {
	_, err := NewArtifactPullStepFactory()("pull", map[string]any{
		"dest": "/tmp/output",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "'source' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArtifactPullStep_InvalidSource(t *testing.T) {
	_, err := NewArtifactPullStepFactory()("pull", map[string]any{
		"source": "ftp",
		"dest":   "/tmp/output",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
	if !strings.Contains(err.Error(), "invalid source") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArtifactPullStep_MissingDest(t *testing.T) {
	_, err := NewArtifactPullStepFactory()("pull", map[string]any{
		"source": "previous_execution",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing dest")
	}
	if !strings.Contains(err.Error(), "'dest' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArtifactPullStep_ValidConfig(t *testing.T) {
	step, err := NewArtifactPullStepFactory()("pull", map[string]any{
		"source":       "previous_execution",
		"dest":         "/tmp/artifacts",
		"execution_id": "exec-123",
		"key":          "build-output",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "pull" {
		t.Errorf("expected name 'pull', got %q", step.Name())
	}
}

func TestArtifactPullStep_AllValidSources(t *testing.T) {
	tests := []struct {
		source string
		extra  map[string]any
	}{
		{"previous_execution", map[string]any{"execution_id": "e1", "key": "k"}},
		{"url", map[string]any{"url": "http://example.com/art.zip"}},
		{"s3", map[string]any{"bucket": "my-bucket", "key": "artifacts/out.zip"}},
	}
	for _, tc := range tests {
		cfg := map[string]any{"source": tc.source, "dest": "/tmp/out"}
		for k, v := range tc.extra {
			cfg[k] = v
		}
		_, err := NewArtifactPullStepFactory()("pull", cfg, nil)
		if err != nil {
			t.Errorf("source %q: unexpected error: %v", tc.source, err)
		}
	}
}
