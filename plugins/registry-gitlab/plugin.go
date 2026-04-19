// Package registrygitlab provides the GitLab Container Registry provider.
package registrygitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

// GitLabProvider implements registry.RegistryProvider for GitLab Container Registry.
type GitLabProvider struct{}

// New returns a new GitLabProvider.
func New() registry.RegistryProvider { return &GitLabProvider{} }

func (g *GitLabProvider) Name() string { return "gitlab" }

// Login authenticates with the GitLab registry host.
// In CI context ($CI_JOB_TOKEN set), uses gitlab-ci-token as username.
// Otherwise uses oauth2 with the token from auth.env.
func (g *GitLabProvider) Login(ctx registry.Context, cfg registry.ProviderConfig) error {
	host := registryHost(cfg.Registry.Path)
	username, token, _, err := resolveCredentials(cfg)
	if err != nil {
		return err
	}

	args := []string{"login", host, "--username", username, "--password-stdin"}
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] echo $TOKEN | docker %s\n", joinArgs(args))
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec
	cmd.Stdin = strings.NewReader(token)
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker login %s: %w", host, err)
	}
	return nil
}

func (g *GitLabProvider) Logout(ctx registry.Context, cfg registry.ProviderConfig) error {
	host := registryHost(cfg.Registry.Path)
	args := []string{"logout", host}
	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] docker %s\n", joinArgs(args))
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec
	cmd.Stdout = ctx.Out()
	cmd.Stderr = ctx.Out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker logout %s: %w", host, err)
	}
	return nil
}

func (g *GitLabProvider) Push(ctx registry.Context, cfg registry.ProviderConfig, imageRef string) error {
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

// Prune deletes tags beyond retention.keep_latest via the GitLab Container Registry API.
// Supports self-managed instances via ci.registries[].api_base_url.
func (g *GitLabProvider) Prune(ctx registry.Context, cfg registry.ProviderConfig) error {
	ret := cfg.Registry.Retention
	if ret == nil || ret.KeepLatest <= 0 {
		return nil
	}

	_, token, tokenType, err := resolveCredentials(cfg)
	if err != nil {
		return err
	}

	projectPath := gitlabProjectPath(cfg.Registry.Path)
	apiBase := glAPIBase(cfg)

	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] GitLab API: prune registry tags for %s, keep latest %d\n",
			projectPath, ret.KeepLatest)
		return nil
	}

	return pruneGitLabTags(ctx, token, tokenType, apiBase, projectPath, ret.KeepLatest)
}

// resolveCredentials returns (username, token, tokenType, error).
// tokenType is "job" when CI_JOB_TOKEN is used, "private" otherwise.
// The JOB-TOKEN header is required for CI_JOB_TOKEN; PRIVATE-TOKEN for PATs.
func resolveCredentials(cfg registry.ProviderConfig) (username, token, tokenType string, err error) {
	if ciToken := os.Getenv("CI_JOB_TOKEN"); ciToken != "" {
		return "gitlab-ci-token", ciToken, "job", nil
	}
	if cfg.Registry.Auth == nil || cfg.Registry.Auth.Env == "" {
		return "", "", "", fmt.Errorf("gitlab registry %q: auth.env is required (or set CI_JOB_TOKEN)", cfg.Registry.Name)
	}
	envVar := cfg.Registry.Auth.Env
	tok := os.Getenv(envVar)
	if tok == "" {
		return "", "", "", fmt.Errorf("gitlab registry %q: env var %s is not set or empty", cfg.Registry.Name, envVar)
	}
	return "oauth2", tok, "private", nil
}

// AuthHeaderFor returns the correct GitLab API auth header name for tokenType.
// Exported for testing. tokenType is "job" (CI_JOB_TOKEN) or "private" (PAT/oauth2).
func AuthHeaderFor(tokenType string) string {
	if tokenType == "job" {
		return "JOB-TOKEN"
	}
	return "PRIVATE-TOKEN"
}

// glAPIBase returns the GitLab API base URL for the registry config.
// Uses cfg.Registry.APIBaseURL if set; defaults to https://gitlab.com.
func glAPIBase(cfg registry.ProviderConfig) string {
	if cfg.Registry.APIBaseURL != "" {
		return strings.TrimRight(cfg.Registry.APIBaseURL, "/")
	}
	return "https://gitlab.com"
}

// registryHost extracts the hostname from a registry path.
// "registry.gitlab.com/myorg/myproject" → "registry.gitlab.com".
func registryHost(path string) string {
	host, _, _ := strings.Cut(path, "/")
	return host
}

// gitlabProjectPath extracts the project path from a registry path.
// "registry.gitlab.com/myorg/myproject" → "myorg/myproject".
func gitlabProjectPath(path string) string {
	_, rest, found := strings.Cut(path, "/")
	if found {
		return rest
	}
	return path
}

type glRepoTag struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type glRepository struct {
	ID int `json:"id"`
}

func pruneGitLabTags(ctx registry.Context, token, tokenType, apiBase, projectPath string, keepLatest int) error {
	encodedProject := url.PathEscape(projectPath)

	repoURL := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories?per_page=100", apiBase, encodedProject)
	repos, err := glPaginatedGet[glRepository](ctx, token, tokenType, repoURL)
	if err != nil {
		return fmt.Errorf("list gitlab registry repositories: %w", err)
	}

	for _, repo := range repos {
		tagsURL := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories/%d/tags?per_page=100", apiBase, encodedProject, repo.ID)
		tags, err := glPaginatedGet[glRepoTag](ctx, token, tokenType, tagsURL)
		if err != nil {
			return fmt.Errorf("list tags for repo %d: %w", repo.ID, err)
		}

		// Sort newest first (ISO 8601 lexicographic works).
		sort.Slice(tags, func(i, j int) bool {
			return tags[i].CreatedAt > tags[j].CreatedAt
		})

		for i, tag := range tags {
			if i < keepLatest {
				continue
			}
			delURL := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories/%d/tags/%s",
				apiBase, encodedProject, repo.ID, url.PathEscape(tag.Name))
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
			if err != nil {
				return err
			}
			req.Header.Set(AuthHeaderFor(tokenType), token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Fprintf(ctx.Out(), "warn: delete tag %s: %v\n", tag.Name, err)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
				fmt.Fprintf(ctx.Out(), "deleted tag %s\n", tag.Name)
			} else {
				fmt.Fprintf(ctx.Out(), "warn: delete tag %s: HTTP %d\n", tag.Name, resp.StatusCode)
			}
		}
	}
	return nil
}

// glPaginatedGet fetches all pages of a GitLab API list endpoint,
// following X-Next-Page headers until exhausted.
func glPaginatedGet[T any](ctx registry.Context, token, tokenType, firstURL string) ([]T, error) {
	var all []T
	nextURL := firstURL
	for nextURL != "" {
		page, nextPage, err := glGetPage[T](ctx, token, tokenType, nextURL)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		nextURL = nextPage
	}
	return all, nil
}

// glGetPage fetches one page and returns items + the URL for the next page (empty if done).
func glGetPage[T any](ctx registry.Context, token, tokenType, rawURL string) ([]T, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set(AuthHeaderFor(tokenType), token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("GitLab API %s: %s", resp.Status, body)
	}

	var items []T
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, "", err
	}

	// Follow X-Next-Page header for pagination.
	nextPage := resp.Header.Get("X-Next-Page")
	if nextPage == "" {
		return items, "", nil
	}

	// Build next page URL by appending/replacing the page parameter.
	nextURL := appendPageParam(rawURL, nextPage)
	return items, nextURL, nil
}

// appendPageParam adds or replaces the page= query parameter in a URL.
func appendPageParam(rawURL, page string) string {
	base, query, hasQuery := strings.Cut(rawURL, "?")
	if hasQuery {
		parts := strings.Split(query, "&")
		filtered := parts[:0]
		for _, p := range parts {
			if !strings.HasPrefix(p, "page=") {
				filtered = append(filtered, p)
			}
		}
		filtered = append(filtered, "page="+page)
		return base + "?" + strings.Join(filtered, "&")
	}
	return rawURL + "?page=" + page
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}
