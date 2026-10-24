package main

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// These catalogs share releases with standalone tools or other plugins. Keep
// their archive and metadata roles aligned with the registry catalog contract.
// Use the catalog directory and repository, not the editable manifest name.
type registrySyncCatalogRole struct {
	artifact     string
	metadataFile string
	platformOS   []string
}

func registrySyncRole(repo, catalog string) registrySyncCatalogRole {
	role := registrySyncCatalogRole{metadataFile: "plugin.json"}
	switch {
	case strings.EqualFold(repo, "GoCodeAlone/workflow-plugin-github") && catalog == "github":
		role.artifact = "workflow-plugin-github"
	case strings.EqualFold(repo, "GoCodeAlone/workflow-plugin-migrations") && catalog == "workflow-plugin-migrations":
		role.artifact = "workflow-plugin-migrations"
	case strings.EqualFold(repo, "GoCodeAlone/workflow-plugin-migrations") && catalog == "workflow-plugin-atlas-migrate":
		role.artifact = "workflow-plugin-atlas-migrate"
		role.metadataFile = "plugin.atlas.json"
	}
	if role.artifact != "" {
		role.platformOS = []string{"darwin", "linux", "windows"}
		if role.metadataFile == "plugin.atlas.json" {
			role.platformOS = []string{"darwin", "linux"}
		}
	}
	return role
}

func registrySyncCatalog(manifestPath string) string {
	return filepath.Base(filepath.Dir(manifestPath))
}

func (role registrySyncCatalogRole) selectDownloads(assets []releaseAsset) ([]releaseAsset, error) {
	if role.artifact == "" {
		return assets, nil
	}
	var selected []releaseAsset
	seen := map[string]bool{}
	for _, asset := range assets {
		if !slices.Contains(role.platformOS, asset.OS) || (asset.Arch != "amd64" && asset.Arch != "arm64") ||
			asset.Name != role.artifact+"-"+asset.OS+"-"+asset.Arch+".tar.gz" {
			continue
		}
		platform := asset.OS + "/" + asset.Arch
		if seen[platform] {
			return nil, fmt.Errorf("duplicate %s release platform %s", role.artifact, platform)
		}
		seen[platform] = true
		selected = append(selected, asset)
	}
	for _, goos := range role.platformOS {
		for _, arch := range []string{"amd64", "arm64"} {
			if !seen[goos+"/"+arch] {
				return nil, fmt.Errorf("missing full plugin archive %s-%s-%s.tar.gz", role.artifact, goos, arch)
			}
		}
	}
	return selected, nil
}

func registrySyncCatalogDownloads(repo, tag, catalog string) ([]releaseAsset, error) {
	assets, err := registrySyncReleaseDownloads(repo, tag)
	if err != nil {
		return nil, err
	}
	return registrySyncRole(repo, catalog).selectDownloads(assets)
}

func registrySyncDownloadsMatchRole(raw map[string]any, repo, catalog string) bool {
	role := registrySyncRole(repo, catalog)
	if role.artifact == "" {
		return true
	}
	downloads, _ := raw["downloads"].([]any)
	assets := make([]releaseAsset, 0, len(downloads))
	for _, value := range downloads {
		entry, _ := value.(map[string]any)
		assetURL, _ := entry["url"].(string)
		parsed, err := url.Parse(assetURL)
		if err != nil {
			return false
		}
		goos, _ := entry["os"].(string)
		arch, _ := entry["arch"].(string)
		assets = append(assets, releaseAsset{Name: path.Base(parsed.Path), OS: goos, Arch: arch})
	}
	selected, err := role.selectDownloads(assets)
	return err == nil && len(selected) == len(downloads)
}

func registrySyncCatalogMetadata(repo, tag, catalog string) (map[string]any, error) {
	role := registrySyncRole(repo, catalog)
	if role.metadataFile == "plugin.json" {
		return registrySyncFetchPluginJSON(repo, tag)
	}
	metadata, err := fetchPluginJSONAtPath(repo, tag, role.metadataFile)
	if err != nil {
		return nil, err
	}
	if metadata["name"] != role.artifact {
		return nil, fmt.Errorf("%s name must be %s", role.metadataFile, role.artifact)
	}
	return metadata, nil
}
