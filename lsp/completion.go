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
		return getTemplateFunctionCompletions()
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
