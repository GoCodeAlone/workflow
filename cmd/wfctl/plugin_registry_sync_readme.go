package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runPluginRegistrySyncReadme ports workflow-registry/scripts/generate-readme.sh
// (workflow#762). Regenerates the plugin/template indexes in
// <registry-dir>/README.md from registry source data.
func runPluginRegistrySyncReadme(args []string) error {
	fs := flag.NewFlagSet("plugin registry-sync readme", flag.ContinueOnError)
	check := fs.Bool("check", false, "Dry-run; exit non-zero on diff")
	registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin registry-sync readme [--check] [--registry-dir <path>]

Regenerates the plugin/template indexes in <registry-dir>/README.md between
marker comments. With --check, exits non-zero on diff (CI dry-run).

Replaces workflow-registry/scripts/generate-readme.sh.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	readmePath := filepath.Join(*registryDir, "README.md")
	current, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read README.md: %w", err)
	}
	next, err := renderRegistryReadme(*registryDir, string(current))
	if err != nil {
		return err
	}
	if *check {
		if string(current) != next {
			fmt.Fprintln(os.Stderr, "README.md is out of date; run wfctl plugin registry-sync readme")
			return fmt.Errorf("README.md is out of date")
		}
		return nil
	}
	if err := os.WriteFile(readmePath, []byte(next), 0o644); err != nil { // #nosec G306 -- README.md is a normal repository file.
		return fmt.Errorf("write README.md: %w", err)
	}
	return nil
}

type registryReadmePlugin struct {
	Dir         string
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Tier        string `json:"tier"`
}

func renderRegistryReadme(registryDir, current string) (string, error) {
	plugins, err := loadRegistryReadmePlugins(filepath.Join(registryDir, "plugins"))
	if err != nil {
		return "", err
	}
	templates, err := loadRegistryReadmeTemplates(filepath.Join(registryDir, "templates"))
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(readmePrefix(current))
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(`## Built-in Plugins

These plugins ship in the ` + "`GoCodeAlone/workflow`" + ` engine and are available without installing a separate plugin repository.

| Plugin | Description |
|--------|-------------|
`)
	writeRegistryReadmePluginRows(&b, plugins, func(p registryReadmePlugin) bool {
		return p.Type == "builtin"
	}, false)

	b.WriteString(`
## First-party External Plugins

These plugins are maintained by GoCodeAlone as core platform capabilities, but are distributed outside the engine repository.

| Plugin | Description |
|--------|-------------|
`)
	writeRegistryReadmePluginRows(&b, plugins, func(p registryReadmePlugin) bool {
		return p.Type == "external" && p.Tier == "core"
	}, false)

	b.WriteString(`
## Community and Premium External Plugins

These plugins are distributed outside the engine repository and are maintained as community or commercial extensions.

| Plugin | Description | Tier |
|--------|-------------|------|
`)
	writeRegistryReadmePluginRows(&b, plugins, func(p registryReadmePlugin) bool {
		return p.Type == "external" && p.Tier != "core"
	}, true)

	b.WriteString(`
## Templates

Starter configurations for common workflow patterns:

| Template | Description |
|----------|-------------|
`)
	for _, tmpl := range templates {
		fmt.Fprintf(&b, "| [%s](./templates/%s) | %s |\n", tmpl.Name, tmpl.File, escapeRegistryReadmeCell(tmpl.Description))
	}
	b.WriteString(`
Initialize a project from a template:

` + "```bash" + `
wfctl init my-project --template api-service
` + "```" + `

---

`)
	b.WriteString(readmeSchemaSuffix(current))
	return b.String(), nil
}

func loadRegistryReadmePlugins(pluginsDir string) ([]registryReadmePlugin, error) {
	matches, err := filepath.Glob(filepath.Join(pluginsDir, "*", "manifest.json"))
	if err != nil {
		return nil, err
	}
	plugins := make([]registryReadmePlugin, 0, len(matches))
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var p registryReadmePlugin
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		p.Dir = filepath.Base(filepath.Dir(path))
		plugins = append(plugins, p)
	}
	sort.SliceStable(plugins, func(i, j int) bool {
		left := strings.ToLower(registryReadmePluginLink(plugins[i].Dir))
		right := strings.ToLower(registryReadmePluginLink(plugins[j].Dir))
		if left == right {
			return plugins[i].Dir < plugins[j].Dir
		}
		return left < right
	})
	return plugins, nil
}

type registryReadmeTemplate struct {
	File        string
	Name        string
	Description string
}

func loadRegistryReadmeTemplates(templatesDir string) ([]registryReadmeTemplate, error) {
	matches, err := filepath.Glob(filepath.Join(templatesDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	out := make([]registryReadmeTemplate, 0, len(matches))
	for _, path := range matches {
		desc, err := registryTemplateDescription(path)
		if err != nil {
			return nil, err
		}
		file := filepath.Base(path)
		out = append(out, registryReadmeTemplate{
			File:        file,
			Name:        strings.TrimSuffix(file, filepath.Ext(file)),
			Description: desc,
		})
	}
	return out, nil
}

func registryTemplateDescription(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:")), nil
		}
	}
	return "", nil
}

func writeRegistryReadmePluginRows(
	b *strings.Builder,
	plugins []registryReadmePlugin,
	include func(registryReadmePlugin) bool,
	includeTier bool,
) {
	for _, p := range plugins {
		if !include(p) {
			continue
		}
		if includeTier {
			fmt.Fprintf(b, "| %s | %s | %s |\n",
				registryReadmePluginLink(p.Dir), escapeRegistryReadmeCell(p.Description), p.Tier)
			continue
		}
		fmt.Fprintf(b, "| %s | %s |\n",
			registryReadmePluginLink(p.Dir), escapeRegistryReadmeCell(p.Description))
	}
}

func registryReadmePluginLink(dir string) string {
	return fmt.Sprintf("[%s](./plugins/%s/manifest.json)", dir, dir)
}

func escapeRegistryReadmeCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func readmePrefix(current string) string {
	headings := []string{
		"## Built-in Plugins",
		"## Core Plugins",
		"## External Plugins",
		"## First-party External Plugins",
		"## Community and Premium External Plugins",
	}
	idx := len(current)
	for _, h := range headings {
		if pos := strings.Index(current, h); pos >= 0 && pos < idx {
			idx = pos
		}
	}
	return current[:idx]
}

func readmeSchemaSuffix(current string) string {
	if idx := strings.Index(current, "## Schema"); idx >= 0 {
		return current[idx:]
	}
	return ""
}
