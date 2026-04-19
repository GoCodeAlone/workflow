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

// Login authenticates with registry.gitlab.com.
// In CI context ($CI_JOB_TOKEN set), uses gitlab-ci-token as username.
// Otherwise uses oauth2 with the token from auth.env.
func (g *GitLabProvider) Login(ctx registry.Context, cfg registry.ProviderConfig) error {
	host := registryHost(cfg.Registry.Path)
	username, token, err := resolveCredentials(cfg)
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
// Uses GET /api/v4/projects/:id/registry/repositories to find repo IDs, then
// DELETE /api/v4/projects/:id/registry/repositories/:repo_id/tags/:tag_name.
func (g *GitLabProvider) Prune(ctx registry.Context, cfg registry.ProviderConfig) error {
	ret := cfg.Registry.Retention
	if ret == nil || ret.KeepLatest <= 0 {
		return nil
	}

	_, token, err := resolveCredentials(cfg)
	if err != nil {
		return err
	}

	projectPath := gitlabProjectPath(cfg.Registry.Path)

	if ctx.DryRun() {
		fmt.Fprintf(ctx.Out(), "[dry-run] GitLab API: prune registry tags for %s, keep latest %d\n",
			projectPath, ret.KeepLatest)
		return nil
	}

	return pruneGitLabTags(ctx, token, projectPath, ret.KeepLatest)
}

// resolveCredentials returns (username, token, error) for GitLab auth.
// In CI (CI_JOB_TOKEN set), uses gitlab-ci-token + CI_JOB_TOKEN.
// Otherwise uses oauth2 + auth.env token.
func resolveCredentials(cfg registry.ProviderConfig) (username, token string, err error) {
	if ciToken := os.Getenv("CI_JOB_TOKEN"); ciToken != "" {
		return "gitlab-ci-token", ciToken, nil
	}
	if cfg.Registry.Auth == nil || cfg.Registry.Auth.Env == "" {
		return "", "", fmt.Errorf("gitlab registry %q: auth.env is required (or set CI_JOB_TOKEN)", cfg.Registry.Name)
	}
	envVar := cfg.Registry.Auth.Env
	tok := os.Getenv(envVar)
	if tok == "" {
		return "", "", fmt.Errorf("gitlab registry %q: env var %s is not set or empty", cfg.Registry.Name, envVar)
	}
	return "oauth2", tok, nil
}

// registryHost extracts the hostname from a registry path.
// "registry.gitlab.com/myorg/myproject" → "registry.gitlab.com".
func registryHost(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}

// gitlabProjectPath extracts the project path from a registry path.
// "registry.gitlab.com/myorg/myproject" → "myorg/myproject".
func gitlabProjectPath(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[i+1:]
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

func pruneGitLabTags(ctx registry.Context, token, projectPath string, keepLatest int) error {
	encodedProject := url.PathEscape(projectPath)
	baseURL := "https://gitlab.com"

	// List registry repositories for the project.
	repoURL := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories", baseURL, encodedProject)
	repos, err := glGetJSON[[]glRepository](ctx, token, repoURL)
	if err != nil {
		return fmt.Errorf("list gitlab registry repositories: %w", err)
	}

	for _, repo := range repos {
		tagsURL := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories/%d/tags", baseURL, encodedProject, repo.ID)
		tags, err := glGetJSON[[]glRepoTag](ctx, token, tagsURL)
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
				baseURL, encodedProject, repo.ID, url.PathEscape(tag.Name))
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
			if err != nil {
				return err
			}
			req.Header.Set("PRIVATE-TOKEN", token)
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

func glGetJSON[T any](ctx registry.Context, token, rawURL string) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("GitLab API %s: %s", resp.Status, body)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}
