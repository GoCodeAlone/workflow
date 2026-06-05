package structpbutil

import "reflect"

// NormalizeMap converts typed string-key maps and typed slices into shapes
// accepted by structpb.NewStruct, preserving values recursively.
func NormalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = NormalizeValue(v)
	}
	return out
}

// NormalizeValue converts typed map/slice values recursively while leaving
// unsupported values in place so structpb.NewStruct can return its own error.
func NormalizeValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		return NormalizeMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = NormalizeValue(item)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = NormalizeValue(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = NormalizeValue(rv.Index(i).Interface())
		}
		return out
	default:
		return v
	}
}
