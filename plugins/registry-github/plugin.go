// Package registrygithub provides the GitHub Container Registry (GHCR) provider.
package registrygithub

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(&GHCRProvider{name: "github"})
	registry.Register(&GHCRProvider{name: "ghcr"})
}

// GHCRProvider implements registry.RegistryProvider for GitHub Container Registry.
type GHCRProvider struct {
	name string
}

// New returns a new GHCRProvider.
func New() registry.RegistryProvider { return &GHCRProvider{name: "github"} }

func (g *GHCRProvider) Name() string {
	if g.name == "" {
		return "github"
	}
	return g.name
}

func (g *GHCRProvider) Login(ctx registry.Context, cfg registry.ProviderConfig) error {
	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}

	args := []string{"login", "ghcr.io", "--username", "x-access-token", "--password-stdin"}
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] echo $GITHUB_TOKEN | docker %s\n", joinArgs(args))
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec
	cmd.Stdin = newStringReader(token)
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker login ghcr.io: %w", err)
	}
	return nil
}

func (g *GHCRProvider) Logout(ctx registry.Context, cfg registry.ProviderConfig) error {
	args := []string{"logout", "ghcr.io"}
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] docker %s\n", joinArgs(args))
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // G204: command constructed from config, not user input
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker logout ghcr.io: %w", err)
	}
	return nil
}

func (g *GHCRProvider) Push(ctx registry.Context, cfg registry.ProviderConfig, imageRef string) error {
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

// Prune fails closed until whole-version deletion can protect deployed and
// rollback digests and require approval of the exact deletion action.
func (g *GHCRProvider) Prune(ctx registry.Context, cfg registry.ProviderConfig) error {
	ret := cfg.Registry.Retention
	if ret == nil {
		return nil
	}
	if _, _, err := parseGHCRPath(cfg.Registry.Path); err != nil {
		return err
	}
	if ret.KeepLatest <= 0 {
		return fmt.Errorf("github registry %q: retention.keep_latest must be positive", cfg.Registry.Name)
	}
	if ret.UntaggedTTL != "" {
		return fmt.Errorf("github registry %q: retention.untagged_ttl is unsupported; whole-version deletion cannot enforce untagged age", cfg.Registry.Name)
	}
	return fmt.Errorf("github registry %q: pruning disabled: deployed/rollback digests must be protected and explicit approval of each deletion is required; recoverability has not been established", cfg.Registry.Name)
}

var pruneComponent = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)

func parseGHCRPath(path string) (org, pkg string, err error) {
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "ghcr.io" {
		return "", "", fmt.Errorf("GHCR prune requires an exact ghcr.io/<org>/<package> path")
	}
	for _, part := range parts[1:] {
		if !pruneComponent.MatchString(part) {
			return "", "", fmt.Errorf("GHCR prune requires an exact ghcr.io/<org>/<package> path")
		}
	}
	return parts[1], strings.Join(parts[2:], "/"), nil
}

func resolveToken(cfg registry.ProviderConfig) (string, error) {
	if cfg.Registry.Auth == nil || cfg.Registry.Auth.Env == "" {
		return "", fmt.Errorf("github registry %q: auth.env is required", cfg.Registry.Name)
	}
	envVar := cfg.Registry.Auth.Env
	token := os.Getenv(envVar)
	if token == "" {
		return "", fmt.Errorf("github registry %q: env var %s is not set or empty", cfg.Registry.Name, envVar)
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

type stringReader struct{ s string }

func newStringReader(s string) io.Reader { return &stringReader{s: s} }
func (r *stringReader) Read(p []byte) (n int, err error) {
	if len(r.s) == 0 {
		return 0, io.EOF
	}
	n = copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}
