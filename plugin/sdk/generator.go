package sdk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/plugin"
)

// TemplateGenerator scaffolds new plugin projects with a manifest and component skeleton.
type TemplateGenerator struct{}

// NewTemplateGenerator creates a new TemplateGenerator.
func NewTemplateGenerator() *TemplateGenerator {
	return &TemplateGenerator{}
}

// GenerateOptions configures what gets generated.
type GenerateOptions struct {
	Name         string
	Version      string
	Author       string
	Description  string
	License      string
	OutputDir    string
	WithContract bool
	GoModule     string // e.g. "github.com/MyOrg/workflow-plugin-foo"
}

// Generate creates a new plugin directory with manifest and component skeleton,
// plus a full project structure (cmd/, internal/, CI workflows, Makefile, README).
func (g *TemplateGenerator) Generate(opts GenerateOptions) error {
	if opts.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.Author == "" {
		return fmt.Errorf("author is required")
	}
	if opts.Description == "" {
		opts.Description = "A workflow plugin"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = opts.Name
	}

	// Validate the name
	manifest := &plugin.PluginManifest{
		Name:        opts.Name,
		Version:     opts.Version,
		Author:      opts.Author,
		Description: opts.Description,
		License:     opts.License,
	}
	if opts.WithContract {
		manifest.Contract = dynamic.NewFieldContract()
		manifest.Contract.RequiredInputs["input"] = dynamic.FieldSpec{
			Type:        dynamic.FieldTypeString,
			Description: "Example input field",
		}
		manifest.Contract.Outputs["output"] = dynamic.FieldSpec{
			Type:        dynamic.FieldTypeString,
			Description: "Example output field",
		}
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("generated manifest is invalid: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write manifest (plugin.json at root — required by tests and engine)
	manifestPath := filepath.Join(opts.OutputDir, "plugin.json")
	if err := plugin.SaveManifest(manifestPath, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Write component skeleton (legacy flat file — preserved for test compatibility)
	componentPath := filepath.Join(opts.OutputDir, opts.Name+".go")
	source := generateComponentSource(opts)
	if err := os.WriteFile(componentPath, []byte(source), 0600); err != nil {
		return fmt.Errorf("write component: %w", err)
	}

	// Write full project structure
	if err := generateProjectStructure(opts); err != nil {
		return fmt.Errorf("generate project structure: %w", err)
	}

	return nil
}

// generateProjectStructure writes the full plugin project layout:
// cmd/workflow-plugin-<name>/main.go, internal/provider.go, internal/steps.go,
// go.mod, .goreleaser.yml, .github/workflows/ci.yml, .github/workflows/release.yml,
// Makefile, README.md.
func generateProjectStructure(opts GenerateOptions) error {
	shortName := normalizeSDKPluginName(opts.Name)
	binaryName := "workflow-plugin-" + shortName
	goModule := opts.GoModule
	if goModule == "" {
		goModule = "github.com/" + opts.Author + "/" + binaryName
	}

	// cmd/workflow-plugin-<name>/main.go
	cmdDir := filepath.Join(opts.OutputDir, "cmd", binaryName)
	if err := os.MkdirAll(cmdDir, 0750); err != nil {
		return fmt.Errorf("create cmd dir: %w", err)
	}
	if err := writeFile(filepath.Join(cmdDir, "main.go"), generateMainGo(goModule, shortName), 0600); err != nil {
		return err
	}

	// internal/provider.go and internal/steps.go
	internalDir := filepath.Join(opts.OutputDir, "internal")
	if err := os.MkdirAll(internalDir, 0750); err != nil {
		return fmt.Errorf("create internal dir: %w", err)
	}
	if err := writeFile(filepath.Join(internalDir, "provider.go"), generateProviderGo(opts, shortName), 0600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(internalDir, "steps.go"), generateStepsGo(shortName), 0600); err != nil {
		return err
	}

	// go.mod
	if err := writeFile(filepath.Join(opts.OutputDir, "go.mod"), generateGoMod(goModule), 0600); err != nil {
		return err
	}

	// .goreleaser.yml
	if err := writeFile(filepath.Join(opts.OutputDir, ".goreleaser.yml"), generateGoReleaserYML(binaryName), 0600); err != nil {
		return err
	}

	// .github/workflows/ci.yml and release.yml
	ghWorkflowsDir := filepath.Join(opts.OutputDir, ".github", "workflows")
	if err := os.MkdirAll(ghWorkflowsDir, 0750); err != nil {
		return fmt.Errorf("create .github/workflows dir: %w", err)
	}
	if err := writeFile(filepath.Join(ghWorkflowsDir, "ci.yml"), generateCIYML(), 0600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(ghWorkflowsDir, "release.yml"), generateReleaseYML(binaryName), 0600); err != nil {
		return err
	}

	// Makefile
	if err := writeFile(filepath.Join(opts.OutputDir, "Makefile"), generateMakefile(binaryName), 0600); err != nil {
		return err
	}

	// README.md
	if err := writeFile(filepath.Join(opts.OutputDir, "README.md"), generateREADME(opts, binaryName, goModule), 0644); err != nil {
		return err
	}

	return nil
}

// writeFile writes content to path with the given mode.
func writeFile(path, content string, mode os.FileMode) error {
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// normalizeSDKPluginName strips the "workflow-plugin-" prefix if present.
func normalizeSDKPluginName(name string) string {
	return strings.TrimPrefix(name, "workflow-plugin-")
}

// writePluginJSON writes a plugin.json with the nested capabilities format.
func writePluginJSON(path string, opts GenerateOptions) error {
	shortName := normalizeSDKPluginName(opts.Name)
	license := opts.License
	if license == "" {
		license = "Apache-2.0"
	}
	type capabilities struct {
		ModuleTypes  []string `json:"moduleTypes"`
		StepTypes    []string `json:"stepTypes"`
		TriggerTypes []string `json:"triggerTypes"`
	}
	pj := map[string]interface{}{
		"name":             "workflow-plugin-" + shortName,
		"version":          opts.Version,
		"description":      opts.Description,
		"author":           opts.Author,
		"license":          license,
		"type":             "external",
		"tier":             "community",
		"private":          false,
		"minEngineVersion": "0.3.30",
		"keywords":         []string{},
		"capabilities": capabilities{
			ModuleTypes:  []string{},
			StepTypes:    []string{"step." + shortName + "_example"},
			TriggerTypes: []string{},
		},
	}
	data, err := json.MarshalIndent(pj, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0640) //nolint:gosec // G306: plugin.json is user-owned output
}

func generateMainGo(goModule, shortName string) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\t%q\n", goModule+"/internal")
	b.WriteString("\t\"github.com/GoCodeAlone/workflow/plugin/external/sdk\"\n")
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	fmt.Fprintf(&b, "\tsdk.Serve(internal.New%sProvider())\n", toCamelCase(shortName))
	b.WriteString("}\n")
	return b.String()
}

func generateProviderGo(opts GenerateOptions, shortName string) string {
	typeName := toCamelCase(shortName) + "Provider"
	var b strings.Builder
	fmt.Fprintf(&b, "package internal\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n\n")
	b.WriteString("\t\"github.com/GoCodeAlone/workflow/plugin/external/sdk\"\n")
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// %s implements sdk.PluginProvider and sdk.StepProvider.\n", typeName)
	fmt.Fprintf(&b, "type %s struct{}\n\n", typeName)
	fmt.Fprintf(&b, "// New%s creates a new %s.\n", typeName, typeName)
	fmt.Fprintf(&b, "func New%s() *%s {\n", typeName, typeName)
	fmt.Fprintf(&b, "\treturn &%s{}\n", typeName)
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Manifest implements sdk.PluginProvider.\n")
	fmt.Fprintf(&b, "func (p *%s) Manifest() sdk.PluginManifest {\n", typeName)
	b.WriteString("\treturn sdk.PluginManifest{\n")
	fmt.Fprintf(&b, "\t\tName:        %q,\n", "workflow-plugin-"+shortName)
	fmt.Fprintf(&b, "\t\tVersion:     %q,\n", opts.Version)
	fmt.Fprintf(&b, "\t\tAuthor:      %q,\n", opts.Author)
	fmt.Fprintf(&b, "\t\tDescription: %q,\n", opts.Description)
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// StepTypes implements sdk.StepProvider.\n")
	fmt.Fprintf(&b, "func (p *%s) StepTypes() []string {\n", typeName)
	fmt.Fprintf(&b, "\treturn []string{%q}\n", "step."+shortName+"_example")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// CreateStep implements sdk.StepProvider.\n")
	fmt.Fprintf(&b, "func (p *%s) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {\n", typeName)
	b.WriteString("\tswitch typeName {\n")
	fmt.Fprintf(&b, "\tcase %q:\n", "step."+shortName+"_example")
	fmt.Fprintf(&b, "\t\treturn &%sExampleStep{config: config}, nil\n", toCamelCase(shortName))
	b.WriteString("\t}\n")
	b.WriteString("\treturn nil, fmt.Errorf(\"unknown step type: %s\", typeName)\n")
	b.WriteString("}\n")
	return b.String()
}

func generateStepsGo(shortName string) string {
	stepType := "step." + shortName + "_example"
	funcName := toCamelCase(shortName) + "ExampleStep"
	var b strings.Builder
	b.WriteString("package internal\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n\n")
	b.WriteString("\t\"github.com/GoCodeAlone/workflow/plugin/external/sdk\"\n")
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// %s implements the %s step (sdk.StepInstance).\n", funcName, stepType)
	fmt.Fprintf(&b, "type %s struct {\n", funcName)
	b.WriteString("\tconfig map[string]any\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Execute implements sdk.StepInstance.\n")
	fmt.Fprintf(&b, "func (s *%s) Execute(\n", funcName)
	b.WriteString("\tctx context.Context,\n")
	b.WriteString("\ttriggerData map[string]any,\n")
	b.WriteString("\tstepOutputs map[string]map[string]any,\n")
	b.WriteString("\tcurrent map[string]any,\n")
	b.WriteString("\tmetadata map[string]any,\n")
	b.WriteString("\tconfig map[string]any,\n")
	fmt.Fprintf(&b, ") (*sdk.StepResult, error) {\n")
	b.WriteString("\treturn &sdk.StepResult{\n")
	b.WriteString("\t\tOutput: map[string]any{\n")
	b.WriteString("\t\t\t\"status\": \"ok\",\n")
	b.WriteString("\t\t},\n")
	b.WriteString("\t}, nil\n")
	b.WriteString("}\n")
	return b.String()
}

func generateGoMod(goModule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\n", goModule)
	b.WriteString("go 1.22\n\n")
	b.WriteString("require (\n")
	b.WriteString("\tgithub.com/GoCodeAlone/workflow v0.3.30\n")
	b.WriteString(")\n")
	return b.String()
}

func generateGoReleaserYML(binaryName string) string {
	var b strings.Builder
	b.WriteString("version: 2\n\n")
	b.WriteString("builds:\n")
	b.WriteString("  - id: plugin\n")
	fmt.Fprintf(&b, "    binary: %s\n", binaryName)
	fmt.Fprintf(&b, "    main: ./cmd/%s\n", binaryName)
	b.WriteString("    env:\n")
	b.WriteString("      - CGO_ENABLED=0\n")
	b.WriteString("    goos:\n")
	b.WriteString("      - linux\n")
	b.WriteString("      - darwin\n")
	b.WriteString("    goarch:\n")
	b.WriteString("      - amd64\n")
	b.WriteString("      - arm64\n\n")
	b.WriteString("archives:\n")
	b.WriteString("  - id: default\n")
	b.WriteString("    format: tar.gz\n")
	b.WriteString("    files:\n")
	b.WriteString("      - plugin.json\n\n")
	b.WriteString("checksum:\n")
	b.WriteString("  name_template: checksums.txt\n\n")
	b.WriteString("release:\n")
	b.WriteString("  draft: false\n")
	return b.String()
}

func generateCIYML() string {
	return `name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Test
        run: go test ./...
      - name: Vet
        run: go vet ./...
`
}

func generateReleaseYML(binaryName string) string {
	return `name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  notify-registry:
    if: startsWith(github.ref, 'refs/tags/v')
    needs: [release]
    runs-on: ubuntu-latest
    steps:
      - name: Notify workflow-registry
        if: env.GH_TOKEN != ''
        uses: peter-evans/repository-dispatch@v3
        with:
          token: ${{ secrets.REGISTRY_PAT }}
          repository: GoCodeAlone/workflow-registry
          event-type: plugin-release
          client-payload: >-
            {"plugin": "${{ github.repository }}", "tag": "${{ github.ref_name }}"}
        env:
          GH_TOKEN: ${{ secrets.REGISTRY_PAT }}
        continue-on-error: true
`
}

func generateMakefile(binaryName string) string {
	return fmt.Sprintf(`.PHONY: build test install-local clean

build:
	go build -o %s ./cmd/%s

test:
	go test ./...

install-local: build
	wfctl plugin install --local .

clean:
	rm -f %s
`, binaryName, binaryName, binaryName)
}

func generateREADME(opts GenerateOptions, binaryName, goModule string) string {
	shortName := normalizeSDKPluginName(opts.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", binaryName)
	fmt.Fprintf(&b, "%s\n\n", opts.Description)
	b.WriteString("## Installation\n\n")
	b.WriteString("```sh\n")
	fmt.Fprintf(&b, "wfctl plugin install %s\n", binaryName)
	b.WriteString("```\n\n")
	b.WriteString("## Development\n\n")
	b.WriteString("```sh\n")
	b.WriteString("# Build\n")
	b.WriteString("make build\n\n")
	b.WriteString("# Test\n")
	b.WriteString("make test\n\n")
	b.WriteString("# Install locally\n")
	b.WriteString("make install-local\n")
	b.WriteString("```\n\n")
	b.WriteString("## Step Types\n\n")
	fmt.Fprintf(&b, "- `step.%s_example` — Example step\n\n", shortName)
	b.WriteString("## Module\n\n")
	fmt.Fprintf(&b, "Go module: `%s`\n", goModule)
	return b.String()
}

func generateComponentSource(opts GenerateOptions) string {
	funcName := toCamelCase(opts.Name)
	var b strings.Builder

	b.WriteString("package component\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// Name returns the name of the %s plugin.\n", opts.Name)
	fmt.Fprintf(&b, "func Name() string { return %q }\n\n", opts.Name)
	fmt.Fprintf(&b, "// Init initializes the %s plugin.\n", funcName)
	b.WriteString("func Init(services map[string]interface{}) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Start starts the %s plugin.\n", funcName)
	b.WriteString("func Start(ctx context.Context) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Stop stops the %s plugin.\n", funcName)
	b.WriteString("func Stop(ctx context.Context) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Execute runs the %s plugin logic.\n", funcName)
	b.WriteString("func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {\n")
	b.WriteString("\tresult := map[string]interface{}{\n")
	b.WriteString("\t\t\"status\": \"ok\",\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn result, nil\n")
	b.WriteString("}\n")

	if opts.WithContract {
		b.WriteString("\n// Contract declares the input/output contract for this plugin.\n")
		b.WriteString("func Contract() map[string]interface{} {\n")
		b.WriteString("\treturn map[string]interface{}{\n")
		b.WriteString("\t\t\"required_inputs\": map[string]interface{}{\n")
		b.WriteString("\t\t\t\"input\": map[string]interface{}{\n")
		b.WriteString("\t\t\t\t\"type\":        \"string\",\n")
		b.WriteString("\t\t\t\t\"description\": \"Example input field\",\n")
		b.WriteString("\t\t\t},\n")
		b.WriteString("\t\t},\n")
		b.WriteString("\t\t\"outputs\": map[string]interface{}{\n")
		b.WriteString("\t\t\t\"output\": map[string]interface{}{\n")
		b.WriteString("\t\t\t\t\"type\":        \"string\",\n")
		b.WriteString("\t\t\t\t\"description\": \"Example output field\",\n")
		b.WriteString("\t\t\t},\n")
		b.WriteString("\t\t},\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n")
	}

	return b.String()
}

// toCamelCase converts a hyphenated name like "my-plugin" to "MyPlugin".
func toCamelCase(s string) string {
	parts := strings.Split(s, "-")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}
