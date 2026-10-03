package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
)

type pipelineClosure struct {
	config *config.WorkflowConfig
	types  map[string]bool
	names  map[string]int
}

// Walk config, not factories: even a rejected closure must execute no plugin code.
func selectPipelineClosure(cfg *config.WorkflowConfig, name string, record bool) (*pipelineClosure, error) {
	if record && (len(cfg.Modules) != 0 || len(cfg.Workflows) != 0 || len(cfg.Triggers) != 0 || len(cfg.Sidecars) != 0) {
		return nil, fmt.Errorf("record mode rejects modules, routes, triggers, and sidecars")
	}
	selected := *cfg
	selected.Pipelines = make(map[string]any)
	if record {
		selected = config.WorkflowConfig{ConfigDir: cfg.ConfigDir, Pipelines: selected.Pipelines}
	}
	closure := &pipelineClosure{config: &selected, types: make(map[string]bool), names: make(map[string]int)}
	visiting := make(map[string]bool)
	var pipeline func(string) error
	var steps func(any, string, string, int) error
	var step func(map[string]any, string, int) error

	steps = func(raw any, parent, naming string, depth int) error {
		list, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s: steps must be a list", parent)
		}
		for i, rawStep := range list {
			cfgStep, ok := rawStep.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: step %d must be an object", parent, i)
			}
			fallback := ""
			switch naming {
			case "indexed":
				fallback = fmt.Sprintf("%s[%d]", parent, i)
			case "sub":
				fallback = fmt.Sprintf("%s-sub-%d", parent, i)
			}
			if err := step(cfgStep, fallback, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	step = func(raw map[string]any, fallbackName string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("pipeline closure nesting exceeds 64")
		}
		stepType, _ := raw["type"].(string)
		if stepType == "" || strings.ContainsAny(stepType, "{}") {
			return fmt.Errorf("%s: step type must be literal", fallbackName)
		}
		stepName, _ := raw["name"].(string)
		if stepName == "" {
			stepName = fallbackName
		}
		if stepName != "" {
			closure.names[stepName]++
		}
		closure.types[stepType] = true
		stepCfg := raw
		if inner, exists := raw["config"]; exists {
			var ok bool
			stepCfg, ok = inner.(map[string]any)
			if !ok && inner != nil {
				return fmt.Errorf("%s: config must be an object", stepName)
			}
		}
		single := func(field string, required bool) error {
			value, exists := stepCfg[field]
			if !exists && !required {
				return nil
			}
			child, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: %s must be a step object", stepName, field)
			}
			return step(child, stepName+"-"+field, depth+1)
		}
		list := func(field string) error {
			if (stepType == "step.foreach" || stepType == "step.while") && field == "steps" {
				return steps(stepCfg[field], stepName, "sub", depth+1)
			}
			naming := "indexed"
			if stepType == "step.parallel" {
				naming = ""
			}
			return steps(stepCfg[field], stepName+"-"+field, naming, depth+1)
		}
		switch stepType {
		case "step.sub_workflow":
			return fmt.Errorf("%s: runtime-owned sub_workflow is not a static closure", stepName)
		case "step.workflow_call":
			target, _ := stepCfg["workflow"].(string)
			if target == "" || strings.ContainsAny(target, "{}") {
				return fmt.Errorf("%s: dynamic workflow target is not permitted", stepName)
			}
			return pipeline(target)
		case "step.branch":
			branches, ok := stepCfg["branches"].(map[string]any)
			if !ok || len(branches) == 0 {
				return fmt.Errorf("%s: branches must be a nonempty object", stepName)
			}
			for _, key := range slices.Sorted(maps.Keys(branches)) {
				if err := steps(branches[key], fmt.Sprintf("%s-%s", stepName, key), "indexed", depth+1); err != nil {
					return err
				}
			}
			if _, exists := stepCfg["default"]; exists {
				return list("default")
			}
		case "step.foreach", "step.while":
			_, hasStep := stepCfg["step"]
			_, hasSteps := stepCfg["steps"]
			if hasStep && hasSteps {
				return fmt.Errorf("%s: step and steps are mutually exclusive", stepName)
			}
			if hasStep {
				return single("step", true)
			}
			if hasSteps {
				return list("steps")
			}
		case "step.parallel":
			return list("steps")
		case "step.retry_with_backoff":
			return single("step", true)
		case "step.resilient_circuit_breaker":
			if err := single("step", true); err != nil {
				return err
			}
			return single("fallback", false)
		default:
			if record {
				for _, field := range []string{"step", "steps", "branches", "fallback"} {
					if _, exists := stepCfg[field]; exists {
						return fmt.Errorf("%s: unsupported composite/control-flow form", stepName)
					}
				}
			}
		}
		return nil
	}
	pipeline = func(name string) error {
		if len(visiting) >= 64 {
			return fmt.Errorf("pipeline call closure exceeds 64")
		}
		if visiting[name] {
			return fmt.Errorf("cyclic workflow target %q", name)
		}
		if _, ok := selected.Pipelines[name]; ok {
			return nil
		}
		raw, ok := cfg.Pipelines[name].(map[string]any)
		if !ok {
			return fmt.Errorf("pipeline %q not found or not an object", name)
		}
		if record {
			if raw["on_error"] == "skip" {
				return fmt.Errorf("record mode rejects skipped pipeline errors")
			}
			if trigger, exists := raw["trigger"]; exists && trigger != nil {
				return fmt.Errorf("record mode rejects pipeline triggers")
			}
		}
		visiting[name] = true
		defer delete(visiting, name)
		if err := steps(raw["steps"], name, "", 0); err != nil {
			return err
		}
		if compensation, exists := raw["compensation"]; exists {
			if err := steps(compensation, name+"-compensation", "", 0); err != nil {
				return err
			}
		}
		selected.Pipelines[name] = maps.Clone(raw)
		return nil
	}
	if err := pipeline(name); err != nil {
		return nil, err
	}
	return closure, nil
}

type pipelineInstallations struct {
	manifests map[string]*plugin.PluginManifest
	owners    map[string]string
}

func inspectPipelineInstallations(dir string) (*pipelineInstallations, error) {
	installed := &pipelineInstallations{
		manifests: make(map[string]*plugin.PluginManifest),
		owners:    make(map[string]string),
	}
	for _, stepType := range pluginpipeline.New().EngineManifest().StepTypes {
		installed.owners[stepType] = "pipeline-steps"
	}
	if dir == "" {
		return installed, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return installed, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect plugin installations: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(path, "plugin.json")
		if _, err := os.Lstat(manifestPath); os.IsNotExist(err) {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("plugin installation identity: directory must not be a symlink")
		}
		manifest, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("inspect plugin manifest: %w", err)
		}
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		if manifest.Name != entry.Name() {
			return nil, fmt.Errorf("plugin installation identity: directory %q does not match manifest %q", entry.Name(), manifest.Name)
		}
		for _, filename := range []string{"plugin.json", manifest.Name} {
			info, err := os.Lstat(filepath.Join(path, filename))
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("plugin installation identity: %q must be a regular file", filename)
			}
		}
		installed.manifests[manifest.Name] = manifest
		for _, stepType := range manifest.StepTypes {
			if owner, exists := installed.owners[stepType]; exists {
				return nil, fmt.Errorf("step ownership collision: %q belongs to %q and %q", stepType, owner, manifest.Name)
			}
			installed.owners[stepType] = manifest.Name
		}
	}
	return installed, nil
}

func (installed *pipelineInstallations) resolve(types map[string]bool) ([]string, error) {
	required := make(map[string]bool)
	for _, stepType := range slices.Sorted(maps.Keys(types)) {
		owner, ok := installed.owners[stepType]
		if !ok {
			return nil, fmt.Errorf("unknown step ownership: %q", stepType)
		}
		if owner != "pipeline-steps" {
			required[owner] = true
		}
	}
	var order []string
	visited := make(map[string]bool)
	active := make(map[string]bool)
	var visit func(string) error
	visit = func(name string) error {
		if active[name] || len(active) >= 64 {
			return fmt.Errorf("cyclic or overdeep plugin dependency")
		}
		if visited[name] {
			return nil
		}
		manifest, ok := installed.manifests[name]
		if !ok {
			return fmt.Errorf("missing plugin dependency %q", name)
		}
		active[name] = true
		defer delete(active, name)
		dependencies := slices.Clone(manifest.Dependencies)
		slices.SortFunc(dependencies, func(a, b plugin.Dependency) int { return strings.Compare(a.Name, b.Name) })
		for _, dependency := range dependencies {
			version := ""
			if dependency.Name == "pipeline-steps" {
				version = pluginpipeline.New().Version()
			} else if found := installed.manifests[dependency.Name]; found != nil {
				version = found.Version
			}
			matches, err := plugin.CheckVersion(version, dependency.Constraint)
			if err != nil || !matches {
				return fmt.Errorf("missing or incompatible plugin dependency %q", dependency.Name)
			}
			if dependency.Name != "pipeline-steps" {
				if err := visit(dependency.Name); err != nil {
					return err
				}
			}
		}
		visited[name] = true
		order = append(order, name)
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(required)) {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
