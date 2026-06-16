package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// WfctlLockfile is the structure of .wfctl-lock.yaml — the machine-generated lockfile.
// It is derived from wfctl.yaml and must not be hand-edited.
// Plugin keys are sorted alphabetically for deterministic git diffs.
type WfctlLockfile struct {
	Version              int                             `yaml:"version"`
	GeneratedAt          time.Time                       `yaml:"generated_at"`
	SourceManifestSHA256 string                          `yaml:"source_manifest_sha256,omitempty"`
	LockfileSHA256       string                          `yaml:"lockfile_sha256,omitempty"`
	Plugins              map[string]WfctlLockPluginEntry `yaml:"plugins"`
}

// WfctlLockPluginEntry is the locked record for a single plugin.
type WfctlLockPluginEntry struct {
	Version string `yaml:"version"`
	Source  string `yaml:"source"`
	// SHA256 is deprecated top-level metadata from early new-format lockfiles.
	// Platform archive checksums live under Platforms; old-format binary
	// checksums are handled by cmd/wfctl/plugin_lockfile.go.
	SHA256    string                       `yaml:"sha256,omitempty"`
	Platforms map[string]WfctlLockPlatform `yaml:"platforms,omitempty"`
}

// WfctlLockPlatform holds platform-specific download info.
type WfctlLockPlatform struct {
	URL           string                  `yaml:"url"`
	SHA256        string                  `yaml:"sha256"`
	Compatibility *WfctlLockCompatibility `yaml:"compatibility,omitempty"`
}

type WfctlLockCompatibility struct {
	Mode           string `yaml:"mode,omitempty"`
	Status         string `yaml:"status,omitempty"`
	EngineVersion  string `yaml:"engine_version,omitempty"`
	EvidenceDigest string `yaml:"evidence_digest,omitempty"`
	Forced         bool   `yaml:"forced,omitempty"`
	Reason         string `yaml:"reason,omitempty"`
}

// LoadWfctlLockfile reads and parses a .wfctl-lock.yaml file.
func LoadWfctlLockfile(path string) (*WfctlLockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lockfile %s: %w", path, err)
	}
	var lf WfctlLockfile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	return &lf, nil
}

// SaveWfctlLockfile writes a lockfile to disk with sorted plugin keys for determinism.
func SaveWfctlLockfile(path string, lf *WfctlLockfile) error {
	// Default zero GeneratedAt to now so the field is always meaningful.
	if lf.GeneratedAt.IsZero() {
		lf.GeneratedAt = time.Now().UTC()
	}
	lockDigest, err := WfctlLockfileDigest(lf)
	if err != nil {
		return fmt.Errorf("digest lockfile: %w", err)
	}
	lf.LockfileSHA256 = lockDigest

	// Build a sorted yaml.Node to ensure deterministic key order.
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	addStr := func(key, val string) {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: val},
		)
	}
	addInt := func(key string, val int) {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", val)},
		)
	}

	addInt("version", lf.Version)
	addStr("generated_at", lf.GeneratedAt.UTC().Format(time.RFC3339))
	if lf.SourceManifestSHA256 != "" {
		addStr("source_manifest_sha256", lf.SourceManifestSHA256)
	}
	if lf.LockfileSHA256 != "" {
		addStr("lockfile_sha256", lf.LockfileSHA256)
	}

	// Sort plugin keys.
	names := make([]string, 0, len(lf.Plugins))
	for k := range lf.Plugins {
		names = append(names, k)
	}
	sort.Strings(names)

	pluginsNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, name := range names {
		entry := lf.Plugins[name]
		entryNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

		addField := func(k, v string) {
			entryNode.Content = append(entryNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: k},
				&yaml.Node{Kind: yaml.ScalarNode, Value: v},
			)
		}
		addField("version", entry.Version)
		addField("source", entry.Source)

		if len(entry.Platforms) > 0 {
			platKeys := make([]string, 0, len(entry.Platforms))
			for pk := range entry.Platforms {
				platKeys = append(platKeys, pk)
			}
			sort.Strings(platKeys)
			platNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for _, pk := range platKeys {
				p := entry.Platforms[pk]
				pNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				pNode.Content = append(pNode.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "url"},
					&yaml.Node{Kind: yaml.ScalarNode, Value: p.URL},
					&yaml.Node{Kind: yaml.ScalarNode, Value: "sha256"},
					&yaml.Node{Kind: yaml.ScalarNode, Value: p.SHA256},
				)
				if p.Compatibility != nil {
					cNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
					addCompatField := func(k, v string) {
						if v == "" {
							return
						}
						cNode.Content = append(cNode.Content,
							&yaml.Node{Kind: yaml.ScalarNode, Value: k},
							&yaml.Node{Kind: yaml.ScalarNode, Value: v},
						)
					}
					addCompatField("mode", p.Compatibility.Mode)
					addCompatField("status", p.Compatibility.Status)
					addCompatField("engine_version", p.Compatibility.EngineVersion)
					addCompatField("evidence_digest", p.Compatibility.EvidenceDigest)
					if p.Compatibility.Forced {
						cNode.Content = append(cNode.Content,
							&yaml.Node{Kind: yaml.ScalarNode, Value: "forced"},
							&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
						)
					}
					addCompatField("reason", p.Compatibility.Reason)
					if len(cNode.Content) > 0 {
						pNode.Content = append(pNode.Content,
							&yaml.Node{Kind: yaml.ScalarNode, Value: "compatibility"},
							cNode,
						)
					}
				}
				platNode.Content = append(platNode.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: pk},
					pNode,
				)
			}
			entryNode.Content = append(entryNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "platforms"},
				platNode,
			)
		}

		pluginsNode.Content = append(pluginsNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: name},
			entryNode,
		)
	}

	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "plugins"},
		pluginsNode,
	)

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// PopulateWfctlLockfileProvenance records the source manifest digest and the
// lockfile's current content digest. SaveWfctlLockfile refreshes the latter
// again after any caller-side mutation.
func PopulateWfctlLockfileProvenance(manifest *WfctlManifest, lf *WfctlLockfile) error {
	sourceDigest, err := WfctlManifestDigest(manifest)
	if err != nil {
		return err
	}
	lf.SourceManifestSHA256 = sourceDigest
	lockDigest, err := WfctlLockfileDigest(lf)
	if err != nil {
		return err
	}
	lf.LockfileSHA256 = lockDigest
	return nil
}

func ValidateWfctlLockfileProvenance(manifest *WfctlManifest, lf *WfctlLockfile) error {
	if lf.SourceManifestSHA256 == "" || lf.LockfileSHA256 == "" {
		return fmt.Errorf("lockfile provenance missing; run 'wfctl plugin install' or 'wfctl plugin lock'")
	}
	sourceDigest, err := WfctlManifestDigest(manifest)
	if err != nil {
		return err
	}
	if lf.SourceManifestSHA256 != sourceDigest {
		return fmt.Errorf("lockfile is stale for wfctl.yaml; run 'wfctl plugin install' or 'wfctl plugin lock'")
	}
	lockDigest, err := WfctlLockfileDigest(lf)
	if err != nil {
		return err
	}
	if lf.LockfileSHA256 != lockDigest {
		return fmt.Errorf("lockfile checksum mismatch; regenerate with 'wfctl plugin install' or 'wfctl plugin lock'")
	}
	return nil
}

type manifestDigestPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

func WfctlManifestDigest(manifest *WfctlManifest) (string, error) {
	plugins := make([]manifestDigestPlugin, 0, len(manifest.Plugins))
	for _, plugin := range manifest.Plugins {
		plugins = append(plugins, manifestDigestPlugin{
			Name:    plugin.Name,
			Version: plugin.Version,
			Source:  plugin.Source,
		})
	}
	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Name != plugins[j].Name {
			return plugins[i].Name < plugins[j].Name
		}
		if plugins[i].Version != plugins[j].Version {
			return plugins[i].Version < plugins[j].Version
		}
		return plugins[i].Source < plugins[j].Source
	})
	payload := struct {
		Version int                    `json:"version"`
		Plugins []manifestDigestPlugin `json:"plugins"`
	}{Version: manifest.Version, Plugins: plugins}
	return canonicalSHA256(payload)
}

type lockDigestPlugin struct {
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Source    string               `json:"source"`
	Platforms []lockDigestPlatform `json:"platforms,omitempty"`
}

type lockDigestPlatform struct {
	Name          string                  `json:"name"`
	URL           string                  `json:"url"`
	SHA256        string                  `json:"sha256"`
	Compatibility *WfctlLockCompatibility `json:"compatibility,omitempty"`
}

func WfctlLockfileDigest(lf *WfctlLockfile) (string, error) {
	names := make([]string, 0, len(lf.Plugins))
	for name := range lf.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	plugins := make([]lockDigestPlugin, 0, len(names))
	for _, name := range names {
		entry := lf.Plugins[name]
		platformKeys := make([]string, 0, len(entry.Platforms))
		for platform := range entry.Platforms {
			platformKeys = append(platformKeys, platform)
		}
		sort.Strings(platformKeys)
		platforms := make([]lockDigestPlatform, 0, len(platformKeys))
		for _, platform := range platformKeys {
			artifact := entry.Platforms[platform]
			platforms = append(platforms, lockDigestPlatform{
				Name:          platform,
				URL:           artifact.URL,
				SHA256:        artifact.SHA256,
				Compatibility: artifact.Compatibility,
			})
		}
		plugins = append(plugins, lockDigestPlugin{
			Name:      name,
			Version:   entry.Version,
			Source:    entry.Source,
			Platforms: platforms,
		})
	}
	payload := struct {
		Version int                `json:"version"`
		Plugins []lockDigestPlugin `json:"plugins"`
	}{Version: lf.Version, Plugins: plugins}
	return canonicalSHA256(payload)
}

func canonicalSHA256(payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}
