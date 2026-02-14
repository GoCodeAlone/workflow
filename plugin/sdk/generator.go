package sdk

import (
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
}

// Generate creates a new plugin directory with manifest and component skeleton.
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
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write manifest
	manifestPath := filepath.Join(opts.OutputDir, "plugin.json")
	if err := plugin.SaveManifest(manifestPath, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Write component skeleton
	componentPath := filepath.Join(opts.OutputDir, opts.Name+".go")
	source := generateComponentSource(opts)
	if err := os.WriteFile(componentPath, []byte(source), 0644); err != nil {
		return fmt.Errorf("write component: %w", err)
	}

	return nil
}

func generateComponentSource(opts GenerateOptions) string {
	funcName := toCamelCase(opts.Name)
	var b strings.Builder

	b.WriteString("package component\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString(")\n\n")
	b.WriteString(fmt.Sprintf("// Name returns the name of the %s plugin.\n", opts.Name))
	b.WriteString(fmt.Sprintf("func Name() string { return %q }\n\n", opts.Name))
	b.WriteString(fmt.Sprintf("// Init initializes the %s plugin.\n", funcName))
	b.WriteString("func Init(services map[string]interface{}) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("// Start starts the %s plugin.\n", funcName))
	b.WriteString("func Start(ctx context.Context) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("// Stop stops the %s plugin.\n", funcName))
	b.WriteString("func Stop(ctx context.Context) error {\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("// Execute runs the %s plugin logic.\n", funcName))
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
