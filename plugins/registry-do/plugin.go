// Package registrydo provides the DigitalOcean container registry provider.
package registrydo

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

// DOProvider implements registry.RegistryProvider for DigitalOcean Container Registry.
type DOProvider struct{}

// New returns a new DOProvider.
func New() registry.RegistryProvider { return &DOProvider{} }

func (d *DOProvider) Name() string { return "do" }

func (d *DOProvider) Login(ctx registry.Context, cfg registry.ProviderConfig) error {
	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}

	args := []string{"registry", "login", "--expiry-seconds", "3600"}
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] DIGITALOCEAN_TOKEN=<token> doctl %s\n",
			joinArgs(args))
		return nil
	}

	cmd := exec.CommandContext(ctx, "doctl", args...) //nolint:gosec
	cmd.Env = append(os.Environ(), "DIGITALOCEAN_TOKEN="+token)
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("doctl registry login: %w", err)
	}
	return nil
}

func (d *DOProvider) Push(ctx registry.Context, cfg registry.ProviderConfig, imageRef string) error {
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] docker push %s\n", imageRef)
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "push", imageRef) //nolint:gosec
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push %s: %w", imageRef, err)
	}
	return nil
}

func (d *DOProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func resolveToken(cfg registry.ProviderConfig) (string, error) {
	if cfg.Registry.Auth == nil || cfg.Registry.Auth.Env == "" {
		return "", fmt.Errorf("do registry %q: auth.env is required", cfg.Registry.Name)
	}
	envVar := cfg.Registry.Auth.Env
	token := os.Getenv(envVar)
	if token == "" {
		return "", fmt.Errorf("do registry %q: env var %s is not set or empty", cfg.Registry.Name, envVar)
	}
	return token, nil
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
