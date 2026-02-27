package lsp

import (
	"fmt"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Hover returns markdown hover content for the given position context, or nil
// if there is nothing to show.
func Hover(reg *Registry, _ *Document, ctx PositionContext) *protocol.Hover {
	if ctx.InTemplate {
		return hoverTemplateFunction(ctx.FieldName)
	}

	switch ctx.Section {
	case SectionModules:
		if ctx.FieldName == "type" && ctx.ModuleType != "" {
			return hoverModuleType(reg, ctx.ModuleType)
		}
		if ctx.ModuleType != "" && ctx.FieldName != "" {
			return hoverConfigField(reg, ctx.ModuleType, ctx.FieldName)
		}
		if ctx.ModuleType != "" {
			return hoverModuleType(reg, ctx.ModuleType)
		}
	case SectionTriggers:
		if ctx.FieldName != "" {
			return hoverTriggerType(reg, ctx.FieldName)
		}
	}
	return nil
}

// hoverModuleType generates hover markdown for a module type.
func hoverModuleType(reg *Registry, moduleType string) *protocol.Hover {
	info, ok := reg.ModuleTypes[moduleType]
	if !ok {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("**")
	sb.WriteString(moduleType)
	sb.WriteString("**")
	if info.Label != "" && info.Label != moduleType {
		sb.WriteString(" — ")
		sb.WriteString(info.Label)
	}
	sb.WriteString("\n\n")
	if info.Description != "" {
		sb.WriteString(info.Description)
		sb.WriteString("\n\n")
	}
	if info.Category != "" {
		fmt.Fprintf(&sb, "**Category:** %s\n\n", info.Category)
	}
	if len(info.ConfigKeys) > 0 {
		sb.WriteString("**Config keys:** `")
		sb.WriteString(strings.Join(info.ConfigKeys, "`, `"))
		sb.WriteString("`\n")
	}

	return markdownHover(sb.String())
}

// hoverConfigField generates hover markdown for a module config field.
func hoverConfigField(reg *Registry, moduleType, field string) *protocol.Hover {
	info, ok := reg.ModuleTypes[moduleType]
	if !ok {
		return nil
	}

	for _, k := range info.ConfigKeys {
		if k == field {
			return markdownHover(fmt.Sprintf("**%s** — config key for `%s`", field, moduleType))
		}
	}
	return nil
}

// hoverTriggerType generates hover markdown for a trigger type.
func hoverTriggerType(reg *Registry, triggerType string) *protocol.Hover {
	info, ok := reg.TriggerTypes[triggerType]
	if !ok {
		return nil
	}
	return markdownHover(fmt.Sprintf("**%s** trigger\n\n%s", info.Type, info.Description))
}

// hoverTemplateFunction generates hover for template function names.
func hoverTemplateFunction(name string) *protocol.Hover {
	docs := map[string]string{
		"uuidv4":     "Generates a new UUID v4 string.",
		"uuid":       "Generates a new UUID v4 string (alias for uuidv4).",
		"now":        "Returns the current UTC time. Accepts an optional Go time layout string.",
		"lower":      "Converts a string to lower case.",
		"default":    "Returns the fallback value if the primary value is empty or nil.",
		"trimPrefix": "Removes the given prefix from a string if present.",
		"trimSuffix": "Removes the given suffix from a string if present.",
		"json":       "Marshals a value to a JSON string.",
		"step":       "Accesses step output by step name and optional nested keys.",
		"trigger":    "Accesses trigger data by nested keys.",
	}
	doc, ok := docs[name]
	if !ok {
		return nil
	}
	return markdownHover(fmt.Sprintf("**%s** — %s", name, doc))
}

// markdownHover wraps a markdown string in a Hover response.
func markdownHover(md string) *protocol.Hover {
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: md,
		},
	}
}
