package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/internal/legacyaws"
	"github.com/GoCodeAlone/workflow/internal/legacydo"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/GoCodeAlone/workflow/validation"
	"gopkg.in/yaml.v3"
)

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	strict := fs.Bool("strict", true, "Enable strict validation (default; retained for compatibility)")
	loose := fs.Bool("loose", false, "Allow legacy loose validation for transitional configs (planned for removal in v1.0)")
	nonStrict := fs.Bool("non-strict", false, "Alias for --loose")
	skipUnknownTypes := fs.Bool("skip-unknown-types", false, "Skip unknown module/workflow/trigger type checks")
	allowNoEntryPoints := fs.Bool("allow-no-entry-points", false, "Allow configs with no entry points (triggers, routes, subscriptions, jobs)")
	dir := fs.String("dir", "", "Validate all .yaml/.yml files in a directory (recursive)")
	pluginDir := fs.String("plugin-dir", "", "Directory of installed external plugins; their types are loaded before validation")
	var pluginManifests stringSliceFlag
	fs.Var(&pluginManifests, "plugin-manifest", "Path to a plugin.json file, or a directory containing one (or one level of subdirs that do). Repeatable. Loaded before validation so the declared types pass.")
	noAutoResolve := fs.Bool("no-resolve-plugins", false, "Disable auto-resolution of requires.plugins[] against sibling/ancestor checkouts")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl validate [options] <config.yaml> [config2.yaml ...]

Validate one or more workflow configuration files.

Examples:
  wfctl validate config.yaml
  wfctl validate example/*.yaml
  wfctl validate --dir ./example/
  wfctl validate --loose legacy/config.yaml
  wfctl validate --skip-unknown-types example/*.yaml
  wfctl validate --plugin-dir data/plugins config.yaml
  wfctl validate --plugin-manifest ../workflow-plugin-foo config.yaml
  wfctl validate --plugin-manifest ../workflow-plugin-foo/plugin.json config.yaml

Options:
`)
		fs.PrintDefaults()
	}
	// Reorder args so flags come before positional args.
	// Go's flag.FlagSet.Parse stops at the first non-flag argument,
	// so "validate config.yaml --skip-unknown-types" would treat the
	// flag as a second file path. Move all flag-like args first.
	args = reorderFlags(args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *loose || *nonStrict {
		*strict = false
	}

	// Load external plugin types before validation so their module/trigger/workflow
	// types are recognised and don't cause false "unknown type" errors.
	if *pluginDir != "" {
		if err := schema.LoadPluginTypesFromDir(*pluginDir); err != nil {
			return fmt.Errorf("failed to load plugins from %s: %w", *pluginDir, err)
		}
		schema.LoadPluginStepSchemasFromDir(*pluginDir)
	}
	for _, manifest := range pluginManifests {
		if err := loadPluginManifestPath(manifest); err != nil {
			return err
		}
	}

	// Collect files to validate
	var files []string

	if *dir != "" {
		found, err := findYAMLFiles(*dir)
		if err != nil {
			return fmt.Errorf("failed to scan directory %s: %w", *dir, err)
		}
		for _, f := range found {
			if !isWorkflowYAML(f) {
				fmt.Fprintf(os.Stderr, "  Skipping non-workflow file: %s\n", f)
				continue
			}
			files = append(files, f)
		}
	}

	files = append(files, fs.Args()...)

	if len(files) == 0 {
		fs.Usage()
		return fmt.Errorf("at least one config file or --dir is required")
	}

	// Validate each file
	var (
		passed int
		failed int
		errors []string
	)

	for _, f := range files {
		if err := validateFile(f, *strict, *skipUnknownTypes, *allowNoEntryPoints, !*noAutoResolve); err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("  FAIL %s\n       %s", f, indentError(err)))
		} else {
			passed++
		}
	}

	// Print summary
	total := passed + failed
	if total > 1 {
		fmt.Printf("\n--- Validation Summary ---\n")
		fmt.Printf("  %d/%d configs passed\n", passed, total)
		if failed > 0 {
			fmt.Printf("  %d/%d configs failed:\n", failed, total)
			for _, e := range errors {
				fmt.Println(e)
			}
		}
		fmt.Println()
	}

	if failed > 0 {
		if total == 1 && len(errors) == 1 {
			return fmt.Errorf("%d config(s) failed validation: %s", failed, indentErrorMessage(errors[0]))
		}
		return fmt.Errorf("%d config(s) failed validation", failed)
	}
	return nil
}

func indentErrorMessage(message string) string {
	lines := strings.Split(message, "\n")
	if len(lines) == 0 {
		return message
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func validateFile(cfgPath string, strict, skipUnknownTypes, allowNoEntryPoints, autoResolvePlugins bool) error {
	// Read raw YAML to extract imports list for verbose feedback.
	imports := extractImports(cfgPath)
	if isLikelyWfctlProjectManifest(cfgPath) {
		return fmt.Errorf("%s is a wfctl project manifest; use 'wfctl config validate %s' instead", cfgPath, cfgPath)
	}
	if err := validateConditionalRouteKeySyntax(cfgPath); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	if len(imports) > 0 {
		fmt.Fprintf(os.Stderr, "  Resolved %d import(s): %s\n", len(imports), strings.Join(imports, ", "))
	}

	if autoResolvePlugins && cfg.Requires != nil {
		autoResolveRequiredPlugins(cfgPath, cfg.Requires.Plugins)
	}

	var opts []schema.ValidationOption
	if !strict {
		opts = append(opts, schema.WithAllowEmptyModules())
	}
	if skipUnknownTypes {
		opts = append(opts, schema.WithSkipModuleTypeCheck(), schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	} else {
		// Still skip workflow/trigger type checks by default (dynamic dispatch)
		opts = append(opts, schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	}
	if allowNoEntryPoints {
		opts = append(opts, schema.WithAllowNoEntryPoints())
	}

	// Pass legacy DO module types through schema validation so the actionable
	// migration error fires below instead of a generic "unknown module type".
	for t := range legacydo.ModuleTypes {
		opts = append(opts, schema.WithExtraModuleTypes(t))
	}
	// Same for legacy AWS module types removed in issue #653.
	for t := range legacyaws.ModuleTypes {
		opts = append(opts, schema.WithExtraModuleTypes(t))
	}

	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		return err
	}

	// Post-validate sweep: reject legacy DO and AWS module/step types with
	// actionable migration errors (issues #617, #653). wfctl validate has no
	// engine, so the iacProviderLoaded flag is always false here.
	for _, m := range cfg.Modules {
		if legacydo.IsModuleType(m.Type) {
			return legacydo.FormatModuleError(m.Type, m.Name, false)
		}
		if legacyaws.IsModuleType(m.Type) {
			return legacyaws.FormatModuleError(m.Type, m.Name, false)
		}
	}
	for _, rawPipeline := range cfg.Pipelines {
		yamlBytes, err := yaml.Marshal(rawPipeline)
		if err != nil {
			continue
		}
		var pipeCfg config.PipelineConfig
		if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
			continue
		}
		for _, s := range pipeCfg.Steps {
			if legacydo.IsStepType(s.Type) {
				return legacydo.FormatStepError(s.Type, false)
			}
			if legacyaws.IsStepType(s.Type) {
				return legacyaws.FormatStepError(s.Type, false)
			}
		}
	}

	// Validate ci:, environments:, and secrets: sections when present.
	if cfg.CI != nil {
		if err := cfg.CI.Validate(); err != nil {
			return fmt.Errorf("ci section: %w", err)
		}
	}

	// Validate services:, mesh:, networking:, security: sections when present.
	if len(cfg.Services) > 0 {
		if err := config.ValidateServices(cfg.Services); err != nil {
			return fmt.Errorf("services section: %w", err)
		}
	}
	if cfg.Mesh != nil && len(cfg.Services) > 0 {
		for _, warn := range config.ValidateMeshRoutes(cfg.Mesh, cfg.Services) {
			fmt.Fprintf(os.Stderr, "  WARN %s: mesh: %s\n", cfgPath, warn)
		}
	}
	if cfg.Networking != nil {
		if err := config.ValidateNetworking(cfg.Networking, cfg.Services); err != nil {
			return fmt.Errorf("networking section: %w", err)
		}
	}
	if cfg.Security != nil {
		if err := config.ValidateSecurity(cfg.Security); err != nil {
			return fmt.Errorf("security section: %w", err)
		}
	}
	for _, warn := range config.CrossValidate(cfg) {
		fmt.Fprintf(os.Stderr, "  WARN %s: %s\n", cfgPath, warn)
	}

	if cfg.Pipelines != nil {
		if refs := validation.ValidatePipelineTemplateRefs(cfg.Pipelines, schema.GetStepSchemaRegistry()); refs.HasIssues() {
			var findings []string
			blocking := refs.BlockingWarningMessages()
			for _, w := range refs.Warnings {
				msg := "pipeline-refs warning: " + w
				findings = append(findings, msg)
				if !strict || !containsString(blocking, w) {
					fmt.Fprintf(os.Stderr, "  WARN %s: %s\n", cfgPath, msg)
				}
			}
			for _, e := range refs.Errors {
				findings = append(findings, "pipeline-refs error: "+e)
			}
			if len(refs.Errors) > 0 {
				return fmt.Errorf("%s", strings.Join(findings, "\n"))
			}
			if strict && len(blocking) > 0 {
				for i, w := range blocking {
					blocking[i] = "pipeline-refs warning: " + w
				}
				return fmt.Errorf("%s", strings.Join(blocking, "\n"))
			}
		}
	}

	fmt.Printf("  PASS %s (%d modules, %d workflows, %d triggers)\n",
		cfgPath, len(cfg.Modules), len(cfg.Workflows), len(cfg.Triggers))
	return nil
}

func validateConditionalRouteKeySyntax(cfgPath string) error {
	return validateConditionalRouteKeySyntaxFile(cfgPath, make(map[string]bool))
}

func validateConditionalRouteKeySyntaxFile(cfgPath string, seen map[string]bool) error {
	absPath, err := filepath.Abs(cfgPath)
	if err != nil {
		return fmt.Errorf("inspect conditional route keys: %w", err)
	}
	if seen[absPath] {
		return nil
	}
	seen[absPath] = true

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("inspect conditional route keys: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("inspect conditional route keys: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	pipelines := mappingValue(root, "pipelines")
	if pipelines == nil || pipelines.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(pipelines.Content); i += 2 {
		pipelineName := pipelines.Content[i].Value
		pipeline := pipelines.Content[i+1]
		if pipeline.Kind != yaml.MappingNode {
			continue
		}
		steps := mappingValue(pipeline, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode || mappingScalarValue(step, "type") != "step.conditional" {
				continue
			}
			stepName := mappingScalarValue(step, "name")
			if stepName == "" {
				stepName = "<unnamed>"
			}
			cfg := mappingValue(step, "config")
			if cfg == nil || cfg.Kind != yaml.MappingNode {
				continue
			}
			routes := mappingValue(cfg, "routes")
			if routes == nil || routes.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(routes.Content); j += 2 {
				key := routes.Content[j]
				if key.ShortTag() == "!!str" {
					continue
				}
				return fmt.Errorf("pipeline %q step %q (type step.conditional): routes key %q is parsed as %s, not string; quote it as '%s'",
					pipelineName, stepName, key.Value, strings.TrimPrefix(key.ShortTag(), "!!"), key.Value)
			}
		}
	}
	for _, imp := range importPathsFromNode(root) {
		impPath := imp
		if !filepath.IsAbs(impPath) {
			impPath = filepath.Join(filepath.Dir(absPath), impPath)
		}
		if err := validateConditionalRouteKeySyntaxFile(impPath, seen); err != nil {
			return fmt.Errorf("import %q: %w", imp, err)
		}
	}
	return nil
}

func importPathsFromNode(root *yaml.Node) []string {
	imports := mappingValue(root, "imports")
	if imports == nil || imports.Kind != yaml.SequenceNode {
		return nil
	}
	paths := make([]string, 0, len(imports.Content))
	for _, item := range imports.Content {
		if item.Kind == yaml.ScalarNode && item.ShortTag() == "!!str" {
			paths = append(paths, item.Value)
		}
	}
	return paths
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mappingScalarValue(n *yaml.Node, key string) string {
	v := mappingValue(n, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// skipDirs are directory names that should be excluded from recursive scanning.
var skipDirs = map[string]bool{
	".playwright-cli": true,
	"node_modules":    true,
	".git":            true,
	"vendor":          true,
	"observability":   true,
}

// skipFiles are filename patterns that are not workflow configs.
var skipFiles = map[string]bool{
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
	"prometheus.yml":      true,
	"prometheus.yaml":     true,
	"datasource.yml":      true,
	"datasource.yaml":     true,
	"dashboard.yml":       true,
	"dashboard.yaml":      true,
}

// isWorkflowYAML reports whether the YAML file at path looks like a workflow
// config by checking the first 100 lines for top-level keys: modules:,
// workflows:, or pipelines:.
func isWorkflowYAML(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for i := 0; i < 100 && scanner.Scan(); i++ {
		line := scanner.Text()
		if strings.HasPrefix(line, "modules:") ||
			strings.HasPrefix(line, "workflows:") ||
			strings.HasPrefix(line, "pipelines:") {
			return true
		}
	}
	return false
}

func findYAMLFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		if skipFiles[info.Name()] {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// extractImports reads the raw YAML at path and returns the top-level imports: list.
// Returns nil if the file cannot be read or has no imports.
func extractImports(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Imports []string `yaml:"imports"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw.Imports
}

func indentError(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "\n       ")
}

// reorderFlags moves flag-like arguments (starting with "-") before
// positional arguments so that Go's flag.FlagSet.Parse handles them
// correctly regardless of where the user places them.
func reorderFlags(args []string) []string {
	var flags, positional []string
	// flags that take a value argument (not self-contained with "=")
	valueFlagNames := map[string]bool{
		"dir":             true,
		"lock-file":       true,
		"manifest":        true,
		"plugin-dir":      true,
		"plugin-manifest": true,
	}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			// If this flag takes a value (e.g. --dir foo), grab next arg too.
			// Flags with "=" are self-contained (--dir=foo).
			if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Peek: could be a flag value or a positional arg.
				// Only consume it if the flag is known to take a value.
				flagName := strings.TrimLeft(args[i], "-")
				if valueFlagNames[flagName] {
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}
