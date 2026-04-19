package builder_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// mockBuilder implements Builder for contract verification.
type mockBuilder struct{ name string }

func (m *mockBuilder) Name() string { return m.name }
func (m *mockBuilder) Validate(_ builder.Config) error {
	return nil
}
func (m *mockBuilder) Build(_ context.Context, _ builder.Config, out *builder.Outputs) error {
	out.Artifacts = append(out.Artifacts, builder.Artifact{
		Name:  "test-binary",
		Kind:  "binary",
		Paths: []string{"/tmp/test-binary"},
	})
	return nil
}
func (m *mockBuilder) SecurityLint(_ builder.Config) []builder.Finding {
	return []builder.Finding{{Severity: "info", Message: "mock finding"}}
}

func TestBuilderInterface(t *testing.T) {
	var b builder.Builder = &mockBuilder{name: "mock"}
	if b.Name() != "mock" {
		t.Fatalf("want name=mock, got %q", b.Name())
	}

	cfg := builder.Config{
		TargetName: "my-binary",
		Path:       "./cmd/server",
		Fields:     map[string]any{"ldflags": "-s -w"},
		Env:        map[string]string{"CGO_ENABLED": "0"},
	}

	if err := b.Validate(cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}

	out := &builder.Outputs{}
	if err := b.Build(context.Background(), cfg, out); err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].Kind != "binary" {
		t.Fatalf("unexpected artifacts: %+v", out.Artifacts)
	}

	findings := b.SecurityLint(cfg)
	if len(findings) == 0 {
		t.Fatal("expected at least one finding from mock")
	}
	if findings[0].Severity != "info" {
		t.Fatalf("want severity=info, got %q", findings[0].Severity)
	}
}
