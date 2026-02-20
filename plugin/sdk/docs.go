package sdk

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/plugin"
)

// DocGenerator produces markdown documentation from plugin manifests and contracts.
type DocGenerator struct{}

// NewDocGenerator creates a new DocGenerator.
func NewDocGenerator() *DocGenerator {
	return &DocGenerator{}
}

// GeneratePluginDoc produces a complete markdown document for a plugin.
func (g *DocGenerator) GeneratePluginDoc(manifest *plugin.PluginManifest) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", manifest.Name)
	fmt.Fprintf(&b, "**Version:** %s  \n", manifest.Version)
	fmt.Fprintf(&b, "**Author:** %s  \n", manifest.Author)
	if manifest.License != "" {
		fmt.Fprintf(&b, "**License:** %s  \n", manifest.License)
	}
	if manifest.Repository != "" {
		fmt.Fprintf(&b, "**Repository:** %s  \n", manifest.Repository)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n\n", manifest.Description)

	if len(manifest.Tags) > 0 {
		b.WriteString("**Tags:** ")
		b.WriteString(strings.Join(manifest.Tags, ", "))
		b.WriteString("\n\n")
	}

	if len(manifest.Dependencies) > 0 {
		b.WriteString("## Dependencies\n\n")
		b.WriteString("| Plugin | Constraint |\n")
		b.WriteString("|--------|------------|\n")
		for _, dep := range manifest.Dependencies {
			fmt.Fprintf(&b, "| %s | %s |\n", dep.Name, dep.Constraint)
		}
		b.WriteString("\n")
	}

	if manifest.Contract != nil {
		b.WriteString(g.GenerateContractDoc(manifest.Contract))
	}

	return b.String()
}

// GenerateContractDoc produces a markdown section documenting a field contract.
func (g *DocGenerator) GenerateContractDoc(contract *dynamic.FieldContract) string {
	if contract == nil {
		return ""
	}
	var b strings.Builder

	if len(contract.RequiredInputs) > 0 {
		b.WriteString("## Required Inputs\n\n")
		b.WriteString(fieldSpecTable(contract.RequiredInputs))
		b.WriteString("\n")
	}

	if len(contract.OptionalInputs) > 0 {
		b.WriteString("## Optional Inputs\n\n")
		b.WriteString(fieldSpecTable(contract.OptionalInputs))
		b.WriteString("\n")
	}

	if len(contract.Outputs) > 0 {
		b.WriteString("## Outputs\n\n")
		b.WriteString(fieldSpecTable(contract.Outputs))
		b.WriteString("\n")
	}

	return b.String()
}

func fieldSpecTable(specs map[string]dynamic.FieldSpec) string {
	var b strings.Builder
	b.WriteString("| Field | Type | Description | Default |\n")
	b.WriteString("|-------|------|-------------|----------|\n")

	// Sort for deterministic output
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := specs[name]
		def := ""
		if spec.Default != nil {
			def = fmt.Sprintf("%v", spec.Default)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", name, spec.Type, spec.Description, def)
	}
	return b.String()
}
