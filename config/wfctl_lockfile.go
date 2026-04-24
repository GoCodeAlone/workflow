package config

import (
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
	Version     int                             `yaml:"version"`
	GeneratedAt time.Time                       `yaml:"generated_at"`
	Plugins     map[string]WfctlLockPluginEntry `yaml:"plugins"`
}

// WfctlLockPluginEntry is the locked record for a single plugin.
type WfctlLockPluginEntry struct {
	Version   string                       `yaml:"version"`
	Source    string                       `yaml:"source"`
	SHA256    string                       `yaml:"sha256"`
	Platforms map[string]WfctlLockPlatform `yaml:"platforms,omitempty"`
}

// WfctlLockPlatform holds platform-specific download info.
type WfctlLockPlatform struct {
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
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
		addField("sha256", entry.SHA256)

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
