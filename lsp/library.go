package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DiagSeverity is the severity level of a diagnostic message.
type DiagSeverity int

const (
	// SeverityError indicates a hard error that prevents correct operation.
	SeverityError DiagSeverity = 1
	// SeverityWarning indicates a potential issue.
	SeverityWarning DiagSeverity = 2
	// SeverityInformation indicates an informational message.
	SeverityInformation DiagSeverity = 3
	// SeverityHint indicates a hint or suggestion.
	SeverityHint DiagSeverity = 4
)

// Diagnostic is a language-server-style diagnostic returned by DiagnoseContent.
type Diagnostic struct {
	Line    int
	Col     int
	EndLine int
	EndCol  int
	Message  string
	Severity DiagSeverity
	Source   string
}

// CompletionResult is a single completion item returned by CompleteAt.
type CompletionResult struct {
	Label  string
	Kind   string
	Detail string
}

// HoverResult is the hover content returned by HoverAt.
type HoverResult struct {
	Content string
}

// DiagnoseContent analyses YAML content in-process and returns diagnostics
// without requiring an LSP client connection. An optional pluginDir can be
// provided to load step schemas from external plugin manifests.
func DiagnoseContent(content string, pluginDir ...string) []Diagnostic {
	s := NewServer(pluginDir...)
	doc := s.store.Set("inmemory://check.yaml", content)
	protoDiags := Diagnostics(s.registry, doc)
	return convertDiagnostics(protoDiags)
}

// CompleteAt returns completion suggestions for the given content at (line, col).
// Both line and col are zero-based. An optional pluginDir can be provided.
func CompleteAt(content string, line, col int, pluginDir ...string) []CompletionResult {
	s := NewServer(pluginDir...)
	doc := s.store.Set("inmemory://check.yaml", content)
	ctx := ContextAt(doc.Content, line, col)
	items := Completions(s.registry, doc, ctx)
	return convertCompletions(items)
}

// HoverAt returns hover documentation for the given content at (line, col).
// Both line and col are zero-based. Returns nil if there is no hover info.
// An optional pluginDir can be provided.
func HoverAt(content string, line, col int, pluginDir ...string) *HoverResult {
	s := NewServer(pluginDir...)
	doc := s.store.Set("inmemory://check.yaml", content)
	ctx := ContextAt(doc.Content, line, col)
	hover := Hover(s.registry, doc, ctx)
	if hover == nil {
		return nil
	}
	if mc, ok := hover.Contents.(protocol.MarkupContent); ok {
		return &HoverResult{Content: mc.Value}
	}
	return nil
}

// convertDiagnostics converts protocol diagnostics to library Diagnostic values.
func convertDiagnostics(diags []protocol.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(diags))
	for _, d := range diags {
		sev := SeverityWarning
		if d.Severity != nil {
			sev = DiagSeverity(*d.Severity)
		}
		src := ""
		if d.Source != nil {
			src = *d.Source
		}
		out = append(out, Diagnostic{
			Line:     int(d.Range.Start.Line),    //nolint:gosec // G115: LSP positions are non-negative
			Col:      int(d.Range.Start.Character), //nolint:gosec // G115: LSP positions are non-negative
			EndLine:  int(d.Range.End.Line),       //nolint:gosec // G115: LSP positions are non-negative
			EndCol:   int(d.Range.End.Character),  //nolint:gosec // G115: LSP positions are non-negative
			Message:  d.Message,
			Severity: sev,
			Source:   src,
		})
	}
	return out
}

// convertCompletions converts protocol completion items to library CompletionResult values.
func convertCompletions(items []protocol.CompletionItem) []CompletionResult {
	out := make([]CompletionResult, 0, len(items))
	for _, item := range items {
		kind := ""
		if item.Kind != nil {
			kind = completionKindName(*item.Kind)
		}
		detail := ""
		if item.Detail != nil {
			detail = *item.Detail
		}
		out = append(out, CompletionResult{
			Label:  item.Label,
			Kind:   kind,
			Detail: detail,
		})
	}
	return out
}

// completionKindName converts a protocol CompletionItemKind to a string name.
func completionKindName(k protocol.CompletionItemKind) string {
	switch k {
	case protocol.CompletionItemKindText:
		return "text"
	case protocol.CompletionItemKindMethod:
		return "method"
	case protocol.CompletionItemKindFunction:
		return "function"
	case protocol.CompletionItemKindConstructor:
		return "constructor"
	case protocol.CompletionItemKindField:
		return "field"
	case protocol.CompletionItemKindVariable:
		return "variable"
	case protocol.CompletionItemKindClass:
		return "class"
	case protocol.CompletionItemKindInterface:
		return "interface"
	case protocol.CompletionItemKindModule:
		return "module"
	case protocol.CompletionItemKindProperty:
		return "property"
	case protocol.CompletionItemKindUnit:
		return "unit"
	case protocol.CompletionItemKindValue:
		return "value"
	case protocol.CompletionItemKindEnum:
		return "enum"
	case protocol.CompletionItemKindKeyword:
		return "keyword"
	case protocol.CompletionItemKindSnippet:
		return "snippet"
	case protocol.CompletionItemKindColor:
		return "color"
	case protocol.CompletionItemKindFile:
		return "file"
	case protocol.CompletionItemKindReference:
		return "reference"
	case protocol.CompletionItemKindFolder:
		return "folder"
	case protocol.CompletionItemKindEnumMember:
		return "enum_member"
	case protocol.CompletionItemKindConstant:
		return "constant"
	case protocol.CompletionItemKindStruct:
		return "struct"
	case protocol.CompletionItemKindEvent:
		return "event"
	case protocol.CompletionItemKindOperator:
		return "operator"
	case protocol.CompletionItemKindTypeParameter:
		return "type_parameter"
	default:
		return "unknown"
	}
}
