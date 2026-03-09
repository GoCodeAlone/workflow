package modernize

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// --- yaml.Node helpers ---

// walkNodes calls fn for every node in the tree (depth-first).
func walkNodes(node *yaml.Node, fn func(n *yaml.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for _, child := range node.Content {
		walkNodes(child, fn)
	}
}

// findMapValue returns the value node for a given key in a mapping node.
func findMapValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// stepNameInfo holds a step name and its YAML line number.
type stepNameInfo struct {
	Name string
	Line int
}

// collectStepNames walks pipelines and collects all step names with line info.
func collectStepNames(root *yaml.Node) []stepNameInfo {
	var names []stepNameInfo
	// root is DocumentNode -> first child is the mapping
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	pipelines := findMapValue(root, "pipelines")
	if pipelines == nil || pipelines.Kind != yaml.MappingNode {
		return names
	}
	// Iterate pipeline values
	for i := 1; i < len(pipelines.Content); i += 2 {
		pipelineVal := pipelines.Content[i]
		steps := findMapValue(pipelineVal, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			nameNode := findMapValue(step, "name")
			if nameNode != nil && nameNode.Kind == yaml.ScalarNode {
				names = append(names, stepNameInfo{Name: nameNode.Value, Line: nameNode.Line})
			}
		}
	}
	return names
}

// forEachStepOfType calls fn for each step node of the given type across all pipelines.
func forEachStepOfType(root *yaml.Node, stepType string, fn func(step *yaml.Node)) {
	docRoot := root
	if docRoot.Kind == yaml.DocumentNode && len(docRoot.Content) > 0 {
		docRoot = docRoot.Content[0]
	}
	pipelines := findMapValue(docRoot, "pipelines")
	if pipelines == nil || pipelines.Kind != yaml.MappingNode {
		return
	}
	for i := 1; i < len(pipelines.Content); i += 2 {
		pipelineVal := pipelines.Content[i]
		steps := findMapValue(pipelineVal, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			typeNode := findMapValue(step, "type")
			if typeNode != nil && typeNode.Value == stepType {
				fn(step)
			}
		}
	}
}

// forEachModule calls fn for each module mapping node.
func forEachModule(root *yaml.Node, fn func(mod *yaml.Node)) {
	docRoot := root
	if docRoot.Kind == yaml.DocumentNode && len(docRoot.Content) > 0 {
		docRoot = docRoot.Content[0]
	}
	modules := findMapValue(docRoot, "modules")
	if modules == nil || modules.Kind != yaml.SequenceNode {
		return
	}
	for _, mod := range modules.Content {
		if mod.Kind == yaml.MappingNode {
			fn(mod)
		}
	}
}

func hyphenStepsRule() Rule {
	return Rule{
		ID:          "hyphen-steps",
		Description: "Rename hyphenated step names to underscores (hyphens break Go templates)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			names := collectStepNames(root)
			for _, info := range names {
				if strings.Contains(info.Name, "-") {
					findings = append(findings, Finding{
						RuleID:  "hyphen-steps",
						Line:    info.Line,
						Message: fmt.Sprintf("Step %q uses hyphens (causes Go template parse errors)", info.Name),
						Fixable: true,
					})
				}
			}
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			names := collectStepNames(root)
			// Build rename map: old -> new
			renames := make(map[string]string)
			for _, info := range names {
				if strings.Contains(info.Name, "-") {
					renames[info.Name] = strings.ReplaceAll(info.Name, "-", "_")
				}
			}
			if len(renames) == 0 {
				return nil
			}

			var changes []Change

			// 1. Rename step name fields themselves (exact match on name values)
			docRoot := root
			if docRoot.Kind == yaml.DocumentNode && len(docRoot.Content) > 0 {
				docRoot = docRoot.Content[0]
			}
			pipelines := findMapValue(docRoot, "pipelines")
			if pipelines != nil && pipelines.Kind == yaml.MappingNode {
				for i := 1; i < len(pipelines.Content); i += 2 {
					pipelineVal := pipelines.Content[i]
					steps := findMapValue(pipelineVal, "steps")
					if steps == nil || steps.Kind != yaml.SequenceNode {
						continue
					}
					for _, step := range steps.Content {
						nameNode := findMapValue(step, "name")
						if nameNode == nil || nameNode.Kind != yaml.ScalarNode {
							continue
						}
						if newName, ok := renames[nameNode.Value]; ok {
							changes = append(changes, Change{
								RuleID:      "hyphen-steps",
								Line:        nameNode.Line,
								Description: fmt.Sprintf("Renamed step %q -> %q", nameNode.Value, newName),
							})
							nameNode.Value = newName
						}

						// 2. Rename "next" field references (exact match)
						nextNode := findMapValue(step, "next")
						if nextNode != nil && nextNode.Kind == yaml.ScalarNode {
							if newName, ok := renames[nextNode.Value]; ok {
								changes = append(changes, Change{
									RuleID:      "hyphen-steps",
									Line:        nextNode.Line,
									Description: fmt.Sprintf("Updated next reference %q -> %q", nextNode.Value, newName),
								})
								nextNode.Value = newName
							}
						}

						// 3. Update references inside config values
						cfg := findMapValue(step, "config")
						if cfg != nil && cfg.Kind == yaml.MappingNode {
							hyphenStepsFixConfig(cfg, renames, &changes)
						}
					}
				}
			}

			return changes
		},
	}
}

// hyphenStepsFixConfig updates step name references inside config mapping values.
// It handles: template index expressions, conditional field dot-paths, route values,
// and default values that are exact step name matches.
func hyphenStepsFixConfig(cfg *yaml.Node, renames map[string]string, changes *[]Change) {
	if cfg.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(cfg.Content); i += 2 {
		key := cfg.Content[i]
		val := cfg.Content[i+1]

		switch {
		case val.Kind == yaml.MappingNode:
			// Recurse into nested maps (e.g., routes map in step.conditional)
			hyphenStepsFixConfig(val, renames, changes)
		case val.Kind == yaml.ScalarNode:
			for oldName, newName := range renames {
				updated := hyphenStepsFixScalar(key.Value, val.Value, oldName, newName)
				if updated != val.Value {
					*changes = append(*changes, Change{
						RuleID:      "hyphen-steps",
						Line:        val.Line,
						Description: fmt.Sprintf("Updated reference %q in config", oldName),
					})
					val.Value = updated
				}
			}
		}
	}
}

// hyphenStepsFixScalar updates a single scalar value, only in safe contexts:
// - "field" key: dot-path like steps.old-name.output
// - "default" key or route values: exact step name match
// - Template index expressions: index .steps "old-name" "field"
// - Template dot-path expressions: .steps.old-name.field
func hyphenStepsFixScalar(key, value, oldName, newName string) string {
	// Exact match (e.g., default: old-name, or route value: old-name)
	if value == oldName {
		return newName
	}

	updated := value

	// Template index expressions: {{ index .steps "old-name" "field" }}
	indexPattern := `index .steps "` + oldName + `"`
	if strings.Contains(updated, indexPattern) {
		updated = strings.ReplaceAll(updated, indexPattern, `index .steps "`+newName+`"`)
	}

	// Template dot-path inside {{ }}: .steps.old-name.field
	dotPattern := ".steps." + oldName + "."
	if strings.Contains(updated, dotPattern) {
		updated = strings.ReplaceAll(updated, dotPattern, ".steps."+newName+".")
	}
	// Also handle end-of-expression: .steps.old-name }}
	dotPatternEnd := ".steps." + oldName + " "
	if strings.Contains(updated, dotPatternEnd) {
		updated = strings.ReplaceAll(updated, dotPatternEnd, ".steps."+newName+" ")
	}

	// Conditional field dot-path (no template braces): steps.old-name.output
	if key == "field" {
		fieldPattern := "steps." + oldName + "."
		if strings.Contains(updated, fieldPattern) {
			updated = strings.ReplaceAll(updated, fieldPattern, "steps."+newName+".")
		}
	}

	return updated
}

// conditionalFieldTemplateRegex matches {{ .some.path }} in a field value.
var conditionalFieldTemplateRegex = regexp.MustCompile(`^\{\{\s*\.?([\w.]+)\s*\}\}$`)

func conditionalFieldRule() Rule {
	return Rule{
		ID:          "conditional-field",
		Description: "Convert template syntax in step.conditional field to dot-path",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				field := findMapValue(cfg, "field")
				if field == nil || field.Kind != yaml.ScalarNode {
					return
				}
				if strings.Contains(field.Value, "{{") {
					findings = append(findings, Finding{
						RuleID:  "conditional-field",
						Line:    field.Line,
						Message: fmt.Sprintf("step.conditional field uses template syntax %q (should be dot-path)", field.Value),
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				field := findMapValue(cfg, "field")
				if field == nil || field.Kind != yaml.ScalarNode {
					return
				}
				if m := conditionalFieldTemplateRegex.FindStringSubmatch(field.Value); m != nil {
					oldVal := field.Value
					field.Value = m[1]
					field.Style = 0 // remove quotes
					changes = append(changes, Change{
						RuleID:      "conditional-field",
						Line:        field.Line,
						Description: fmt.Sprintf("Converted field %q -> %q", oldVal, field.Value),
					})
				}
			})
			return changes
		},
	}
}

func dbQueryModeRule() Rule {
	return Rule{
		ID:          "db-query-mode",
		Description: "Add mode:single to step.db_query when downstream uses .row or .found",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			rawStr := string(raw)
			forEachStepOfType(root, "step.db_query", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				mode := findMapValue(cfg, "mode")
				if mode != nil {
					return // already has mode set
				}
				nameNode := findMapValue(step, "name")
				if nameNode == nil {
					return
				}
				stepName := nameNode.Value
				// Check if raw YAML references .row or .found for this step
				if strings.Contains(rawStr, stepName+`" "row"`) ||
					strings.Contains(rawStr, stepName+".row") ||
					strings.Contains(rawStr, stepName+".found") {
					findings = append(findings, Finding{
						RuleID:  "db-query-mode",
						Line:    step.Line,
						Message: fmt.Sprintf("step.db_query %q missing mode:single (downstream uses .row/.found)", stepName),
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			// We need the raw text for reference checking — marshal current state
			rawBytes, _ := yaml.Marshal(root)
			rawStr := string(rawBytes)

			forEachStepOfType(root, "step.db_query", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				mode := findMapValue(cfg, "mode")
				if mode != nil {
					return
				}
				nameNode := findMapValue(step, "name")
				if nameNode == nil {
					return
				}
				stepName := nameNode.Value
				if strings.Contains(rawStr, stepName+`" "row"`) ||
					strings.Contains(rawStr, stepName+".row") ||
					strings.Contains(rawStr, stepName+".found") {
					// Add mode: single to config mapping
					cfg.Content = append(cfg.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "mode"},
						&yaml.Node{Kind: yaml.ScalarNode, Value: "single"},
					)
					changes = append(changes, Change{
						RuleID:      "db-query-mode",
						Line:        step.Line,
						Description: fmt.Sprintf("Added mode: single to step.db_query %q", stepName),
					})
				}
			})
			return changes
		},
	}
}

// dotRowAccessRegex matches patterns like .steps.stepname.row.column inside {{ }}.
var dotRowAccessRegex = regexp.MustCompile(`\.steps\.(\w+)\.row\.(\w+)`)

func dbQueryIndexRule() Rule {
	return Rule{
		ID:          "db-query-index",
		Description: "Convert .steps.X.row.Y dot-access to index syntax (dot-access causes nil pointer)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			walkNodes(root, func(n *yaml.Node) {
				if n.Kind != yaml.ScalarNode {
					return
				}
				if matches := dotRowAccessRegex.FindAllString(n.Value, -1); len(matches) > 0 {
					for _, m := range matches {
						findings = append(findings, Finding{
							RuleID:  "db-query-index",
							Line:    n.Line,
							Message: fmt.Sprintf("Dot-access %q will cause nil pointer (use index syntax)", m),
							Fixable: true,
						})
					}
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			walkNodes(root, func(n *yaml.Node) {
				if n.Kind != yaml.ScalarNode {
					return
				}
				if !dotRowAccessRegex.MatchString(n.Value) {
					return
				}
				oldVal := n.Value
				n.Value = dotRowAccessRegex.ReplaceAllStringFunc(n.Value, func(match string) string {
					parts := dotRowAccessRegex.FindStringSubmatch(match)
					// parts[1] = step name, parts[2] = column name
					return fmt.Sprintf(`index .steps %q "row" %q`, parts[1], parts[2])
				})
				if n.Value != oldVal {
					changes = append(changes, Change{
						RuleID:      "db-query-index",
						Line:        n.Line,
						Description: "Converted dot-access to index syntax",
					})
				}
			})
			return changes
		},
	}
}

func absoluteDbPathRule() Rule {
	return Rule{
		ID:          "absolute-dbpath",
		Description: "Warn on absolute dbPath in storage.sqlite (should be relative to config dir)",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode == nil || typeNode.Value != "storage.sqlite" {
					return
				}
				cfg := findMapValue(mod, "config")
				if cfg == nil {
					return
				}
				dbPath := findMapValue(cfg, "dbPath")
				if dbPath != nil && strings.HasPrefix(dbPath.Value, "/") {
					nameNode := findMapValue(mod, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "absolute-dbpath",
						Line:    dbPath.Line,
						Message: fmt.Sprintf("Module %q has absolute dbPath %q (use relative path)", name, dbPath.Value),
						Fixable: false,
					})
				}
			})
			return findings
		},
	}
}

func emptyRoutesRule() Rule {
	return Rule{
		ID:          "empty-routes",
		Description: "Detect empty routes map in step.conditional (engine requires at least one route)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				routes := findMapValue(cfg, "routes")
				if routes == nil {
					nameNode := findMapValue(step, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "empty-routes",
						Line:    step.Line,
						Message: fmt.Sprintf("step.conditional %q missing routes map", name),
						Fixable: false,
					})
					return
				}
				if routes.Kind == yaml.MappingNode && len(routes.Content) == 0 {
					nameNode := findMapValue(step, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "empty-routes",
						Line:    routes.Line,
						Message: fmt.Sprintf("step.conditional %q has empty routes (at least one route required)", name),
						Fixable: false,
					})
				}
			})
			return findings
		},
	}
}

func requestParseConfigRule() Rule {
	return Rule{
		ID:          "request-parse-config",
		Description: "Detect invalid step.request_parse config keys (parse_headers as bool, unnecessary parse_body)",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, "step.request_parse", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				nameNode := findMapValue(step, "name")
				stepName := ""
				if nameNode != nil {
					stepName = nameNode.Value
				}

				// Check parse_headers: if it's a boolean (scalar "true"/"false"), warn
				parseHeaders := findMapValue(cfg, "parse_headers")
				if parseHeaders != nil && parseHeaders.Kind == yaml.ScalarNode {
					v := strings.ToLower(parseHeaders.Value)
					if v == "true" || v == "false" {
						findings = append(findings, Finding{
							RuleID:  "request-parse-config",
							Line:    parseHeaders.Line,
							Message: fmt.Sprintf("step.request_parse %q has parse_headers: %s (boolean); use an array of header names like [\"Authorization\"] or migrate to headers:", stepName, parseHeaders.Value),
							Fixable: false,
						})
					}
				}

				// Check parse_body: true — unnecessary since body is auto-parsed
				parseBody := findMapValue(cfg, "parse_body")
				if parseBody != nil && parseBody.Kind == yaml.ScalarNode {
					v := strings.ToLower(parseBody.Value)
					if v == "true" {
						findings = append(findings, Finding{
							RuleID:  "request-parse-config",
							Line:    parseBody.Line,
							Message: fmt.Sprintf("step.request_parse %q has parse_body: true (unnecessary, body is auto-parsed)", stepName),
							Fixable: false,
						})
					}
				}
			})
			return findings
		},
	}
}

// snakeCaseKeyRegex matches keys with underscores (snake_case).
var snakeCaseKeyRegex = regexp.MustCompile(`^[a-z]+(_[a-z0-9]+)+$`)

func camelCaseConfigRule() Rule {
	return Rule{
		ID:          "camelcase-config",
		Description: "Detect snake_case config field names (engine requires camelCase)",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				cfg := findMapValue(mod, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				nameNode := findMapValue(mod, "name")
				modName := ""
				if nameNode != nil {
					modName = nameNode.Value
				}
				for i := 0; i+1 < len(cfg.Content); i += 2 {
					key := cfg.Content[i]
					if key.Kind == yaml.ScalarNode && snakeCaseKeyRegex.MatchString(key.Value) {
						findings = append(findings, Finding{
							RuleID:  "camelcase-config",
							Line:    key.Line,
							Message: fmt.Sprintf("Module %q config key %q is snake_case (use camelCase)", modName, key.Value),
							Fixable: false,
						})
					}
				}
			})
			return findings
		},
	}
}
