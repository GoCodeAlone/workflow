package lsp

import (
	"strings"
	"testing"
)

func TestDiagnoseContent_ValidConfig(t *testing.T) {
	content := "modules:\n  - name: server\n    type: http.server\n    config:\n      port: 8080\n"
	diags := DiagnoseContent(content)
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d.Message)
		}
	}
}

func TestDiagnoseContent_InvalidStepType(t *testing.T) {
	content := "modules:\n  - name: thing\n    type: totally.unknown.type\n"
	diags := DiagnoseContent(content)
	found := false
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, "unknown module type") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error diagnostic for unknown module type, got: %v", diags)
	}
}

func TestDiagnoseContent_EmptyContent(t *testing.T) {
	diags := DiagnoseContent("")
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for empty content, got %d: %v", len(diags), diags)
	}
}

func TestCompleteAt(t *testing.T) {
	// Line 2 (0-indexed): "    type: " — cursor at end requesting module type completions
	content := "modules:\n  - name: server\n    type: "
	items := CompleteAt(content, 2, len("    type: "))
	if len(items) == 0 {
		t.Fatal("expected completions for module type field")
	}
	found := false
	for _, item := range items {
		if item.Label == "http.server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected http.server in completions, got: %v", items)
	}
}

func TestHoverAt(t *testing.T) {
	// Line 2 (0-indexed): "    type: http.server" — col 10 is on "http.server"
	content := "modules:\n  - name: server\n    type: http.server\n"
	result := HoverAt(content, 2, 10)
	if result == nil {
		t.Fatal("expected hover result for http.server module type")
	}
	if !strings.Contains(result.Content, "http.server") {
		t.Errorf("expected hover content to mention http.server, got: %s", result.Content)
	}
}
