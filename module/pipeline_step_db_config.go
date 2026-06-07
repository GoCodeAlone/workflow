package module

func configStringAlias(cfg map[string]any, canonical string, aliases ...string) string {
	if v, ok := cfg[canonical].(string); ok && v != "" {
		return v
	}
	for _, alias := range aliases {
		if v, ok := cfg[alias].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func configStringListAlias(cfg map[string]any, canonical string, aliases ...string) []string {
	if values, ok := configStringList(cfg[canonical]); ok {
		return values
	}
	for _, alias := range aliases {
		if values, ok := configStringList(cfg[alias]); ok {
			return values
		}
	}
	return nil
}

func configStringList(v any) ([]string, bool) {
	switch list := v.(type) {
	case []any:
		values := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return values, true
	case []string:
		values := make([]string, len(list))
		copy(values, list)
		return values, true
	default:
		return nil, false
	}
}

func normalizeDBMode(mode string) string {
	switch mode {
	case "many":
		return "list"
	case "one":
		return "single"
	default:
		return mode
	}
}
