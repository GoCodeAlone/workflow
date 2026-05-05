package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

const destructiveApprovalArtifactName = "wfctl-destructive-approval.json"

type destructiveDecision struct {
	Operation            string `json:"operation"`
	Env                  string `json:"env"`
	App                  string `json:"app,omitempty"`
	Database             string `json:"database,omitempty"`
	ExpectedDirtyVersion string `json:"expected_dirty_version,omitempty"`
	ForceVersion         string `json:"force_version,omitempty"`
	RequiresApproval     bool   `json:"requires_approval"`
}

func requireDestructiveApproval(decision destructiveDecision, approved bool, artifactPath string) (*interfaces.MigrationRepairResult, error) {
	if !destructiveEnvRequiresApproval(decision.Env) || approved {
		return nil, nil
	}

	decision.RequiresApproval = true
	path := destructiveApprovalArtifactPath(artifactPath)
	if err := writeDestructiveApprovalArtifact(path, decision); err != nil {
		return nil, err
	}

	return &interfaces.MigrationRepairResult{
		Status: interfaces.MigrationRepairStatusApprovalRequired,
	}, fmt.Errorf("approval required for destructive operation %q in environment %q; review %s and rerun with approval", decision.Operation, decision.Env, path)
}

func destructiveEnvRequiresApproval(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return false
	default:
		return true
	}
}

func destructiveApprovalArtifactPath(artifactPath string) string {
	if strings.TrimSpace(artifactPath) != "" {
		return artifactPath
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		if runnerTemp := strings.TrimSpace(os.Getenv("RUNNER_TEMP")); runnerTemp != "" {
			return filepath.Join(runnerTemp, destructiveApprovalArtifactName)
		}
	}
	return destructiveApprovalArtifactName
}

func writeDestructiveApprovalArtifact(path string, decision destructiveDecision) error {
	data, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("encode destructive approval artifact: %w", err)
	}
	data = append(data, '\n')
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create destructive approval artifact directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write destructive approval artifact: %w", err)
	}
	return nil
}
