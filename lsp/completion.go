package lsp

import (
	"sort"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

type topLevelKeyEntry struct {
	key       string
	doc       string
	sectionID string // DSL reference section ID for enriched documentation
	kind      protocol.CompletionItemKind
}

// getTopLevelKeys returns completion items for top-level YAML keys.
// When a registry is provided, descriptions are enriched from the DSL reference.
func getTopLevelKeys(reg *Registry) []protocol.CompletionItem {
	entries := []topLevelKeyEntry{
		{"modules", "List of module definitions to instantiate", "modules", protocol.CompletionItemKindKeyword},
		{"workflows", "Workflow handler configurations (http, messaging, statemachine, events)", "workflows", protocol.CompletionItemKindKeyword},
		{"triggers", "Trigger configurations (http, cron, event)", "triggers", protocol.CompletionItemKindKeyword},
		{"pipelines", "Named pipeline definitions with ordered steps", "pipelines", protocol.CompletionItemKindKeyword},
		{"imports", "List of external config files to import", "imports", protocol.CompletionItemKindKeyword},
		{"requires", "Plugin and version dependency declarations", "application", protocol.CompletionItemKindKeyword},
		{"platform", "Platform-level IaC configuration", "platform", protocol.CompletionItemKindKeyword},
		{"infrastructure", "Cloud resource provisioning declarations", "platform", protocol.CompletionItemKindKeyword},
		{"sidecars", "Auxiliary containers to run alongside the application", "platform", protocol.CompletionItemKindKeyword},
		{"name", "Application name", "application", protocol.CompletionItemKindKeyword},
		{"version", "Application version (semver recommended)", "application", protocol.CompletionItemKindKeyword},
		{"configProviders", "Configuration value providers (env, file, defaults)", "config-providers", protocol.CompletionItemKindKeyword},
	}

	items := make([]protocol.CompletionItem, 0, len(entries))
	for _, e := range entries {
		kind := e.kind
		doc := e.doc
		// Enrich with DSL reference description if available.
		if reg != nil && reg.DSLSections != nil {
			if sec, ok := reg.DSLSections[e.sectionID]; ok && sec.Description != "" {
				doc = sec.Description
			}
		}
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
	for i := range info.ConfigDefs {
		cf := &info.ConfigDefs[i]
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
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
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
		return getTemplateCompletions(reg, doc, ctx)
	}
	if ctx.InExpr {
		return getExprCompletions(reg, doc, ctx)
	}

	switch ctx.Section {
	case SectionTopLevel:
		return getTopLevelKeys(reg)
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
// reg is threaded through from Completions so BuildPipelineContext can use it.
func getTemplateCompletions(reg *Registry, doc *Document, ctx PositionContext) []protocol.CompletionItem {
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
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
		if tp.StepName == "" {
			// Suggest step names, filtered by FieldPrefix.
			return filterCompletions(stepNameCompletions(pipCtx), tp.FieldPrefix)
		}
		// Suggest output keys for the named step, filtered by FieldPrefix.
		return filterCompletions(stepOutputKeyCompletions(pipCtx, tp.StepName), tp.FieldPrefix)

	case "trigger":
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
		if tp.SubField != "" {
			// Drill into trigger sub-namespace (e.g. .trigger.path_params.)
			return filterCompletions(triggerSubFieldCompletions(pipCtx, tp.SubField), tp.FieldPrefix)
		}
		return filterCompletions(triggerFieldCompletions(pipCtx), tp.FieldPrefix)

	case "body":
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
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
	items := make([]protocol.CompletionItem, 0, len(pipCtx.StepOrder))
	for _, name := range pipCtx.StepOrder {
		so := pipCtx.Steps[name]
		stype := ""
		if so != nil {
			stype = so.StepType
		}
		stepName := name
		items = append(items, protocol.CompletionItem{
			Label:         stepName,
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
	so := pipCtx.Steps[stepName]
	if so == nil {
		return nil
	}
	kind := protocol.CompletionItemKindField
	fields := make([]struct{ key, typ, desc string }, 0, len(so.Fields))
	for _, f := range so.Fields {
		fields = append(fields, struct{ key, typ, desc string }{f.Key, f.Type, f.Description})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].key < fields[j].key })
	items := make([]protocol.CompletionItem, 0, len(fields))
	for _, f := range fields {
		key := f.key
		detail := f.typ
		items = append(items, protocol.CompletionItem{
			Label:         key,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: f.desc,
		})
	}
	return items
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
		for _, fs := range pipCtx.Trigger.PathParams {
			name := fs.Name
			items = append(items, protocol.CompletionItem{
				Label:         name,
				Kind:          &kind,
				Documentation: "Path parameter",
			})
		}
		for _, fs := range pipCtx.Trigger.QueryParams {
			name := fs.Name
			items = append(items, protocol.CompletionItem{
				Label:         name,
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
	items := make([]protocol.CompletionItem, 0, len(pipCtx.Trigger.BodyFields))
	for _, fs := range pipCtx.Trigger.BodyFields {
		name := fs.Name
		detail := fs.Type
		items = append(items, protocol.CompletionItem{
			Label:         name,
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
	for i := range items {
		if len(items[i].Label) >= len(prefix) && items[i].Label[:len(prefix)] == prefix {
			filtered = append(filtered, items[i])
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
	var source []*FieldSchema
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
	items := make([]protocol.CompletionItem, 0, len(source))
	for _, fs := range source {
		name := fs.Name
		detail := fs.Type
		items = append(items, protocol.CompletionItem{
			Label:         name,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: fs.Description,
		})
	}
	return items
}

// getExprCompletions returns context-aware completions for ${ } expr expressions.
// It reuses the same pipeline data context as Go template completions but uses
// expr-style suggestions (bracket notation, function call syntax).
func getExprCompletions(reg *Registry, doc *Document, ctx PositionContext) []protocol.CompletionItem {
	tp := ctx.TemplatePath
	if tp == nil {
		// No parsed path: suggest namespaces and functions.
		items := exprNamespaceCompletions()
		items = append(items, getTemplateFunctionCompletions()...)
		return items
	}

	switch tp.Namespace {
	case "":
		items := exprNamespaceCompletions()
		items = append(items, getTemplateFunctionCompletions()...)
		if tp.FieldPrefix != "" {
			return filterCompletions(items, tp.FieldPrefix)
		}
		return items

	case "steps":
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
		if tp.StepName == "" {
			return filterCompletions(stepNameCompletions(pipCtx), tp.FieldPrefix)
		}
		return filterCompletions(stepOutputKeyCompletions(pipCtx, tp.StepName), tp.FieldPrefix)

	case "trigger":
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
		if tp.SubField != "" {
			return filterCompletions(triggerSubFieldCompletions(pipCtx, tp.SubField), tp.FieldPrefix)
		}
		return filterCompletions(triggerFieldCompletions(pipCtx), tp.FieldPrefix)

	case "body":
		pipCtx := BuildPipelineContext(reg, doc, ctx.Line)
		return filterCompletions(bodyFieldCompletions(pipCtx), tp.FieldPrefix)

	case "meta":
		return filterCompletions(metaFieldCompletions(), tp.FieldPrefix)

	default:
		return getTemplateFunctionCompletions()
	}
}

// exprNamespaceCompletions returns top-level namespace completions for expr syntax.
func exprNamespaceCompletions() []protocol.CompletionItem {
	kind := protocol.CompletionItemKindKeyword
	entries := []struct{ label, doc string }{
		{"steps", `Step outputs: steps["step-name"]["field"]`},
		{"trigger", `Trigger data: trigger["path_params"]["id"]`},
		{"body", `Request body shorthand: body["field"]`},
		{"meta", `Pipeline metadata: meta["pipeline"]`},
		{"current", "Merged pipeline context at current step"},
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
