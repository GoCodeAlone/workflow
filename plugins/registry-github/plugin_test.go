package registrygithub_test

import (
	"bytes"
	"fmt"
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

func TestGHCRProvider_Prune_FailsClosedBeforeCredentials(t *testing.T) {
	// An empty fixture token makes even the historical implementation unable to
	// reach a real command or registry API. The safety check must run first.
	t.Setenv("WFCTL_PRUNE_TEST_UNUSED_TOKEN", "")
	cases := []struct {
		name, path, ttl, want string
		keep                  int
	}{
		{"registry root", "ghcr.io/myorg", "", "requires an exact", 5},
		{"wrong host", "example.com/myorg/image", "", "requires an exact", 5},
		{"URL scheme", "https://ghcr.io/myorg/image", "", "requires an exact", 5},
		{"empty owner", "ghcr.io//image", "", "requires an exact", 5},
		{"empty image", "ghcr.io/myorg/", "", "requires an exact", 5},
		{"tag suffix", "ghcr.io/myorg/image:latest", "", "requires an exact", 5},
		{"digest suffix", "ghcr.io/myorg/image@sha256:abc", "", "requires an exact", 5},
		{"traversal", "ghcr.io/myorg/../image", "", "requires an exact", 5},
		{"query", "ghcr.io/myorg/image?scope=all", "", "requires an exact", 5},
		{"encoded separator", "ghcr.io/myorg/image%2fall", "", "requires an exact", 5},
		{"whitespace", "ghcr.io/myorg/image all", "", "requires an exact", 5},
		{"exact scope without protection or approval", "ghcr.io/myorg/image", "", "deployed/rollback digests", 5},
		{"nested exact scope without protection or approval", "ghcr.io/myorg/group/image", "", "deployed/rollback digests", 5},
		{"unsupported TTL", "ghcr.io/myorg/image", "168h", "untagged_ttl is unsupported", 5},
		{"invalid keep", "ghcr.io/myorg/image", "", "keep_latest must be positive", 0},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dry), func(t *testing.T) {
				reg := config.CIRegistry{
					Name: "fixture", Type: "github", Path: tc.path,
					Auth:      &config.CIRegistryAuth{Env: "WFCTL_PRUNE_TEST_UNUSED_TOKEN"},
					Retention: &config.CIRegistryRetention{KeepLatest: tc.keep, UntaggedTTL: tc.ttl},
				}
				var out bytes.Buffer
				err := registrygithub.New().Prune(registry.NewContext(t.Context(), &out, dry), registry.ProviderConfig{Registry: reg})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Prune() = %v, want safety refusal containing %q", err, tc.want)
				}
				if tc.want == "deployed/rollback digests" && !strings.Contains(err.Error(), "explicit approval of each") {
					t.Fatalf("safety refusal omits action approval: %v", err)
				}
				if out.Len() != 0 {
					t.Fatalf("refused prune must not advertise or execute commands: %q", out.String())
				}
			})
		}
	}
}

func TestGHCRProvider_Prune_NoRetention_NoOp(t *testing.T) {
	for _, dry := range []bool{false, true} {
		var out bytes.Buffer
		reg := config.CIRegistry{Name: "fixture", Type: "github", Path: "ghcr.io/myorg"}
		if err := registrygithub.New().Prune(registry.NewContext(t.Context(), &out, dry), registry.ProviderConfig{Registry: reg}); err != nil {
			t.Fatalf("no retention: %v", err)
		}
		if out.Len() != 0 {
			t.Fatalf("no retention must produce no commands: %q", out.String())
		}
	}
}
