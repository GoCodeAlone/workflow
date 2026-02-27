package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// RegistrySource is the interface for a plugin registry backend.
type RegistrySource interface {
	// Name returns the configured name of this registry.
	Name() string
	// ListPlugins returns all plugin names in this registry.
	ListPlugins() ([]string, error)
	// FetchManifest retrieves the manifest for a named plugin.
	FetchManifest(name string) (*RegistryManifest, error)
	// SearchPlugins returns plugins matching the query string.
	SearchPlugins(query string) ([]PluginSearchResult, error)
}

// PluginSearchResult is a search result from a registry source.
type PluginSearchResult struct {
	PluginSummary
	Source string // registry name this came from
}

// GitHubRegistrySource implements RegistrySource backed by a GitHub repo with manifest.json files.
type GitHubRegistrySource struct {
	name   string
	owner  string
	repo   string
	branch string
}

// NewGitHubRegistrySource creates a new GitHub-backed registry source.
func NewGitHubRegistrySource(cfg RegistrySourceConfig) *GitHubRegistrySource {
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}
	return &GitHubRegistrySource{
		name:   cfg.Name,
		owner:  cfg.Owner,
		repo:   cfg.Repo,
		branch: branch,
	}
}

func (g *GitHubRegistrySource) Name() string { return g.name }

func (g *GitHubRegistrySource) ListPlugins() ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/plugins", g.owner, g.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list registry plugins from %s: %w", g.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s API returned HTTP %d", g.name, resp.StatusCode)
	}
	var entries []githubContentsEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parse registry %s listing: %w", g.name, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type == "dir" {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

func (g *GitHubRegistrySource) FetchManifest(name string) (*RegistryManifest, error) {
	url := fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/%s/plugins/%s/manifest.json",
		g.owner, g.repo, g.branch, name,
	)
	resp, err := http.Get(url) //nolint:gosec // URL constructed from configured registry
	if err != nil {
		return nil, fmt.Errorf("fetch manifest for %q from %s: %w", name, g.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %q not found in registry %s", name, g.name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s returned HTTP %d for plugin %q", g.name, resp.StatusCode, name)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest for %q from %s: %w", name, g.name, err)
	}
	var m RegistryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest for %q from %s: %w", name, g.name, err)
	}
	return &m, nil
}

func (g *GitHubRegistrySource) SearchPlugins(query string) ([]PluginSearchResult, error) {
	names, err := g.ListPlugins()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []PluginSearchResult
	for _, name := range names {
		m, fetchErr := g.FetchManifest(name)
		if fetchErr != nil {
			continue
		}
		if matchesRegistryQuery(m, q) {
			results = append(results, PluginSearchResult{
				PluginSummary: PluginSummary{
					Name:        m.Name,
					Version:     m.Version,
					Description: m.Description,
					Tier:        m.Tier,
				},
				Source: g.name,
			})
		}
	}
	return results, nil
}

// matchesRegistryQuery checks if a manifest matches a search query.
func matchesRegistryQuery(m *RegistryManifest, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(m.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(m.Description), q) {
		return true
	}
	for _, kw := range m.Keywords {
		if strings.Contains(strings.ToLower(kw), q) {
			return true
		}
	}
	return false
}
