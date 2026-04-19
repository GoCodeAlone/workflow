package registrydo_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrydo "github.com/GoCodeAlone/workflow/plugins/registry-do"
)

func TestDOProvider_Name(t *testing.T) {
	p := registrydo.New()
	if p.Name() != "do" {
		t.Fatalf("want name=do, got %q", p.Name())
	}
}

func TestDOProvider_Login_DryRun(t *testing.T) {
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
	if err := p.Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Login dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "doctl") {
		t.Errorf("dry-run output should mention doctl, got: %q", out)
	}
	if !strings.Contains(out, "registry") {
		t.Errorf("dry-run output should mention registry, got: %q", out)
	}
}

func TestDOProvider_Login_MissingToken(t *testing.T) {
	p := registrydo.New()
	reg := config.CIRegistry{
		Name: "docr",
		Type: "do",
		Path: "registry.digitalocean.com/myorg",
		Auth: &config.CIRegistryAuth{Env: "MISSING_TOKEN_VAR"},
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, false)
	err := p.Login(ctx, registry.ProviderConfig{Registry: reg})
	if err == nil {
		t.Fatal("want error for missing token env var")
	}
	if !strings.Contains(err.Error(), "MISSING_TOKEN_VAR") {
		t.Errorf("error should mention env var name, got: %v", err)
	}
}
