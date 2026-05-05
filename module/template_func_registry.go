package module

// TemplateFuncDef describes a single template function available in pipeline templates.
type TemplateFuncDef struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// TemplateFuncDescriptions returns descriptions for all built-in pipeline template functions.
func TemplateFuncDescriptions() []TemplateFuncDef {
	return buildTemplateFuncDefs()
}

func buildTemplateFuncDefs() []TemplateFuncDef {
	return []TemplateFuncDef{
		{
			Name:        "uuid",
			Signature:   "uuid() string",
			Description: "Generates a new random UUID v4 string.",
			Example:     `{{ uuid }}`,
		},
		{
			Name:        "uuidv4",
			Signature:   "uuidv4() string",
			Description: "Generates a new random UUID v4 string. Alias for uuid.",
			Example:     `{{ uuidv4 }}`,
		},
		{
			Name:        "now",
			Signature:   "now(layout ...string) string",
			Description: "Returns the current UTC time formatted with the given Go time layout or named constant (e.g. RFC3339, DateOnly). Defaults to RFC3339 when called with no arguments.",
			Example:     `{{ now "RFC3339" }} or {{ now "2006-01-02" }}`,
		},
		{
			Name:        "lower",
			Signature:   "lower(s string) string",
			Description: "Converts a string to lowercase.",
			Example:     `{{ lower .name }}`,
		},
		{
			Name:        "default",
			Signature:   "default(fallback any, val any) any",
			Description: "Returns fallback when val is nil or an empty string, otherwise returns val.",
			Example:     `{{ default "anonymous" .username }}`,
		},
		{
			Name:        "trimPrefix",
			Signature:   "trimPrefix(prefix string, s string) string",
			Description: "Removes the given prefix from s if present.",
			Example:     `{{ trimPrefix "/api" .path }}`,
		},
		{
			Name:        "trimSuffix",
			Signature:   "trimSuffix(suffix string, s string) string",
			Description: "Removes the given suffix from s if present.",
			Example:     `{{ trimSuffix "/" .path }}`,
		},
		{
			Name:        "json",
			Signature:   "json(v any) string",
			Description: "Marshals a value to a JSON string. Returns '{}' on marshal error.",
			Example:     `{{ json .data }}`,
		},
		{
			Name:        "config",
			Signature:   "config(key string) string",
			Description: "Looks up a value from the global config registry (populated by a config.provider module). Returns an empty string if the key is not found.",
			Example:     `{{ config "db_host" }}`,
		},
		{
			Name:        "upper",
			Signature:   "upper(s string) string",
			Description: "Converts a string to uppercase.",
			Example:     `{{ .name | upper }}`,
		},
		{
			Name:        "title",
			Signature:   "title(s string) string",
			Description: "Converts a string to title case (first letter of each word capitalized).",
			Example:     `{{ "hello world" | title }}`,
		},
		{
			Name:        "replace",
			Signature:   "replace(old string, new string, s string) string",
			Description: "Replaces all occurrences of old with new in s.",
			Example:     `{{ replace "-" "_" .slug }}`,
		},
		{
			Name:        "contains",
			Signature:   "contains(substr string, s string) bool",
			Description: "Reports whether substr is within s.",
			Example:     `{{ contains "admin" .role }}`,
		},
		{
			Name:        "hasPrefix",
			Signature:   "hasPrefix(prefix string, s string) bool",
			Description: "Tests whether s begins with prefix.",
			Example:     `{{ hasPrefix "/api" .path }}`,
		},
		{
			Name:        "hasSuffix",
			Signature:   "hasSuffix(suffix string, s string) bool",
			Description: "Tests whether s ends with suffix.",
			Example:     `{{ hasSuffix ".json" .filename }}`,
		},
		{
			Name:        "split",
			Signature:   "split(sep string, s string) []string",
			Description: "Splits s by sep and returns a string slice.",
			Example:     `{{ $parts := split "," .tags }}{{ index $parts 0 }}`,
		},
		{
			Name:        "join",
			Signature:   "join(sep string, v any) string",
			Description: "Concatenates elements of a slice with sep. Works with []string and []any.",
			Example:     `{{ join ", " .items }}`,
		},
		{
			Name:        "trimSpace",
			Signature:   "trimSpace(s string) string",
			Description: "Removes leading and trailing whitespace from a string.",
			Example:     `{{ .input | trimSpace }}`,
		},
		{
			Name:        "urlEncode",
			Signature:   "urlEncode(s string) string",
			Description: "Percent-encodes a string for safe use in URL query parameters.",
			Example:     `{{ .query | urlEncode }}`,
		},
		{
			Name:        "b64",
			Signature:   "b64(s string) string",
			Description: "Encodes a string as standard base64 (RFC 4648). Typical use: HTTP Basic auth header from an id:secret pair.",
			Example:     `Basic {{ b64 (printf "%s:%s" .client_id .client_secret) }}`,
		},
		{
			Name:        "add",
			Signature:   "add(a any, b any) any",
			Description: "Returns a + b. Returns int64 if both are integer types, float64 otherwise.",
			Example:     `{{ add .offset .limit }}`,
		},
		{
			Name:        "sub",
			Signature:   "sub(a any, b any) any",
			Description: "Returns a - b. Returns int64 if both are integer types, float64 otherwise.",
			Example:     `{{ sub .total .discount }}`,
		},
		{
			Name:        "mul",
			Signature:   "mul(a any, b any) any",
			Description: "Returns a * b. Returns int64 if both are integer types, float64 otherwise.",
			Example:     `{{ mul .quantity .price }}`,
		},
		{
			Name:        "div",
			Signature:   "div(a any, b any) any",
			Description: "Returns a / b as float64. Returns 0 on divide-by-zero.",
			Example:     `{{ div .total .count }}`,
		},
		{
			Name:        "toInt",
			Signature:   "toInt(v any) int64",
			Description: "Converts a value (number or string) to int64.",
			Example:     `{{ toInt .page_size }}`,
		},
		{
			Name:        "toFloat",
			Signature:   "toFloat(v any) float64",
			Description: "Converts a value (number or string) to float64.",
			Example:     `{{ toFloat .price }}`,
		},
		{
			Name:        "toString",
			Signature:   "toString(v any) string",
			Description: "Converts any value to its string representation.",
			Example:     `{{ toString .count }}`,
		},
		{
			Name:        "length",
			Signature:   "length(v any) int",
			Description: "Returns the length of a string, slice, array, or map. Returns 0 for other types.",
			Example:     `{{ length .items }}`,
		},
		{
			Name:        "coalesce",
			Signature:   "coalesce(vals ...any) any",
			Description: "Returns the first non-nil, non-empty-string value from the arguments.",
			Example:     `{{ coalesce .preferred_name .display_name .username }}`,
		},
		{
			Name:        "sum",
			Signature:   "sum(slice any, keys ...string) any",
			Description: "Returns the sum of numeric values in a slice. Accepts an optional key to extract from map elements. Returns int64 when all values are integers, float64 otherwise.",
			Example:     `{{ sum .nums }} or {{ sum .items "amount" }}`,
		},
		{
			Name:        "pluck",
			Signature:   "pluck(slice any, key string) []any",
			Description: "Extracts a single named field from each map element in a slice.",
			Example:     `{{ pluck .users "name" }}`,
		},
		{
			Name:        "flatten",
			Signature:   "flatten(slice any) []any",
			Description: "Flattens one level of nested slices into a single slice.",
			Example:     `{{ flatten .nested }}`,
		},
		{
			Name:        "unique",
			Signature:   "unique(slice any, keys ...string) []any",
			Description: "Deduplicates a slice preserving insertion order. For maps, pass a key to deduplicate by that field's value.",
			Example:     `{{ unique .tags }} or {{ unique .items "id" }}`,
		},
		{
			Name:        "groupBy",
			Signature:   "groupBy(slice any, key string) map[string][]any",
			Description: "Groups slice elements by the value of a named key, returning a map from key value to slice of matching elements.",
			Example:     `{{ groupBy .items "category" }}`,
		},
		{
			Name:        "sortBy",
			Signature:   "sortBy(slice any, key string) []any",
			Description: "Sorts a slice of maps by the value of a named key ascending (stable sort). Numeric values sort numerically, strings lexicographically.",
			Example:     `{{ sortBy .items "price" }}`,
		},
		{
			Name:        "first",
			Signature:   "first(slice any) any",
			Description: "Returns the first element of a slice, or nil if the slice is empty.",
			Example:     `{{ first .items }}`,
		},
		{
			Name:        "last",
			Signature:   "last(slice any) any",
			Description: "Returns the last element of a slice, or nil if the slice is empty.",
			Example:     `{{ last .items }}`,
		},
		{
			Name:        "min",
			Signature:   "min(slice any, keys ...string) any",
			Description: "Returns the minimum numeric value in a slice. Accepts an optional key to extract from map elements. Returns int64 when all values are integers, float64 otherwise.",
			Example:     `{{ min .nums }} or {{ min .items "price" }}`,
		},
		{
			Name:        "max",
			Signature:   "max(slice any, keys ...string) any",
			Description: "Returns the maximum numeric value in a slice. Accepts an optional key to extract from map elements. Returns int64 when all values are integers, float64 otherwise.",
			Example:     `{{ max .nums }} or {{ max .items "price" }}`,
		},
		{
			Name:        "step",
			Signature:   "step(name string, keys ...string) any",
			Description: "Accesses step outputs by step name and optional nested keys. Returns nil if the step does not exist or a key is missing. Context-bound: only available during pipeline execution.",
			Example:     `{{ step "parse-request" "body" "id" }}`,
		},
		{
			Name:        "trigger",
			Signature:   "trigger(keys ...string) any",
			Description: "Accesses trigger data by nested keys. Returns nil if keys do not exist. Context-bound: only available during pipeline execution.",
			Example:     `{{ trigger "path_params" "id" }}`,
		},
	}
}
