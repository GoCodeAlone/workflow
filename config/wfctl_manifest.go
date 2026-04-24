package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WfctlManifest is the structure of wfctl.yaml — the human-editable plugin manifest.
// It lists plugins with their declared versions and sources.
// The machine-generated lockfile (.wfctl-lock.yaml) is derived from this manifest.
type WfctlManifest struct {
	Version int                `yaml:"version"`
	Plugins []WfctlPluginEntry `yaml:"plugins"`
}

// WfctlPluginEntry is a single plugin declared in wfctl.yaml.
type WfctlPluginEntry struct {
	Name    string             `yaml:"name"`
	Version string             `yaml:"version"`
	Source  string             `yaml:"source"`
	Auth    *WfctlPluginAuth   `yaml:"auth,omitempty"`
	Verify  *WfctlPluginVerify `yaml:"verify,omitempty"`
}

// WfctlPluginAuth holds auth configuration for private plugin registries.
type WfctlPluginAuth struct {
	Env string `yaml:"env"`
}

// WfctlPluginVerify holds sigstore/cosign identity for supply-chain verification.
type WfctlPluginVerify struct {
	Identity string `yaml:"identity"`
}

// LoadWfctlManifest reads and parses a wfctl.yaml manifest file.
func LoadWfctlManifest(path string) (*WfctlManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m WfctlManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// SaveWfctlManifest writes a manifest to disk in canonical YAML form.
func SaveWfctlManifest(path string, m *WfctlManifest) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}
