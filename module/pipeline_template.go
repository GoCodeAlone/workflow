package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// toFloat64 converts any numeric type (or numeric string) to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// isIntType returns true if the value is an integer type.
func isIntType(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64:
		return true
	default:
		return false
	}
}

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

// dotChainRe matches dot-access chains like .steps.my-step.field.
// Hyphens are intentionally allowed within identifier segments so that
// hyphenated step names and fields (e.g. .steps.my-step.field) are
// treated as a single chain. This means ambiguous cases like ".x-1"
// are interpreted as a hyphenated identifier ("x-1") rather than as
// subtraction ".x - 1" when applying the auto-fix rewrite.
var dotChainRe = regexp.MustCompile(`\.[a-zA-Z_][a-zA-Z0-9_-]*(?:\.[a-zA-Z_][a-zA-Z0-9_-]*)*`)

// stringLiteralRe matches double-quoted and backtick-quoted string literals.
// Go templates only support double-quoted and backtick strings (not single-quoted),
// so single quotes are intentionally not handled here.
// Note: Go's regexp package uses RE2 (linear-time matching), so there is no risk
// of catastrophic backtracking / ReDoS with this pattern.
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

		// Skip pure template comments {{/* ... */}}. Only actions whose entire
		// content (after trimming) is a block comment are skipped. Mixed actions
		// like {{ x /* comment */ y }} are not skipped since they contain code.
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
		const placeholderSentinel = "\x00<TMPL_PLACEHOLDER>"
		stripped := stringLiteralRe.ReplaceAllStringFunc(action, func(m string) string {
			placeholders = append(placeholders, m)
			return placeholderSentinel
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

		// Restore string literals from placeholders using strings.Index for O(n) scanning.
		var restored string
		if len(placeholders) > 0 {
			phIdx := 0
			var final strings.Builder
			remaining := rewritten
			for {
				idx := strings.Index(remaining, placeholderSentinel)
				if idx < 0 {
					final.WriteString(remaining)
					break
				}
				final.WriteString(remaining[:idx])
				if phIdx < len(placeholders) {
					final.WriteString(placeholders[phIdx])
					phIdx++
				}
				remaining = remaining[idx+len(placeholderSentinel):]
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
	// Returns nil if the step doesn't exist, a key is missing, or an
	// intermediate value is not a map (consistent with missingkey=zero).
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

// toAnySlice converts any slice type to []any using reflect. Returns nil for non-slices.
func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = rv.Index(i).Interface()
	}
	return result
}

// extractField extracts a value from an item. If keys is provided and item is a map,
// returns map[key]. Otherwise returns item itself.
func extractField(item any, keys []string) any {
	if len(keys) > 0 {
		if m, ok := item.(map[string]any); ok {
			return m[keys[0]]
		}
	}
	return item
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
		// config looks up a value from the global config registry (populated by
		// a config.provider module). Returns an empty string if the key is not found.
		"config": func(key string) string {
			if v, ok := GetConfigRegistry().Get(key); ok {
				return v
			}
			return ""
		},

		// --- String functions ---

		// upper converts a string to uppercase.
		"upper": strings.ToUpper,
		// title converts a string to title case (first letter of each word capitalized).
		"title": func(s string) string {
			words := strings.Fields(s)
			for i, w := range words {
				if len(w) > 0 {
					words[i] = strings.ToUpper(w[:1]) + w[1:]
				}
			}
			return strings.Join(words, " ")
		},
		// replace replaces all occurrences of old with new in s.
		"replace": func(old, new_, s string) string { return strings.ReplaceAll(s, old, new_) },
		// contains reports whether substr is within s.
		"contains": func(substr, s string) bool { return strings.Contains(s, substr) },
		// hasPrefix tests whether s begins with prefix.
		"hasPrefix": func(prefix, s string) bool { return strings.HasPrefix(s, prefix) },
		// hasSuffix tests whether s ends with suffix.
		"hasSuffix": func(suffix, s string) bool { return strings.HasSuffix(s, suffix) },
		// split splits s by sep and returns a slice.
		"split": func(sep, s string) []string { return strings.Split(s, sep) },
		// join concatenates elements of a slice with sep.
		"join": func(sep string, v any) string {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Slice {
				return fmt.Sprintf("%v", v)
			}
			parts := make([]string, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
			}
			return strings.Join(parts, sep)
		},
		// trimSpace removes leading and trailing whitespace.
		"trimSpace": strings.TrimSpace,
		// urlEncode percent-encodes a string for use in URLs.
		"urlEncode": url.QueryEscape,

		// --- Math functions ---

		// add returns a + b. Returns int if both are ints, float64 otherwise.
		"add": func(a, b any) any {
			if isIntType(a) && isIntType(b) {
				return int64(toFloat64(a)) + int64(toFloat64(b))
			}
			return toFloat64(a) + toFloat64(b)
		},
		// sub returns a - b. Returns int if both are ints, float64 otherwise.
		"sub": func(a, b any) any {
			if isIntType(a) && isIntType(b) {
				return int64(toFloat64(a)) - int64(toFloat64(b))
			}
			return toFloat64(a) - toFloat64(b)
		},
		// mul returns a * b. Returns int if both are ints, float64 otherwise.
		"mul": func(a, b any) any {
			if isIntType(a) && isIntType(b) {
				return int64(toFloat64(a)) * int64(toFloat64(b))
			}
			return toFloat64(a) * toFloat64(b)
		},
		// div returns a / b as float64. Returns 0 on divide-by-zero.
		"div": func(a, b any) any {
			fb := toFloat64(b)
			if fb == 0 {
				return float64(0)
			}
			return toFloat64(a) / fb
		},

		// --- Type/Utility functions ---

		// toInt converts a value to int64.
		"toInt": func(v any) int64 { return int64(toFloat64(v)) },
		// toFloat converts a value to float64.
		"toFloat": toFloat64,
		// toString converts a value to its string representation.
		"toString": func(v any) string { return fmt.Sprintf("%v", v) },
		// length returns the length of a string, slice, array, or map. Returns 0 for other types.
		"length": func(v any) int {
			rv := reflect.ValueOf(v)
			switch rv.Kind() {
			case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
				return rv.Len()
			default:
				return 0
			}
		},
		// coalesce returns the first non-nil, non-empty-string value.
		"coalesce": func(vals ...any) any {
			for _, v := range vals {
				if v == nil {
					continue
				}
				if s, ok := v.(string); ok && s == "" {
					continue
				}
				return v
			}
			return nil
		},

		// --- Collection functions ---
		// These functions operate on slices ([]any) with optional KEY for map elements.

		// sum returns the sum of numeric values in a slice. O(n) single pass.
		// Usage: {{ sum .nums }} or {{ sum .items "amount" }}
		"sum": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if items == nil {
				return float64(0)
			}
			total := float64(0)
			allInt := true
			for _, item := range items {
				v := extractField(item, keys)
				if !isIntType(v) {
					allInt = false
				}
				total += toFloat64(v)
			}
			if allInt {
				return int64(total)
			}
			return total
		},
		// pluck extracts a single field from each map in a slice. O(n).
		// Usage: {{ pluck .users "name" }}
		"pluck": func(slice any, key string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			result := make([]any, 0, len(items))
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					result = append(result, m[key])
				}
			}
			return result
		},
		// flatten flattens one level of nested slices. O(n×m).
		// Usage: {{ flatten .nested }}
		"flatten": func(slice any) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			var result []any
			for _, item := range items {
				if inner := toAnySlice(item); inner != nil {
					result = append(result, inner...)
				} else {
					result = append(result, item)
				}
			}
			return result
		},
		// unique deduplicates a slice preserving insertion order. O(n).
		// For maps: {{ unique .items "id" }} deduplicates by key value.
		// For scalars: {{ unique .tags }}
		"unique": func(slice any, keys ...string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			seen := make(map[string]bool)
			var result []any
			for _, item := range items {
				v := extractField(item, keys)
				key := fmt.Sprintf("%v", v)
				if !seen[key] {
					seen[key] = true
					result = append(result, item)
				}
			}
			return result
		},
		// groupBy groups slice elements by a key value. O(n).
		// Usage: {{ groupBy .items "category" }} → map[string][]any
		"groupBy": func(slice any, key string) map[string][]any {
			items := toAnySlice(slice)
			if items == nil {
				return map[string][]any{}
			}
			groups := make(map[string][]any)
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					k := fmt.Sprintf("%v", m[key])
					groups[k] = append(groups[k], item)
				}
			}
			return groups
		},
		// sortBy sorts a slice of maps by a key value ascending. O(n log n) stable sort.
		// Usage: {{ sortBy .items "price" }}
		"sortBy": func(slice any, key string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			sorted := make([]any, len(items))
			copy(sorted, items)
			sort.SliceStable(sorted, func(i, j int) bool {
				vi := extractField(sorted[i], []string{key})
				vj := extractField(sorted[j], []string{key})
				return toFloat64(vi) < toFloat64(vj)
			})
			return sorted
		},
		// first returns the first element of a slice. O(1).
		"first": func(slice any) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			return items[0]
		},
		// last returns the last element of a slice. O(1).
		"last": func(slice any) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			return items[len(items)-1]
		},
		// min returns the minimum numeric value in a slice. O(n) single pass.
		// Usage: {{ min .nums }} or {{ min .items "price" }}
		"min": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			minVal := toFloat64(extractField(items[0], keys))
			allInt := isIntType(extractField(items[0], keys))
			for _, item := range items[1:] {
				v := extractField(item, keys)
				f := toFloat64(v)
				if !isIntType(v) {
					allInt = false
				}
				if f < minVal {
					minVal = f
				}
			}
			if allInt {
				return int64(minVal)
			}
			return minVal
		},
		// max returns the maximum numeric value in a slice. O(n) single pass.
		// Usage: {{ max .nums }} or {{ max .items "price" }}
		"max": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			maxVal := toFloat64(extractField(items[0], keys))
			allInt := isIntType(extractField(items[0], keys))
			for _, item := range items[1:] {
				v := extractField(item, keys)
				f := toFloat64(v)
				if !isIntType(v) {
					allInt = false
				}
				if f > maxVal {
					maxVal = f
				}
			}
			if allInt {
				return int64(maxVal)
			}
			return maxVal
		},
	}
}
