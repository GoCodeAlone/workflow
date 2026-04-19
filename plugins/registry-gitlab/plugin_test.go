package registrygitlab_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrygitlab "github.com/GoCodeAlone/workflow/plugins/registry-gitlab"
)

func TestGitLabProvider_Name(t *testing.T) {
	p := registrygitlab.New()
	if p.Name() != "gitlab" {
		t.Fatalf("want name=gitlab, got %q", p.Name())
	}
}

func TestGitLabProvider_Login_DryRun_WithToken(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
		Auth: &config.CIRegistryAuth{Env: "GITLAB_TOKEN"},
	}
	t.Setenv("GITLAB_TOKEN", "glpat-test-token")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Login dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "docker") {
		t.Errorf("dry-run should mention docker, got: %q", out)
	}
	if !strings.Contains(out, "login") {
		t.Errorf("dry-run should mention login, got: %q", out)
	}
	if !strings.Contains(out, "registry.gitlab.com") {
		t.Errorf("dry-run should mention gitlab registry host, got: %q", out)
	}
}

func TestGitLabProvider_Login_DryRun_CIJobToken(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
	}
	t.Setenv("CI_JOB_TOKEN", "ci-job-token-value")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Login dry-run with CI_JOB_TOKEN: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "gitlab-ci-token") {
		t.Errorf("expected gitlab-ci-token username in CI context, got: %q", out)
	}
}

func TestGitLabProvider_Login_MissingToken(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
		Auth: &config.CIRegistryAuth{Env: "MISSING_GITLAB_TOKEN_XYZ"},
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, false)
	err := p.Login(ctx, registry.ProviderConfig{Registry: reg})
	if err == nil {
		t.Fatal("want error for missing token env var")
	}
	if !strings.Contains(err.Error(), "MISSING_GITLAB_TOKEN_XYZ") {
		t.Errorf("error should mention env var name, got: %v", err)
	}
}

func TestGitLabProvider_Push_DryRun(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	imageRef := "registry.gitlab.com/myorg/myproject/app:v1.0.0"
	if err := p.Push(ctx, registry.ProviderConfig{Registry: reg}, imageRef); err != nil {
		t.Fatalf("Push dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, imageRef) {
		t.Errorf("dry-run should mention image ref, got: %q", out)
	}
	if !strings.Contains(out, "docker push") {
		t.Errorf("dry-run should mention docker push, got: %q", out)
	}
}

func TestGitLabProvider_Prune_DryRun_NoRetention(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
	}

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	// No retention set — should be a no-op.
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune dry-run (no retention): %v", err)
	}
}

func TestGitLabProvider_Prune_DryRun_WithRetention(t *testing.T) {
	p := registrygitlab.New()
	reg := config.CIRegistry{
		Name: "gitlab-registry",
		Type: "gitlab",
		Path: "registry.gitlab.com/myorg/myproject",
		Auth: &config.CIRegistryAuth{Env: "GITLAB_TOKEN"},
		Retention: &config.CIRegistryRetention{
			KeepLatest: 5,
		},
	}
	t.Setenv("GITLAB_TOKEN", "glpat-test-token")

	var buf bytes.Buffer
	ctx := registry.NewContext(t.Context(), &buf, true)
	if err := p.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
		t.Fatalf("Prune dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "prune") && !strings.Contains(out, "keep") {
		t.Errorf("expected prune/keep info in dry-run output, got: %q", out)
	}
}
