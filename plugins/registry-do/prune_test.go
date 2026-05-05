package registrydo_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrydo "github.com/GoCodeAlone/workflow/plugins/registry-do"
)

func TestDOProvider_Prune_DryRun(t *testing.T) {
	p := registrydo.New()
	reg := config.CIRegistry{
		Name: "docr",
		Type: "do",
		Path: "registry.digitalocean.com/myorg",
		Auth: &config.CIRegistryAuth{Env: "DIGITALOCEAN_TOKEN"},
		Retention: &config.CIRegistryRetention{
			KeepLatest: 5,
			Schedule:   "0 7 * * 0",
		},
	}
	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune dry-run: %v", err)
	}

	out := buf.String()
	// Must mention garbage-collection
	if !strings.Contains(out, "garbage-collection") {
		t.Errorf("dry-run should mention garbage-collection, got: %q", out)
	}
	// Must mention doctl
	if !strings.Contains(out, "doctl") {
		t.Errorf("dry-run should mention doctl, got: %q", out)
	}
}

func TestDOProvider_Prune_NoRetention_NoOp(t *testing.T) {
	p := registrydo.New()
	reg := config.CIRegistry{
		Name: "docr",
		Type: "do",
		Path: "registry.digitalocean.com/myorg",
		Auth: &config.CIRegistryAuth{Env: "DIGITALOCEAN_TOKEN"},
	}
	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune no-retention: %v", err)
	}
}

func TestDOProvider_Prune_MissingToken(t *testing.T) {
	p := registrydo.New()
	reg := config.CIRegistry{
		Name:      "docr",
		Type:      "do",
		Path:      "registry.digitalocean.com/myorg",
		Auth:      &config.CIRegistryAuth{Env: "MISSING_TOKEN"},
		Retention: &config.CIRegistryRetention{KeepLatest: 5},
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, false)
	err := p.Prune(ctx, registry.ProviderConfig{Registry: reg})
	if err == nil {
		t.Fatal("want error for missing token")
	}
	if !strings.Contains(err.Error(), "MISSING_TOKEN") {
		t.Errorf("error should mention env var, got: %v", err)
	}
}
