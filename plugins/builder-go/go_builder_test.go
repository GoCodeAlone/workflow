package buildergo_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/builder"
	buildergo "github.com/GoCodeAlone/workflow/plugins/builder-go"
)

func TestGoBuilder_Name(t *testing.T) {
	b := buildergo.New()
	if b.Name() != "go" {
		t.Fatalf("want name=go, got %q", b.Name())
	}
}

func TestGoBuilder_Validate_MissingPath(t *testing.T) {
	b := buildergo.New()
	err := b.Validate(builder.Config{TargetName: "server", Path: ""})
	if err == nil {
		t.Fatal("want error for empty path")
	}
}

func TestGoBuilder_Validate_OK(t *testing.T) {
	b := buildergo.New()
	err := b.Validate(builder.Config{TargetName: "server", Path: "./cmd/server"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoBuilder_Build_DrysOut(t *testing.T) {
	b := buildergo.New()
	cfg := builder.Config{
		TargetName: "server",
		Path:       "./cmd/server",
		Fields: map[string]any{
			"ldflags": "-s -w",
			"os":      "linux",
			"arch":    "amd64",
		},
		Env: map[string]string{"CGO_ENABLED": "0"},
	}
	out := &builder.Outputs{}
	// In test environment go build may fail (no source); we verify the
	// invocation is constructed correctly via the dry-run env var.
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := b.Build(context.Background(), cfg, out); err != nil {
		t.Fatalf("dry-run build: %v", err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].Kind != "binary" {
		t.Fatalf("expected 1 binary artifact, got %+v", out.Artifacts)
	}
}

func TestGoBuilder_SecurityLint_FlagsSecret(t *testing.T) {
	b := buildergo.New()
	cfg := builder.Config{
		TargetName: "server",
		Path:       "./cmd/server",
		Fields:     map[string]any{"ldflags": "-X main.secret=abc123"},
	}
	findings := b.SecurityLint(cfg)
	for _, f := range findings {
		if f.Severity == "warn" {
			return
		}
	}
	t.Fatal("want warn finding for -X ldflags with secret")
}

func TestGoBuilder_SecurityLint_CGOWithoutLinkMode(t *testing.T) {
	b := buildergo.New()
	cfg := builder.Config{
		TargetName: "server",
		Path:       "./cmd/server",
		Fields:     map[string]any{"cgo": true},
	}
	findings := b.SecurityLint(cfg)
	for _, f := range findings {
		if f.Severity == "warn" {
			return
		}
	}
	t.Fatal("want warn finding for cgo without link_mode")
}
