package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

func runDocs(args []string) error {
	if len(args) < 1 {
		return docsUsage()
	}
	switch args[0] {
	case "generate":
		return runDocsGenerate(args[1:])
	default:
		return docsUsage()
	}
}

func docsUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl docs <subcommand> [options]

Generate documentation from workflow configurations.

Subcommands:
  generate     Generate Markdown documentation with Mermaid diagrams

Examples:
  wfctl docs generate workflow.yaml
  wfctl docs generate -output ./docs/ workflow.yaml
  wfctl docs generate -output ./docs/ -plugin-dir ./plugins/ workflow.yaml
`)
	return fmt.Errorf("subcommand is required (generate)")
}

func runDocsGenerate(args []string) error {
	fs := flag.NewFlagSet("docs generate", flag.ContinueOnError)
	output := fs.String("output", "./docs/generated/", "Output directory for generated documentation")
	pluginDir := fs.String("plugin-dir", "", "Directory containing external plugin manifests (plugin.json)")
	title := fs.String("title", "", "Application title (default: derived from config)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl docs generate [options] <config.yaml>

Generate Markdown documentation with Mermaid diagrams from a workflow
configuration file. If -plugin-dir is specified, external plugin manifests
(plugin.json) are loaded and described in the output.

Options:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	configFile := fs.Arg(0)
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load external plugin manifests if a plugin directory is specified.
	var plugins []*plugin.PluginManifest
	if *pluginDir != "" {
		plugins, err = loadPluginManifests(*pluginDir)
		if err != nil {
			return fmt.Errorf("failed to load plugin manifests from %s: %w", *pluginDir, err)
		}
	}

	appTitle := *title
	if appTitle == "" {
		appTitle = deriveTitle(configFile)
	}

	if err := os.MkdirAll(*output, 0750); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", *output, err)
	}

	gen := &docsGenerator{
		cfg:       cfg,
		plugins:   plugins,
		title:     appTitle,
		outputDir: *output,
	}

	files, err := gen.generate()
	if err != nil {
		return fmt.Errorf("failed to generate documentation: %w", err)
	}

	for _, f := range files {
		fmt.Printf("  create  %s\n", f)
	}
	fmt.Printf("\nGenerated %d documentation file(s) in %s\n", len(files), *output)
	return nil
}

// loadPluginManifests recursively walks a directory tree looking for
// plugin.json files and returns the parsed manifests.
func loadPluginManifests(dir string) ([]*plugin.PluginManifest, error) {
	var manifests []*plugin.PluginManifest
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || info.Name() != "plugin.json" {
			return nil
		}
		m, loadErr := plugin.LoadManifest(path)
		if loadErr != nil {
			return nil //nolint:nilerr // intentionally skip invalid manifests
		}
		manifests = append(manifests, m)
		return nil
	})
	return manifests, err
}

// deriveTitle creates a human-readable title from the config file path.
func deriveTitle(configFile string) string {
	base := filepath.Base(configFile)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.Title(name) //nolint:staticcheck // strings.Title is adequate here
}

// mermaidQuote wraps a string for safe use as a mermaid node label.
// If the string contains characters that could break mermaid syntax it is
// wrapped in double-quotes with internal quotes escaped.
func mermaidQuote(s string) string {
	needsQuote := false
	for _, c := range s {
		switch c {
		case ' ', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'',
			'|', '#', '&', ';', ':', ',', '.', '/', '\\', '-', '+',
			'=', '!', '?', '@', '$', '%', '^', '*', '~', '`':
			needsQuote = true
		}
		if needsQuote {
			break
		}
	}
	if !needsQuote && s != "" {
		return s
	}
	escaped := strings.ReplaceAll(s, `"`, `#quot;`)
	return `"` + escaped + `"`
}

// mermaidID generates a safe mermaid node identifier from an arbitrary string.
func mermaidID(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	id := b.String()
	if id == "" {
		return "_empty"
	}
	return id
}

// docsGenerator holds state for a single documentation generation run.
type docsGenerator struct {
	cfg       *config.WorkflowConfig
	plugins   []*plugin.PluginManifest
	title     string
	outputDir string
}

func (g *docsGenerator) generate() ([]string, error) {
	var files []string

	// 1. README.md – overview
	readmePath := filepath.Join(g.outputDir, "README.md")
	if err := g.writeOverview(readmePath); err != nil {
		return nil, fmt.Errorf("writing overview: %w", err)
	}
	files = append(files, readmePath)

	// 2. modules.md – module inventory
	if len(g.cfg.Modules) > 0 {
		modPath := filepath.Join(g.outputDir, "modules.md")
		if err := g.writeModules(modPath); err != nil {
			return nil, fmt.Errorf("writing modules: %w", err)
		}
		files = append(files, modPath)
	}

	// 3. pipelines.md – pipeline details with mermaid diagrams
	if len(g.cfg.Pipelines) > 0 {
		pipPath := filepath.Join(g.outputDir, "pipelines.md")
		if err := g.writePipelines(pipPath); err != nil {
			return nil, fmt.Errorf("writing pipelines: %w", err)
		}
		files = append(files, pipPath)
	}

	// 4. workflows.md – workflow details (HTTP routes, messaging, etc.)
	if len(g.cfg.Workflows) > 0 {
		wfPath := filepath.Join(g.outputDir, "workflows.md")
		if err := g.writeWorkflows(wfPath); err != nil {
			return nil, fmt.Errorf("writing workflows: %w", err)
		}
		files = append(files, wfPath)
	}

	// 5. plugins.md – external plugin documentation
	if len(g.plugins) > 0 {
		plPath := filepath.Join(g.outputDir, "plugins.md")
		if err := g.writePlugins(plPath); err != nil {
			return nil, fmt.Errorf("writing plugins: %w", err)
		}
		files = append(files, plPath)
	}

	// 6. architecture.md – system architecture diagram
	archPath := filepath.Join(g.outputDir, "architecture.md")
	if err := g.writeArchitecture(archPath); err != nil {
		return nil, fmt.Errorf("writing architecture: %w", err)
	}
	files = append(files, archPath)

	return files, nil
}

// ---------- Overview (README.md) ----------

func (g *docsGenerator) writeOverview(path string) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", g.title)
	b.WriteString("> Auto-generated documentation from workflow configuration.\n\n")

	// Quick stats
	b.WriteString("## Overview\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n")
	fmt.Fprintf(&b, "|--------|-------|\n")
	fmt.Fprintf(&b, "| Modules | %d |\n", len(g.cfg.Modules))
	fmt.Fprintf(&b, "| Workflows | %d |\n", len(g.cfg.Workflows))
	fmt.Fprintf(&b, "| Pipelines | %d |\n", len(g.cfg.Pipelines))
	if g.cfg.Requires != nil {
		fmt.Fprintf(&b, "| Required Plugins | %d |\n", len(g.cfg.Requires.Plugins))
		fmt.Fprintf(&b, "| Required Capabilities | %d |\n", len(g.cfg.Requires.Capabilities))
	}
	if len(g.plugins) > 0 {
		fmt.Fprintf(&b, "| External Plugins (loaded) | %d |\n", len(g.plugins))
	}
	b.WriteString("\n")

	// Required plugins
	if g.cfg.Requires != nil && len(g.cfg.Requires.Plugins) > 0 {
		b.WriteString("## Required Plugins\n\n")
		b.WriteString("| Plugin | Version |\n")
		b.WriteString("|--------|---------|\n")
		for _, p := range g.cfg.Requires.Plugins {
			ver := p.Version
			if ver == "" {
				ver = "*"
			}
			fmt.Fprintf(&b, "| `%s` | %s |\n", p.Name, ver)
		}
		b.WriteString("\n")
	}

	// Required capabilities
	if g.cfg.Requires != nil && len(g.cfg.Requires.Capabilities) > 0 {
		b.WriteString("## Required Capabilities\n\n")
		for _, c := range g.cfg.Requires.Capabilities {
			fmt.Fprintf(&b, "- `%s`\n", c)
		}
		b.WriteString("\n")
	}

	// Sidecars
	if len(g.cfg.Sidecars) > 0 {
		b.WriteString("## Sidecars\n\n")
		b.WriteString("| Name | Type |\n")
		b.WriteString("|------|------|\n")
		for _, sc := range g.cfg.Sidecars {
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", sc.Name, sc.Type)
		}
		b.WriteString("\n")
	}

	// Table of contents
	b.WriteString("## Documentation Index\n\n")
	if len(g.cfg.Modules) > 0 {
		b.WriteString("- [Modules](modules.md) — Module inventory and dependency graph\n")
	}
	if len(g.cfg.Pipelines) > 0 {
		b.WriteString("- [Pipelines](pipelines.md) — Pipeline definitions with workflow diagrams\n")
	}
	if len(g.cfg.Workflows) > 0 {
		b.WriteString("- [Workflows](workflows.md) — HTTP routes, messaging, and workflow details\n")
	}
	if len(g.plugins) > 0 {
		b.WriteString("- [Plugins](plugins.md) — External plugin details and capabilities\n")
	}
	b.WriteString("- [Architecture](architecture.md) — System architecture diagram\n")
	b.WriteString("\n")

	return os.WriteFile(path, []byte(b.String()), 0600)
}

// ---------- Modules (modules.md) ----------

func (g *docsGenerator) writeModules(path string) error {
	var b strings.Builder

	b.WriteString("# Modules\n\n")

	// Module table
	b.WriteString("## Module Inventory\n\n")
	b.WriteString("| Name | Type | Dependencies |\n")
	b.WriteString("|------|------|--------------|\n")
	for _, mod := range g.cfg.Modules {
		deps := "—"
		if len(mod.DependsOn) > 0 {
			parts := make([]string, len(mod.DependsOn))
			for i, d := range mod.DependsOn {
				parts[i] = "`" + d + "`"
			}
			deps = strings.Join(parts, ", ")
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", mod.Name, mod.Type, deps)
	}
	b.WriteString("\n")

	// Module type summary
	typeCount := make(map[string]int)
	for _, mod := range g.cfg.Modules {
		typeCount[mod.Type]++
	}
	b.WriteString("## Module Types\n\n")
	b.WriteString("| Type | Count |\n")
	b.WriteString("|------|-------|\n")
	types := sortedKeys(typeCount)
	for _, t := range types {
		fmt.Fprintf(&b, "| `%s` | %d |\n", t, typeCount[t])
	}
	b.WriteString("\n")

	// Module configurations
	b.WriteString("## Module Configuration Details\n\n")
	for _, mod := range g.cfg.Modules {
		fmt.Fprintf(&b, "### `%s`\n\n", mod.Name)
		fmt.Fprintf(&b, "- **Type:** `%s`\n", mod.Type)
		if len(mod.DependsOn) > 0 {
			fmt.Fprintf(&b, "- **Dependencies:** %s\n", strings.Join(mod.DependsOn, ", "))
		}
		if len(mod.Config) > 0 {
			b.WriteString("\n**Configuration:**\n\n```yaml\n")
			writeConfigYAML(&b, mod.Config, "")
			b.WriteString("```\n")
		}
		b.WriteString("\n")
	}

	// Dependency graph
	hasDeps := false
	for _, mod := range g.cfg.Modules {
		if len(mod.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	if hasDeps {
		b.WriteString("## Dependency Graph\n\n")
		b.WriteString("```mermaid\ngraph LR\n")
		for _, mod := range g.cfg.Modules {
			for _, dep := range mod.DependsOn {
				fmt.Fprintf(&b, "    %s[%s] --> %s[%s]\n",
					mermaidID(mod.Name), mermaidQuote(mod.Name),
					mermaidID(dep), mermaidQuote(dep))
			}
		}
		b.WriteString("```\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

// ---------- Pipelines (pipelines.md) ----------

func (g *docsGenerator) writePipelines(path string) error {
	var b strings.Builder

	b.WriteString("# Pipelines\n\n")

	names := sortedMapKeys(g.cfg.Pipelines)
	for _, name := range names {
		pipelineRaw := g.cfg.Pipelines[name]
		fmt.Fprintf(&b, "## %s\n\n", name)

		pMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			b.WriteString("_Unable to parse pipeline configuration._\n\n")
			continue
		}

		// Trigger info
		if trigRaw, ok := pMap["trigger"]; ok {
			if trig, ok := trigRaw.(map[string]any); ok {
				trigType, _ := trig["type"].(string)
				fmt.Fprintf(&b, "**Trigger:** `%s`\n\n", trigType)
				if trigCfg, ok := trig["config"].(map[string]any); ok {
					if method, ok := trigCfg["method"].(string); ok {
						fmt.Fprintf(&b, "- **Method:** `%s`\n", method)
					}
					if path, ok := trigCfg["path"].(string); ok {
						fmt.Fprintf(&b, "- **Path:** `%s`\n", path)
					}
					if cmd, ok := trigCfg["command"].(string); ok {
						fmt.Fprintf(&b, "- **Command:** `%s`\n", cmd)
					}
				}
				b.WriteString("\n")
			}
		}

		// Timeout & on_error
		if timeout, ok := pMap["timeout"].(string); ok {
			fmt.Fprintf(&b, "**Timeout:** `%s`\n\n", timeout)
		}
		if onErr, ok := pMap["on_error"].(string); ok {
			fmt.Fprintf(&b, "**On Error:** `%s`\n\n", onErr)
		}

		// Steps table
		steps := extractSteps(pMap, "steps")
		if len(steps) > 0 {
			b.WriteString("### Steps\n\n")
			b.WriteString("| # | Name | Type |\n")
			b.WriteString("|---|------|------|\n")
			for i, step := range steps {
				fmt.Fprintf(&b, "| %d | `%s` | `%s` |\n", i+1, step.name, step.typ)
			}
			b.WriteString("\n")

			// Mermaid workflow diagram
			b.WriteString("### Workflow Diagram\n\n")
			b.WriteString("```mermaid\ngraph TD\n")
			fmt.Fprintf(&b, "    trigger([%s]) --> %s[%s]\n",
				mermaidQuote("trigger"), mermaidID(steps[0].name), mermaidQuote(steps[0].name))
			for i := 0; i < len(steps)-1; i++ {
				fmt.Fprintf(&b, "    %s[%s] --> %s[%s]\n",
					mermaidID(steps[i].name), mermaidQuote(steps[i].name),
					mermaidID(steps[i+1].name), mermaidQuote(steps[i+1].name))
			}
			last := steps[len(steps)-1]
			fmt.Fprintf(&b, "    %s[%s] --> done([%s])\n",
				mermaidID(last.name), mermaidQuote(last.name), mermaidQuote("done"))
			b.WriteString("```\n\n")
		}

		// Compensation steps
		compSteps := extractSteps(pMap, "compensation")
		if len(compSteps) > 0 {
			b.WriteString("### Compensation Steps\n\n")
			b.WriteString("| # | Name | Type |\n")
			b.WriteString("|---|------|------|\n")
			for i, step := range compSteps {
				fmt.Fprintf(&b, "| %d | `%s` | `%s` |\n", i+1, step.name, step.typ)
			}
			b.WriteString("\n")
		}

		b.WriteString("---\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

// ---------- Workflows (workflows.md) ----------

func (g *docsGenerator) writeWorkflows(path string) error {
	var b strings.Builder

	b.WriteString("# Workflows\n\n")

	names := sortedMapKeys(g.cfg.Workflows)
	for _, name := range names {
		wfRaw := g.cfg.Workflows[name]
		fmt.Fprintf(&b, "## %s\n\n", name)

		wfMap, ok := wfRaw.(map[string]any)
		if !ok {
			b.WriteString("_Unable to parse workflow configuration._\n\n")
			continue
		}

		switch name {
		case "http":
			g.writeHTTPWorkflow(&b, wfMap)
		case "messaging":
			g.writeMessagingWorkflow(&b, wfMap)
		case "statemachine":
			g.writeStateMachineWorkflow(&b, wfMap)
		default:
			// Generic workflow: dump keys
			g.writeGenericWorkflow(&b, name, wfMap)
		}

		b.WriteString("---\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

func (g *docsGenerator) writeHTTPWorkflow(b *strings.Builder, wf map[string]any) {
	routesRaw, ok := wf["routes"]
	if !ok {
		return
	}
	routesList, ok := routesRaw.([]any)
	if !ok {
		return
	}

	b.WriteString("### HTTP Routes\n\n")
	b.WriteString("| Method | Path | Handler | Middlewares |\n")
	b.WriteString("|--------|------|---------|-------------|\n")

	type routeInfo struct {
		method, path, handler string
		middlewares           []string
	}
	var routes []routeInfo

	for _, rRaw := range routesList {
		rMap, ok := rRaw.(map[string]any)
		if !ok {
			continue
		}
		ri := routeInfo{}
		ri.method, _ = rMap["method"].(string)
		ri.path, _ = rMap["path"].(string)
		ri.handler, _ = rMap["handler"].(string)
		if mws, ok := rMap["middlewares"].([]any); ok {
			for _, mw := range mws {
				if s, ok := mw.(string); ok {
					ri.middlewares = append(ri.middlewares, s)
				}
			}
		}
		routes = append(routes, ri)
	}

	for _, r := range routes {
		mw := "—"
		if len(r.middlewares) > 0 {
			parts := make([]string, len(r.middlewares))
			for i, m := range r.middlewares {
				parts[i] = "`" + m + "`"
			}
			mw = strings.Join(parts, ", ")
		}
		fmt.Fprintf(b, "| `%s` | `%s` | `%s` | %s |\n", r.method, r.path, r.handler, mw)
	}
	b.WriteString("\n")

	// Route diagram
	if len(routes) > 0 {
		b.WriteString("### Route Diagram\n\n")
		b.WriteString("```mermaid\ngraph LR\n")
		b.WriteString("    Client([Client])\n")
		for i, r := range routes {
			routeID := fmt.Sprintf("route%d", i)
			label := fmt.Sprintf("%s %s", r.method, r.path)
			fmt.Fprintf(b, "    Client --> %s[%s]\n", routeID, mermaidQuote(label))
			if len(r.middlewares) > 0 {
				prevID := routeID
				for j, mw := range r.middlewares {
					mwID := fmt.Sprintf("%s_mw%d", routeID, j)
					fmt.Fprintf(b, "    %s --> %s{{%s}}\n", prevID, mwID, mermaidQuote(mw))
					prevID = mwID
				}
				fmt.Fprintf(b, "    %s --> %s[%s]\n", prevID, mermaidID(r.handler), mermaidQuote(r.handler))
			} else {
				fmt.Fprintf(b, "    %s --> %s[%s]\n", routeID, mermaidID(r.handler), mermaidQuote(r.handler))
			}
		}
		b.WriteString("```\n\n")
	}
}

func (g *docsGenerator) writeMessagingWorkflow(b *strings.Builder, wf map[string]any) {
	// Subscriptions
	if subsRaw, ok := wf["subscriptions"]; ok {
		if subsList, ok := subsRaw.([]any); ok && len(subsList) > 0 {
			b.WriteString("### Subscriptions\n\n")
			b.WriteString("| Topic | Handler |\n")
			b.WriteString("|-------|---------|\n")

			type sub struct{ topic, handler string }
			var subs []sub
			for _, sRaw := range subsList {
				if sMap, ok := sRaw.(map[string]any); ok {
					s := sub{}
					s.topic, _ = sMap["topic"].(string)
					s.handler, _ = sMap["handler"].(string)
					subs = append(subs, s)
					fmt.Fprintf(b, "| `%s` | `%s` |\n", s.topic, s.handler)
				}
			}
			b.WriteString("\n")

			// Messaging diagram
			if len(subs) > 0 {
				b.WriteString("### Messaging Diagram\n\n")
				b.WriteString("```mermaid\ngraph LR\n")
				for _, s := range subs {
					topicID := mermaidID("topic_" + s.topic)
					fmt.Fprintf(b, "    %s>%s] --> %s[%s]\n",
						topicID, mermaidQuote(s.topic),
						mermaidID(s.handler), mermaidQuote(s.handler))
				}
				b.WriteString("```\n\n")
			}
		}
	}

	// Producers
	if prodsRaw, ok := wf["producers"]; ok {
		if prodsList, ok := prodsRaw.([]any); ok && len(prodsList) > 0 {
			b.WriteString("### Producers\n\n")
			b.WriteString("| Producer | Publishes To |\n")
			b.WriteString("|----------|--------------|\n")
			for _, pRaw := range prodsList {
				if pMap, ok := pRaw.(map[string]any); ok {
					name, _ := pMap["name"].(string)
					var topics []string
					if fwdRaw, ok := pMap["forwardTo"].([]any); ok {
						for _, t := range fwdRaw {
							if ts, ok := t.(string); ok {
								topics = append(topics, "`"+ts+"`")
							}
						}
					}
					fmt.Fprintf(b, "| `%s` | %s |\n", name, strings.Join(topics, ", "))
				}
			}
			b.WriteString("\n")
		}
	}
}

func (g *docsGenerator) writeStateMachineWorkflow(b *strings.Builder, wf map[string]any) {
	defsRaw, ok := wf["definitions"]
	if !ok {
		return
	}
	defsList, ok := defsRaw.([]any)
	if !ok {
		return
	}

	for _, dRaw := range defsList {
		dMap, ok := dRaw.(map[string]any)
		if !ok {
			continue
		}
		smName, _ := dMap["name"].(string)
		smDesc, _ := dMap["description"].(string)
		initial, _ := dMap["initialState"].(string)

		fmt.Fprintf(b, "### State Machine: %s\n\n", smName)
		if smDesc != "" {
			fmt.Fprintf(b, "%s\n\n", smDesc)
		}
		fmt.Fprintf(b, "**Initial State:** `%s`\n\n", initial)

		// States table
		if statesRaw, ok := dMap["states"].(map[string]any); ok {
			b.WriteString("#### States\n\n")
			b.WriteString("| State | Description | Final | Error |\n")
			b.WriteString("|-------|-------------|-------|-------|\n")

			stateNames := sortedMapKeys(statesRaw)
			for _, sName := range stateNames {
				sRaw := statesRaw[sName]
				sMap, ok := sRaw.(map[string]any)
				if !ok {
					continue
				}
				desc, _ := sMap["description"].(string)
				isFinal := toBool(sMap["isFinal"])
				isError := toBool(sMap["isError"])
				fmt.Fprintf(b, "| `%s` | %s | %v | %v |\n", sName, desc, isFinal, isError)
			}
			b.WriteString("\n")
		}

		// Transitions table and diagram
		if transRaw, ok := dMap["transitions"].(map[string]any); ok {
			b.WriteString("#### Transitions\n\n")
			b.WriteString("| Transition | From | To |\n")
			b.WriteString("|------------|------|----|\n")

			type transition struct {
				name, from, to string
			}
			var transitions []transition
			transNames := sortedMapKeys(transRaw)
			for _, tName := range transNames {
				tRaw := transRaw[tName]
				tMap, ok := tRaw.(map[string]any)
				if !ok {
					continue
				}
				from, _ := tMap["fromState"].(string)
				to, _ := tMap["toState"].(string)
				transitions = append(transitions, transition{tName, from, to})
				fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", tName, from, to)
			}
			b.WriteString("\n")

			// State machine diagram
			if len(transitions) > 0 {
				b.WriteString("#### State Diagram\n\n")
				b.WriteString("```mermaid\nstateDiagram-v2\n")
				if initial != "" {
					fmt.Fprintf(b, "    [*] --> %s\n", mermaidID(initial))
				}
				for _, t := range transitions {
					fmt.Fprintf(b, "    %s --> %s : %s\n",
						mermaidID(t.from), mermaidID(t.to), mermaidQuote(t.name))
				}
				// Mark final states
				if statesRaw, ok := dMap["states"].(map[string]any); ok {
					for sName, sRaw := range statesRaw {
						if sMap, ok := sRaw.(map[string]any); ok {
							if toBool(sMap["isFinal"]) {
								fmt.Fprintf(b, "    %s --> [*]\n", mermaidID(sName))
							}
						}
					}
				}
				b.WriteString("```\n\n")
			}
		}
	}
}

func (g *docsGenerator) writeGenericWorkflow(b *strings.Builder, name string, wf map[string]any) {
	b.WriteString("### Configuration\n\n")
	b.WriteString("```yaml\n")
	writeConfigYAML(b, wf, "")
	b.WriteString("```\n\n")
}

// ---------- Plugins (plugins.md) ----------

func (g *docsGenerator) writePlugins(path string) error {
	var b strings.Builder

	b.WriteString("# External Plugins\n\n")

	for _, p := range g.plugins {
		fmt.Fprintf(&b, "## %s\n\n", p.Name)
		fmt.Fprintf(&b, "- **Version:** `%s`\n", p.Version)
		fmt.Fprintf(&b, "- **Author:** %s\n", p.Author)
		if p.Description != "" {
			fmt.Fprintf(&b, "- **Description:** %s\n", p.Description)
		}
		if p.License != "" {
			fmt.Fprintf(&b, "- **License:** %s\n", p.License)
		}
		if p.Tier != "" {
			fmt.Fprintf(&b, "- **Tier:** %s\n", string(p.Tier))
		}
		if p.Repository != "" {
			fmt.Fprintf(&b, "- **Repository:** [%s](%s)\n", p.Repository, p.Repository)
		}
		b.WriteString("\n")

		// Module types
		if len(p.ModuleTypes) > 0 {
			b.WriteString("### Module Types\n\n")
			for _, mt := range p.ModuleTypes {
				fmt.Fprintf(&b, "- `%s`\n", mt)
			}
			b.WriteString("\n")
		}

		// Step types
		if len(p.StepTypes) > 0 {
			b.WriteString("### Step Types\n\n")
			for _, st := range p.StepTypes {
				fmt.Fprintf(&b, "- `%s`\n", st)
			}
			b.WriteString("\n")
		}

		// Trigger types
		if len(p.TriggerTypes) > 0 {
			b.WriteString("### Trigger Types\n\n")
			for _, tt := range p.TriggerTypes {
				fmt.Fprintf(&b, "- `%s`\n", tt)
			}
			b.WriteString("\n")
		}

		// Workflow types
		if len(p.WorkflowTypes) > 0 {
			b.WriteString("### Workflow Types\n\n")
			for _, wt := range p.WorkflowTypes {
				fmt.Fprintf(&b, "- `%s`\n", wt)
			}
			b.WriteString("\n")
		}

		// Capabilities
		if len(p.Capabilities) > 0 {
			b.WriteString("### Capabilities\n\n")
			b.WriteString("| Name | Role | Priority |\n")
			b.WriteString("|------|------|----------|\n")
			for _, cap := range p.Capabilities {
				fmt.Fprintf(&b, "| `%s` | %s | %d |\n", cap.Name, cap.Role, cap.Priority)
			}
			b.WriteString("\n")
		}

		// Dependencies
		if len(p.Dependencies) > 0 {
			b.WriteString("### Dependencies\n\n")
			b.WriteString("| Plugin | Constraint |\n")
			b.WriteString("|--------|------------|\n")
			for _, dep := range p.Dependencies {
				fmt.Fprintf(&b, "| `%s` | `%s` |\n", dep.Name, dep.Constraint)
			}
			b.WriteString("\n")
		}

		// Tags
		if len(p.Tags) > 0 {
			b.WriteString("### Tags\n\n")
			tagStrs := make([]string, len(p.Tags))
			for i, t := range p.Tags {
				tagStrs[i] = "`" + t + "`"
			}
			fmt.Fprintf(&b, "%s\n\n", strings.Join(tagStrs, " "))
		}

		b.WriteString("---\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

// ---------- Architecture (architecture.md) ----------

func (g *docsGenerator) writeArchitecture(path string) error {
	var b strings.Builder

	b.WriteString("# System Architecture\n\n")

	// Categorize modules by layer
	layers := g.categorizeLayers()

	b.WriteString("## Architecture Diagram\n\n")
	b.WriteString("```mermaid\ngraph TB\n")

	// Define subgraphs for each layer
	layerOrder := []string{"HTTP", "Processing", "State Management", "Messaging", "Storage", "Observability", "Other"}
	for _, layer := range layerOrder {
		mods, ok := layers[layer]
		if !ok || len(mods) == 0 {
			continue
		}
		layerID := mermaidID("layer_" + layer)
		fmt.Fprintf(&b, "    subgraph %s[%s]\n", layerID, mermaidQuote(layer))
		for _, mod := range mods {
			fmt.Fprintf(&b, "        %s[%s]\n", mermaidID(mod.Name), mermaidQuote(mod.Name))
		}
		b.WriteString("    end\n")
	}

	// Draw dependency edges
	for _, mod := range g.cfg.Modules {
		for _, dep := range mod.DependsOn {
			fmt.Fprintf(&b, "    %s --> %s\n", mermaidID(mod.Name), mermaidID(dep))
		}
	}

	// External systems
	hasExternal := false
	for _, mod := range g.cfg.Modules {
		t := strings.ToLower(mod.Type)
		if strings.Contains(t, "http.server") {
			if !hasExternal {
				b.WriteString("    Clients([External Clients])\n")
				hasExternal = true
			}
			fmt.Fprintf(&b, "    Clients --> %s\n", mermaidID(mod.Name))
		}
	}

	b.WriteString("```\n\n")

	// Plugin architecture (if plugins are loaded)
	if len(g.plugins) > 0 {
		b.WriteString("## Plugin Architecture\n\n")
		b.WriteString("```mermaid\ngraph LR\n")
		b.WriteString("    Engine([Workflow Engine])\n")
		for _, p := range g.plugins {
			pID := mermaidID("plugin_" + p.Name)
			fmt.Fprintf(&b, "    Engine --> %s[%s]\n", pID, mermaidQuote(p.Name+" v"+p.Version))
			for _, mt := range p.ModuleTypes {
				mtID := mermaidID("mt_" + p.Name + "_" + mt)
				fmt.Fprintf(&b, "    %s --> %s[%s]\n", pID, mtID, mermaidQuote(mt))
			}
			for _, st := range p.StepTypes {
				stID := mermaidID("st_" + p.Name + "_" + st)
				fmt.Fprintf(&b, "    %s --> %s[%s]\n", pID, stID, mermaidQuote(st))
			}
		}
		b.WriteString("```\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

// categorizeLayers groups modules by their architectural layer.
func (g *docsGenerator) categorizeLayers() map[string][]config.ModuleConfig {
	layers := make(map[string][]config.ModuleConfig)
	for _, mod := range g.cfg.Modules {
		layer := classifyModuleLayer(mod.Type)
		layers[layer] = append(layers[layer], mod)
	}
	return layers
}

func classifyModuleLayer(modType string) string {
	t := strings.ToLower(modType)
	switch {
	case strings.HasPrefix(t, "http.") || strings.HasPrefix(t, "static.") || strings.Contains(t, "reverseproxy"):
		return "HTTP"
	case strings.HasPrefix(t, "messaging.") || strings.HasPrefix(t, "event."):
		return "Messaging"
	case strings.HasPrefix(t, "state") || strings.HasPrefix(t, "statemachine"):
		return "State Management"
	case strings.HasPrefix(t, "storage.") || strings.HasPrefix(t, "database.") ||
		strings.Contains(t, "sqlite") || strings.Contains(t, "postgres"):
		return "Storage"
	case strings.HasPrefix(t, "metrics.") || strings.HasPrefix(t, "health.") ||
		strings.HasPrefix(t, "observability."):
		return "Observability"
	case strings.HasPrefix(t, "data.") || strings.HasPrefix(t, "auth.") ||
		strings.Contains(t, "transformer") || strings.Contains(t, "handler"):
		return "Processing"
	default:
		return "Other"
	}
}

// ---------- Helpers ----------

type stepInfo struct {
	name string
	typ  string
}

func extractSteps(pMap map[string]any, key string) []stepInfo {
	stepsRaw, ok := pMap[key]
	if !ok {
		return nil
	}
	stepsList, ok := stepsRaw.([]any)
	if !ok {
		return nil
	}
	var steps []stepInfo
	for _, sRaw := range stepsList {
		sMap, ok := sRaw.(map[string]any)
		if !ok {
			continue
		}
		si := stepInfo{}
		si.name, _ = sMap["name"].(string)
		si.typ, _ = sMap["type"].(string)
		steps = append(steps, si)
	}
	return steps
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeConfigYAML(b *strings.Builder, m map[string]any, indent string) {
	keys := sortedMapKeys(m)
	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case map[string]any:
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			writeConfigYAML(b, val, indent+"  ")
		case []any:
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			for _, item := range val {
				if subMap, ok := item.(map[string]any); ok {
					fmt.Fprintf(b, "%s  -\n", indent)
					writeConfigYAML(b, subMap, indent+"    ")
				} else {
					fmt.Fprintf(b, "%s  - %v\n", indent, item)
				}
			}
		default:
			fmt.Fprintf(b, "%s%s: %v\n", indent, k, v)
		}
	}
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	}
	return false
}

// writeJSON writes an indented JSON representation of v to the given path.
// This is used only by test helpers that need to create plugin.json fixtures.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
