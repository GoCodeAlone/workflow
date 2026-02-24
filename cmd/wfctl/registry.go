package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	registryOwner  = "GoCodeAlone"
	registryRepo   = "workflow-registry"
	registryBranch = "main"
)

// RegistryManifest is the manifest format for the GoCodeAlone/workflow-registry.
type RegistryManifest struct {
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	Author           string          `json:"author"`
	Description      string          `json:"description"`
	Source           string          `json:"source,omitempty"`
	Type             string          `json:"type"`
	Tier             string          `json:"tier"`
	License          string          `json:"license"`
	MinEngineVersion string          `json:"minEngineVersion,omitempty"`
	Repository       string          `json:"repository,omitempty"`
	Keywords         []string        `json:"keywords,omitempty"`
	Downloads        []PluginDownload `json:"downloads,omitempty"`
	Assets           *PluginAssets   `json:"assets,omitempty"`
}

// PluginDownload describes a platform-specific binary download for a plugin.
type PluginDownload struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

// PluginAssets describes optional asset files bundled with a plugin release.
type PluginAssets struct {
	UI     bool `json:"ui"`
	Config bool `json:"config"`
}

// PluginSummary is a brief description of a plugin from the registry.
type PluginSummary struct {
	Name        string
	Version     string
	Description string
	Tier        string
}

// githubContentsEntry is an entry from the GitHub contents API response.
type githubContentsEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "dir" or "file"
}

// FetchManifest fetches a plugin manifest from the registry by plugin name.
func FetchManifest(name string) (*RegistryManifest, error) {
	url := fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/%s/plugins/%s/manifest.json",
		registryOwner, registryRepo, registryBranch, name,
	)
	resp, err := http.Get(url) //nolint:gosec // G107: URL constructed from validated constant base + user name
	if err != nil {
		return nil, fmt.Errorf("fetch manifest for %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %q not found in registry", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned HTTP %d for plugin %q", resp.StatusCode, name)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest for %q: %w", name, err)
	}
	var m RegistryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest for %q: %w", name, err)
	}
	return &m, nil
}

// ListPluginNames returns all plugin names available in the registry.
func ListPluginNames() ([]string, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/contents/plugins",
		registryOwner, registryRepo,
	)
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
		return nil, fmt.Errorf("list registry plugins: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry API returned HTTP %d", resp.StatusCode)
	}
	var entries []githubContentsEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parse registry listing: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.Type == "dir" {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

// SearchPlugins returns plugins from the registry whose name or description matches query.
// An empty query returns all plugins.
func SearchPlugins(query string) ([]PluginSummary, error) {
	names, err := ListPluginNames()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []PluginSummary
	for _, name := range names {
		// Check name first to avoid unnecessary manifest fetches.
		nameMatch := query == "" || strings.Contains(strings.ToLower(name), q)
		m, fetchErr := FetchManifest(name)
		if fetchErr != nil {
			continue // skip plugins we can't fetch
		}
		descMatch := query == "" || strings.Contains(strings.ToLower(m.Description), q)
		kwMatch := false
		for _, kw := range m.Keywords {
			if strings.Contains(strings.ToLower(kw), q) {
				kwMatch = true
				break
			}
		}
		if nameMatch || descMatch || kwMatch {
			results = append(results, PluginSummary{
				Name:        m.Name,
				Version:     m.Version,
				Description: m.Description,
				Tier:        m.Tier,
			})
		}
	}
	return results, nil
}

// FindDownload returns the Download entry matching the given OS and arch.
func (m *RegistryManifest) FindDownload(goos, goarch string) (*PluginDownload, error) {
	for i, d := range m.Downloads {
		if d.OS == goos && d.Arch == goarch {
			return &m.Downloads[i], nil
		}
	}
	return nil, fmt.Errorf("no download available for %s/%s in plugin %q", goos, goarch, m.Name)
}
