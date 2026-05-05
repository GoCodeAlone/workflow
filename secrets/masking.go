package secrets

const maskedValue = "(sensitive)"

// DefaultSensitiveKeys returns the default set of output keys considered sensitive.
func DefaultSensitiveKeys() []string {
	return []string{
		"uri", "password", "secret", "token", "connection_string",
		"dsn", "secret_key", "access_key", "private_key", "api_key",
	}
}

// MergeSensitiveKeys combines driver-specific keys with the defaults, deduplicating.
func MergeSensitiveKeys(driverKeys []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, k := range DefaultSensitiveKeys() {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, k)
		}
	}
	for _, k := range driverKeys {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, k)
		}
	}
	return result
}

// MaskSensitiveOutputs returns a copy of outputs with sensitive values replaced by "(sensitive)".
// sensitiveKeys is the merged set of keys to mask.
func MaskSensitiveOutputs(outputs map[string]any, sensitiveKeys []string) map[string]any {
	if len(outputs) == 0 {
		return outputs
	}
	mask := make(map[string]struct{}, len(sensitiveKeys))
	for _, k := range sensitiveKeys {
		mask[k] = struct{}{}
	}
	result := make(map[string]any, len(outputs))
	for k, v := range outputs {
		if _, sensitive := mask[k]; sensitive {
			result[k] = maskedValue
		} else {
			result[k] = v
		}
	}
	return result
}
