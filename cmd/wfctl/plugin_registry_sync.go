package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
//  5. Skips built-in/core manifests; those are owned by "registry-sync core".
//  6. Compares manifest.version + downloads URLs; with --fix rewrites.
//  7. Fetches tagged plugin.json from upstream; syncs capabilities,
//     minEngineVersion, iacProvider into registry manifest.
//  8. (--verify-capabilities) Downloads release tarball + spawns binary;
//     reuses wfctl plugin verify-capabilities to diff runtime GetManifest
//     against the registry manifest.
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
	verifyCaps := fs.Bool("verify-capabilities", false, "Spawn binary + diff capabilities (registry-side; slow)")
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
		if isCoreRegistryManifestType(manifestType) {
			fmt.Printf("  SKIP  %s — %s manifests are synced by registry-sync core\n", pluginName, manifestType)
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
			latestDownloads, err := releaseDownloads(ghRepo, latestTag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ERROR  %s — list release downloads for %s: %v\n", pluginName, latestTag, err)
				mismatches++
				continue
			}
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
			verifyName, _ := raw["name"].(string)
			if verifyName == "" {
				verifyName = pluginName
			}
			if err := verifyRegistryPluginCapabilities(verifyName, manifestPath, ghRepo, targetTag); err != nil {
				fmt.Fprintf(os.Stderr, "  ERROR  %s — verify capabilities: %v\n", pluginName, err)
				mismatches++
				continue
			}
			fmt.Printf("    OK  %s capabilities verified against %s (%s/%s)\n", pluginName, targetTag, runtime.GOOS, runtime.GOARCH)
		}
	}

	if mismatches > 0 && !fix {
		return fmt.Errorf("%d plugin manifest(s) need updates; re-run with --fix", mismatches)
	}
	return nil
}

func isCoreRegistryManifestType(manifestType string) bool {
	switch manifestType {
	case "builtin", "core":
		return true
	default:
		return false
	}
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
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
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
	checksums, err := releaseChecksums(ghRepo, tag)
	if err != nil {
		return nil, err
	}
	var assets []releaseAsset
	for _, a := range resp.Assets {
		goos, goarch, ok := releaseAssetPlatform(a.Name)
		if !ok {
			continue
		}
		assets = append(assets, releaseAsset{
			Name:   a.Name,
			OS:     goos,
			Arch:   goarch,
			URL:    a.URL,
			SHA256: checksums[a.Name],
		})
	}
	return assets, nil
}

func releaseChecksums(ghRepo, tag string) (map[string]string, error) {
	cmd := exec.Command("gh", "release", "download", tag, "--repo", ghRepo, "--pattern", "checksums.txt", "--output", "-") // #nosec G204 -- ghRepo+tag from trusted manifest
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "no assets match") || strings.Contains(msg, "no matching assets") || strings.Contains(msg, "not found") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("download checksums.txt: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseReleaseChecksums(string(out)), nil
}

func parseReleaseChecksums(text string) map[string]string {
	checksums := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sha, err := NormalizeSHA256Hex(parts[0])
		if err != nil {
			continue
		}
		name := strings.TrimSpace(parts[1])
		name = strings.TrimPrefix(name, "*")
		if name == "" {
			continue
		}
		checksums[filepath.Base(name)] = sha
	}
	return checksums
}

func releaseAssetPlatform(assetName string) (string, string, bool) {
	nameNoExt := strings.TrimSuffix(assetName, ".tar.gz")
	nameNoExt = strings.TrimSuffix(nameNoExt, ".tgz")
	for _, sep := range []string{"-", "_"} {
		parts := strings.Split(nameNoExt, sep)
		if len(parts) < 3 {
			continue
		}
		os := parts[len(parts)-2]
		arch := parts[len(parts)-1]
		if isKnownOS(os) && isKnownArch(arch) {
			return os, arch, true
		}
	}
	return "", "", false
}

var (
	registrySyncReleaseDownloads     = releaseDownloads
	registrySyncDownloadReleaseAsset = downloadReleaseAsset
	registrySyncVerifyManifest       = verifyPluginManifestAgainstBinaryWithOptions
)

func verifyRegistryPluginCapabilities(pluginName, manifestPath, ghRepo, tag string) error {
	assets, err := registrySyncReleaseDownloads(ghRepo, tag)
	if err != nil {
		return fmt.Errorf("list release downloads for %s %s: %w", ghRepo, tag, err)
	}
	asset, ok := selectPlatformReleaseAsset(assets, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no %s/%s release asset found for %s %s", runtime.GOOS, runtime.GOARCH, ghRepo, tag)
	}

	tmpDir, err := os.MkdirTemp("", "wfctl-registry-sync-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	assetPath, err := registrySyncDownloadReleaseAsset(ghRepo, tag, asset.Name, tmpDir)
	if err != nil {
		return err
	}

	searchDir := tmpDir
	if isTarGz(assetPath) {
		extractDir := filepath.Join(tmpDir, "extracted")
		file, err := os.Open(assetPath) // #nosec G304 -- release asset downloaded to agent tempdir
		if err != nil {
			return err
		}
		if err := extractTarGzReader(file, extractDir); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		searchDir = extractDir
	}

	binaryPath, err := locateRegistrySyncBinary(searchDir, pluginName, assetBinaryName(asset.Name))
	if err != nil {
		return err
	}
	return registrySyncVerifyManifest(binaryPath, manifestPath, manifestCompareOptions{SkipName: true})
}

func selectPlatformReleaseAsset(assets []releaseAsset, goos, goarch string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.OS == goos && asset.Arch == goarch {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func downloadReleaseAsset(ghRepo, tag, assetName, dir string) (string, error) {
	if assetName == "" {
		return "", fmt.Errorf("release asset name is empty")
	}
	cmd := exec.Command("gh", "release", "download", tag, "--repo", ghRepo, "--pattern", assetName, "--dir", dir, "--clobber") // #nosec G204 -- ghRepo+tag+assetName from trusted registry manifest/release metadata
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gh release download %s %s %s: %w: %s", ghRepo, tag, assetName, err, strings.TrimSpace(string(out)))
	}
	return filepath.Join(dir, assetName), nil
}

func isTarGz(path string) bool {
	return strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tgz")
}

func assetBinaryName(assetName string) string {
	name := strings.TrimSuffix(assetName, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")
	for _, sep := range []string{"-", "_"} {
		parts := strings.Split(name, sep)
		if len(parts) >= 3 && isKnownOS(parts[len(parts)-2]) && isKnownArch(parts[len(parts)-1]) {
			return strings.Join(parts[:len(parts)-2], sep)
		}
	}
	return name
}

func locateRegistrySyncBinary(root string, names ...string) (string, error) {
	wanted := map[string]bool{}
	for _, name := range names {
		if name == "" {
			continue
		}
		wanted[name] = true
		wanted[name+".exe"] = true
	}
	var candidates []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if !wanted[base] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		var requested []string
		for name := range wanted {
			requested = append(requested, name)
		}
		sort.Strings(requested)
		return "", fmt.Errorf("no executable matching %v found under %s", requested, root)
	}
	return candidates[0], nil
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
	downloads, err := registrySyncReleaseDownloads(ghRepo, targetTag)
	if err != nil {
		return fmt.Errorf("list release downloads: %w", err)
	}
	if len(downloads) == 0 {
		raw["version"] = targetVersion
	} else {
		existingSHAs := existingDownloadSHA256(raw)
		raw["version"] = targetVersion
		dlAny := make([]any, 0, len(downloads))
		for _, dl := range downloads {
			sha := dl.SHA256
			if sha == "" {
				sha = existingSHAs[downloadIdentity(dl.OS, dl.Arch, dl.URL)]
			}
			entry := map[string]any{
				"os":   dl.OS,
				"arch": dl.Arch,
				"url":  dl.URL,
			}
			if sha != "" {
				entry["sha256"] = sha
			}
			dlAny = append(dlAny, entry)
		}
		raw["downloads"] = dlAny
	}

	// workflow#703 — also sync capabilities + minEngineVersion + iacProvider
	// from the tagged plugin.json (source-of-truth in the upstream repo).
	if pluginJSON, _ := fetchPluginJSON(ghRepo, targetTag); pluginJSON != nil {
		syncManifestMetadataFromPluginJSON(raw, pluginJSON)
	}

	// Marshal with 2-space indent + trailing newline (matches bash jq output).
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(manifestPath, out, 0644) // #nosec G306
}

func existingDownloadSHA256(raw map[string]any) map[string]string {
	out := make(map[string]string)
	downloads, _ := raw["downloads"].([]any)
	for _, item := range downloads {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		goos, _ := entry["os"].(string)
		goarch, _ := entry["arch"].(string)
		url, _ := entry["url"].(string)
		sha, _ := entry["sha256"].(string)
		if goos == "" || goarch == "" || url == "" || sha == "" {
			continue
		}
		normalized, err := NormalizeSHA256Hex(sha)
		if err != nil {
			continue
		}
		out[downloadIdentity(goos, goarch, url)] = normalized
	}
	return out
}

func downloadIdentity(goos, goarch, url string) string {
	return goos + "\x00" + goarch + "\x00" + url
}

func syncManifestMetadataFromPluginJSON(raw, pluginJSON map[string]any) {
	var caps map[string]any
	if caps, ok := pluginJSON["capabilities"]; ok && caps != nil {
		if capsObj, ok := caps.(map[string]any); ok {
			raw["capabilities"] = capsObj
		}
	}
	caps, _ = raw["capabilities"].(map[string]any)

	services := registrySyncStringSliceFromAny(pluginJSON["iacServices"])
	if nestedServices := registrySyncStringSliceFromAny(caps["iacServices"]); len(nestedServices) > 0 {
		services = appendUniqueStrings(services, nestedServices...)
		delete(caps, "iacServices")
	}
	if len(services) > 0 {
		if caps == nil {
			caps = map[string]any{}
			raw["capabilities"] = caps
		}
		caps["serviceMethods"] = appendUniqueStrings(registrySyncStringSliceFromAny(caps["serviceMethods"]), services...)
	}
	if mev, ok := pluginJSON["minEngineVersion"]; ok && mev != nil {
		raw["minEngineVersion"] = mev
	}
	if iac, ok := pluginJSON["iacProvider"]; ok && iac != nil {
		raw["iacProvider"] = iac
	}
}

func registrySyncStringSliceFromAny(v any) []string {
	switch values := v.(type) {
	case []string:
		return values
	case []any:
		var out []string
		for _, value := range values {
			if s, ok := value.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]bool, len(base)+len(values))
	var out []string
	for _, value := range append(base, values...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
