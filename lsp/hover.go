package lsp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/schema"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Hover returns markdown hover content for the given position context, or nil
// if there is nothing to show.
func Hover(reg *Registry, doc *Document, ctx PositionContext) *protocol.Hover {
	if ctx.InTemplate {
		return hoverTemplateExpr(reg, doc, ctx)
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

// hoverTemplateExpr provides hover documentation for template expressions.
func hoverTemplateExpr(reg *Registry, doc *Document, ctx PositionContext) *protocol.Hover {
	tp := ctx.TemplatePath
	if tp == nil {
		return hoverTemplateFunction(ctx.FieldName)
	}

	// Non-dot expression with no namespace — check if it's a function name.
	if tp.Namespace == "" && tp.Raw != "" && tp.Raw != "." {
		return hoverTemplateFunction(tp.Raw)
	}

	switch tp.Namespace {
	case "steps":
		return hoverTemplateStepOutput(reg, doc, ctx, tp)
	case "trigger":
		return hoverTemplateTrigger(ctx, tp)
	case "body":
		return hoverTemplateBody(tp)
	case "meta":
		return hoverTemplateMeta(tp)
	case "":
		return hoverTemplateNamespaces()
	}
	return nil
}

// hoverTemplateStepOutput shows docs for .steps.stepName.field.
func hoverTemplateStepOutput(reg *Registry, doc *Document, ctx PositionContext, tp *TemplateExprPath) *protocol.Hover {
	if tp.StepName == "" {
		return markdownHover("**Steps namespace**\n\nAccess step outputs via `.steps.<step-name>.<field>`")
	}

	if doc == nil {
		return markdownHover(fmt.Sprintf("**Step:** `%s`\n\nStep output data.", tp.StepName))
	}

	pctx := BuildPipelineContext(reg, doc, ctx.Line)

	stepCtx := pctx.Steps[tp.StepName]
	if stepCtx == nil {
		return markdownHover(fmt.Sprintf("**Step:** `%s`\n\nNo output info available (step may not precede current position).", tp.StepName))
	}

	// Show a specific field if one is targeted.
	fieldName := tp.FieldPrefix
	if fieldName == "" {
		fieldName = tp.SubField
	}
	if fieldName != "" {
		for _, f := range stepCtx.Fields {
			if f.Key == fieldName {
				var sb strings.Builder
				fmt.Fprintf(&sb, "**`.steps.%s.%s`** (`%s`)\n\n", tp.StepName, fieldName, f.Type)
				if f.Description != "" {
					sb.WriteString(f.Description)
					sb.WriteString("\n")
				}
				fmt.Fprintf(&sb, "\n*Step type:* `%s`", stepCtx.StepType)
				return markdownHover(sb.String())
			}
		}
	}

	// Show all outputs for the step.
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Step outputs:** `%s` (`%s`)\n\n", tp.StepName, stepCtx.StepType)
	if len(stepCtx.Fields) == 0 {
		sb.WriteString("No outputs defined.\n")
	} else {
		sb.WriteString("**Available fields:**\n\n")
		fields := make([]schema.InferredOutput, len(stepCtx.Fields))
		copy(fields, stepCtx.Fields)
		sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
		for _, f := range fields {
			if f.Description != "" {
				fmt.Fprintf(&sb, "- `%s` (%s): %s\n", f.Key, f.Type, f.Description)
			} else {
				fmt.Fprintf(&sb, "- `%s` (%s)\n", f.Key, f.Type)
			}
		}
	}
	return markdownHover(sb.String())
}

// hoverTemplateTrigger shows docs for .trigger.* expressions.
func hoverTemplateTrigger(ctx PositionContext, tp *TemplateExprPath) *protocol.Hover {
	var sb strings.Builder
	sb.WriteString("**Trigger context** — `.trigger.*`\n\n")

	// If we have a pipeline context with trigger schema, use it.
	fieldName := tp.FieldPrefix
	if fieldName == "" {
		fieldName = tp.SubField
	}

	if tp.SubField != "" {
		fmt.Fprintf(&sb, "**Sub-namespace:** `.trigger.%s`\n\n", tp.SubField)
		switch tp.SubField {
		case "path_params":
			sb.WriteString("URL path parameters (e.g. `/users/{id}` → `.trigger.path_params.id`)\n")
		case "query":
			sb.WriteString("URL query parameters (e.g. `?page=1` → `.trigger.query.page`)\n")
		case "headers":
			sb.WriteString("HTTP request headers (e.g. `.trigger.headers.Authorization`)\n")
		case "body":
			sb.WriteString("Request body fields (accessible via `.trigger.body.<field>`)\n")
		}
	} else {
		sb.WriteString("**Available sub-namespaces:**\n\n")
		sb.WriteString("- `.trigger.path_params` — URL path parameters\n")
		sb.WriteString("- `.trigger.query` — Query string parameters\n")
		sb.WriteString("- `.trigger.headers` — HTTP headers\n")
		sb.WriteString("- `.trigger.body` — Request body fields\n")
	}

	// Suppress unused parameter warning.
	_ = ctx

	return markdownHover(sb.String())
}

// hoverTemplateBody shows docs for .body.* expressions.
func hoverTemplateBody(tp *TemplateExprPath) *protocol.Hover {
	var sb strings.Builder
	sb.WriteString("**Body context** — `.body.*`\n\n")
	if tp.SubField != "" {
		fmt.Fprintf(&sb, "**Field:** `.body.%s`\n\nNested field within the request body.\n", tp.SubField)
	} else if tp.FieldPrefix != "" {
		fmt.Fprintf(&sb, "**Field:** `.body.%s` — request body field\n", tp.FieldPrefix)
	} else {
		sb.WriteString("Request body data. Access individual fields via `.body.<field-name>`.\n")
	}
	return markdownHover(sb.String())
}

// hoverTemplateMeta shows docs for .meta.* expressions.
func hoverTemplateMeta(tp *TemplateExprPath) *protocol.Hover {
	var sb strings.Builder
	sb.WriteString("**Meta context** — `.meta.*`\n\n")

	fieldName := tp.FieldPrefix
	if fieldName == "" {
		fieldName = tp.SubField
	}

	metaDocs := map[string]string{
		"pipeline_name": "Name of the currently executing pipeline.",
		"trigger_type":  "Type of trigger that started this pipeline (e.g. 'http').",
		"timestamp":     "ISO 8601 timestamp of when the pipeline was triggered.",
	}
	if fieldName != "" {
		if doc, ok := metaDocs[fieldName]; ok {
			fmt.Fprintf(&sb, "**`.meta.%s`**\n\n%s\n", fieldName, doc)
		} else {
			fmt.Fprintf(&sb, "**`.meta.%s`** — pipeline metadata field\n", fieldName)
		}
	} else {
		sb.WriteString("**Available fields:**\n\n")
		sb.WriteString("- `.meta.pipeline_name` — Name of the current pipeline\n")
		sb.WriteString("- `.meta.trigger_type` — Trigger type that started this pipeline\n")
		sb.WriteString("- `.meta.timestamp` — Pipeline start timestamp\n")
	}
	return markdownHover(sb.String())
}

// hoverTemplateNamespaces shows the top-level template namespaces.
func hoverTemplateNamespaces() *protocol.Hover {
	md := "**Template context namespaces**\n\n" +
		"- `.steps.<name>.<field>` — Output fields from a completed pipeline step\n" +
		"- `.trigger.path_params.*` — URL path parameters from the trigger\n" +
		"- `.trigger.query.*` — Query string parameters from the trigger\n" +
		"- `.trigger.headers.*` — HTTP headers from the trigger\n" +
		"- `.trigger.body.*` — Request body fields from the trigger\n" +
		"- `.body.*` — Request body shorthand\n" +
		"- `.meta.*` — Pipeline metadata (name, trigger_type, timestamp)\n"
	return markdownHover(md)
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
