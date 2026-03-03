package module

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticFileStep_ServesFile(t *testing.T) {
	// Write a temporary file to serve.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "spec.yaml")
	content := "openapi: 3.0.0\ninfo:\n  title: Test\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	factory := NewStaticFileStepFactory()
	step, err := factory("serve_spec", map[string]any{
		"file":          filePath,
		"content_type":  "application/yaml",
		"cache_control": "public, max-age=3600",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("expected Content-Type application/yaml, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("expected Cache-Control header, got %q", cc)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Errorf("expected body %q, got %q", content, string(body))
	}

	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true")
	}
}

func TestStaticFileStep_NoHTTPWriter(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "data.json")
	content := `{"key":"value"}`
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	factory := NewStaticFileStepFactory()
	step, err := factory("serve_json", map[string]any{
		"file":         filePath,
		"content_type": "application/json",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, map[string]any{})
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["body"] != content {
		t.Errorf("expected body %q, got %q", content, result.Output["body"])
	}
	if result.Output["content_type"] != "application/json" {
		t.Errorf("unexpected content_type: %v", result.Output["content_type"])
	}
}

func TestStaticFileStep_MissingFile(t *testing.T) {
	factory := NewStaticFileStepFactory()
	_, err := factory("bad_step", map[string]any{
		"file":         "",
		"content_type": "text/plain",
	}, nil)
	if err == nil {
		t.Error("expected error for missing 'file' config")
	}
}

func TestStaticFileStep_MissingContentType(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "data.txt")
	_ = os.WriteFile(filePath, []byte("hello"), 0o600)

	factory := NewStaticFileStepFactory()
	_, err := factory("bad_step", map[string]any{
		"file": filePath,
	}, nil)
	if err == nil {
		t.Error("expected error for missing 'content_type' config")
	}
}

func TestStaticFileStep_FileNotFound(t *testing.T) {
	factory := NewStaticFileStepFactory()
	_, err := factory("bad_step", map[string]any{
		"file":         "/nonexistent/path/file.yaml",
		"content_type": "application/yaml",
	}, nil)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
