package lsp

import (
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Document holds the content and parsed state of an open YAML file.
type Document struct {
	URI     string
	Content string
	Node    *yaml.Node // root node (Kind == DocumentNode)
}

// DocumentStore is a thread-safe store of open LSP documents.
type DocumentStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

// NewDocumentStore creates an empty DocumentStore.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: make(map[string]*Document)}
}

// Set stores or replaces a document.
func (ds *DocumentStore) Set(uri, content string) *Document {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	doc := &Document{URI: uri, Content: content}
	doc.Node = parseYAML(content)
	ds.docs[uri] = doc
	return doc
}

// Get returns a document by URI, or nil if not found.
func (ds *DocumentStore) Get(uri string) *Document {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.docs[uri]
}

// Delete removes a document from the store.
func (ds *DocumentStore) Delete(uri string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.docs, uri)
}

// parseYAML parses YAML content and returns the root node, or nil on error.
func parseYAML(content string) *yaml.Node {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil
	}
	return &root
}

// SectionKind identifies the YAML section the cursor is in.
type SectionKind string

const (
	SectionUnknown  SectionKind = "unknown"
	SectionModules  SectionKind = "modules"
	SectionWorkflow SectionKind = "workflows"
	SectionTriggers SectionKind = "triggers"
	SectionPipeline SectionKind = "pipelines"
	SectionRequires SectionKind = "requires"
	SectionImports  SectionKind = "imports"
	SectionTopLevel SectionKind = "top_level"
)

// PositionContext describes what context the cursor is in within the document.
type PositionContext struct {
	Section         SectionKind
	ModuleType      string            // if inside a modules[] item config, the type value
	StepType        string            // if inside a pipeline step config, the step type value
	FieldName       string            // the field name at the cursor
	InTemplate      bool              // cursor is inside {{ }}
	InExpr          bool              // cursor is inside ${ }
	DependsOn       bool              // cursor is in a dependsOn array value
	PipelineName    string            // name of the pipeline containing the cursor (if any)
	CurrentStepName string            // name of the step containing the cursor (if any)
	TemplatePath    *TemplateExprPath // parsed template expression at cursor, if InTemplate or InExpr
	Line            int
	Character       int
}

// ContextAt analyses the document content at the given (zero-based) line and
// character position and returns a PositionContext describing what the cursor
// is positioned on.
func ContextAt(content string, line, char int) PositionContext {
	ctx := PositionContext{
		Section:   SectionUnknown,
		Line:      line,
		Character: char,
	}

	lines := strings.Split(content, "\n")
	if line >= len(lines) {
		return ctx
	}
	currentLine := lines[line]

	// Check for template expression.
	if isInTemplate(lines, line, char) {
		ctx.InTemplate = true
		ctx.TemplatePath = ParseTemplateExprAt(currentLine, char)
	} else if isInExpr(lines, line, char) {
		ctx.InExpr = true
		ctx.TemplatePath = ParseExprAt(currentLine, char)
	}

	// Determine indentation level and section.
	indent := leadingSpaces(currentLine)

	if indent == 0 {
		ctx.Section = SectionTopLevel
		return ctx
	}

	// Walk up the lines to find parent keys.
	section, moduleType, stepType, field := inferContext(lines, line, indent)
	ctx.Section = section
	ctx.ModuleType = moduleType
	ctx.StepType = stepType
	ctx.FieldName = field

	// If we're on a "type:" line, extract the value for hover support.
	if field == "type" {
		trimmedCur := strings.TrimSpace(currentLine)
		if strings.HasPrefix(trimmedCur, "type:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmedCur, "type:"))
			if val != "" {
				if ctx.Section == SectionPipeline && strings.HasPrefix(val, "step.") {
					ctx.StepType = val
				} else if ctx.Section == SectionModules {
					ctx.ModuleType = val
				}
			}
		}
	}

	// Check if in dependsOn.
	for i := line; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "dependsOn:") {
			ctx.DependsOn = true
			break
		}
		if leadingSpaces(lines[i]) < indent && leadingSpaces(lines[i]) > 0 {
			break
		}
	}

	// Populate pipeline/step names for template completion support.
	if ctx.Section == SectionPipeline {
		ctx.PipelineName = findPipelineName(lines, line)
		ctx.CurrentStepName = findCurrentStepName(lines, line)
	}

	return ctx
}

// findPipelineName walks upward from lineIdx to find the pipeline name key
// (the key directly under "pipelines:" at indent 2).
func findPipelineName(lines []string, lineIdx int) string {
	for i := lineIdx; i >= 0; i-- {
		l := lines[i]
		ind := leadingSpaces(l)
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if ind == 0 {
			break
		}
		if ind == 2 {
			// At indent 2: this could be the pipeline name key
			// Keep walking to find "pipelines:" at indent 0
			if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
				candidate := strings.TrimSpace(trimmed[:colonIdx])
				// Continue searching; mark as candidate if we find pipelines: above
				for j := i - 1; j >= 0; j-- {
					jl := lines[j]
					jind := leadingSpaces(jl)
					jt := strings.TrimSpace(jl)
					if jt == "" {
						continue
					}
					if jind == 0 {
						if strings.HasPrefix(jt, "pipelines:") {
							return candidate
						}
						break
					}
				}
			}
		}
	}
	return ""
}

// findCurrentStepName walks upward from lineIdx to find the closest "name:"
// field at step-item indentation level.
func findCurrentStepName(lines []string, lineIdx int) string {
	curIndent := leadingSpaces(lines[lineIdx])
	for i := lineIdx; i >= 0; i-- {
		l := lines[i]
		ind := leadingSpaces(l)
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		// Step items are sequence entries (typically at indent 6+).
		// "name:" at the same or lower indent within the step block.
		if ind <= curIndent && strings.HasPrefix(trimmed, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		}
		// If we go up to steps: level, stop.
		if strings.HasPrefix(trimmed, "steps:") {
			break
		}
	}
	return ""
}

// isInTemplate returns true if position (line, char) is inside a {{ }} expression.
func isInTemplate(lines []string, line, char int) bool {
	if line >= len(lines) {
		return false
	}
	l := lines[line]
	if char > len(l) {
		char = len(l)
	}
	prefix := l[:char]
	openIdx := strings.LastIndex(prefix, "{{")
	closeIdx := strings.LastIndex(prefix, "}}")
	return openIdx >= 0 && openIdx > closeIdx
}

// isInExpr returns true if position (line, char) is inside a ${ } expression.
func isInExpr(lines []string, line, char int) bool {
	if line >= len(lines) {
		return false
	}
	l := lines[line]
	if char > len(l) {
		char = len(l)
	}
	prefix := l[:char]
	openIdx := strings.LastIndex(prefix, "${")
	closeIdx := strings.LastIndex(prefix, "}")
	// closeIdx must come after openIdx+1 (the { of ${) to count as closing.
	if openIdx < 0 {
		return false
	}
	return closeIdx < openIdx+1
}

// leadingSpaces returns the number of leading spaces in a string.
func leadingSpaces(s string) int {
	for i, c := range s {
		if c != ' ' {
			return i
		}
	}
	return len(s)
}

// inferContext walks upward through lines to determine the YAML section,
// current module/step type (if any), and field name at the given line.
func inferContext(lines []string, line, curIndent int) (SectionKind, string, string, string) {
	section := SectionUnknown
	contextType := "" // type value found in the block (module or step type)
	field := ""

	// Get the field on the current line.
	cur := strings.TrimSpace(lines[line])
	if colonIdx := strings.Index(cur, ":"); colonIdx >= 0 {
		field = strings.TrimSpace(cur[:colonIdx])
	} else {
		// Could be a list item value.
		field = strings.TrimPrefix(cur, "- ")
	}

	// Walk upward to detect structure.
	prevIndent := curIndent
	for i := line - 1; i >= 0; i-- {
		l := lines[i]
		ind := leadingSpaces(l)
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if ind < prevIndent {
			prevIndent = ind
			key := ""
			if colonIdx := strings.Index(trimmed, ":"); colonIdx >= 0 {
				key = strings.TrimSpace(trimmed[:colonIdx])
			} else {
				key = strings.TrimPrefix(trimmed, "- ")
			}

			switch key {
			case "modules":
				if section == SectionUnknown {
					section = SectionModules
				}
				return section, contextType, "", field
			case "workflows":
				if section == SectionUnknown {
					section = SectionWorkflow
				}
				return section, contextType, "", field
			case "triggers":
				if section == SectionUnknown {
					section = SectionTriggers
				}
				return section, contextType, "", field
			case "pipelines":
				if section == SectionUnknown {
					section = SectionPipeline
				}
				// If contextType starts with "step.", it's a step type.
				if strings.HasPrefix(contextType, "step.") {
					return section, "", contextType, field
				}
				return section, contextType, "", field
			case "requires":
				if section == SectionUnknown {
					section = SectionRequires
				}
				return section, contextType, "", field
			case "imports":
				if section == SectionUnknown {
					section = SectionImports
				}
				return section, contextType, "", field
			case "config":
				// The parent is config — find the type field in the same block.
				contextType = findTypeInBlock(lines, i)
				// Don't return — continue walking to find the section.
			case "type":
				// Inside a type field value — look for surrounding block.
			}
		}
	}

	// If we exhausted lines without finding a section but have a contextType,
	// check if it's a step type.
	if strings.HasPrefix(contextType, "step.") {
		return section, "", contextType, field
	}
	return section, contextType, "", field
}

// findTypeInBlock searches upward from lineIdx to find a "type:" key
// at the same module-item indentation level.
func findTypeInBlock(lines []string, lineIdx int) string {
	refIndent := leadingSpaces(lines[lineIdx])
	for i := lineIdx - 1; i >= 0; i-- {
		l := lines[i]
		ind := leadingSpaces(l)
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if ind < refIndent {
			// Moved up out of the block.
			break
		}
		if strings.HasPrefix(trimmed, "type:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
			return val
		}
	}
	return ""
}
