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
	case SectionPipeline:
		if ctx.FieldName == "type" && ctx.StepType != "" {
			return hoverStepType(reg, ctx.StepType)
		}
		if ctx.StepType != "" && ctx.FieldName != "" {
			return hoverStepConfigField(reg, ctx.StepType, ctx.FieldName)
		}
		if ctx.StepType != "" {
			return hoverStepType(reg, ctx.StepType)
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

// hoverStepType generates hover markdown for a pipeline step type.
func hoverStepType(reg *Registry, stepType string) *protocol.Hover {
	info, ok := reg.StepTypes[stepType]
	if !ok {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("**")
	sb.WriteString(stepType)
	sb.WriteString("**\n\n")
	if info.Description != "" {
		sb.WriteString(info.Description)
		sb.WriteString("\n")
	}
	if len(info.ConfigDefs) > 0 {
		sb.WriteString("\n**Config:**\n")
		for _, cf := range info.ConfigDefs {
			req := ""
			if cf.Required {
				req = " *(required)*"
			}
			fmt.Fprintf(&sb, "- `%s` (%s): %s%s\n", cf.Key, cf.Type, cf.Description, req)
		}
	}
	if len(info.Outputs) > 0 {
		sb.WriteString("\n**Outputs:**\n")
		for _, o := range info.Outputs {
			fmt.Fprintf(&sb, "- `%s` (%s): %s\n", o.Key, o.Type, o.Description)
		}
	}

	return markdownHover(sb.String())
}

// hoverStepConfigField generates hover markdown for a step config field.
func hoverStepConfigField(reg *Registry, stepType, field string) *protocol.Hover {
	info, ok := reg.StepTypes[stepType]
	if !ok {
		return nil
	}

	for _, cf := range info.ConfigDefs {
		if cf.Key == field {
			var sb strings.Builder
			fmt.Fprintf(&sb, "**%s** — config key for `%s`\n\n", field, stepType)
			fmt.Fprintf(&sb, "**Type:** %s\n\n", cf.Type)
			if cf.Description != "" {
				sb.WriteString(cf.Description)
				sb.WriteString("\n")
			}
			if cf.Required {
				sb.WriteString("\n*Required*\n")
			}
			if cf.DefaultValue != nil {
				fmt.Fprintf(&sb, "\n**Default:** `%v`\n", cf.DefaultValue)
			}
			if len(cf.Options) > 0 {
				sb.WriteString("\n**Options:** `")
				sb.WriteString(strings.Join(cf.Options, "`, `"))
				sb.WriteString("`\n")
			}
			return markdownHover(sb.String())
		}
	}

	// Fallback for config keys without rich metadata.
	for _, k := range info.ConfigKeys {
		if k == field {
			return markdownHover(fmt.Sprintf("**%s** — config key for `%s`", field, stepType))
		}
	}
	return nil
}

// hoverTemplateFunction generates hover for template function names.
func hoverTemplateFunction(name string) *protocol.Hover {
	docs := map[string]string{
		"uuidv4":     "Generates a new UUID v4 string.",
		"uuid":       "Generates a new UUID v4 string (alias for uuidv4).",
		"now":        "Returns the current UTC time. Accepts an optional Go time layout string.",
		"lower":      "Converts a string to lower case.",
		"upper":      "Converts a string to upper case.",
		"title":      "Converts a string to title case.",
		"default":    "Returns the fallback value if the primary value is empty or nil.",
		"trimPrefix": "Removes the given prefix from a string if present.",
		"trimSuffix": "Removes the given suffix from a string if present.",
		"json":       "Marshals a value to a JSON string.",
		"step":       "Accesses step output by step name and optional nested keys.",
		"trigger":    "Accesses trigger data by nested keys.",
		"replace":    "Replaces occurrences of a substring. Usage: replace old new str",
		"contains":   "Tests whether a string contains a substring.",
		"hasPrefix":  "Tests whether a string starts with a prefix.",
		"hasSuffix":  "Tests whether a string ends with a suffix.",
		"split":      "Splits a string by a separator into a list.",
		"join":       "Joins a list of strings with a separator.",
		"trimSpace":  "Removes leading and trailing whitespace.",
		"urlEncode":  "URL-encodes a string.",
		"add":        "Adds two numbers.",
		"sub":        "Subtracts the second number from the first.",
		"mul":        "Multiplies two numbers.",
		"div":        "Divides the first number by the second.",
		"toInt":      "Converts a value to an integer.",
		"toFloat":    "Converts a value to a float.",
		"toString":   "Converts a value to a string.",
		"length":     "Returns the length of a string, array, or map.",
		"coalesce":   "Returns the first non-empty value from arguments.",
		"config":     "Reads a value from the config provider by key.",
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
