package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	stateFile := fs.String("state", "", "Path to deployment state file for resource correlation")
	format := fs.String("format", "text", "Output format: text or json")
	checkBreaking := fs.Bool("check-breaking", false, "Warn about breaking changes (removed stateful modules, changed types)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl diff [options] <old-config.yaml> <new-config.yaml>

Compare two workflow configuration files and show what changed.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 2 {
		fs.Usage()
		return fmt.Errorf("two config files are required: <old-config.yaml> <new-config.yaml>")
	}

	oldPath := fs.Arg(0)
	newPath := fs.Arg(1)

	oldCfg, err := config.LoadFromFile(oldPath)
	if err != nil {
		return fmt.Errorf("load old config %q: %w", oldPath, err)
	}
	newCfg, err := config.LoadFromFile(newPath)
	if err != nil {
		return fmt.Errorf("load new config %q: %w", newPath, err)
	}

	// Optionally load the deployment state for resource correlation.
	var state *DeploymentState
	if *stateFile != "" {
		state, err = LoadState(*stateFile)
		if err != nil {
			return fmt.Errorf("load state file %q: %w", *stateFile, err)
		}
	}

	result := diffConfigs(oldCfg, newCfg, state)

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("encode diff result: %w", err)
		}
	default:
		printDiffText(result)
	}

	if *checkBreaking && len(result.BreakingChanges) > 0 {
		return fmt.Errorf("%d breaking change(s) detected", len(result.BreakingChanges))
	}
	return nil
}

// --- Data types for diff output ---

// DiffStatus describes what happened to a named element between two configs.
type DiffStatus string

const (
	DiffStatusAdded     DiffStatus = "added"
	DiffStatusRemoved   DiffStatus = "removed"
	DiffStatusChanged   DiffStatus = "changed"
	DiffStatusUnchanged DiffStatus = "unchanged"
)

// ModuleDiff captures the diff for a single module.
type ModuleDiff struct {
	Name     string     `json:"name"`
	Status   DiffStatus `json:"status"`
	Type     string     `json:"type,omitempty"`
	Stateful bool       `json:"stateful"`
	// Detail holds a human-readable description of what changed.
	Detail string `json:"detail,omitempty"`
	// ResourceID is the correlated infrastructure resource ID from state, if known.
	ResourceID string `json:"resourceId,omitempty"`
	// BreakingChanges lists data-loss risks for this module.
	BreakingChanges []BreakingChange `json:"breakingChanges,omitempty"`
}

// PipelineDiff captures the diff for a single pipeline.
type PipelineDiff struct {
	Name   string     `json:"name"`
	Status DiffStatus `json:"status"`
	// Trigger describes the pipeline trigger (e.g. "http POST /api/v1/orders").
	Trigger string `json:"trigger,omitempty"`
	// Detail holds a human-readable description of what changed.
	Detail string `json:"detail,omitempty"`
}

// BreakingChangeSummary aggregates breaking-change warnings across the diff.
type BreakingChangeSummary struct {
	ModuleName string         `json:"moduleName"`
	Changes    []BreakingChange `json:"changes"`
}

// DiffResult is the full structured output of diffConfigs.
type DiffResult struct {
	OldConfig       string                  `json:"oldConfig"`
	NewConfig       string                  `json:"newConfig"`
	Modules         []ModuleDiff            `json:"modules"`
	Pipelines       []PipelineDiff          `json:"pipelines"`
	BreakingChanges []BreakingChangeSummary `json:"breakingChanges,omitempty"`
}

// --- Core diffing logic ---

// diffConfigs produces a DiffResult comparing two workflow configs.
// state is optional; when provided, resource IDs are correlated from it.
func diffConfigs(oldCfg, newCfg *config.WorkflowConfig, state *DeploymentState) DiffResult {
	result := DiffResult{}

	// Index old modules by name.
	oldModules := make(map[string]*config.ModuleConfig, len(oldCfg.Modules))
	for i := range oldCfg.Modules {
		m := &oldCfg.Modules[i]
		oldModules[m.Name] = m
	}
	newModules := make(map[string]*config.ModuleConfig, len(newCfg.Modules))
	for i := range newCfg.Modules {
		m := &newCfg.Modules[i]
		newModules[m.Name] = m
	}

	// Collect all module names, sorted for deterministic output.
	allModuleNames := unionKeys(oldModules, newModules)
	sort.Strings(allModuleNames)

	for _, name := range allModuleNames {
		oldMod := oldModules[name]
		newMod := newModules[name]
		diff := diffModule(name, oldMod, newMod, state)
		result.Modules = append(result.Modules, diff)

		if len(diff.BreakingChanges) > 0 {
			result.BreakingChanges = append(result.BreakingChanges, BreakingChangeSummary{
				ModuleName: name,
				Changes:    diff.BreakingChanges,
			})
		}
	}

	// Pipelines.
	oldPipelines := normalisePipelines(oldCfg.Pipelines)
	newPipelines := normalisePipelines(newCfg.Pipelines)

	allPipelineNames := unionStringKeys(oldPipelines, newPipelines)
	sort.Strings(allPipelineNames)

	for _, name := range allPipelineNames {
		oldP, hasOld := oldPipelines[name]
		newP, hasNew := newPipelines[name]
		result.Pipelines = append(result.Pipelines, diffPipeline(name, oldP, hasOld, newP, hasNew))
	}

	return result
}

// diffModule computes the diff for a single module entry.
func diffModule(name string, oldMod, newMod *config.ModuleConfig, state *DeploymentState) ModuleDiff {
	d := ModuleDiff{Name: name}

	// Correlate resource ID from state if available.
	if state != nil {
		if ms, ok := state.Resources.Modules[name]; ok {
			d.ResourceID = ms.ResourceID
		}
	}

	switch {
	case oldMod == nil && newMod != nil:
		// Added.
		d.Status = DiffStatusAdded
		d.Type = newMod.Type
		d.Stateful = IsStateful(newMod.Type)
		d.Detail = "NEW"

	case oldMod != nil && newMod == nil:
		// Removed.
		d.Status = DiffStatusRemoved
		d.Type = oldMod.Type
		d.Stateful = IsStateful(oldMod.Type)
		if d.Stateful {
			d.Detail = "REMOVED — WARNING: stateful resource may still hold data"
		} else {
			d.Detail = "REMOVED (stateless, safe to remove)"
		}

	default:
		// Both present — check for changes.
		d.Type = newMod.Type
		d.Stateful = IsStateful(newMod.Type)

		breaking := DetectBreakingChanges(oldMod, newMod)
		configChanged := isConfigChanged(oldMod.Config, newMod.Config)
		typeChanged := oldMod.Type != newMod.Type

		switch {
		case typeChanged:
			d.Status = DiffStatusChanged
			d.Detail = fmt.Sprintf("TYPE CHANGED: %s → %s", oldMod.Type, newMod.Type)
			d.BreakingChanges = breaking
		case len(breaking) > 0:
			d.Status = DiffStatusChanged
			parts := make([]string, 0, len(breaking))
			for _, bc := range breaking {
				parts = append(parts, fmt.Sprintf("%s: %s → %s", bc.Field, describeValue(bc.OldValue), describeValue(bc.NewValue)))
			}
			d.Detail = "CONFIG CHANGED: " + strings.Join(parts, "; ")
			d.BreakingChanges = breaking
		case configChanged:
			d.Status = DiffStatusChanged
			d.Detail = "CONFIG CHANGED"
		default:
			d.Status = DiffStatusUnchanged
			d.Detail = "UNCHANGED"
		}
	}

	return d
}

// diffPipeline computes the diff for a single pipeline.
func diffPipeline(name string, oldP map[string]any, hasOld bool, newP map[string]any, hasNew bool) PipelineDiff {
	d := PipelineDiff{Name: name}

	switch {
	case !hasOld && hasNew:
		d.Status = DiffStatusAdded
		d.Trigger = describePipelineTrigger(newP)
		d.Detail = "NEW"

	case hasOld && !hasNew:
		d.Status = DiffStatusRemoved
		d.Trigger = describePipelineTrigger(oldP)
		d.Detail = "REMOVED"

	default:
		d.Trigger = describePipelineTrigger(newP)

		oldSteps := countSteps(oldP)
		newSteps := countSteps(newP)

		oldTrigger := describePipelineTrigger(oldP)
		newTrigger := describePipelineTrigger(newP)
		triggerChanged := oldTrigger != newTrigger
		stepsChanged := oldSteps != newSteps

		switch {
		case triggerChanged:
			d.Status = DiffStatusChanged
			d.Detail = fmt.Sprintf("TRIGGER CHANGED: %s → %s", oldTrigger, newTrigger)
		case stepsChanged:
			d.Status = DiffStatusChanged
			d.Detail = fmt.Sprintf("STEPS CHANGED: %d → %d steps", oldSteps, newSteps)
		default:
			d.Status = DiffStatusUnchanged
			d.Detail = "UNCHANGED"
		}
	}

	return d
}

// --- Rendering ---

// statusSymbol returns the one-character prefix for a diff status.
func statusSymbol(s DiffStatus) string {
	switch s {
	case DiffStatusAdded:
		return "+"
	case DiffStatusRemoved:
		return "-"
	case DiffStatusChanged:
		return "~"
	default:
		return "="
	}
}

// printDiffText writes a human-readable diff report to stdout.
func printDiffText(result DiffResult) {
	fmt.Println("Modules:")
	for _, m := range result.Modules {
		statefulTag := ""
		if m.Stateful && m.Status != DiffStatusUnchanged {
			statefulTag = " [STATEFUL]"
		}
		fmt.Printf("  %s %-28s  (%-30s)  [%s]%s\n",
			statusSymbol(m.Status),
			m.Name,
			moduleTypeLabel(m.Type),
			m.Detail,
			statefulTag,
		)
		if m.ResourceID != "" && m.Status != DiffStatusAdded {
			fmt.Printf("    resource: %s\n", m.ResourceID)
		}
	}

	if len(result.Pipelines) > 0 {
		fmt.Println("\nPipelines:")
		for _, p := range result.Pipelines {
			fmt.Printf("  %s %-28s  %-36s  [%s]\n",
				statusSymbol(p.Status),
				p.Name,
				fmt.Sprintf("(%s)", p.Trigger),
				p.Detail,
			)
		}
	}

	if len(result.BreakingChanges) > 0 {
		fmt.Println("\n[BREAKING CHANGES]")
		for _, bc := range result.BreakingChanges {
			fmt.Printf("  Module %q:\n", bc.ModuleName)
			for _, ch := range bc.Changes {
				fmt.Printf("    - %s\n", ch.Message)
				if ch.Field != "" && ch.Field != "type" {
					fmt.Printf("      This is a STATEFUL module. Data at the old location may be lost.\n")
					fmt.Printf("      Recommendation: add a migration step or keep the old value.\n")
				}
			}
		}
	}
}

// --- Helpers ---

// normalisePipelines converts cfg.Pipelines (map[string]any) to a typed map.
func normalisePipelines(raw map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(raw))
	for name, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out[name] = m
		} else {
			// Preserve the entry with an empty map so it still appears in diffs.
			out[name] = nil
		}
	}
	return out
}

// unionKeys returns the union of keys across two string-keyed maps.
func unionKeys[V any](a, b map[string]*V) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

// unionStringKeys returns the union of keys across two map[string]map[string]any.
func unionStringKeys(a, b map[string]map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

// isConfigChanged returns true if the two config maps differ in any key or value.
func isConfigChanged(oldCfg, newCfg map[string]any) bool {
	if len(oldCfg) != len(newCfg) {
		return true
	}
	for k, ov := range oldCfg {
		nv, ok := newCfg[k]
		if !ok {
			return true
		}
		if fmt.Sprintf("%v", ov) != fmt.Sprintf("%v", nv) {
			return true
		}
	}
	return false
}

// describePipelineTrigger builds a short trigger description from a raw pipeline map.
func describePipelineTrigger(p map[string]any) string {
	if p == nil {
		return "unknown"
	}
	triggerRaw, ok := p["trigger"]
	if !ok {
		return "unknown"
	}
	triggerMap, ok := triggerRaw.(map[string]any)
	if !ok {
		return "unknown"
	}
	triggerType, _ := triggerMap["type"].(string)

	cfgRaw, ok := triggerMap["config"]
	if !ok {
		return triggerType
	}
	triggerCfg, ok := cfgRaw.(map[string]any)
	if !ok {
		return triggerType
	}

	method, _ := triggerCfg["method"].(string)
	path, _ := triggerCfg["path"].(string)

	if method != "" && path != "" {
		return fmt.Sprintf("%s %s %s", triggerType, method, path)
	}
	if path != "" {
		return fmt.Sprintf("%s %s", triggerType, path)
	}
	return triggerType
}

// countSteps returns the number of steps in a raw pipeline map.
func countSteps(p map[string]any) int {
	if p == nil {
		return 0
	}
	stepsRaw, ok := p["steps"]
	if !ok {
		return 0
	}
	steps, ok := stepsRaw.([]any)
	if !ok {
		return 0
	}
	return len(steps)
}
