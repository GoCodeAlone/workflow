package lsp

import (
	"testing"
)

func TestParseTemplateExprAt(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		char      int
		wantNil   bool
		namespace string
		stepName  string
		subField  string
		prefix    string
	}{
		{
			name:      "top-level dot",
			line:      `        value: "{{ .`,
			char:      22,
			namespace: "",
		},
		{
			name:      "steps namespace",
			line:      `        value: "{{ .steps.`,
			char:      28,
			namespace: "steps",
			stepName:  "",
		},
		{
			name:      "step output keys",
			line:      `        value: "{{ .steps.lookup.`,
			char:      35,
			namespace: "steps",
			stepName:  "lookup",
		},
		{
			name:      "step output prefix",
			line:      `        value: "{{ .steps.lookup.ro`,
			char:      37,
			namespace: "steps",
			stepName:  "lookup",
			prefix:    "ro",
		},
		{
			name:      "trigger namespace",
			line:      `        value: "{{ .trigger.`,
			char:      30,
			namespace: "trigger",
		},
		{
			name:      "trigger subfield",
			line:      `        value: "{{ .trigger.path_params.`,
			char:      42,
			namespace: "trigger",
			subField:  "path_params",
		},
		{
			name:      "body namespace",
			line:      `        value: "{{ .body.`,
			char:      27,
			namespace: "body",
		},
		{
			name:      "body nested",
			line:      `        value: "{{ .body.address.`,
			char:      35,
			namespace: "body",
			subField:  "address",
		},
		{
			name:      "meta namespace",
			line:      `        value: "{{ .meta.`,
			char:      27,
			namespace: "meta",
		},
		{
			name:      "index syntax",
			line:      `        value: '{{ index .steps "lookup" "`,
			char:      44,
			namespace: "steps",
			stepName:  "lookup",
		},
		{
			name:      "step function",
			line:      `        value: '{{ step "lookup" "`,
			char:      37,
			namespace: "steps",
			stepName:  "lookup",
		},
		{
			name:      "pipe chain - parse before pipe",
			line:      `        value: "{{ .steps.lookup.name | default `,
			char:      51,
			namespace: "steps",
			stepName:  "lookup",
			prefix:    "name",
		},
		{
			name:    "outside template",
			line:    `        value: "hello world"`,
			char:    20,
			wantNil: true,
		},
		{
			name:    "after closed template",
			line:    `        value: "{{ .foo }} bar`,
			char:    30,
			wantNil: true,
		},
		{
			name:      "cursor at opening",
			line:      `        value: "{{ `,
			char:      21,
			namespace: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTemplateExprAt(tt.line, tt.char)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Namespace != tt.namespace {
				t.Errorf("Namespace: got %q, want %q", result.Namespace, tt.namespace)
			}
			if result.StepName != tt.stepName {
				t.Errorf("StepName: got %q, want %q", result.StepName, tt.stepName)
			}
			if result.SubField != tt.subField {
				t.Errorf("SubField: got %q, want %q", result.SubField, tt.subField)
			}
			if result.FieldPrefix != tt.prefix {
				t.Errorf("FieldPrefix: got %q, want %q", result.FieldPrefix, tt.prefix)
			}
		})
	}
}

func TestParseExprAt(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		char      int
		wantNil   bool
		namespace string
		stepName  string
		prefix    string
	}{
		{
			name:      "empty expr",
			line:      `        value: "${ `,
			char:      20,
			namespace: "",
		},
		{
			name:      "steps namespace bracket",
			line:      `        value: "${ steps["`,
			char:      26,
			namespace: "steps",
			stepName:  "",
		},
		{
			name:      "steps with step name",
			line:      `        value: "${ steps["parse"]`,
			char:      33,
			namespace: "steps",
			stepName:  "parse",
		},
		{
			name:      "trigger namespace",
			line:      `        value: "${ trigger["`,
			char:      28,
			namespace: "trigger",
		},
		{
			name:      "body namespace",
			line:      `        value: "${ body["`,
			char:      25,
			namespace: "body",
		},
		{
			name:      "meta namespace",
			line:      `        value: "${ meta["`,
			char:      25,
			namespace: "meta",
		},
		{
			name:    "not in expr - after close brace",
			line:    `        value: "${ name } rest`,
			char:    28,
			wantNil: true,
		},
		{
			name:    "not in expr - no open",
			line:    `        value: "hello"`,
			char:    20,
			wantNil: true,
		},
		{
			name:      "function call prefix",
			line:      `        value: "${ uppe`,
			char:      23,
			namespace: "",
			prefix:    "uppe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseExprAt(tt.line, tt.char)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Namespace != tt.namespace {
				t.Errorf("Namespace: got %q, want %q", result.Namespace, tt.namespace)
			}
			if result.StepName != tt.stepName {
				t.Errorf("StepName: got %q, want %q", result.StepName, tt.stepName)
			}
			if result.FieldPrefix != tt.prefix {
				t.Errorf("FieldPrefix: got %q, want %q", result.FieldPrefix, tt.prefix)
			}
		})
	}
}

func TestIsInExpr(t *testing.T) {
	tests := []struct {
		name string
		line string
		char int
		want bool
	}{
		{"in expr", `value: "${ name `, 16, true},
		{"after close", `value: "${ name }"`, 18, false},
		{"no open", `value: "hello"`, 10, false},
		{"at open", `value: "${`, 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := []string{tt.line}
			got := isInExpr(lines, 0, tt.char)
			if got != tt.want {
				t.Errorf("isInExpr(%q, 0, %d) = %v, want %v", tt.line, tt.char, got, tt.want)
			}
		})
	}
}
