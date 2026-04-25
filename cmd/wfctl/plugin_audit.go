package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pluginAuditResult struct {
	RepoPath        string        `json:"repoPath"`
	Name            string        `json:"name"`
	HasGoMod        bool          `json:"hasGoMod"`
	HasPluginJSON   bool          `json:"hasPluginJSON"`
	ManifestShape   string        `json:"manifestShape"`
	ManifestName    string        `json:"manifestName"`
	ManifestVersion string        `json:"manifestVersion"`
	Findings        []planFinding `json:"findings"`
}

func auditPluginRepo(path string) pluginAuditResult {
	result := pluginAuditResult{
		RepoPath:        path,
		Name:            filepath.Base(path),
		HasGoMod:        pluginAuditFileExists(filepath.Join(path, "go.mod")),
		HasPluginJSON:   pluginAuditFileExists(filepath.Join(path, "plugin.json")),
		ManifestShape:   "missing",
		ManifestName:    "",
		ManifestVersion: "",
	}
	if !result.HasPluginJSON {
		if result.HasGoMod {
			result.Findings = append(result.Findings, planFinding{
				Path:    filepath.Join(path, "plugin.json"),
				Level:   "ERROR",
				Code:    "missing_plugin_manifest",
				Message: "workflow plugin repo has no plugin.json",
			})
		}
		return result
	}

	data, err := os.ReadFile(filepath.Join(path, "plugin.json"))
	if err != nil {
		result.ManifestShape = "invalid-json"
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(path, "plugin.json"),
			Level:   "ERROR",
			Code:    "read_plugin_manifest",
			Message: err.Error(),
		})
		return result
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		result.ManifestShape = "invalid-json"
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(path, "plugin.json"),
			Level:   "ERROR",
			Code:    "invalid_plugin_manifest_json",
			Message: err.Error(),
		})
		return result
	}

	result.ManifestName = stringFromAny(manifest["name"])
	result.ManifestVersion = stringFromAny(manifest["version"])
	result.ManifestShape = classifyPluginManifestShape(manifest)
	addPluginManifestFindings(&result)
	return result
}

func auditPluginRepos(root string) ([]pluginAuditResult, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	results := make([]pluginAuditResult, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "workflow-plugin-") {
			continue
		}
		repoPath := filepath.Join(root, entry.Name())
		if !pluginAuditFileExists(filepath.Join(repoPath, "go.mod")) && !pluginAuditFileExists(filepath.Join(repoPath, "plugin.json")) {
			continue
		}
		results = append(results, auditPluginRepo(repoPath))
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func classifyPluginManifestShape(manifest map[string]any) string {
	if _, ok := manifest["moduleTypes"]; ok {
		return "top-level-types"
	}
	if _, ok := manifest["stepTypes"]; ok {
		return "top-level-types"
	}
	if capabilities, ok := manifest["capabilities"]; ok {
		switch capabilities.(type) {
		case []any:
			return "capabilities-array"
		case map[string]any:
			return "canonical"
		}
	}
	if stringFromAny(manifest["type"]) == "iac_provider" {
		if _, ok := manifest["resources"]; ok {
			return "provider-resources"
		}
	}
	return "unknown"
}

func addPluginManifestFindings(result *pluginAuditResult) {
	switch result.ManifestShape {
	case "canonical":
	case "top-level-types", "capabilities-array", "provider-resources":
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "WARN",
			Code:    "legacy_plugin_manifest",
			Message: fmt.Sprintf("plugin manifest uses %s shape", result.ManifestShape),
		})
	default:
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "WARN",
			Code:    "unknown_plugin_manifest_shape",
			Message: "plugin manifest shape is not recognized",
		})
	}

	if strings.Contains(result.ManifestName, "TEMPLATE") || strings.Contains(result.Name, "template") && strings.Contains(result.ManifestName, "workflow-plugin-TEMPLATE") {
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "ERROR",
			Code:    "placeholder_plugin_identity",
			Message: fmt.Sprintf("plugin manifest name %q appears to be a placeholder", result.ManifestName),
		})
	}
}

func pluginAuditFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
