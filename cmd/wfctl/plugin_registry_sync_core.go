package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// runPluginRegistrySyncCore ports workflow-registry/scripts/sync-core-manifests.sh
// (workflow#762). Compiles + runs an inspect program against a
// workflow checkout to discover the canonical core-plugin module/step/trigger
// surface, then syncs into <registry-dir>/plugins/<core-plugin>/manifest.json.
func runPluginRegistrySyncCore(args []string) error {
	fs := flag.NewFlagSet("plugin registry-sync core", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "Apply changes (default: dry-run)")
	workflowRepo := fs.String("workflow-repo", "", "Path to a workflow checkout (required)")
	registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin registry-sync core --workflow-repo <path> [--fix] [--registry-dir <path>]

Syncs core (built-in workflow) plugin manifests in <registry-dir>/plugins/
by compiling an inspect program against the workflow checkout at
<workflow-repo> and diffing the result against the registry's manifest.json
files for those core plugins.

Replaces workflow-registry/scripts/sync-core-manifests.sh.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workflowRepo == "" {
		fs.Usage()
		return fmt.Errorf("--workflow-repo is required")
	}
	if _, err := os.Stat(filepath.Join(*workflowRepo, "go.mod")); err != nil {
		return fmt.Errorf("--workflow-repo %q must point to a workflow checkout: %w", *workflowRepo, err)
	}

	plugins, err := inspectCoreRegistryPlugins(*workflowRepo)
	if err != nil {
		return err
	}
	return syncCorePluginManifests(*registryDir, plugins, *fix, os.Stderr)
}

type coreRegistryPlugin struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Description      string   `json:"description"`
	ModuleTypes      []string `json:"moduleTypes"`
	StepTypes        []string `json:"stepTypes"`
	TriggerTypes     []string `json:"triggerTypes"`
	WorkflowHandlers []string `json:"workflowHandlers"`
	WiringHooks      []string `json:"wiringHooks"`
}

func inspectCoreRegistryPlugins(workflowRepo string) ([]coreRegistryPlugin, error) {
	inspectDir, err := os.MkdirTemp(workflowRepo, ".workflow-core-inspect-*")
	if err != nil {
		return nil, fmt.Errorf("create workflow core inspect dir: %w", err)
	}
	defer os.RemoveAll(inspectDir)

	if err := os.WriteFile(filepath.Join(inspectDir, "main.go"), []byte(coreRegistryInspectProgram), 0o600); err != nil {
		return nil, fmt.Errorf("write workflow core inspector: %w", err)
	}

	cmd := exec.Command("go", "run", "./"+filepath.Base(inspectDir)) // #nosec G204 -- fixed go command with generated package path
	cmd.Dir = workflowRepo
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("inspect workflow core plugins: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("inspect workflow core plugins: %w", err)
	}

	var plugins []coreRegistryPlugin
	if err := json.Unmarshal(out, &plugins); err != nil {
		return nil, fmt.Errorf("parse workflow core inspector output: %w", err)
	}
	return plugins, nil
}

func syncCorePluginManifests(registryDir string, plugins []coreRegistryPlugin, fix bool, stderr io.Writer) error {
	pluginsDir := filepath.Join(registryDir, "plugins")
	failures := 0
	for i := range plugins {
		p := plugins[i]
		expectedPath := manifestPathForCorePlugin(pluginsDir, p.Name)
		dir := filepath.Base(filepath.Dir(expectedPath))
		expected := expectedCoreManifest(p, "plugins/"+dir)

		if _, err := os.Stat(expectedPath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", expectedPath, err)
			}
			if fix {
				if err := writeCoreManifest(expectedPath, expected, nil); err != nil {
					return err
				}
				fmt.Fprintf(stderr, "created %s\n", relRegistryPath(registryDir, expectedPath))
				continue
			}
			fmt.Fprintf(stderr, "missing core plugin manifest for %s: expected %s\n", p.Name, relRegistryPath(registryDir, expectedPath))
			failures++
			continue
		}

		current, currentRaw, err := readNormalizedCoreManifest(expectedPath)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, expected) {
			if fix {
				if err := writeCoreManifest(expectedPath, expected, currentRaw); err != nil {
					return err
				}
				fmt.Fprintf(stderr, "updated %s\n", relRegistryPath(registryDir, expectedPath))
				continue
			}
			fmt.Fprintf(stderr, "core plugin manifest drift for %s: %s\n", p.Name, relRegistryPath(registryDir, expectedPath))
			failures++
		}
	}
	if failures > 0 {
		fmt.Fprintf(stderr, "core manifest validation failed: %d issue(s)\n", failures)
		return fmt.Errorf("core manifest validation failed: %d issue(s)", failures)
	}
	if fix {
		fmt.Fprintln(stderr, "Core plugin manifests synced.")
	} else {
		fmt.Fprintln(stderr, "Core plugin manifests match workflow plugin declarations.")
	}
	return nil
}

type coreManifest struct {
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	Author       string                   `json:"author"`
	Description  string                   `json:"description"`
	Source       string                   `json:"source"`
	Path         string                   `json:"path"`
	Type         string                   `json:"type"`
	Tier         string                   `json:"tier"`
	License      string                   `json:"license"`
	Homepage     string                   `json:"homepage"`
	Repository   string                   `json:"repository"`
	Capabilities coreManifestCapabilities `json:"capabilities"`
}

type coreManifestCapabilities struct {
	ModuleTypes      []string `json:"moduleTypes"`
	StepTypes        []string `json:"stepTypes"`
	TriggerTypes     []string `json:"triggerTypes"`
	WorkflowHandlers []string `json:"workflowHandlers"`
	WiringHooks      []string `json:"wiringHooks"`
}

func expectedCoreManifest(p coreRegistryPlugin, path string) coreManifest {
	return coreManifest{
		Name:        p.Name,
		Version:     p.Version,
		Author:      "GoCodeAlone",
		Description: p.Description,
		Source:      "github.com/GoCodeAlone/workflow",
		Path:        path,
		Type:        "builtin",
		Tier:        "core",
		License:     "MIT",
		Homepage:    "https://github.com/GoCodeAlone/workflow",
		Repository:  "https://github.com/GoCodeAlone/workflow",
		Capabilities: coreManifestCapabilities{
			ModuleTypes:      sortedCopy(p.ModuleTypes),
			StepTypes:        sortedCopy(p.StepTypes),
			TriggerTypes:     sortedCopy(p.TriggerTypes),
			WorkflowHandlers: sortedCopy(p.WorkflowHandlers),
			WiringHooks:      sortedCopy(p.WiringHooks),
		},
	}
}

func readNormalizedCoreManifest(path string) (coreManifest, map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return coreManifest{}, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var currentRaw map[string]any
	if err := json.Unmarshal(raw, &currentRaw); err != nil {
		return coreManifest{}, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	manifest := coreManifest{
		Name:        stringField(currentRaw, "name"),
		Version:     stringField(currentRaw, "version"),
		Author:      stringField(currentRaw, "author"),
		Description: stringField(currentRaw, "description"),
		Source:      stringField(currentRaw, "source"),
		Path:        stringField(currentRaw, "path"),
		Type:        stringField(currentRaw, "type"),
		Tier:        stringField(currentRaw, "tier"),
		License:     stringField(currentRaw, "license"),
		Homepage:    stringField(currentRaw, "homepage"),
		Repository:  stringField(currentRaw, "repository"),
	}
	if caps, _ := currentRaw["capabilities"].(map[string]any); caps != nil {
		manifest.Capabilities = coreManifestCapabilities{
			ModuleTypes:      sortedStringSlice(caps["moduleTypes"]),
			StepTypes:        sortedStringSlice(caps["stepTypes"]),
			TriggerTypes:     sortedStringSlice(caps["triggerTypes"]),
			WorkflowHandlers: sortedStringSlice(caps["workflowHandlers"]),
			WiringHooks:      sortedStringSlice(caps["wiringHooks"]),
		}
	}
	return manifest, currentRaw, nil
}

func writeCoreManifest(path string, expected coreManifest, current map[string]any) error {
	if current == nil {
		current = map[string]any{}
	}
	current["name"] = expected.Name
	current["version"] = expected.Version
	current["author"] = expected.Author
	current["description"] = expected.Description
	current["source"] = expected.Source
	current["path"] = expected.Path
	current["type"] = expected.Type
	current["tier"] = expected.Tier
	current["license"] = expected.License
	current["homepage"] = expected.Homepage
	current["repository"] = expected.Repository
	delete(current, "downloads")

	caps, _ := current["capabilities"].(map[string]any)
	if caps == nil {
		caps = map[string]any{}
	}
	caps["moduleTypes"] = expected.Capabilities.ModuleTypes
	caps["stepTypes"] = expected.Capabilities.StepTypes
	caps["triggerTypes"] = expected.Capabilities.TriggerTypes
	caps["workflowHandlers"] = expected.Capabilities.WorkflowHandlers
	caps["wiringHooks"] = expected.Capabilities.WiringHooks
	current["capabilities"] = caps

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- registry manifests are normal repository files.
		return fmt.Errorf("create manifest dir %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil { // #nosec G306 -- registry manifests are normal repository files.
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func manifestPathForCorePlugin(pluginsDir, name string) string {
	if matches, err := filepath.Glob(filepath.Join(pluginsDir, "*", "manifest.json")); err == nil {
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var manifest struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &manifest) == nil && manifest.Name == name {
				return path
			}
		}
	}

	alias := strings.TrimPrefix(name, "workflow-plugin-")
	alias = strings.TrimSuffix(alias, "-plugin")
	switch alias {
	case "feature-flags":
		alias = "featureflags"
	case "pipeline-steps":
		alias = "pipelinesteps"
	case "modular-compat":
		alias = "modularcompat"
	case "kubernetes-deploy":
		alias = "k8s"
	}
	return filepath.Join(pluginsDir, alias, "manifest.json")
}

func relRegistryPath(registryDir, path string) string {
	rel, err := filepath.Rel(registryDir, path)
	if err != nil {
		return path
	}
	return rel
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func sortedStringSlice(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

const coreRegistryInspectProgram = `package main

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugins/all"
)

type corePlugin struct {
	Name             string   ` + "`json:\"name\"`" + `
	Version          string   ` + "`json:\"version\"`" + `
	Description      string   ` + "`json:\"description\"`" + `
	ModuleTypes      []string ` + "`json:\"moduleTypes\"`" + `
	StepTypes        []string ` + "`json:\"stepTypes\"`" + `
	TriggerTypes     []string ` + "`json:\"triggerTypes\"`" + `
	WorkflowHandlers []string ` + "`json:\"workflowHandlers\"`" + `
	WiringHooks      []string ` + "`json:\"wiringHooks\"`" + `
}

func mapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func hookNames(hooks []plugin.WiringHook) []string {
	names := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		if hook.Name != "" {
			names = append(names, hook.Name)
		}
	}
	sort.Strings(names)
	return names
}

func main() {
	out := make([]corePlugin, 0)
	for _, p := range all.DefaultPlugins() {
		m := p.EngineManifest()
		out = append(out, corePlugin{
			Name:             m.Name,
			Version:          m.Version,
			Description:      m.Description,
			ModuleTypes:      mapKeys(p.ModuleFactories()),
			StepTypes:        mapKeys(p.StepFactories()),
			TriggerTypes:     mapKeys(p.TriggerFactories()),
			WorkflowHandlers: mapKeys(p.WorkflowHandlers()),
			WiringHooks:      hookNames(p.WiringHooks()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
`
