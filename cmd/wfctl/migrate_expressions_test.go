package main

import (
	"strings"
	"testing"
)

func TestConvertGoTemplateExpr_SimpleDotPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{".name", "name"},
		{".body.name", "body.name"},
		{".trigger.status", "trigger.status"},
		{".steps", "steps"},
	}
	for _, tc := range tests {
		got, ok := convertGoTemplateExpr(tc.input)
		if !ok {
			t.Errorf("convertGoTemplateExpr(%q): expected ok=true", tc.input)
			continue
		}
		if got != tc.want {
			t.Errorf("convertGoTemplateExpr(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestConvertGoTemplateExpr_StepsDotPath(t *testing.T) {
	got, ok := convertGoTemplateExpr(".steps.parse-request.user_id")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != `steps["parse-request"]["user_id"]` {
		t.Errorf("got %q", got)
	}
}

func TestConvertGoTemplateExpr_IndexSteps(t *testing.T) {
	got, ok := convertGoTemplateExpr(`index .steps "my-step" "field"`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != `steps["my-step"]["field"]` {
		t.Errorf("got %q", got)
	}
}

func TestConvertGoTemplateExpr_Equality(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`eq .status "active"`, `status == "active"`},
		{`ne .status "active"`, `status != "active"`},
		{`gt .count 5`, `count > 5`},
		{`lt .count 5`, `count < 5`},
		{`ge .count 5`, `count >= 5`},
		{`le .count 5`, `count <= 5`},
	}
	for _, tc := range tests {
		got, ok := convertGoTemplateExpr(tc.input)
		if !ok {
			t.Errorf("convertGoTemplateExpr(%q): expected ok=true", tc.input)
			continue
		}
		if got != tc.want {
			t.Errorf("convertGoTemplateExpr(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestConvertGoTemplateExpr_And(t *testing.T) {
	got, ok := convertGoTemplateExpr(`and (eq .x "a") (gt .y 5)`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := `x == "a" && y > 5`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConvertGoTemplateExpr_Or(t *testing.T) {
	got, ok := convertGoTemplateExpr(`or (eq .x "a") (eq .x "b")`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := `x == "a" || x == "b"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConvertGoTemplateExpr_FuncCall(t *testing.T) {
	got, ok := convertGoTemplateExpr("upper .name")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "upper(name)" {
		t.Errorf("got %q", got)
	}
}

func TestMigrateExpressions_FullYAML(t *testing.T) {
	input := `pipelines:
  my-pipeline:
    steps:
      - name: set-name
        type: step.set
        config:
          event_type: "{{ .type }}"
          user_name: "{{ upper .name }}"
          status_check: "{{ eq .status \"active\" }}"
          skip_if: "{{ if eq .env \"prod\" }}true{{ end }}"
`
	result, stats := migrateExpressions(input)

	if stats.converted != 3 {
		t.Errorf("expected 3 converted, got %d", stats.converted)
	}

	// Verify specific conversions
	if !strings.Contains(result, `${ type }`) {
		t.Errorf("expected ${ type } in result, got:\n%s", result)
	}
	if !strings.Contains(result, `${ upper(name) }`) {
		t.Errorf("expected ${ upper(name) } in result, got:\n%s", result)
	}
	// In the raw YAML the quotes are escaped, so the output has \"active\"
	if !strings.Contains(result, `${ status == \"active\" }`) {
		t.Errorf("expected ${ status == \"active\" } in result, got:\n%s", result)
	}

	// Control block should remain unchanged
	if strings.Contains(result, `${ if`) {
		t.Errorf("control block should not be converted, got:\n%s", result)
	}
}

func TestMigrateExpressions_ControlBlocksPreserved(t *testing.T) {
	input := `value: "{{ if eq .env \"prod\" }}production{{ else }}staging{{ end }}"`
	result, stats := migrateExpressions(input)
	if stats.converted != 0 {
		t.Errorf("expected 0 converted (control blocks), got %d", stats.converted)
	}
	// Original should be preserved
	if !strings.Contains(result, "{{") {
		t.Errorf("expected original {{ }} blocks preserved, got:\n%s", result)
	}
}
