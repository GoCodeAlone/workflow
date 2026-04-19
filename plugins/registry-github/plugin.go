// Package registrygithub provides the GitHub Container Registry (GHCR) provider.
package registrygithub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

// GHCRProvider implements registry.RegistryProvider for GitHub Container Registry.
type GHCRProvider struct{}

// New returns a new GHCRProvider.
func New() registry.RegistryProvider { return &GHCRProvider{} }

func (g *GHCRProvider) Name() string { return "github" }

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

// Prune deletes package versions beyond retention.keep_latest via the GH API.
func (g *GHCRProvider) Prune(ctx registry.Context, cfg registry.ProviderConfig) error {
	ret := cfg.Registry.Retention
	if ret == nil || ret.KeepLatest <= 0 {
		return nil
	}

	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}

	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] GH API: list package versions for %s, keep latest %d\n",
			cfg.Registry.Path, ret.KeepLatest)
		return nil
	}

	// Derive org + package name from the registry path (ghcr.io/<org>/<package>).
	org, pkg, err := parseGHCRPath(cfg.Registry.Path)
	if err != nil {
		return err
	}

	versions, err := listPackageVersions(ctx, token, org, pkg)
	if err != nil {
		return fmt.Errorf("list versions for %s/%s: %w", org, pkg, err)
	}

	// Sort newest first by created_at (API returns ISO 8601 strings; lexicographic sort works).
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].CreatedAt > versions[j].CreatedAt
	})

	for i, v := range versions {
		if i < ret.KeepLatest {
			continue
		}
		if err := deletePackageVersion(ctx, token, org, pkg, v.ID); err != nil {
			fmt.Fprintf(ctx.Out(), "warn: failed to delete version %d: %v\n", v.ID, err)
		} else {
			fmt.Fprintf(ctx.Out(), "deleted version %d (%s)\n", v.ID, v.CreatedAt)
		}
	}
	return nil
}

type ghPackageVersion struct {
	ID        int    `json:"id"`
	CreatedAt string `json:"created_at"`
}

func listPackageVersions(ctx registry.Context, token, org, pkg string) ([]ghPackageVersion, error) {
	url := fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions?per_page=100", org, pkg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GH API %s: %s", resp.Status, body)
	}

	var versions []ghPackageVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func deletePackageVersion(ctx registry.Context, token, org, pkg string, versionID int) error {
	url := fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions/%d", org, pkg, versionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GH API DELETE %s: %s", resp.Status, body)
	}
	return nil
}

func parseGHCRPath(path string) (org, pkg string, err error) {
	// path: ghcr.io/<org>/<package>  or  ghcr.io/<org> (pkg == org)
	// Strip "ghcr.io/" prefix.
	const prefix = "ghcr.io/"
	rest := path
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		rest = path[len(prefix):]
	}
	parts := splitTwo(rest, '/')
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return rest, rest, nil
}

func splitTwo(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
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
