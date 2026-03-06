package lsp

import (
	"sort"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

type topLevelKeyEntry struct {
	key  string
	doc  string
	kind protocol.CompletionItemKind
}

// getTopLevelKeys returns completion items for top-level YAML keys.
func getTopLevelKeys() []protocol.CompletionItem {
	entries := []topLevelKeyEntry{
		{"modules", "List of module definitions to instantiate", protocol.CompletionItemKindKeyword},
		{"workflows", "Workflow handler configurations", protocol.CompletionItemKindKeyword},
		{"triggers", "Trigger configurations", protocol.CompletionItemKindKeyword},
		{"pipelines", "Named pipeline definitions", protocol.CompletionItemKindKeyword},
		{"imports", "List of external config files to import", protocol.CompletionItemKindKeyword},
		{"requires", "Plugin and version dependency declarations", protocol.CompletionItemKindKeyword},
		{"platform", "Platform-level configuration", protocol.CompletionItemKindKeyword},
	}

	items := make([]protocol.CompletionItem, 0, len(entries))
	for _, e := range entries {
		kind := e.kind
		doc := e.doc
		label := e.key
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	return items
}

// getModuleTypeCompletions returns completion items for module type values.
func getModuleTypeCompletions(reg *Registry) []protocol.CompletionItem {
	types := make([]string, 0, len(reg.ModuleTypes))
	for t := range reg.ModuleTypes {
		types = append(types, t)
	}
	sort.Strings(types)

	kind := protocol.CompletionItemKindClass
	items := make([]protocol.CompletionItem, 0, len(types))
	for _, t := range types {
		info := reg.ModuleTypes[t]
		label := t
		doc := info.Description
		cat := info.Category
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
			Detail:        &cat,
		})
	}
	return items
}

// getStepTypeCompletions returns completion items for step type values.
func getStepTypeCompletions(reg *Registry) []protocol.CompletionItem {
	types := make([]string, 0, len(reg.StepTypes))
	for t := range reg.StepTypes {
		types = append(types, t)
	}
	sort.Strings(types)

	kind := protocol.CompletionItemKindFunction
	items := make([]protocol.CompletionItem, 0, len(types))
	for _, t := range types {
		info := reg.StepTypes[t]
		label := t
		doc := info.Description
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	return items
}

// getTriggerTypeCompletions returns completion items for trigger type values.
func getTriggerTypeCompletions(reg *Registry) []protocol.CompletionItem {
	types := make([]string, 0, len(reg.TriggerTypes))
	for t := range reg.TriggerTypes {
		types = append(types, t)
	}
	sort.Strings(types)

	kind := protocol.CompletionItemKindEvent
	items := make([]protocol.CompletionItem, 0, len(types))
	for _, t := range types {
		info := reg.TriggerTypes[t]
		label := t
		doc := info.Description
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	return items
}

// getModuleConfigKeyCompletions returns completions for config keys of a given module type.
func getModuleConfigKeyCompletions(reg *Registry, moduleType string) []protocol.CompletionItem {
	info, ok := reg.ModuleTypes[moduleType]
	if !ok {
		return nil
	}

	kind := protocol.CompletionItemKindProperty
	items := make([]protocol.CompletionItem, 0, len(info.ConfigKeys))
	for _, k := range info.ConfigKeys {
		key := k
		items = append(items, protocol.CompletionItem{
			Label: key,
			Kind:  &kind,
		})
	}
	return items
}

// getStepConfigKeyCompletions returns completions for config keys of a given step type.
func getStepConfigKeyCompletions(reg *Registry, stepType string) []protocol.CompletionItem {
	info, ok := reg.StepTypes[stepType]
	if !ok {
		return nil
	}

	kind := protocol.CompletionItemKindProperty
	items := make([]protocol.CompletionItem, 0, len(info.ConfigDefs))

	// Prefer rich config defs with descriptions.
	for _, cf := range info.ConfigDefs {
		detail := string(cf.Type)
		if cf.Required {
			detail += " (required)"
		}
		doc := cf.Description
		items = append(items, protocol.CompletionItem{
			Label:         cf.Key,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: doc,
		})
	}

	// Fallback to plain config key names if no ConfigDefs.
	if len(items) == 0 {
		for _, k := range info.ConfigKeys {
			key := k
			items = append(items, protocol.CompletionItem{
				Label: key,
				Kind:  &kind,
			})
		}
	}
	return items
}

// getTemplateFunctionCompletions returns completion items for template functions.
func getTemplateFunctionCompletions() []protocol.CompletionItem {
	fns := templateFunctions()
	kind := protocol.CompletionItemKindFunction
	items := make([]protocol.CompletionItem, 0, len(fns))
	for _, fn := range fns {
		name := fn
		items = append(items, protocol.CompletionItem{
			Label:         name,
			Kind:          &kind,
			Documentation: "Template function: " + name,
		})
	}
	return items
}

// getModuleNamesFromContent returns module names declared in the document content
// for dependsOn completion.
func getModuleNamesFromContent(content string) []protocol.CompletionItem {
	kind := protocol.CompletionItemKindValue
	items := []protocol.CompletionItem{}
	for _, line := range splitLines(content) {
		trimmed := trimSpace(line)
		if hasPrefix(trimmed, "name:") {
			val := trimSpace(trimPrefix(trimmed, "name:"))
			if val != "" {
				name := val
				items = append(items, protocol.CompletionItem{
					Label: name,
					Kind:  &kind,
				})
			}
		}
	}
	return items
}

// Completions returns completion items for the given document and position context.
func Completions(reg *Registry, doc *Document, ctx PositionContext) []protocol.CompletionItem {
	if ctx.InTemplate {
		return getTemplateCompletions(doc, ctx)
	}

	switch ctx.Section {
	case SectionTopLevel:
		return getTopLevelKeys()
	case SectionModules:
		if ctx.DependsOn {
			return getModuleNamesFromContent(doc.Content)
		}
		switch ctx.FieldName {
		case "type":
			return getModuleTypeCompletions(reg)
		case "dependsOn":
			return getModuleNamesFromContent(doc.Content)
		}
		if ctx.ModuleType != "" {
			return getModuleConfigKeyCompletions(reg, ctx.ModuleType)
		}
		// Module-level field keys.
		return moduleItemKeys()
	case SectionPipeline:
		if ctx.FieldName == "type" {
			// Could be trigger or step type depending on nesting.
			items := getStepTypeCompletions(reg)
			items = append(items, getTriggerTypeCompletions(reg)...)
			return items
		}
		if ctx.StepType != "" {
			return getStepConfigKeyCompletions(reg, ctx.StepType)
		}
	case SectionTriggers:
		return getTriggerTypeCompletions(reg)
	case SectionWorkflow:
		return getWorkflowTypeCompletions(reg)
	}

	return nil
}

// moduleItemKeys returns field key completions for a modules[] item.
func moduleItemKeys() []protocol.CompletionItem {
	kind := protocol.CompletionItemKindProperty
	keys := []string{"name", "type", "config", "dependsOn", "branches"}
	items := make([]protocol.CompletionItem, 0, len(keys))
	for _, k := range keys {
		key := k
		items = append(items, protocol.CompletionItem{
			Label: key,
			Kind:  &kind,
		})
	}
	return items
}

// getWorkflowTypeCompletions returns completions for workflow types.
func getWorkflowTypeCompletions(reg *Registry) []protocol.CompletionItem {
	kind := protocol.CompletionItemKindModule
	items := make([]protocol.CompletionItem, 0, len(reg.WorkflowTypes))
	for _, t := range reg.WorkflowTypes {
		wt := t
		items = append(items, protocol.CompletionItem{
			Label: wt,
			Kind:  &kind,
		})
	}
	return items
}

// getTemplateCompletions returns context-aware completions for template expressions.
func getTemplateCompletions(doc *Document, ctx PositionContext) []protocol.CompletionItem {
	tp := ctx.TemplatePath
	if tp == nil {
		return getTemplateFunctionCompletions()
	}

	switch tp.Namespace {
	case "":
		// Top-level: suggest namespace prefixes + template functions.
		items := namespaceCompletions()
		items = append(items, getTemplateFunctionCompletions()...)
		return items

	case "steps":
		pipCtx := BuildPipelineContext(doc.Content, ctx.PipelineName, ctx.CurrentStepName, "", "", "")
		if tp.StepName == "" {
			// Suggest step names, filtered by FieldPrefix.
			return filterCompletions(stepNameCompletions(pipCtx), tp.FieldPrefix)
		}
		// Suggest output keys for the named step, filtered by FieldPrefix.
		return filterCompletions(stepOutputKeyCompletions(pipCtx, tp.StepName), tp.FieldPrefix)

	case "trigger":
		pipCtx := BuildPipelineContext(doc.Content, ctx.PipelineName, ctx.CurrentStepName, "", "", "")
		if tp.SubField != "" {
			// Drill into trigger sub-namespace (e.g. .trigger.path_params.)
			return filterCompletions(triggerSubFieldCompletions(pipCtx, tp.SubField), tp.FieldPrefix)
		}
		return filterCompletions(triggerFieldCompletions(pipCtx), tp.FieldPrefix)

	case "body":
		pipCtx := BuildPipelineContext(doc.Content, ctx.PipelineName, ctx.CurrentStepName, "", "", "")
		return filterCompletions(bodyFieldCompletions(pipCtx), tp.FieldPrefix)

	case "meta":
		return filterCompletions(metaFieldCompletions(), tp.FieldPrefix)

	default:
		return getTemplateFunctionCompletions()
	}
}

// namespaceCompletions returns completion items for top-level template namespaces.
func namespaceCompletions() []protocol.CompletionItem {
	kind := protocol.CompletionItemKindKeyword
	entries := []struct{ label, doc string }{
		{".steps", "Access step output by step name"},
		{".trigger", "Access trigger data (path params, query, body)"},
		{".body", "Access parsed request body fields"},
		{".meta", "Access request metadata"},
	}
	items := make([]protocol.CompletionItem, 0, len(entries))
	for _, e := range entries {
		label := e.label
		doc := e.doc
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	return items
}

// stepNameCompletions returns completion items for step names in a pipeline.
func stepNameCompletions(pipCtx *PipelineDataContext) []protocol.CompletionItem {
	if pipCtx == nil {
		return nil
	}
	kind := protocol.CompletionItemKindVariable
	items := make([]protocol.CompletionItem, 0, len(pipCtx.StepOutputs))
	for _, so := range pipCtx.StepOutputs {
		name := so.StepName
		stype := so.StepType
		items = append(items, protocol.CompletionItem{
			Label:         name,
			Kind:          &kind,
			Documentation: "Step output from " + stype,
		})
	}
	return items
}

// stepOutputKeyCompletions returns completion items for a step's output keys.
func stepOutputKeyCompletions(pipCtx *PipelineDataContext, stepName string) []protocol.CompletionItem {
	if pipCtx == nil {
		return nil
	}
	for _, so := range pipCtx.StepOutputs {
		if so.StepName != stepName {
			continue
		}
		kind := protocol.CompletionItemKindField
		keys := make([]string, 0, len(so.Outputs))
		for k := range so.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		items := make([]protocol.CompletionItem, 0, len(keys))
		for _, k := range keys {
			key := k
			fs := so.Outputs[k]
			detail := fs.Type
			items = append(items, protocol.CompletionItem{
				Label:         key,
				Kind:          &kind,
				Detail:        &detail,
				Documentation: fs.Description,
			})
		}
		return items
	}
	return nil
}

// triggerFieldCompletions returns completion items for trigger data fields.
func triggerFieldCompletions(pipCtx *PipelineDataContext) []protocol.CompletionItem {
	kind := protocol.CompletionItemKindField
	// Static trigger subfields.
	entries := []struct{ label, doc string }{
		{"path_params", "URL path parameters"},
		{"query", "Query string parameters"},
		{"body", "Request body fields"},
		{"headers", "Request headers"},
	}
	items := make([]protocol.CompletionItem, 0, len(entries))
	for _, e := range entries {
		label := e.label
		doc := e.doc
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	// Append OpenAPI-derived path/query params if available.
	if pipCtx != nil && pipCtx.Trigger != nil {
		for k := range pipCtx.Trigger.PathParams {
			key := k
			items = append(items, protocol.CompletionItem{
				Label:         key,
				Kind:          &kind,
				Documentation: "Path parameter",
			})
		}
		for k := range pipCtx.Trigger.QueryParams {
			key := k
			items = append(items, protocol.CompletionItem{
				Label:         key,
				Kind:          &kind,
				Documentation: "Query parameter",
			})
		}
	}
	return items
}

// bodyFieldCompletions returns completion items for request body fields.
func bodyFieldCompletions(pipCtx *PipelineDataContext) []protocol.CompletionItem {
	if pipCtx == nil || pipCtx.Trigger == nil || len(pipCtx.Trigger.BodyFields) == 0 {
		return nil
	}
	kind := protocol.CompletionItemKindField
	keys := make([]string, 0, len(pipCtx.Trigger.BodyFields))
	for k := range pipCtx.Trigger.BodyFields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]protocol.CompletionItem, 0, len(keys))
	for _, k := range keys {
		key := k
		fs := pipCtx.Trigger.BodyFields[k]
		detail := fs.Type
		items = append(items, protocol.CompletionItem{
			Label:         key,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: fs.Description,
		})
	}
	return items
}

// filterCompletions filters completion items by a prefix string.
// If prefix is empty, all items are returned.
func filterCompletions(items []protocol.CompletionItem, prefix string) []protocol.CompletionItem {
	if prefix == "" {
		return items
	}
	var filtered []protocol.CompletionItem
	for _, item := range items {
		if len(item.Label) >= len(prefix) && item.Label[:len(prefix)] == prefix {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// triggerSubFieldCompletions returns completions for trigger sub-namespace fields
// (e.g. .trigger.path_params. → list specific path parameter names from OpenAPI).
func triggerSubFieldCompletions(pipCtx *PipelineDataContext, subField string) []protocol.CompletionItem {
	if pipCtx == nil || pipCtx.Trigger == nil {
		return nil
	}
	kind := protocol.CompletionItemKindField
	var source map[string]FieldSchema
	switch subField {
	case "path_params":
		source = pipCtx.Trigger.PathParams
	case "query":
		source = pipCtx.Trigger.QueryParams
	case "body":
		source = pipCtx.Trigger.BodyFields
	default:
		return nil
	}
	if len(source) == 0 {
		return nil
	}
	keys := make([]string, 0, len(source))
	for k := range source {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]protocol.CompletionItem, 0, len(keys))
	for _, k := range keys {
		key := k
		fs := source[k]
		detail := fs.Type
		items = append(items, protocol.CompletionItem{
			Label:         key,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: fs.Description,
		})
	}
	return items
}

// metaFieldCompletions returns completion items for pipeline metadata fields.
func metaFieldCompletions() []protocol.CompletionItem {
	kind := protocol.CompletionItemKindField
	entries := []struct{ label, doc string }{
		{"pipeline_name", "Name of the currently executing pipeline"},
		{"trigger_type", "Type of trigger that started this pipeline (e.g. 'http')"},
		{"timestamp", "ISO 8601 timestamp of when the pipeline was triggered"},
	}
	items := make([]protocol.CompletionItem, 0, len(entries))
	for _, e := range entries {
		label := e.label
		doc := e.doc
		items = append(items, protocol.CompletionItem{
			Label:         label,
			Kind:          &kind,
			Documentation: doc,
		})
	}
	return items
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

func trimSpace(s string) string {
	return trimLeft(trimRight(s))
}

func trimLeft(s string) string {
	for i, c := range s {
		if c != ' ' && c != '\t' {
			return s[i:]
		}
	}
	return ""
}

func trimRight(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\r' && s[i] != '\n' {
			return s[:i+1]
		}
	}
	return ""
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}
