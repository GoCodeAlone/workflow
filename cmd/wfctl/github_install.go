package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// parseGitHubRef parses a plugin reference that may be a GitHub owner/repo[@version] path.
// Returns (owner, repo, version, isGitHub).
// "GoCodeAlone/workflow-plugin-authz@v0.3.1" → ("GoCodeAlone","workflow-plugin-authz","v0.3.1",true)
// "GoCodeAlone/workflow-plugin-authz" → ("GoCodeAlone","workflow-plugin-authz","",true)
// "authz" → ("","","",false)
func parseGitHubRef(input string) (owner, repo, version string, isGitHub bool) {
	// Must contain "/" to be a GitHub ref.
	if !strings.Contains(input, "/") {
		return "", "", "", false
	}

	ownerRepo := input
	if atIdx := strings.Index(input, "@"); atIdx > 0 {
		version = input[atIdx+1:]
		ownerRepo = input[:atIdx]
	}

	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], version, true
}

// ghRelease is a minimal subset of the GitHub Releases API response.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// installFromGitHub downloads and extracts a plugin directly from a GitHub Release.
// owner/repo@version is resolved to a tarball asset matching {repo}_{os}_{arch}.tar.gz.
func installFromGitHub(owner, repo, version, destDir string) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, version)
	if version == "" || version == "latest" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	}

	fmt.Fprintf(os.Stderr, "Fetching GitHub release from %s/%s@%s...\n", owner, repo, version)
	body, err := downloadURL(apiURL)
	if err != nil {
		return fmt.Errorf("fetch GitHub release: %w", err)
	}

	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("parse GitHub release response: %w", err)
	}

	// Find asset matching {repo}_{os}_{arch}.tar.gz
	wantSuffix := fmt.Sprintf("%s_%s_%s.tar.gz", repo, runtime.GOOS, runtime.GOARCH)
	var assetURL string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, wantSuffix) {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no asset matching %q found in release %s for %s/%s", wantSuffix, rel.TagName, owner, repo)
	}

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", assetURL)
	data, err := downloadURL(assetURL)
	if err != nil {
		return fmt.Errorf("download plugin from GitHub: %w", err)
	}

	if err := os.MkdirAll(destDir, 0750); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", destDir, err)
	}

	fmt.Fprintf(os.Stderr, "Extracting to %s...\n", destDir)
	if err := extractTarGz(data, destDir); err != nil {
		return fmt.Errorf("extract plugin: %w", err)
	}

	return nil
}
