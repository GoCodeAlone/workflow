package registrygitlab_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrygitlab "github.com/GoCodeAlone/workflow/plugins/registry-gitlab"
)

func TestGitLabProvider_Name(t *testing.T) {
	p := registrygitlab.New()
	if p.Name() != "gitlab" {
		t.Fatalf("want name=gitlab, got %q", p.Name())
	}
}

func TestGitLabProvider_ReturnsNotImplemented(t *testing.T) {
	p := registrygitlab.New()
	var buf noopWriter
	ctx := registry.NewContext(t.Context(), &buf, false)
	cfg := registry.ProviderConfig{}

	if err := p.Login(ctx, cfg); err == nil {
		t.Fatal("want ErrNotImplemented from Login")
	}
	if err := p.Push(ctx, cfg, "img"); err == nil {
		t.Fatal("want ErrNotImplemented from Push")
	}
	if err := p.Prune(ctx, cfg); err == nil {
		t.Fatal("want ErrNotImplemented from Prune")
	}
}

type noopWriter struct{}

func (n *noopWriter) Write(p []byte) (int, error) { return len(p), nil }
