package registrygithub_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrygithub "github.com/GoCodeAlone/workflow/plugins/registry-github"
)

func TestGHCRProvider_Name(t *testing.T) {
	p := registrygithub.New()
	if p.Name() != "github" {
		t.Fatalf("want name=github, got %q", p.Name())
	}
}

func TestGHCRProvider_Login_DryRun(t *testing.T) {
	p := registrygithub.New()
	reg := config.CIRegistry{
		Name: "ghcr",
		Type: "github",
		Path: "ghcr.io/myorg",
		Auth: &config.CIRegistryAuth{Env: "GITHUB_TOKEN"},
	}
	t.Setenv("GITHUB_TOKEN", "test-ghtoken")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Login dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "docker") {
		t.Errorf("dry-run should mention docker, got: %q", out)
	}
	if !strings.Contains(out, "ghcr.io") {
		t.Errorf("dry-run should mention ghcr.io, got: %q", out)
	}
}

func TestGHCRProvider_Login_MissingToken(t *testing.T) {
	p := registrygithub.New()
	reg := config.CIRegistry{
		Name: "ghcr",
		Type: "github",
		Path: "ghcr.io/myorg",
		Auth: &config.CIRegistryAuth{Env: "MISSING_GH_TOKEN"},
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, false)
	err := p.Login(ctx, registry.ProviderConfig{Registry: reg})
	if err == nil {
		t.Fatal("want error for missing token")
	}
	if !strings.Contains(err.Error(), "MISSING_GH_TOKEN") {
		t.Errorf("error should mention env var, got: %v", err)
	}
}

func TestGHCRProvider_Prune_DryRun(t *testing.T) {
	p := registrygithub.New()
	reg := config.CIRegistry{
		Name:      "ghcr",
		Type:      "github",
		Path:      "ghcr.io/myorg",
		Auth:      &config.CIRegistryAuth{Env: "GITHUB_TOKEN"},
		Retention: &config.CIRegistryRetention{KeepLatest: 10},
	}
	t.Setenv("GITHUB_TOKEN", "test-ghtoken")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "dry-run") {
		t.Errorf("dry-run output should mention dry-run, got: %q", out)
	}
}

func TestGHCRProvider_Prune_NoRetention_NoOp(t *testing.T) {
	p := registrygithub.New()
	reg := config.CIRegistry{
		Name: "ghcr",
		Type: "github",
		Path: "ghcr.io/myorg",
	}
	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune no-retention: %v", err)
	}
}
