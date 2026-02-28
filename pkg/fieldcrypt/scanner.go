package fieldcrypt

// ScanAndEncrypt recursively scans a map, encrypting protected fields that have Encryption=true.
// maxDepth limits recursion depth.
func ScanAndEncrypt(data map[string]any, registry *Registry, keyFn func() ([]byte, int, error), maxDepth int) error {
	return scanEncrypt(data, registry, keyFn, 0, maxDepth)
}

func scanEncrypt(data map[string]any, registry *Registry, keyFn func() ([]byte, int, error), depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil
	}
	for k, v := range data {
		switch val := v.(type) {
		case string:
			if pf, ok := registry.GetField(k); ok && pf.Encryption && !IsEncrypted(val) {
				key, version, err := keyFn()
				if err != nil {
					return err
				}
				encrypted, err := Encrypt(val, key, version)
				if err != nil {
					return err
				}
				data[k] = encrypted
			}
		case map[string]any:
			if err := scanEncrypt(val, registry, keyFn, depth+1, maxDepth); err != nil {
				return err
			}
		case []any:
			for _, elem := range val {
				if m, ok := elem.(map[string]any); ok {
					if err := scanEncrypt(m, registry, keyFn, depth+1, maxDepth); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// ScanAndDecrypt recursively scans a map, decrypting epf:-prefixed (and enc::-prefixed) protected fields.
func ScanAndDecrypt(data map[string]any, registry *Registry, keyFn func(version int) ([]byte, error), maxDepth int) error {
	return scanDecrypt(data, registry, keyFn, 0, maxDepth)
}

func scanDecrypt(data map[string]any, registry *Registry, keyFn func(version int) ([]byte, error), depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil
	}
	for k, v := range data {
		switch val := v.(type) {
		case string:
			if registry.IsProtected(k) && IsEncrypted(val) {
				decrypted, err := Decrypt(val, keyFn)
				if err != nil {
					return err
				}
				data[k] = decrypted
			}
		case map[string]any:
			if err := scanDecrypt(val, registry, keyFn, depth+1, maxDepth); err != nil {
				return err
			}
		case []any:
			for _, elem := range val {
				if m, ok := elem.(map[string]any); ok {
					if err := scanDecrypt(m, registry, keyFn, depth+1, maxDepth); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// ScanAndMask returns a deep copy of data with protected fields masked (for logging).
// Does NOT modify the original map.
func ScanAndMask(data map[string]any, registry *Registry, maxDepth int) map[string]any {
	return scanMask(data, registry, 0, maxDepth)
}

func scanMask(data map[string]any, registry *Registry, depth, maxDepth int) map[string]any {
	result := make(map[string]any, len(data))
	for k, v := range data {
		switch val := v.(type) {
		case string:
			if pf, ok := registry.GetField(k); ok {
				result[k] = MaskValue(val, pf.LogBehavior, pf.MaskPattern)
			} else {
				result[k] = val
			}
		case map[string]any:
			if depth < maxDepth {
				result[k] = scanMask(val, registry, depth+1, maxDepth)
			} else {
				result[k] = val
			}
		case []any:
			masked := make([]any, len(val))
			for i, elem := range val {
				if m, ok := elem.(map[string]any); ok && depth < maxDepth {
					masked[i] = scanMask(m, registry, depth+1, maxDepth)
				} else {
					masked[i] = elem
				}
			}
			result[k] = masked
		default:
			result[k] = v
		}
	}
	return result
}
