package schema

import (
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// GenerateConfigFields produces ConfigFieldDef entries from a Go struct's
// field tags. Fields without an `editor` tag are skipped. The `json` tag
// provides the key name.
func GenerateConfigFields(configStruct interface{}) []ConfigFieldDef {
	t := reflect.TypeOf(configStruct)
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var fields []ConfigFieldDef
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		editorTag := f.Tag.Get("editor")
		if editorTag == "" {
			continue
		}
		jsonTag := f.Tag.Get("json")
		key := strings.SplitN(jsonTag, ",", 2)[0]
		if key == "" || key == "-" {
			continue
		}

		field := ConfigFieldDef{Key: key, Label: toLabel(key)}
		parseEditorTag(editorTag, &field)

		// Infer type from Go type if not specified in tag.
		if field.Type == "" {
			field.Type = inferFieldType(f.Type)
		}

		fields = append(fields, field)
	}
	return fields
}

// parseEditorTag parses the comma-separated directives from an `editor` struct tag
// and populates the given ConfigFieldDef.
func parseEditorTag(tag string, field *ConfigFieldDef) {
	for _, part := range strings.Split(tag, ",") {
		kv := strings.SplitN(part, "=", 2)
		switch kv[0] {
		case "type":
			if len(kv) == 2 {
				field.Type = ConfigFieldType(kv[1])
			}
		case "description":
			if len(kv) == 2 {
				field.Description = kv[1]
			}
		case "required":
			field.Required = true
		case "sensitive":
			field.Sensitive = true
		case "options":
			if len(kv) == 2 {
				field.Options = strings.Split(kv[1], "|")
			}
		case "default":
			if len(kv) == 2 {
				field.DefaultValue = parseDefault(kv[1])
			}
		case "placeholder":
			if len(kv) == 2 {
				field.Placeholder = kv[1]
			}
		case "label":
			if len(kv) == 2 {
				field.Label = kv[1]
			}
		case "group":
			if len(kv) == 2 {
				field.Group = kv[1]
			}
		case "arrayItemType":
			if len(kv) == 2 {
				field.ArrayItemType = kv[1]
			}
		case "mapValueType":
			if len(kv) == 2 {
				field.MapValueType = kv[1]
			}
		}
	}
}

// inferFieldType maps a Go reflect.Type to a ConfigFieldType.
func inferFieldType(t reflect.Type) ConfigFieldType {
	switch t.Kind() {
	case reflect.String:
		return FieldTypeString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return FieldTypeNumber
	case reflect.Bool:
		return FieldTypeBool
	case reflect.Slice:
		return FieldTypeArray
	case reflect.Map:
		return FieldTypeMap
	default:
		return FieldTypeString
	}
}

// parseDefault attempts to parse a default value string into a typed Go value.
// Integers and floats are returned as their numeric types; booleans as bool;
// everything else as a plain string.
func parseDefault(s string) any {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return int(i)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// toLabel converts a camelCase or lowercase key into a human-readable label.
// Example: "maxOpenConns" → "Max Open Conns", "driver" → "Driver".
func toLabel(key string) string {
	var words []string
	var current strings.Builder

	runes := []rune(key)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(runes[i-1]) {
			words = append(words, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		rr := []rune(w)
		rr[0] = unicode.ToUpper(rr[0])
		words[i] = string(rr)
	}
	return strings.Join(words, " ")
}
