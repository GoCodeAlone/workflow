package module

import "strings"

// SensitiveFieldPatterns contains field name substrings that trigger redaction.
// Matching is case-insensitive via strings.Contains on the lowercased field name.
var SensitiveFieldPatterns = []string{
	"secret",
	"password",
	"token",
	"credential",
	"api_key",
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
		if nested, ok := v.(map[string]any); ok {
			out[k] = redactMap(nested, patterns)
		} else {
			out[k] = v
		}
	}
	return out
}

// isSensitiveField returns true when the lowercased field name contains any of
// the patterns and does not have the safe suffix.
func isSensitiveField(name string, patterns []string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, safeFieldSuffix) {
		return false
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
