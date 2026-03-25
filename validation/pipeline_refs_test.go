package validation_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/validation"
)

// TestValidatePipelineTemplateRefs_ValidRefs ensures no warnings are produced for
// well-formed pipelines where all step references are correct.
func TestValidatePipelineTemplateRefs_ValidRefs(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "query",
					"type": "step.db_query",
					"config": map[string]any{
						"database": "db",
						"query":    "SELECT id, name FROM users WHERE id = $1",
						"mode":     "single",
					},
				},
				map[string]any{
					"name": "respond",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"user_id":   "{{ .steps.query.row.id }}",
							"user_name": "{{ .steps.query.row.name }}",
						},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if result.HasIssues() {
		t.Errorf("expected no issues for valid pipeline, got warnings=%v errors=%v", result.Warnings, result.Errors)
	}
}

// TestValidatePipelineTemplateRefs_MissingStep checks that referencing a step that
// does not exist in the pipeline produces a warning.
func TestValidatePipelineTemplateRefs_MissingStep(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "respond",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"x": "{{ .steps.nonexistent.value }}",
						},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if len(result.Warnings) == 0 {
		t.Error("expected warning for reference to nonexistent step")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "nonexistent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("warning should mention 'nonexistent' step, got: %v", result.Warnings)
	}
}

// TestValidatePipelineTemplateRefs_ForwardRef checks that referencing a step that
// appears later in the pipeline produces a warning.
func TestValidatePipelineTemplateRefs_ForwardRef(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "step-a",
					"type": "step.set",
					"config": map[string]any{
						// References step-b which comes AFTER step-a
						"values": map[string]any{"x": "{{ .steps.step_b.value }}"},
					},
				},
				map[string]any{
					"name":   "step_b",
					"type":   "step.set",
					"config": map[string]any{"values": map[string]any{"value": "hello"}},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if len(result.Warnings) == 0 {
		t.Error("expected warning for forward reference to later step")
	}
}

// TestValidatePipelineTemplateRefs_UnknownOutputField checks that referencing an
// output field that is not declared in a step's schema produces a warning.
func TestValidatePipelineTemplateRefs_UnknownOutputField(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name":   "query",
					"type":   "step.db_query",
					"config": map[string]any{"mode": "single"},
				},
				map[string]any{
					"name": "respond",
					"type": "step.set",
					"config": map[string]any{
						// "rows" is invalid for mode=single (should be "row")
						"values": map[string]any{"x": "{{ .steps.query.rows }}"},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if len(result.Warnings) == 0 {
		t.Error("expected warning for invalid output field 'rows' on single-mode db_query")
	}
}

// TestValidatePipelineTemplateRefs_KnownOutputField verifies that referencing a
// known output field does NOT produce a warning.
func TestValidatePipelineTemplateRefs_KnownOutputField(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name":   "query",
					"type":   "step.db_query",
					"config": map[string]any{"mode": "single"},
				},
				map[string]any{
					"name": "respond",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"x": "{{ .steps.query.row }}"},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	for _, w := range result.Warnings {
		if strings.Contains(w, "declares outputs") {
			t.Errorf("unexpected output field warning for known field: %s", w)
		}
	}
}

// TestValidatePipelineTemplateRefs_SelfReference checks that a step referencing
// its own output produces a warning.
func TestValidatePipelineTemplateRefs_SelfReference(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "self",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"x": "{{ .steps.self.value }}"},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if len(result.Warnings) == 0 {
		t.Error("expected warning for self-reference")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "references itself") || strings.Contains(w, "self") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("warning should mention self-reference, got: %v", result.Warnings)
	}
}

// TestValidatePipelineTemplateRefs_HyphenatedStep ensures that a step with a
// hyphenated name correctly generates a warning when referenced via dot-access,
// and does NOT produce spurious output-field or unknown-step warnings alongside it.
func TestValidatePipelineTemplateRefs_HyphenatedStep(t *testing.T) {
	pipelines := map[string]any{
		"api": map[string]any{
			"steps": []any{
				map[string]any{
					"name":   "my-step",
					"type":   "step.set",
					"config": map[string]any{"values": map[string]any{"val": "ok"}},
				},
				map[string]any{
					"name": "next",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"x": "{{ .steps.my-step.val }}"},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	// Should warn about hyphenated dot-access syntax
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "hyphenated") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hyphenated dot-access warning, got: %v", result.Warnings)
	}
	// Should NOT produce spurious "does not exist" or output-field warnings —
	// the single hyphen warning is sufficient guidance for the user.
	for _, w := range result.Warnings {
		if strings.Contains(w, "does not exist") || strings.Contains(w, "declares outputs") {
			t.Errorf("unexpected spurious warning alongside hyphen warning: %s", w)
		}
	}
}

// TestValidatePipelineTemplateRefs_EmptyPipelines ensures no panic on empty input.
func TestValidatePipelineTemplateRefs_EmptyPipelines(t *testing.T) {
	result := validation.ValidatePipelineTemplateRefs(nil)
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.HasIssues() {
		t.Errorf("expected no issues for nil pipelines, got: %+v", result)
	}

	result = validation.ValidatePipelineTemplateRefs(map[string]any{})
	if result.HasIssues() {
		t.Errorf("expected no issues for empty pipelines, got: %+v", result)
	}
}

// TestValidatePipelineTemplateRefs_MultiplePipelines checks that multiple pipelines
// are all validated and results are aggregated.
func TestValidatePipelineTemplateRefs_MultiplePipelines(t *testing.T) {
	pipelines := map[string]any{
		"pipeline-a": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "step1",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"x": "{{ .steps.missing.val }}"},
					},
				},
			},
		},
		"pipeline-b": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "ok-step",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"y": "static"},
					},
				},
			},
		},
	}
	result := validation.ValidatePipelineTemplateRefs(pipelines)
	if len(result.Warnings) == 0 {
		t.Error("expected at least one warning from pipeline-a")
	}
	// pipeline-b should not contribute warnings
	foundPipelineA := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "pipeline-a") {
			foundPipelineA = true
		}
		if strings.Contains(w, "pipeline-b") {
			t.Errorf("unexpected warning for pipeline-b: %s", w)
		}
	}
	if !foundPipelineA {
		t.Errorf("expected warning mentioning 'pipeline-a', got: %v", result.Warnings)
	}
}

// TestExtractSQLColumns verifies SQL column extraction from SELECT statements.
func TestExtractSQLColumns(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected []string
	}{
		{
			name:     "simple columns",
			query:    "SELECT id, name FROM users",
			expected: []string{"id", "name"},
		},
		{
			name:     "table.column notation",
			query:    "SELECT u.id, u.email FROM users u",
			expected: []string{"id", "email"},
		},
		{
			name:     "AS alias",
			query:    "SELECT id AS user_id, name AS user_name FROM users",
			expected: []string{"user_id", "user_name"},
		},
		{
			name:     "DISTINCT",
			query:    "SELECT DISTINCT id, name FROM users",
			expected: []string{"id", "name"},
		},
		{
			name:     "function with alias",
			query:    "SELECT COALESCE(name, 'unknown') AS display_name FROM users",
			expected: []string{"display_name"},
		},
		{
			name:     "no FROM clause",
			query:    "INSERT INTO users (name) VALUES ('test')",
			expected: nil,
		},
		{
			name:     "wildcard",
			query:    "SELECT * FROM users",
			expected: nil, // star is filtered
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validation.ExtractSQLColumns(tc.query)
			if len(got) != len(tc.expected) {
				t.Errorf("expected columns %v, got %v", tc.expected, got)
				return
			}
			for i, col := range tc.expected {
				if got[i] != col {
					t.Errorf("column[%d]: expected %q, got %q", i, col, got[i])
				}
			}
		})
	}
}

// TestRefValidationResult_HasIssues checks the HasIssues helper.
func TestRefValidationResult_HasIssues(t *testing.T) {
	r := &validation.RefValidationResult{}
	if r.HasIssues() {
		t.Error("expected HasIssues()=false for empty result")
	}
	r.Warnings = append(r.Warnings, "some warning")
	if !r.HasIssues() {
		t.Error("expected HasIssues()=true after adding warning")
	}
	r2 := &validation.RefValidationResult{}
	r2.Errors = append(r2.Errors, "some error")
	if !r2.HasIssues() {
		t.Error("expected HasIssues()=true after adding error")
	}
}
