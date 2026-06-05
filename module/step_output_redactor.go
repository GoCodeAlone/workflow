package module

import "strings"

// SensitiveFieldPatterns contains field name substrings that trigger redaction.
// Matching is case-insensitive via strings.Contains on the lowercased field name.
var SensitiveFieldPatterns = []string{
	"secret",
	"password",
	"token",
	"credential",
	"authorization",
	"cookie",
	"signature",
	"api_key",
	"api-key",
	"apikey",
	"private_key",
	"access_key",
	"backup_code",
	"totp_secret",
	"mfa_secret",
}

// RedactionPlaceholder is substituted for sensitive field values.
const RedactionPlaceholder = "[REDACTED]"

// safeFieldSuffix marks a field as explicitly safe and exempt from redaction.
const safeFieldSuffix = "_display"

// refFieldSuffix marks a field as a reference (a module/resource name, not a
// secret value). A "_ref" key is exempt from redaction ONLY when its sensitive
// match comes from a structural-reference word ("credential"). A key like
// "bearer_token_ref" still redacts, because "token" is a value-bearing secret
// pattern, not a structural reference — the "_ref" suffix must not be a blanket
// bypass for every sensitive pattern.
const refFieldSuffix = "_ref"

// refExemptPatterns are the sensitive patterns that a "_ref" suffix is allowed
// to exempt: words that describe a *reference to* a credential-holding module
// (e.g. "credentials_ref"), not words that name a secret value itself.
var refExemptPatterns = []string{"credential"}

// RedactStepOutput recursively scans output and replaces values of sensitive
// fields with RedactionPlaceholder. Field names are matched case-insensitively
// against SensitiveFieldPatterns. Fields ending with "_display" are never
// redacted regardless of their name. The original map is not modified.
func RedactStepOutput(output map[string]any) map[string]any {
	return redactMap(output, SensitiveFieldPatterns)
}

// RedactStepOutputWithPatterns is like RedactStepOutput but appends
// extraPatterns to the default SensitiveFieldPatterns.
func RedactStepOutputWithPatterns(output map[string]any, extraPatterns []string) map[string]any {
	patterns := make([]string, 0, len(SensitiveFieldPatterns)+len(extraPatterns))
	patterns = append(patterns, SensitiveFieldPatterns...)
	patterns = append(patterns, extraPatterns...)
	return redactMap(output, patterns)
}

func redactMap(m map[string]any, patterns []string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveField(k, patterns) {
			out[k] = RedactionPlaceholder
			continue
		}
		out[k] = redactValue(v, patterns)
	}
	return out
}

func redactValue(v any, patterns []string) any {
	switch val := v.(type) {
	case map[string]any:
		return redactMap(val, patterns)
	case []map[string]any:
		out := make([]map[string]any, len(val))
		for i, item := range val {
			out[i] = redactMap(item, patterns)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = redactValue(item, patterns)
		}
		return out
	default:
		return v
	}
}

// isSensitiveField returns true when the lowercased field name contains any of
// the patterns and is not exempted by a safe/reference suffix.
func isSensitiveField(name string, patterns []string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, safeFieldSuffix) {
		return false
	}
	var matched []string
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return false
	}
	// A "_ref" key is exempt ONLY when every sensitive pattern it matched is a
	// structural-reference word (e.g. "credentials_ref" → "credential"). A key
	// like "bearer_token_ref" still redacts because "token" names a secret
	// value, so "_ref" must not blanket-bypass it.
	if strings.HasSuffix(lower, refFieldSuffix) && allRefExempt(matched) {
		return false
	}
	return true
}

// allRefExempt reports whether every matched pattern is in refExemptPatterns.
func allRefExempt(matched []string) bool {
	for _, m := range matched {
		exempt := false
		for _, e := range refExemptPatterns {
			if m == e {
				exempt = true
				break
			}
		}
		if !exempt {
			return false
		}
	}
	return true
}
