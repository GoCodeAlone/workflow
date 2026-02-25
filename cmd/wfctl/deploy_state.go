package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

const deployStateSchemaVersion = 1

// DeployedModuleState records what was deployed for a single module.
type DeployedModuleState struct {
	// Type is the module type string (e.g. "storage.sqlite").
	Type string `json:"type"`
	// Stateful indicates whether this module manages persistent state.
	Stateful bool `json:"stateful"`
	// ResourceID is the infrastructure resource identifier generated for this
	// module (e.g. "database/prod-orders-db").
	ResourceID string `json:"resourceId,omitempty"`
	// Config is a snapshot of the module's config at deploy time.
	Config map[string]any `json:"config,omitempty"`
}

// DeployedPipelineState records what was deployed for a single pipeline.
type DeployedPipelineState struct {
	// Trigger is the pipeline trigger type (e.g. "http").
	Trigger string `json:"trigger"`
	// Path is the HTTP path if the trigger is HTTP-based.
	Path string `json:"path,omitempty"`
	// Method is the HTTP method if the trigger is HTTP-based.
	Method string `json:"method,omitempty"`
}

// DeployedResources is the top-level resource map inside a DeploymentState.
type DeployedResources struct {
	Modules   map[string]DeployedModuleState   `json:"modules,omitempty"`
	Pipelines map[string]DeployedPipelineState `json:"pipelines,omitempty"`
}

// DeploymentState is the full state manifest written after a successful deploy.
// It is serialised to deployment.state.json alongside the workflow config.
type DeploymentState struct {
	// Version is the manifest format version (currently "1").
	Version string `json:"version"`
	// ConfigHash is a SHA-256 hex digest of the config file contents at deploy time.
	ConfigHash string `json:"configHash"`
	// DeployedAt is the RFC 3339 timestamp of the deployment.
	DeployedAt time.Time `json:"deployedAt"`
	// ConfigFile is the path to the workflow config file that was deployed.
	ConfigFile string `json:"configFile"`
	// Resources contains per-module and per-pipeline state records.
	Resources DeployedResources `json:"resources"`
	// SchemaVersion is an integer version for the state file format.
	SchemaVersion int `json:"schemaVersion"`
	// Migrations lists migration IDs that have been applied.
	Migrations []string `json:"migrations,omitempty"`
}

// SaveState writes the DeploymentState to a JSON file at path.
func SaveState(state *DeploymentState, path string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal deployment state: %w", err)
	}
	if err := os.WriteFile(path, data, 0640); err != nil { //nolint:gosec // G306: deploy state file
		return fmt.Errorf("write deployment state: %w", err)
	}
	return nil
}

// LoadState reads and deserialises a DeploymentState from a JSON file at path.
// Returns an error if the file does not exist or cannot be parsed.
func LoadState(path string) (*DeploymentState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deployment state: %w", err)
	}
	var state DeploymentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse deployment state: %w", err)
	}
	return &state, nil
}

// BuildStateFromConfig constructs a DeploymentState from a loaded WorkflowConfig.
// configFile is the original config file path (used for display and hashing).
// namespace is used when generating resource IDs (may be empty).
// migrations is the optional list of already-applied migration IDs.
func BuildStateFromConfig(cfg *config.WorkflowConfig, configFile, namespace string, migrations []string) (*DeploymentState, error) {
	// Hash the config file if it exists.
	configHash := ""
	if configFile != "" {
		h, err := hashFile(configFile)
		if err == nil {
			configHash = "sha256:" + h
		}
	}

	state := &DeploymentState{
		Version:       "1",
		ConfigHash:    configHash,
		DeployedAt:    time.Now().UTC(),
		ConfigFile:    configFile,
		SchemaVersion: deployStateSchemaVersion,
		Migrations:    migrations,
		Resources: DeployedResources{
			Modules:   make(map[string]DeployedModuleState),
			Pipelines: make(map[string]DeployedPipelineState),
		},
	}

	// Populate modules.
	for _, mod := range cfg.Modules {
		stateful := IsStateful(mod.Type)
		resourceID := ""
		if stateful {
			resourceID = GenerateResourceID(mod.Name, mod.Type, namespace)
		}

		// Deep-copy the config map so mutations to the original don't bleed in.
		var cfgCopy map[string]any
		if len(mod.Config) > 0 {
			cfgCopy = make(map[string]any, len(mod.Config))
			for k, v := range mod.Config {
				cfgCopy[k] = v
			}
		}

		state.Resources.Modules[mod.Name] = DeployedModuleState{
			Type:       mod.Type,
			Stateful:   stateful,
			ResourceID: resourceID,
			Config:     cfgCopy,
		}
	}

	// Populate pipelines.
	for name, raw := range cfg.Pipelines {
		pipelineMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ps := buildPipelineState(pipelineMap)
		state.Resources.Pipelines[name] = ps
	}

	return state, nil
}

// buildPipelineState extracts trigger metadata from a raw pipeline map.
func buildPipelineState(pipelineMap map[string]any) DeployedPipelineState {
	ps := DeployedPipelineState{}

	triggerRaw, ok := pipelineMap["trigger"]
	if !ok {
		return ps
	}

	triggerMap, ok := triggerRaw.(map[string]any)
	if !ok {
		return ps
	}

	ps.Trigger, _ = triggerMap["type"].(string)

	cfgRaw, ok := triggerMap["config"]
	if !ok {
		return ps
	}
	triggerCfg, ok := cfgRaw.(map[string]any)
	if !ok {
		return ps
	}

	ps.Path, _ = triggerCfg["path"].(string)
	ps.Method, _ = triggerCfg["method"].(string)

	return ps
}

// hashFile computes a hex-encoded SHA-256 digest of the file at path.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
