package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// runPluginRegistrySync ports workflow-registry/scripts/sync-versions.sh +
// sync-core-manifests.sh + generate-readme.sh into a single Go subcommand
// (workflow#762). Sub-modes: default (sync-versions), "core", "readme".
//
// Default mode walks <registry-dir>/plugins/*/manifest.json; for each:
//  1. Parses repository/source to derive gh_repo.
//  2. gh release view → latestTag.
//  3. Rejects non-publish-grade-semver tags (shared PublishGradeSemverRe).
//  4. Rejects plugin.json.type values outside the registry allowlist
//     (catches accidental scaffold re-registration per workflow#762
//     Layer (d) step 5).
//  5. Compares manifest.version + downloads URLs; with --fix rewrites.
//  6. Fetches tagged plugin.json from upstream; syncs capabilities,
//     minEngineVersion, iacProvider into registry manifest.
//  7. (--verify-capabilities) Downloads release tarball + spawns binary;
//     diffs GetManifest's capabilities vs committed; with --fix rewrites.
//     NOTE: deferred to a follow-up PR per plan I4 / I-P9 — initial impl
//     stubs this with a clear "not implemented yet" message.
func runPluginRegistrySync(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "core":
			return runPluginRegistrySyncCore(args[1:])
		case "readme":
			return runPluginRegistrySyncReadme(args[1:])
		}
	}

	fs := flag.NewFlagSet("plugin registry-sync", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "Apply changes (default: dry-run)")
	pluginFilter := fs.String("plugin", "", "Restrict to single plugin directory name")
	verifyCaps := fs.Bool("verify-capabilities", false, "Spawn binary + diff capabilities (registry-side; slow; not yet implemented)")
	registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin registry-sync [options]
       wfctl plugin registry-sync core [options]
       wfctl plugin registry-sync readme [options]

Default mode: walks <registry-dir>/plugins/*/manifest.json and syncs each
plugin's version + downloads URLs + capabilities against its upstream
GitHub release tag. Replaces workflow-registry/scripts/sync-versions.sh.

Sub-modes:
  core     — sync core (built-in workflow) plugin manifests by compiling
             an inspect program against a workflow checkout; replaces
             scripts/sync-core-manifests.sh.
  readme   — regenerate the README plugin/template indexes from registry
             source data; replaces scripts/generate-readme.sh.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	return syncDefault(*registryDir, *fix, *pluginFilter, *verifyCaps)
}

// registryAllowedTypes is the set of plugin.json type values that legitimately
// belong in the registry. Scaffold repos use type:"scaffold" which is NOT
// allowed here — registry-sync rejects them to defend against accidental
// re-registration (workflow#762 Layer d step 5, plan C-P3 fix).
var registryAllowedTypes = map[string]bool{
	"external": true,
	"builtin":  true,
	"core":     true,
	"iac":      true,
}

func syncDefault(registryDir string, fix bool, pluginFilter string, verifyCaps bool) error {
	pluginsDir := filepath.Join(registryDir, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fmt.Errorf("read plugins dir %q: %w", pluginsDir, err)
	}

	var pluginNames []string
	for _, e := range entries {
		if e.IsDir() {
			pluginNames = append(pluginNames, e.Name())
		}
	}
	sort.Strings(pluginNames)

	mismatches := 0

	for _, pluginName := range pluginNames {
		if pluginFilter != "" && pluginFilter != pluginName {
			continue
		}
		manifestPath := filepath.Join(pluginsDir, pluginName, "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}

		raw, err := readJSONFile(manifestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR  %s — read manifest: %v\n", pluginName, err)
			mismatches++
			continue
		}

		// Type allowlist (plan C-P3).
		manifestType, _ := raw["type"].(string)
		if manifestType != "" && !registryAllowedTypes[manifestType] {
			fmt.Fprintf(os.Stderr, "  REJECT  %s — plugin.json.type=%q is not in the registry allowlist (scaffold repos must not be registered)\n", pluginName, manifestType)
			mismatches++
			continue
		}

		repoURL, _ := raw["repository"].(string)
		if repoURL == "" {
			repoURL, _ = raw["source"].(string)
		}
		if repoURL == "" {
			continue
		}
		ghRepo := normalizeRepo(repoURL)
		if ghRepo == "" || !strings.Contains(ghRepo, "/") {
			continue
		}

		manifestVersion, _ := raw["version"].(string)

		latestTag, err := ghReleaseLatestTag(ghRepo)
		if err != nil || latestTag == "" {
			fmt.Printf("  SKIP  %s — no release found for %s\n", pluginName, ghRepo)
			continue
		}

		if !PublishGradeSemverRe.MatchString(latestTag) {
			fmt.Fprintf(os.Stderr, "  REJECT  %s — upstream release tag %s is not release-grade semver (engine ParseSemver requires flat M.m.p)\n", pluginName, latestTag)
			mismatches++
			continue
		}

		latestVersion := strings.TrimPrefix(latestTag, "v")

		downloadsOK := downloadsMatchVersion(raw, manifestVersion)

		targetVersion := manifestVersion
		targetTag := "v" + manifestVersion
		bumpVersion := false
		currentReleaseExists := releaseExists(ghRepo, targetTag)
		if !currentReleaseExists {
			currentReleaseExists = false
		}
		if versionGT(latestVersion, manifestVersion) || !currentReleaseExists {
			latestDownloads, _ := releaseDownloads(ghRepo, latestTag)
			switch {
			case len(latestDownloads) > 0:
				targetVersion = latestVersion
				targetTag = latestTag
				bumpVersion = true
			case !currentReleaseExists:
				fmt.Printf("  SKIP  %s — manifest version %s has no release and latest %s has no platform release assets\n", pluginName, manifestVersion, latestVersion)
				continue
			default:
				fmt.Printf("  SKIP  %s — latest %s has no platform release assets\n", pluginName, latestVersion)
				continue
			}
		}

		if manifestVersion == targetVersion && downloadsOK {
			fmt.Printf("    OK  %s %s\n", pluginName, manifestVersion)
		} else {
			if bumpVersion {
				fmt.Fprintf(os.Stderr, " MISMATCH  %s: manifest=%s latest=%s (%s)\n", pluginName, manifestVersion, latestVersion, ghRepo)
			}
			if !downloadsOK {
				fmt.Fprintf(os.Stderr, " MISMATCH  %s: download URLs do not match manifest version %s\n", pluginName, manifestVersion)
			}
			mismatches++
			if fix {
				if err := applyFix(manifestPath, raw, ghRepo, targetTag, targetVersion); err != nil {
					fmt.Fprintf(os.Stderr, "  ERROR  %s — apply fix: %v\n", pluginName, err)
				}
			}
		}

		if verifyCaps {
			fmt.Fprintf(os.Stderr, "  NOTE   %s — --verify-capabilities not yet implemented (workflow#762 follow-up)\n", pluginName)
		}
	}

	if mismatches > 0 && !fix {
		return fmt.Errorf("%d plugin manifest(s) need updates; re-run with --fix", mismatches)
	}
	return nil
}

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied path
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return raw, nil
}

// normalizeRepo extracts owner/repo from a GitHub URL or already-normalized
// path string. Ports the bash normalize_repo function.
func normalizeRepo(repoURL string) string {
	repoURL = strings.TrimPrefix(repoURL, "https://github.com/")
	repoURL = strings.TrimPrefix(repoURL, "http://github.com/")
	repoURL = strings.TrimPrefix(repoURL, "github.com/")
	repoURL = strings.TrimSuffix(repoURL, ".git")
	repoURL = strings.TrimSuffix(repoURL, "/")
	parts := strings.SplitN(repoURL, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func ghReleaseLatestTag(ghRepo string) (string, error) {
	cmd := exec.Command("gh", "release", "view", "--repo", ghRepo, "--json", "tagName", "-q", ".tagName") // #nosec G204 -- ghRepo is from trusted committed manifest
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func releaseExists(ghRepo, tag string) bool {
	cmd := exec.Command("gh", "release", "view", tag, "--repo", ghRepo, "--json", "tagName") // #nosec G204 -- ghRepo+tag from trusted manifest
	return cmd.Run() == nil
}

type releaseAsset struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	URL  string `json:"url"`
}

// releaseDownloads returns the platform release-asset list for a tag, in the
// shape the registry's manifest.json expects. Matches the bash
// release_downloads helper.
func releaseDownloads(ghRepo, tag string) ([]releaseAsset, error) {
	cmd := exec.Command("gh", "release", "view", tag, "--repo", ghRepo, "--json", "assets") // #nosec G204 -- ghRepo+tag from trusted manifest
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	var assets []releaseAsset
	for _, a := range resp.Assets {
		// Match goreleaser pattern: <name>-<os>-<arch>.tar.gz OR <name>_<os>_<arch>.tar.gz
		nameNoExt := strings.TrimSuffix(a.Name, ".tar.gz")
		nameNoExt = strings.TrimSuffix(nameNoExt, ".tgz")
		parts := strings.Split(nameNoExt, "-")
		if len(parts) < 3 {
			parts = strings.Split(nameNoExt, "_")
			if len(parts) < 3 {
				continue
			}
		}
		os := parts[len(parts)-2]
		arch := parts[len(parts)-1]
		// Sanity-check os/arch values
		if !isKnownOS(os) || !isKnownArch(arch) {
			continue
		}
		assets = append(assets, releaseAsset{OS: os, Arch: arch, URL: a.URL})
	}
	return assets, nil
}

func isKnownOS(s string) bool {
	switch s {
	case "linux", "darwin", "windows":
		return true
	}
	return false
}

func isKnownArch(s string) bool {
	switch s {
	case "amd64", "arm64", "386":
		return true
	}
	return false
}

func downloadsMatchVersion(raw map[string]any, version string) bool {
	downloadsRaw, _ := raw["downloads"].([]any)
	if len(downloadsRaw) == 0 {
		// No downloads → trivially match (registry handles empty download
		// lists by leaving manifest version as-is).
		return true
	}
	wantSubstr := "/releases/download/v" + version + "/"
	for _, dl := range downloadsRaw {
		dlMap, ok := dl.(map[string]any)
		if !ok {
			return false
		}
		url, _ := dlMap["url"].(string)
		if !strings.Contains(url, wantSubstr) {
			return false
		}
	}
	return true
}

// versionGT returns true when newVer > oldVer using `sort -V` semantics
// (the bash script's comparator). Preserves bash parity per plan C2; a
// semver-correct comparator can swap in after the parity cycle.
func versionGT(newVer, oldVer string) bool {
	cmd := exec.Command("sort", "-V")
	cmd.Stdin = strings.NewReader(newVer + "\n" + oldVer + "\n")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return false
	}
	// If sorted ascending, the larger value is at index 1. newVer > oldVer
	// iff newVer appears second.
	return lines[1] == newVer && newVer != oldVer
}

func applyFix(manifestPath string, raw map[string]any, ghRepo, targetTag, targetVersion string) error {
	downloads, _ := releaseDownloads(ghRepo, targetTag)
	if len(downloads) == 0 {
		raw["version"] = targetVersion
	} else {
		raw["version"] = targetVersion
		dlAny := make([]any, 0, len(downloads))
		for _, dl := range downloads {
			dlAny = append(dlAny, map[string]any{
				"os":   dl.OS,
				"arch": dl.Arch,
				"url":  dl.URL,
			})
		}
		raw["downloads"] = dlAny
	}

	// workflow#703 — also sync capabilities + minEngineVersion + iacProvider
	// from the tagged plugin.json (source-of-truth in the upstream repo).
	if pluginJSON, _ := fetchPluginJSON(ghRepo, targetTag); pluginJSON != nil {
		if caps, ok := pluginJSON["capabilities"]; ok && caps != nil {
			raw["capabilities"] = caps
		}
		if mev, ok := pluginJSON["minEngineVersion"]; ok && mev != nil {
			raw["minEngineVersion"] = mev
		}
		if iac, ok := pluginJSON["iacProvider"]; ok && iac != nil {
			raw["iacProvider"] = iac
		}
	}

	// Marshal with 2-space indent + trailing newline (matches bash jq output).
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(manifestPath, out, 0644) // #nosec G306
}

// fetchPluginJSON gets the tagged plugin.json from the upstream repo via the
// GitHub Contents API. Returns nil on any failure (silent fallback per
// bash behavior — plan C2 fix preserves this).
func fetchPluginJSON(ghRepo, tag string) (map[string]any, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/contents/plugin.json?ref=%s", ghRepo, tag), "--jq", ".content") // #nosec G204 -- ghRepo+tag from trusted manifest
	out, err := cmd.Output()
	if err != nil {
		return nil, nil //nolint:nilerr // silent fallback per bash semantics
	}
	encoded := strings.TrimSpace(string(out))
	if encoded == "" {
		return nil, nil
	}
	// GitHub Contents API returns base64-encoded content with newlines.
	encoded = strings.ReplaceAll(encoded, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	var pluginJSON map[string]any
	if err := json.Unmarshal(decoded, &pluginJSON); err != nil {
		return nil, nil //nolint:nilerr
	}
	return pluginJSON, nil
}
