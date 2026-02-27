package lsp

import (
	"fmt"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// Diagnostics analyses a document and returns LSP diagnostics.
func Diagnostics(reg *Registry, doc *Document) []protocol.Diagnostic {
	var diags []protocol.Diagnostic

	if doc.Node == nil {
		return diags
	}

	// Walk the root document node.
	if doc.Node.Kind != yaml.DocumentNode || len(doc.Node.Content) == 0 {
		return diags
	}

	root := doc.Node.Content[0]
	if root.Kind != yaml.MappingNode {
		return diags
	}

	// Collect module names for dependsOn validation.
	moduleNames := collectModuleNames(root)

	// Check for unclosed template expressions.
	diags = append(diags, checkUnclosedTemplates(doc.Content)...)

	// Validate each top-level section.
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]

		switch keyNode.Value {
		case "modules":
			diags = append(diags, validateModules(reg, valNode, moduleNames)...)
		case "triggers":
			diags = append(diags, validateTriggers(reg, valNode)...)
		case "workflows":
			diags = append(diags, validateWorkflows(reg, valNode)...)
		}
	}

	return diags
}

// collectModuleNames returns a set of module names defined in the modules section.
func collectModuleNames(root *yaml.Node) map[string]bool {
	names := make(map[string]bool)
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "modules" {
			continue
		}
		seq := root.Content[i+1]
		if seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				if item.Content[j].Value == "name" {
					names[item.Content[j+1].Value] = true
				}
			}
		}
	}
	return names
}

// validateModules validates the modules sequence.
func validateModules(reg *Registry, node *yaml.Node, moduleNames map[string]bool) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	if node.Kind != yaml.SequenceNode {
		return diags
	}

	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}

		var modType, modName string
		var typeNode *yaml.Node
		var dependsOnNode *yaml.Node

		for j := 0; j+1 < len(item.Content); j += 2 {
			k := item.Content[j]
			v := item.Content[j+1]
			switch k.Value {
			case "type":
				modType = v.Value
				typeNode = v
			case "name":
				modName = v.Value
			case "dependsOn":
				dependsOnNode = v
			}
		}

		// Validate module type.
		if modType != "" && typeNode != nil {
			if _, ok := reg.ModuleTypes[modType]; !ok {
				sev := protocol.DiagnosticSeverityError
				diags = append(diags, protocol.Diagnostic{
					Range:    nodeRange(typeNode),
					Severity: &sev,
					Message:  fmt.Sprintf("unknown module type %q", modType),
					Source:   strPtr("workflow-lsp"),
				})
			}
		}

		// Validate config keys if we know the module type.
		if modType != "" {
			for j := 0; j+1 < len(item.Content); j += 2 {
				k := item.Content[j]
				if k.Value == "config" {
					configNode := item.Content[j+1]
					diags = append(diags, validateModuleConfig(reg, modType, configNode)...)
				}
			}
		}

		// Validate dependsOn references.
		if dependsOnNode != nil && dependsOnNode.Kind == yaml.SequenceNode {
			for _, dep := range dependsOnNode.Content {
				if dep.Value != "" && !moduleNames[dep.Value] && dep.Value != modName {
					sev := protocol.DiagnosticSeverityWarning
					diags = append(diags, protocol.Diagnostic{
						Range:    nodeRange(dep),
						Severity: &sev,
						Message:  fmt.Sprintf("dependsOn references unknown module %q", dep.Value),
						Source:   strPtr("workflow-lsp"),
					})
				}
			}
		}
	}
	return diags
}

// validateModuleConfig checks that config keys are recognized for the module type.
func validateModuleConfig(reg *Registry, moduleType string, configNode *yaml.Node) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	info, ok := reg.ModuleTypes[moduleType]
	if !ok || len(info.ConfigKeys) == 0 || configNode.Kind != yaml.MappingNode {
		return diags
	}

	knownKeys := make(map[string]bool, len(info.ConfigKeys))
	for _, k := range info.ConfigKeys {
		knownKeys[k] = true
	}

	for i := 0; i+1 < len(configNode.Content); i += 2 {
		k := configNode.Content[i]
		if k.Value != "" && !knownKeys[k.Value] {
			sev := protocol.DiagnosticSeverityWarning
			diags = append(diags, protocol.Diagnostic{
				Range:    nodeRange(k),
				Severity: &sev,
				Message:  fmt.Sprintf("unknown config key %q for module type %q", k.Value, moduleType),
				Source:   strPtr("workflow-lsp"),
			})
		}
	}
	return diags
}

// validateTriggers checks trigger types.
func validateTriggers(reg *Registry, node *yaml.Node) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	if node.Kind != yaml.MappingNode {
		return diags
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if _, ok := reg.TriggerTypes[k.Value]; !ok {
			sev := protocol.DiagnosticSeverityError
			diags = append(diags, protocol.Diagnostic{
				Range:    nodeRange(k),
				Severity: &sev,
				Message:  fmt.Sprintf("unknown trigger type %q", k.Value),
				Source:   strPtr("workflow-lsp"),
			})
		}
	}
	return diags
}

// validateWorkflows checks workflow types.
func validateWorkflows(reg *Registry, node *yaml.Node) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	if node.Kind != yaml.MappingNode {
		return diags
	}
	knownTypes := make(map[string]bool, len(reg.WorkflowTypes))
	for _, t := range reg.WorkflowTypes {
		knownTypes[t] = true
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if !knownTypes[k.Value] {
			sev := protocol.DiagnosticSeverityWarning
			diags = append(diags, protocol.Diagnostic{
				Range:    nodeRange(k),
				Severity: &sev,
				Message:  fmt.Sprintf("unknown workflow type %q", k.Value),
				Source:   strPtr("workflow-lsp"),
			})
		}
	}
	return diags
}

// checkUnclosedTemplates scans for unclosed {{ template expressions.
func checkUnclosedTemplates(content string) []protocol.Diagnostic {
	var diags []protocol.Diagnostic
	lines := strings.Split(content, "\n")
	for lineIdx, line := range lines {
		rest := line
		col := 0
		for {
			openIdx := strings.Index(rest, "{{")
			if openIdx < 0 {
				break
			}
			closeIdx := strings.Index(rest[openIdx:], "}}")
			if closeIdx < 0 {
				// Unclosed template.
				startCol := col + openIdx
				sev := protocol.DiagnosticSeverityWarning
				diags = append(diags, protocol.Diagnostic{
					Range: protocol.Range{
						Start: protocol.Position{Line: uint32(lineIdx), Character: uint32(startCol)}, //nolint:gosec // G115: line/col indexes are non-negative
						End:   protocol.Position{Line: uint32(lineIdx), Character: uint32(len(line))}, //nolint:gosec // G115: line/col indexes are non-negative
					},
					Severity: &sev,
					Message:  "unclosed template expression {{",
					Source:   strPtr("workflow-lsp"),
				})
				break
			}
			col += openIdx + closeIdx + 2
			rest = rest[openIdx+closeIdx+2:]
		}
	}
	return diags
}

// nodeRange converts a yaml.Node position to an LSP Range.
// yaml.Node lines are 1-based; LSP positions are 0-based.
func nodeRange(n *yaml.Node) protocol.Range {
	line := uint32(0)
	col := uint32(0)
	if n.Line > 0 {
		line = uint32(n.Line - 1) //nolint:gosec // G115: yaml line numbers are positive
	}
	if n.Column > 0 {
		col = uint32(n.Column - 1) //nolint:gosec // G115: yaml column numbers are positive
	}
	endCol := col + uint32(len(n.Value)) //nolint:gosec // G115: string length is non-negative
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: col},
		End:   protocol.Position{Line: line, Character: endCol},
	}
}

func strPtr(s string) *string { return &s }
