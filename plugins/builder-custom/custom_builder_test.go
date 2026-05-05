package buildercustom_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/builder"
	buildercustom "github.com/GoCodeAlone/workflow/plugins/builder-custom"
)

func TestCustomBuilder_Name(t *testing.T) {
	b := buildercustom.New()
	if b.Name() != "custom" {
		t.Fatalf("want name=custom, got %q", b.Name())
	}
}

func TestCustomBuilder_Validate_MissingCommand(t *testing.T) {
	b := buildercustom.New()
	err := b.Validate(builder.Config{TargetName: "gen", Fields: map[string]any{}})
	if err == nil {
		t.Fatal("want error for missing command")
	}
}

func TestCustomBuilder_Validate_OK(t *testing.T) {
	b := buildercustom.New()
	err := b.Validate(builder.Config{
		TargetName: "gen",
		Fields:     map[string]any{"command": "make build"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCustomBuilder_Build_DryRun(t *testing.T) {
	b := buildercustom.New()
	cfg := builder.Config{
		TargetName: "gen",
		Fields: map[string]any{
			"command": "make build",
			"outputs": []any{"./dist/app"},
			"env":     map[string]any{"BUILD_ENV": "test"},
			"timeout": "30s",
		},
	}
	out := &builder.Outputs{}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := b.Build(context.Background(), cfg, out); err != nil {
		t.Fatalf("dry-run build: %v", err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].Kind != "other" {
		t.Fatalf("expected 1 artifact of kind=other, got %+v", out.Artifacts)
	}
}

func TestCustomBuilder_SecurityLint_AlwaysWarns(t *testing.T) {
	b := buildercustom.New()
	cfg := builder.Config{
		TargetName: "gen",
		Fields:     map[string]any{"command": "make build"},
	}
	findings := b.SecurityLint(cfg)
	if len(findings) == 0 {
		t.Fatal("want at least one finding from custom builder")
	}
	for _, f := range findings {
		if f.Severity == "warn" {
			return
		}
	}
	t.Fatal("want warn severity finding")
}
