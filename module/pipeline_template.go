package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// TemplateEngine resolves {{ .field }} expressions against a PipelineContext.
type TemplateEngine struct{}

// NewTemplateEngine creates a new TemplateEngine.
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{}
}

// templateData builds the data map that Go templates see.
func (te *TemplateEngine) templateData(pc *PipelineContext) map[string]any {
	data := make(map[string]any)

	// Current values are top-level
	for k, v := range pc.Current {
		data[k] = v
	}

	// Step outputs accessible under "steps"
	data["steps"] = pc.StepOutputs

	// Trigger data accessible under "trigger"
	data["trigger"] = pc.TriggerData

	// Metadata accessible under "meta"
	data["meta"] = pc.Metadata

	return data
}

// dotChainRe matches dot-access chains like .steps.my-step.field
var dotChainRe = regexp.MustCompile(`\.[a-zA-Z_][a-zA-Z0-9_-]*(?:\.[a-zA-Z_][a-zA-Z0-9_-]*)*`)

// stringLiteralRe matches double-quoted and backtick-quoted string literals.
var stringLiteralRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"` + "|`[^`]*`")

// preprocessTemplate rewrites dot-access chains containing hyphens into index
// syntax so that Go's text/template parser does not treat hyphens as minus.
// For example: {{ .steps.my-step.field }} → {{ (index .steps "my-step" "field") }}
func preprocessTemplate(tmplStr string) string {
	// Quick exit: nothing to do if there are no actions or no hyphens.
	if !strings.Contains(tmplStr, "{{") || !strings.Contains(tmplStr, "-") {
		return tmplStr
	}

	var out strings.Builder
	rest := tmplStr

	for {
		openIdx := strings.Index(rest, "{{")
		if openIdx < 0 {
			out.WriteString(rest)
			break
		}
		closeIdx := strings.Index(rest[openIdx:], "}}")
		if closeIdx < 0 {
			out.WriteString(rest)
			break
		}
		closeIdx += openIdx // absolute position

		// Write text before the action.
		out.WriteString(rest[:openIdx])

		action := rest[openIdx+2 : closeIdx] // content between {{ and }}

		// Skip template comments {{/* ... */}}.
		trimmed := strings.TrimSpace(action)
		if strings.HasPrefix(trimmed, "/*") && strings.HasSuffix(trimmed, "*/") {
			out.WriteString("{{")
			out.WriteString(action)
			out.WriteString("}}")
			rest = rest[closeIdx+2:]
			continue
		}

		// Strip string literals to avoid false matches on quoted hyphens.
		var placeholders []string
		stripped := stringLiteralRe.ReplaceAllStringFunc(action, func(m string) string {
			placeholders = append(placeholders, m)
			return "\x00"
		})

		// Rewrite hyphenated dot-chains in the stripped action.
		rewritten := dotChainRe.ReplaceAllStringFunc(stripped, func(chain string) string {
			segments := strings.Split(chain[1:], ".") // drop leading dot
			hasHyphen := false
			for _, seg := range segments {
				if strings.Contains(seg, "-") {
					hasHyphen = true
					break
				}
			}
			if !hasHyphen {
				return chain // no hyphens → leave as-is
			}

			// Find the first hyphenated segment.
			firstHyphen := -1
			for i, seg := range segments {
				if strings.Contains(seg, "-") {
					firstHyphen = i
					break
				}
			}

			// Build the prefix (non-hyphenated dot-access) and the quoted tail.
			var prefix string
			if firstHyphen == 0 {
				prefix = "."
			} else {
				prefix = "." + strings.Join(segments[:firstHyphen], ".")
			}

			var quoted []string
			for _, seg := range segments[firstHyphen:] {
				quoted = append(quoted, `"`+seg+`"`)
			}

			return "(index " + prefix + " " + strings.Join(quoted, " ") + ")"
		})

		// Restore string literals from placeholders.
		var restored string
		if len(placeholders) > 0 {
			phIdx := 0
			var final strings.Builder
			for i := 0; i < len(rewritten); i++ {
				if rewritten[i] == '\x00' && phIdx < len(placeholders) {
					final.WriteString(placeholders[phIdx])
					phIdx++
				} else {
					final.WriteByte(rewritten[i])
				}
			}
			restored = final.String()
		} else {
			restored = rewritten
		}

		out.WriteString("{{")
		out.WriteString(restored)
		out.WriteString("}}")
		rest = rest[closeIdx+2:]
	}

	return out.String()
}

// funcMapWithContext returns the base template functions plus context-aware
// helper functions (step, trigger) that access PipelineContext data directly.
func (te *TemplateEngine) funcMapWithContext(pc *PipelineContext) template.FuncMap {
	fm := templateFuncMap()

	// step accesses step outputs by name and optional nested keys.
	// Usage: {{ step "parse-request" "path_params" "id" }}
	fm["step"] = func(name string, keys ...string) any {
		stepMap, ok := pc.StepOutputs[name]
		if !ok || stepMap == nil {
			return nil
		}
		var val any = stepMap
		for _, key := range keys {
			m, ok := val.(map[string]any)
			if !ok {
				return nil
			}
			val = m[key]
		}
		return val
	}

	// trigger accesses trigger data by nested keys.
	// Usage: {{ trigger "path_params" "id" }}
	fm["trigger"] = func(keys ...string) any {
		if pc.TriggerData == nil {
			return nil
		}
		var val any = map[string]any(pc.TriggerData)
		for _, key := range keys {
			m, ok := val.(map[string]any)
			if !ok {
				return nil
			}
			val = m[key]
		}
		return val
	}

	return fm
}

// Resolve evaluates a template string against a PipelineContext.
// If the string does not contain {{ }}, it is returned as-is.
func (te *TemplateEngine) Resolve(tmplStr string, pc *PipelineContext) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}

	tmplStr = preprocessTemplate(tmplStr)

	t, err := template.New("").Funcs(te.funcMapWithContext(pc)).Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, te.templateData(pc)); err != nil {
		return "", fmt.Errorf("template exec error: %w", err)
	}
	return buf.String(), nil
}

// ResolveMap evaluates all string values in a map that contain {{ }} expressions.
// Non-string values and nested maps/slices are processed recursively.
func (te *TemplateEngine) ResolveMap(data map[string]any, pc *PipelineContext) (map[string]any, error) {
	result := make(map[string]any, len(data))
	for k, v := range data {
		resolved, err := te.resolveValue(v, pc)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		result[k] = resolved
	}
	return result, nil
}

func (te *TemplateEngine) resolveValue(v any, pc *PipelineContext) (any, error) {
	switch val := v.(type) {
	case string:
		return te.Resolve(val, pc)
	case map[string]any:
		return te.ResolveMap(val, pc)
	case []any:
		resolved := make([]any, len(val))
		for i, item := range val {
			r, err := te.resolveValue(item, pc)
			if err != nil {
				return nil, err
			}
			resolved[i] = r
		}
		return resolved, nil
	default:
		return v, nil
	}
}

// timeLayouts maps common Go time constant names to their layout strings.
var timeLayouts = map[string]string{
	"ANSIC":       time.ANSIC,
	"UnixDate":    time.UnixDate,
	"RubyDate":    time.RubyDate,
	"RFC822":      time.RFC822,
	"RFC822Z":     time.RFC822Z,
	"RFC850":      time.RFC850,
	"RFC1123":     time.RFC1123,
	"RFC1123Z":    time.RFC1123Z,
	"RFC3339":     time.RFC3339,
	"RFC3339Nano": time.RFC3339Nano,
	"Kitchen":     time.Kitchen,
	"Stamp":       time.Stamp,
	"StampMilli":  time.StampMilli,
	"StampMicro":  time.StampMicro,
	"StampNano":   time.StampNano,
	"DateTime":    time.DateTime,
	"DateOnly":    time.DateOnly,
	"TimeOnly":    time.TimeOnly,
}

// templateFuncMap returns the function map available in pipeline templates.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		// uuid generates a new UUID v4 string.
		"uuid": func() string {
			return uuid.New().String()
		},
		// uuidv4 generates a new UUID v4 string (alias for uuid).
		"uuidv4": func() string {
			return uuid.New().String()
		},
		// now returns the current UTC time formatted with the given Go time layout
		// string or named constant (e.g. "RFC3339", "2006-01-02").
		// When called with no argument it defaults to RFC3339.
		"now": func(args ...string) string {
			layout := time.RFC3339
			if len(args) > 0 && args[0] != "" {
				if l, ok := timeLayouts[args[0]]; ok {
					layout = l
				} else {
					layout = args[0]
				}
			}
			return time.Now().UTC().Format(layout)
		},
		// lower converts a string to lowercase.
		"lower": strings.ToLower,
		// default returns the fallback value if the primary value is empty.
		"default": func(fallback, val any) any {
			if val == nil {
				return fallback
			}
			if s, ok := val.(string); ok && s == "" {
				return fallback
			}
			return val
		},
		// trimPrefix removes the given prefix from a string if present.
		"trimPrefix": func(prefix, s string) string {
			return strings.TrimPrefix(s, prefix)
		},
		// trimSuffix removes the given suffix from a string if present.
		"trimSuffix": func(suffix, s string) string {
			return strings.TrimSuffix(s, suffix)
		},
		// json marshals a value to a JSON string.
		"json": func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				return "{}"
			}
			return string(b)
		},
	}
}
