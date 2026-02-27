package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGetSnippets_Count(t *testing.T) {
	snips := GetSnippets()
	if len(snips) < 20 {
		t.Errorf("expected at least 20 snippets, got %d", len(snips))
	}
}

func TestGetSnippets_NonEmptyFields(t *testing.T) {
	snips := GetSnippets()
	for _, s := range snips {
		if s.Name == "" {
			t.Errorf("snippet has empty Name: %+v", s)
		}
		if s.Prefix == "" {
			t.Errorf("snippet %q has empty Prefix", s.Name)
		}
		if len(s.Body) == 0 {
			t.Errorf("snippet %q has empty Body", s.Name)
		}
	}
}

func TestGetSnippets_HasModuleSnippets(t *testing.T) {
	snips := GetSnippets()
	prefixes := make(map[string]bool, len(snips))
	for _, s := range snips {
		prefixes[s.Prefix] = true
	}
	required := []string{
		"mod-http-server",
		"mod-jwt",
		"mod-broker",
		"mod-statemachine",
		"pipeline",
		"step-set",
		"step-http-call",
		"step-json-response",
		"trigger-http",
		"workflow-http",
		"app",
	}
	for _, p := range required {
		if !prefixes[p] {
			t.Errorf("missing snippet with prefix %q", p)
		}
	}
}

func TestExportSnippetsVSCode_ValidJSON(t *testing.T) {
	data, err := ExportSnippetsVSCode()
	if err != nil {
		t.Fatalf("VSCode export failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("VSCode export is empty")
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("VSCode export is not valid JSON: %v", err)
	}
	if len(m) == 0 {
		t.Fatal("VSCode export has no snippets")
	}
	// Each entry should have prefix and body.
	for name, v := range m {
		entry, ok := v.(map[string]any)
		if !ok {
			t.Errorf("snippet %q is not an object", name)
			continue
		}
		if entry["prefix"] == nil {
			t.Errorf("snippet %q missing prefix", name)
		}
		if entry["body"] == nil {
			t.Errorf("snippet %q missing body", name)
		}
	}
}

func TestExportSnippetsJetBrains_ValidXML(t *testing.T) {
	data, err := ExportSnippetsJetBrains()
	if err != nil {
		t.Fatalf("JetBrains export failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("JetBrains export is empty")
	}
	// Should be valid XML starting with XML declaration.
	s := string(data)
	if !strings.Contains(s, "<templateSet") {
		t.Error("JetBrains export should contain <templateSet>")
	}
	if !strings.Contains(s, "<template") {
		t.Error("JetBrains export should contain <template> elements")
	}
}

func TestExportSnippetsVSCode_BodyIsStringArray(t *testing.T) {
	data, _ := ExportSnippetsVSCode()
	var m map[string]map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	for name, entry := range m {
		body, ok := entry["body"]
		if !ok {
			continue
		}
		arr, ok := body.([]any)
		if !ok {
			t.Errorf("snippet %q body should be array, got %T", name, body)
			continue
		}
		for i, item := range arr {
			if _, ok := item.(string); !ok {
				t.Errorf("snippet %q body[%d] should be string, got %T", name, i, item)
			}
		}
	}
}
