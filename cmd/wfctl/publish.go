package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// registryManifest is the manifest format for the GoCodeAlone/workflow-registry.
// It differs from plugin.PluginManifest in field names and structure to match the registry schema.
type registryManifest struct {
	Name             string               `json:"name"`
	Version          string               `json:"version"`
	Author           string               `json:"author"`
	Description      string               `json:"description"`
	Source           string               `json:"source,omitempty"`
	Path             string               `json:"path,omitempty"`
	Type             string               `json:"type"`
	Tier             string               `json:"tier"`
	License          string               `json:"license"`
	MinEngineVersion string               `json:"minEngineVersion,omitempty"`
	Keywords         []string             `json:"keywords,omitempty"`
	Homepage         string               `json:"homepage,omitempty"`
	Repository       string               `json:"repository,omitempty"`
	Capabilities     *registryCapabilities `json:"capabilities,omitempty"`
	Checksums        map[string]string    `json:"checksums,omitempty"`
}

// registryCapabilities holds plugin capability declarations for the registry.
type registryCapabilities struct {
	ModuleTypes      []string `json:"moduleTypes,omitempty"`
	StepTypes        []string `json:"stepTypes,omitempty"`
	TriggerTypes     []string `json:"triggerTypes,omitempty"`
	WorkflowHandlers []string `json:"workflowHandlers,omitempty"`
	WiringHooks      []string `json:"wiringHooks,omitempty"`
}

// validTypes and validTiers match the registry schema enums.
var (
	validTypes = map[string]bool{"builtin": true, "external": true, "ui": true}
	validTiers = map[string]bool{"core": true, "community": true, "premium": true}
	semverRe   = regexp.MustCompile(`^\d+\.\d+\.\d+`)
)

func runPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	dir := fs.String("dir", ".", "Plugin project directory (default: current dir)")
	registry := fs.String("registry", "GoCodeAlone/workflow-registry", "Registry repo (owner/repo)")
	dryRun := fs.Bool("dry-run", false, "Validate and print manifest without submitting")
	output := fs.String("output", "", "Write manifest to file instead of submitting")
	build := fs.Bool("build", false, "Build plugin binary for current platform")
	pluginType := fs.String("type", "external", "Plugin type: builtin, external, or ui")
	pluginTier := fs.String("tier", "community", "Plugin tier: core, community, or premium")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl publish [options]

Prepare and publish a plugin manifest to the workflow-registry.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nExamples:\n  wfctl publish --dry-run\n  wfctl publish --output manifest.json\n  wfctl publish --build --dry-run\n  wfctl publish --dir ./my-plugin --type external --tier community\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("invalid directory %q: %w", *dir, err)
	}

	// Step 1: Load or auto-detect manifest
	m, err := loadOrDetectManifest(absDir, *pluginType, *pluginTier)
	if err != nil {
		return err
	}

	// Step 2: Build binary if requested
	if *build {
		checksum, err := buildPlugin(absDir, m.Name)
		if err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
		if m.Checksums == nil {
			m.Checksums = make(map[string]string)
		}
		m.Checksums[m.Version] = checksum
		fmt.Fprintf(os.Stderr, "built plugin binary, SHA256: %s\n", checksum)
	}

	// Step 3: Validate manifest
	if err := validateRegistryManifest(m); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	// Step 4: Marshal manifest
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')

	// Step 5: Output or print submission instructions
	switch {
	case *dryRun:
		fmt.Printf("%s\n", data)
		fmt.Fprintf(os.Stderr, "dry-run: manifest validated successfully\n")
	case *output != "":
		if err := os.WriteFile(*output, data, 0600); err != nil { //nolint:gosec // G306: manifest files are user-owned output
			return fmt.Errorf("write manifest: %w", err)
		}
		fmt.Printf("manifest written to %s\n", *output)
		printSubmissionInstructions(m.Name, *registry)
	default:
		fmt.Printf("%s\n", data)
		printSubmissionInstructions(m.Name, *registry)
	}

	return nil
}

// loadOrDetectManifest reads manifest.json if present, otherwise auto-detects from source.
func loadOrDetectManifest(dir, pluginType, pluginTier string) (*registryManifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return loadRegistryManifest(manifestPath)
	}

	// Fall back to plugin.json (internal manifest format)
	pluginJSONPath := filepath.Join(dir, "plugin.json")
	if _, err := os.Stat(pluginJSONPath); err == nil {
		return loadRegistryManifestFromPluginJSON(pluginJSONPath, pluginType, pluginTier)
	}

	// Auto-detect from Go source
	return autoDetectManifest(dir, pluginType, pluginTier)
}

// loadRegistryManifest loads a manifest.json that already uses registry format.
func loadRegistryManifest(path string) (*registryManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	var m registryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest.json: %w", err)
	}
	return &m, nil
}

// pluginJSONSchema mirrors the internal plugin.PluginManifest for loading plugin.json.
type pluginJSONSchema struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Tags        []string `json:"tags"`
	Repository  string   `json:"repository"`
	Tier        string   `json:"tier"`
	ModuleTypes []string `json:"moduleTypes"`
	StepTypes   []string `json:"stepTypes"`
	TriggerTypes []string `json:"triggerTypes"`
	WorkflowTypes []string `json:"workflowTypes"`
	WiringHooks []string `json:"wiringHooks"`
}

// loadRegistryManifestFromPluginJSON converts an internal plugin.json to registry format.
func loadRegistryManifestFromPluginJSON(path, pluginType, pluginTier string) (*registryManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin.json: %w", err)
	}
	var p pluginJSONSchema
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plugin.json: %w", err)
	}

	tier := p.Tier
	if tier == "" {
		tier = pluginTier
	}
	pType := pluginType

	m := &registryManifest{
		Name:        p.Name,
		Version:     p.Version,
		Author:      p.Author,
		Description: p.Description,
		License:     p.License,
		Repository:  p.Repository,
		Type:        pType,
		Tier:        tier,
		Keywords:    p.Tags,
	}
	if hasCapabilities(p) {
		m.Capabilities = &registryCapabilities{
			ModuleTypes:      p.ModuleTypes,
			StepTypes:        p.StepTypes,
			TriggerTypes:     p.TriggerTypes,
			WorkflowHandlers: p.WorkflowTypes,
			WiringHooks:      p.WiringHooks,
		}
	}
	return m, nil
}

func hasCapabilities(p pluginJSONSchema) bool {
	return len(p.ModuleTypes) > 0 || len(p.StepTypes) > 0 ||
		len(p.TriggerTypes) > 0 || len(p.WorkflowTypes) > 0 || len(p.WiringHooks) > 0
}

// detectedMeta holds metadata found by scanning Go source files.
type detectedMeta struct {
	name        string
	version     string
	description string
	modulePath  string
	license     string
	isPlugin    bool
}

// autoDetectManifest scans Go source files to build a registry manifest.
func autoDetectManifest(dir, pluginType, pluginTier string) (*registryManifest, error) {
	meta, err := scanGoSource(dir)
	if err != nil {
		return nil, fmt.Errorf("auto-detect failed: %w", err)
	}
	if !meta.isPlugin {
		return nil, fmt.Errorf("no manifest.json or plugin.json found, and no EngineManifest() function detected in Go source\n" +
			"tip: run 'wfctl plugin init' to scaffold a plugin, or create manifest.json manually")
	}

	// Derive a plugin name from the Go module path if not found in source
	name := meta.name
	if name == "" && meta.modulePath != "" {
		parts := strings.Split(meta.modulePath, "/")
		name = parts[len(parts)-1]
	}

	m := &registryManifest{
		Name:        name,
		Version:     meta.version,
		Description: meta.description,
		Source:      meta.modulePath,
		License:     meta.license,
		Type:        pluginType,
		Tier:        pluginTier,
	}
	return m, nil
}

// scanGoSource parses Go source files in dir for plugin metadata.
func scanGoSource(dir string) (detectedMeta, error) {
	meta := detectedMeta{}

	// Read go.mod for module path
	goMod := filepath.Join(dir, "go.mod")
	if data, err := os.ReadFile(goMod); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				meta.modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}

	// Read LICENSE file
	for _, licenseFile := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt"} {
		lp := filepath.Join(dir, licenseFile)
		if data, err := os.ReadFile(lp); err == nil {
			meta.license = detectLicenseType(string(data))
			break
		}
	}

	// Parse Go source files looking for EngineManifest() function.
	// We iterate files manually to avoid the deprecated parser.ParseDir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return meta, nil //nolint:nilerr // non-fatal: directory unreadable
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil || f == nil {
			continue // skip files that don't parse
		}
		if f.Name == nil || f.Name.Name != "main" {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			if fn.Name.Name == "EngineManifest" {
				meta.isPlugin = true
				extractManifestFromAST(fn, &meta)
			}
		}
	}

	return meta, nil
}

// extractManifestFromAST tries to extract name/version/description from an EngineManifest() function.
func extractManifestFromAST(fn *ast.FuncDecl, meta *detectedMeta) {
	if fn.Body == nil {
		return
	}
	// Walk the function body looking for composite literals that look like manifest structs
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			val, ok := kv.Value.(*ast.BasicLit)
			if !ok {
				continue
			}
			strVal := strings.Trim(val.Value, `"`)
			switch key.Name {
			case "Name":
				if meta.name == "" {
					meta.name = strVal
				}
			case "Version":
				if meta.version == "" {
					meta.version = strVal
				}
			case "Description":
				if meta.description == "" {
					meta.description = strVal
				}
			}
		}
		return true
	})
}

// detectLicenseType returns a best-guess SPDX identifier from license file content.
func detectLicenseType(content string) string {
	c := strings.ToLower(content)
	switch {
	case strings.Contains(c, "mit license") || strings.Contains(c, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(c, "apache license, version 2.0") || strings.Contains(c, "apache-2.0"):
		return "Apache-2.0"
	case strings.Contains(c, "gnu general public license") && strings.Contains(c, "version 3"):
		return "GPL-3.0"
	case strings.Contains(c, "gnu general public license") && strings.Contains(c, "version 2"):
		return "GPL-2.0"
	case strings.Contains(c, "bsd 2-clause") || strings.Contains(c, `redistribution and use in source and binary forms`):
		return "BSD-2-Clause"
	case strings.Contains(c, "bsd 3-clause"):
		return "BSD-3-Clause"
	case strings.Contains(c, "mozilla public license"):
		return "MPL-2.0"
	case strings.Contains(c, "isc license"):
		return "ISC"
	default:
		return ""
	}
}

// validateRegistryManifest checks required fields and enum values against the registry schema.
func validateRegistryManifest(m *registryManifest) error {
	var errs []string

	if m.Name == "" {
		errs = append(errs, "name is required")
	}
	if m.Version == "" {
		errs = append(errs, "version is required")
	} else if !semverRe.MatchString(m.Version) {
		errs = append(errs, fmt.Sprintf("version %q must be semantic version (e.g. 1.0.0)", m.Version))
	}
	if m.Author == "" {
		errs = append(errs, "author is required")
	}
	if m.Description == "" {
		errs = append(errs, "description is required")
	}
	if m.Type == "" {
		errs = append(errs, "type is required (builtin, external, ui)")
	} else if !validTypes[m.Type] {
		errs = append(errs, fmt.Sprintf("type %q must be one of: builtin, external, ui", m.Type))
	}
	if m.Tier == "" {
		errs = append(errs, "tier is required (core, community, premium)")
	} else if !validTiers[m.Tier] {
		errs = append(errs, fmt.Sprintf("tier %q must be one of: core, community, premium", m.Tier))
	}
	if m.License == "" {
		errs = append(errs, "license is required (SPDX identifier, e.g. MIT, Apache-2.0)")
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// buildPlugin runs `go build -o <name>.plugin ./` in the given directory and returns the SHA256 of the binary.
func buildPlugin(dir, name string) (string, error) {
	binaryName := name + ".plugin"
	binaryPath := filepath.Join(dir, binaryName)

	fmt.Fprintf(os.Stderr, "building plugin binary: %s\n", binaryPath)

	// Use os/exec indirectly through a helper to keep imports clean
	if err := runGoBuild(dir, binaryPath); err != nil {
		return "", err
	}

	f, err := os.Open(binaryPath)
	if err != nil {
		return "", fmt.Errorf("open binary for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute checksum: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func printSubmissionInstructions(name, registry string) {
	fmt.Printf(`
To publish to the registry:
  1. Fork github.com/%s
  2. Add your manifest to plugins/%s/manifest.json
  3. Submit a pull request
`, registry, name)
}

// runGoBuild executes `go build -o <output> ./` in the given directory.
func runGoBuild(dir, output string) error {
	cmd := exec.Command("go", "build", "-o", output, "./") //nolint:gosec // G204: dir and output come from validated user input
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}
