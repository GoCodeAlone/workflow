package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// CompatibilityInfo describes the compatibility requirements and test history of a config.
type CompatibilityInfo struct {
	MinEngineVersion string   `json:"minEngineVersion"`
	MaxEngineVersion string   `json:"maxEngineVersion,omitempty"`
	RequiredSteps    []string `json:"requiredSteps"`
	RequiredModules  []string `json:"requiredModules"`
	TestedVersions   []string `json:"testedVersions"`
}

// compatCheckResult holds the result of a compatibility check.
type compatCheckResult struct {
	EngineVersion   string          `json:"engineVersion"`
	RequiredModules []compatItem    `json:"requiredModules"`
	RequiredSteps   []compatItem    `json:"requiredSteps"`
	Compatible      bool            `json:"compatible"`
	Issues          []string        `json:"issues,omitempty"`
}

// compatItem represents a single required type and whether it's available.
type compatItem struct {
	Type      string `json:"type"`
	Available bool   `json:"available"`
}

// runCompat dispatches compat subcommands.
func runCompat(args []string) error {
	if len(args) < 1 {
		return compatUsage()
	}
	switch args[0] {
	case "check":
		return runCompatCheck(args[1:])
	default:
		return compatUsage()
	}
}

func compatUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl compat <subcommand> [options]

Subcommands:
  check   Check config compatibility with the current engine version

Run 'wfctl compat check -h' for details.
`)
	return fmt.Errorf("compat subcommand is required")
}

// runCompatCheck checks a config file for compatibility with the current engine version.
func runCompatCheck(args []string) error {
	fs2 := flag.NewFlagSet("compat check", flag.ContinueOnError)
	format := fs2.String("format", "text", "Output format: text or json")
	fs2.Usage = func() {
		fmt.Fprintf(fs2.Output(), `Usage: wfctl compat check [options] <config.yaml>

Check whether a workflow config is compatible with the current engine version.
Reports which module and step types are available in the engine.

Options:
`)
		fs2.PrintDefaults()
	}
	if err := fs2.Parse(args); err != nil {
		return err
	}
	if fs2.NArg() < 1 {
		fs2.Usage()
		return fmt.Errorf("config.yaml path is required")
	}

	configPath := fs2.Arg(0)
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	result := checkCompatibility(cfg)

	switch strings.ToLower(*format) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	default:
		printCompatResult(result)
	}

	if !result.Compatible {
		return fmt.Errorf("compatibility check failed: %d issue(s)", len(result.Issues))
	}
	return nil
}

// checkCompatibility checks a config against the current engine's known types.
func checkCompatibility(cfg *config.WorkflowConfig) *compatCheckResult {
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()

	result := &compatCheckResult{
		EngineVersion: version,
		Compatible:    true,
	}

	// Check module types
	for _, mod := range cfg.Modules {
		item := compatItem{
			Type: mod.Type,
		}
		if _, ok := knownModules[mod.Type]; ok {
			item.Available = true
		} else {
			item.Available = false
			result.Compatible = false
			result.Issues = append(result.Issues, fmt.Sprintf("module type %q is not available in this engine version", mod.Type))
		}
		result.RequiredModules = append(result.RequiredModules, item)
	}

	// Deduplicate modules
	result.RequiredModules = deduplicateCompatItems(result.RequiredModules)

	// Check step types from pipelines
	stepSet := make(map[string]bool)
	for _, pipelineRaw := range cfg.Pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}
		if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
			for _, stepRaw := range stepsRaw {
				if stepMap, ok := stepRaw.(map[string]any); ok {
					if stepType, ok := stepMap["type"].(string); ok && stepType != "" {
						stepSet[stepType] = true
					}
				}
			}
		}
	}

	for stepType := range stepSet {
		item := compatItem{
			Type: stepType,
		}
		if _, ok := knownSteps[stepType]; ok {
			item.Available = true
		} else {
			item.Available = false
			result.Compatible = false
			result.Issues = append(result.Issues, fmt.Sprintf("step type %q is not available in this engine version", stepType))
		}
		result.RequiredSteps = append(result.RequiredSteps, item)
	}

	// Sort for determinism
	sortCompatItems(result.RequiredModules)
	sortCompatItems(result.RequiredSteps)

	return result
}

// deduplicateCompatItems removes duplicate items, keeping the first occurrence.
func deduplicateCompatItems(items []compatItem) []compatItem {
	seen := make(map[string]bool)
	var out []compatItem
	for _, item := range items {
		if !seen[item.Type] {
			seen[item.Type] = true
			out = append(out, item)
		}
	}
	return out
}

// sortCompatItems sorts compat items by type name.
func sortCompatItems(items []compatItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Type < items[j-1].Type; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// printCompatResult prints a human-readable compatibility check result.
func printCompatResult(r *compatCheckResult) {
	fmt.Printf("Engine version: %s\n", r.EngineVersion)

	if len(r.RequiredModules) > 0 {
		fmt.Printf("\nRequired modules:\n")
		for _, item := range r.RequiredModules {
			if item.Available {
				fmt.Printf("  %s +\n", item.Type)
			} else {
				fmt.Printf("  %s  (NOT AVAILABLE)\n", item.Type)
			}
		}
	}

	if len(r.RequiredSteps) > 0 {
		fmt.Printf("\nRequired steps:\n")
		for _, item := range r.RequiredSteps {
			if item.Available {
				fmt.Printf("  %s +\n", item.Type)
			} else {
				fmt.Printf("  %s  (NOT AVAILABLE)\n", item.Type)
			}
		}
	}

	if r.Compatible {
		fmt.Println("\nCompatibility: PASS")
	} else {
		fmt.Printf("\nCompatibility: FAIL (%d issue(s))\n", len(r.Issues))
		for _, issue := range r.Issues {
			fmt.Printf("  - %s\n", issue)
		}
	}
}
