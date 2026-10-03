package main

import (
	"bytes"
	stdcmp "cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

type registrySyncGitObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type registrySyncReleaseSnapshot struct {
	Release         *githubReleaseMetadata
	TagObjects      []registrySyncGitObject
	SourceSHA       string
	ChecksumsSHA256 string
	Checksums       map[string]string
	PluginJSON      []byte
	Downloads       []releaseAsset
}

func registrySyncStableTag(version string) (string, error) {
	tag := "v" + strings.TrimPrefix(version, "v")
	if !PublishGradeSemverRe.MatchString(tag) || !semver.IsValid(tag) || semver.Canonical(tag) != tag {
		return "", fmt.Errorf("target-version %q must be an exact stable M.m.p version", version)
	}
	return tag, nil
}

func syncRegistryTarget(registryDir, plugin, target string, fix, verifyCaps bool) error {
	tag, err := registrySyncStableTag(target)
	if err != nil {
		return err
	}
	return syncRegistryRelease(registryDir, plugin, tag, "", fix, verifyCaps)
}

func syncRegistryRelease(registryDir, plugin, tag, reportPath string, fix, verifyCaps bool) error {
	manifestPath, err := registrySyncManifestPath(registryDir, plugin)
	if err != nil {
		return err
	}
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := registrySyncDecodeJSON(before, &manifest); err != nil {
		return fmt.Errorf("read registry manifest: %w", err)
	}
	oldVersion, _ := manifest["version"].(string)
	var report *registrySyncReportContext
	if reportPath != "" {
		report, err = prepareRegistrySyncReport(registryDir, plugin, reportPath)
		if err != nil {
			return err
		}
		defer report.close()
	}
	manifestType, _ := manifest["type"].(string)
	if !registryAllowedTypes[manifestType] || isCoreRegistryManifestType(manifestType) {
		return fmt.Errorf("plugin %q type %q cannot be synced from a plugin release", plugin, manifestType)
	}
	repository, _ := manifest["repository"].(string)
	if repository == "" {
		repository, _ = manifest["source"].(string)
	}
	repo, err := registrySyncRepository(repository)
	if err != nil {
		return err
	}
	if tag == "" {
		latest, err := ghReleaseLatestTag(repo)
		if err != nil {
			return err
		}
		tag, err = registrySyncStableTag(latest)
		if err != nil {
			return err
		}
		if current, err := registrySyncStableTag(oldVersion); err == nil && semver.Compare(current, tag) > 0 {
			tag = current
		}
	}
	first, err := registrySyncReadRelease(repo, tag)
	if err != nil {
		return err
	}
	after, err := registrySyncTargetManifest(manifest, first, strings.TrimPrefix(tag, "v"))
	if err != nil {
		return err
	}
	if after == nil {
		after = before
	}
	if verifyCaps {
		if err := verifyRegistryPluginCapabilities(plugin, manifestPath, repo, tag); err != nil {
			return err
		}
	}
	second, err := registrySyncReadRelease(repo, tag)
	if err != nil {
		return fmt.Errorf("release/source changed or became unreadable during sync: %w", err)
	}
	firstBytes, err := json.Marshal(first)
	if err != nil {
		return err
	}
	secondBytes, err := json.Marshal(second)
	if err != nil {
		return err
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		return fmt.Errorf("release, tag, source, assets or checksums changed during sync")
	}
	current, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return fmt.Errorf("registry manifest changed during sync")
	}
	if report != nil {
		if err := report.write(plugin, repository, repo, oldVersion, strings.TrimPrefix(tag, "v"), first, after); err != nil {
			return err
		}
	}
	if bytes.Equal(before, after) {
		fmt.Printf("  OK  %s — %s\n", plugin, strings.TrimPrefix(tag, "v"))
		return nil
	}
	if !fix {
		return fmt.Errorf("plugin %q differs from target %s (use --fix)", plugin, tag)
	}
	if _, err := registrySyncManifestPath(registryDir, plugin); err != nil {
		return err
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(manifestPath, after, info.Mode().Perm()); err != nil {
		return err
	}
	fmt.Printf("  FIXED  %s — %s\n", plugin, strings.TrimPrefix(tag, "v"))
	return nil
}

func registrySyncManifestPath(registryDir, plugin string) (string, error) {
	if plugin == "" || plugin == "." || plugin == ".." || strings.ContainsAny(plugin, `/\\`) || !filepath.IsLocal(plugin) {
		return "", fmt.Errorf("--plugin must name one plugin directory without path separators")
	}
	root, err := filepath.Abs(registryDir)
	if err != nil {
		return "", err
	}
	path := root
	for _, component := range []string{"plugins", plugin, "manifest.json"} {
		path = filepath.Join(path, component)
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("registry plugin path contains symlink: %s", path)
		}
	}
	return path, nil
}

func registrySyncRepository(repository string) (string, error) {
	repo := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(repository, "https://"), "github.com/"), ".git")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("plugin repository %q must name a GitHub owner/repository", repository)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ \t\r\n") {
			return "", fmt.Errorf("invalid plugin repository %q", repository)
		}
	}
	return repo, nil
}

func registrySyncReadRelease(repo, tag string) (*registrySyncReleaseSnapshot, error) {
	apiRepo := "repos/" + escapedGitHubRepoPath(repo)
	var ref struct {
		Object registrySyncGitObject `json:"object"`
	}
	if err := registrySyncGitHubJSON(apiRepo+"/git/ref/tags/"+url.PathEscape(tag), &ref); err != nil {
		return nil, err
	}
	snapshot := &registrySyncReleaseSnapshot{TagObjects: []registrySyncGitObject{ref.Object}}
	object := ref.Object
	for depth := 0; object.Type == "tag" && depth < 8; depth++ {
		if !registrySyncValidGitSHA(object.SHA) {
			return nil, fmt.Errorf("invalid tag object SHA")
		}
		var annotated struct {
			SHA    string                `json:"sha"`
			Object registrySyncGitObject `json:"object"`
		}
		if err := registrySyncGitHubJSON(apiRepo+"/git/tags/"+object.SHA, &annotated); err != nil {
			return nil, err
		}
		if annotated.SHA != object.SHA {
			return nil, fmt.Errorf("tag object identity mismatch")
		}
		object = annotated.Object
		snapshot.TagObjects = append(snapshot.TagObjects, object)
	}
	if object.Type != "commit" || !registrySyncValidGitSHA(object.SHA) {
		return nil, fmt.Errorf("release tag must resolve to an exact commit SHA")
	}
	snapshot.SourceSHA = object.SHA
	release, err := githubReleaseByTag(repo, tag)
	if err != nil {
		return nil, err
	}
	if release.ID <= 0 || release.TagName != tag || release.Draft || release.Prerelease || !release.Immutable {
		return nil, fmt.Errorf("target must have an identified stable, published immutable release matching %s", tag)
	}
	slices.SortFunc(release.Assets, func(a, b githubReleaseAsset) int { return stdcmp.Compare(a.Name, b.Name) })
	snapshot.Release = release
	var checksumURL string
	seenNames, seenIDs := map[string]bool{}, map[int64]bool{}
	for _, asset := range release.Assets {
		if asset.ID <= 0 || seenNames[asset.Name] || seenIDs[asset.ID] || asset.Size < 0 || asset.BrowserDownloadURL == "" || asset.Name == "" || strings.ContainsAny(asset.Name, `/\\`) {
			return nil, fmt.Errorf("release contains invalid or duplicate asset identity")
		}
		seenNames[asset.Name], seenIDs[asset.ID] = true, true
		if asset.Name == "checksums.txt" {
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if checksumURL == "" {
		return nil, fmt.Errorf("release is missing checksums.txt")
	}
	checksumBytes, err := downloadURL(checksumURL)
	if err != nil {
		return nil, fmt.Errorf("read release checksums: %w", err)
	}
	snapshot.ChecksumsSHA256 = registrySyncSHA256(checksumBytes)
	checksums, err := registrySyncStrictChecksums(string(checksumBytes))
	if err != nil {
		return nil, err
	}
	snapshot.Checksums = checksums
	seenPlatforms := map[string]bool{}
	for _, asset := range release.Assets {
		goos, goarch, ok := releaseAssetPlatform(asset.Name)
		if !ok {
			continue
		}
		checksum := checksums[asset.Name]
		if checksum == "" || (asset.Digest != "" && asset.Digest != "sha256:"+checksum) {
			return nil, fmt.Errorf("missing or inconsistent checksum for asset %s", asset.Name)
		}
		platform := goos + "/" + goarch
		if seenPlatforms[platform] {
			return nil, fmt.Errorf("duplicate release platform %s", platform)
		}
		seenPlatforms[platform] = true
		snapshot.Downloads = append(snapshot.Downloads, releaseAsset{Name: asset.Name, OS: goos, Arch: goarch, URL: asset.BrowserDownloadURL, SHA256: checksum})
	}
	if len(snapshot.Downloads) == 0 {
		return nil, fmt.Errorf("release has no platform assets")
	}
	var content struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := registrySyncGitHubJSON(apiRepo+"/contents/plugin.json?ref="+snapshot.SourceSHA, &content); err != nil {
		return nil, err
	}
	if content.Encoding != "base64" {
		return nil, fmt.Errorf("source plugin.json must have base64 encoding")
	}
	snapshot.PluginJSON, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode source plugin.json: %w", err)
	}
	return snapshot, nil
}

func registrySyncTargetManifest(manifest map[string]any, snapshot *registrySyncReleaseSnapshot, target string) ([]byte, error) {
	before, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var pluginJSON map[string]any
	if err := registrySyncDecodeJSON(snapshot.PluginJSON, &pluginJSON); err != nil {
		return nil, fmt.Errorf("parse pinned source plugin.json: %w", err)
	}
	if version, ok := pluginJSON["version"].(string); ok && strings.TrimPrefix(version, "v") != target {
		return nil, fmt.Errorf("pinned plugin.json version does not match target release")
	}
	manifest["version"] = target
	downloads := make([]any, 0, len(snapshot.Downloads))
	for _, asset := range snapshot.Downloads {
		downloads = append(downloads, map[string]any{"os": asset.OS, "arch": asset.Arch, "url": asset.URL, "sha256": asset.SHA256})
	}
	manifest["downloads"] = downloads
	syncManifestMetadataFromPluginJSON(manifest, pluginJSON)
	after, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(before, after) {
		return nil, nil
	}
	formatted, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(formatted, '\n'), nil
}

func registrySyncStrictChecksums(text string) (map[string]string, error) {
	checksums := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		hash, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		sha, err := NormalizeSHA256Hex(hash)
		if !ok || err != nil || name == "" || strings.ContainsAny(name, `/\\`) || checksums[name] != "" {
			return nil, fmt.Errorf("release checksums contain invalid or duplicate entries")
		}
		checksums[name] = sha
	}
	return checksums, nil
}

func registrySyncDecodeJSON(data []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("JSON contains trailing data: %v", err)
	}
	return nil
}

func registrySyncValidGitSHA(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 20 && strings.ToLower(value) == value
}

func registrySyncSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
