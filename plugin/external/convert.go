package external

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/schema"
	"google.golang.org/protobuf/encoding/protojson"
	goproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// mapToStruct converts a Go map to a protobuf Struct.
// Returns nil if the input is nil. Propagates errors from structpb.NewStruct
// (workflow#537) so callers can surface conversion failures rather than
// silently dropping unrepresentable values.
func mapToStruct(m map[string]any) (*structpb.Struct, error) {
	if m == nil {
		return nil, nil
	}
	return structpb.NewStruct(normalizeStructMap(m))
}

func normalizeStructMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = normalizeStructValue(v)
	}
	return out
}

func normalizeStructValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		return normalizeStructMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = normalizeStructValue(item)
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
			out[iter.Key().String()] = normalizeStructValue(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = normalizeStructValue(rv.Index(i).Interface())
		}
		return out
	default:
		return v
	}
}

// structToMap converts a protobuf Struct to a Go map.
// Returns nil if the input is nil.
func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

// stripInternalKeys returns a fresh copy of m with all keys having the "_"
// prefix removed. The engine injects internal keys (e.g., "_config_dir") into
// every module config to support legacy modules that resolve filesystem-
// relative paths. STRICT_PROTO modules declare their schema explicitly via
// protobuf and reject unknown fields at protojson decode time — so engine
// internals must be stripped before mapToTypedAny is called.
//
// Returns nil if m is nil. Copy-on-clean: the caller's original map is not
// mutated; legacy *structpb.Struct paths continue to receive "_config_dir".
//
// The "_" prefix is the reserved namespace for engine internals; STRICT_PROTO
// module proto schemas must not declare fields with this prefix.
func stripInternalKeys(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cleaned := make(map[string]any, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, "_") {
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

func mapToTypedAny(messageName string, values map[string]any, resolver protoregistry.MessageTypeResolver) (*anypb.Any, error) {
	return mapToTypedAnyWithOptions(messageName, values, resolver, false)
}

func mapToTypedAnyKnownFields(messageName string, values map[string]any, resolver protoregistry.MessageTypeResolver) (*anypb.Any, error) {
	return mapToTypedAnyWithOptions(messageName, values, resolver, true)
}

func mapToTypedAnyWithOptions(messageName string, values map[string]any, resolver protoregistry.MessageTypeResolver, filterUnknown bool) (*anypb.Any, error) {
	if messageName == "" {
		return nil, fmt.Errorf("missing protobuf message name")
	}
	msg, err := newMessageByName(messageName, resolver)
	if err != nil {
		return nil, err
	}
	if values != nil {
		if filterUnknown {
			values = filterMapToMessageFields(values, msg.ProtoReflect().Descriptor())
		}
		// Workflow template expansion produces text/template string output
		// even when the underlying config value is a scalar. Engine v0.51.x
		// strict-proto requires protojson.Unmarshal which rejects string-
		// encoded scalars for bool/int/float proto fields. Pre-coerce known
		// string scalars to their declared field types so a template-emitted
		// "false" for a bool field decodes correctly without forcing every
		// plugin to publish a coerce-aware contract or every BMW-style
		// template author to hand-quote in YAML.
		values = coerceMapScalars(values, msg.ProtoReflect().Descriptor())
		raw, err := json.Marshal(values)
		if err != nil {
			return nil, fmt.Errorf("marshal %s input as JSON: %w", messageName, err)
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, msg); err != nil {
			return nil, fmt.Errorf("decode %s input as protobuf JSON: %w", messageName, err)
		}
	}
	typed, err := anypb.New(msg)
	if err != nil {
		return nil, fmt.Errorf("pack %s typed payload: %w", messageName, err)
	}
	return typed, nil
}

// coerceMapScalars returns a fresh map where string values whose paired
// proto field is a scalar (bool / int* / uint* / float* / sfixed* / fixed*)
// have been parsed into the matching Go type. The workflow template engine
// only emits strings, so config values that originate from `{{ config ... }}`
// arrive as strings even when the source default is a bool/int literal. The
// downstream protojson.Unmarshal is strict: it will not accept a string for
// a bool field. Without this pre-pass, every plugin author would have to
// either declare bool fields as string (defeats strict-proto) or every
// BMW-style template would have to be rewritten with provider-specific
// glue. Coercing at the gateway keeps the contract clean.
//
// Coercion rules (all case-insensitive for string parse):
//   - bool field:    "true"/"false"/"1"/"0"/"yes"/"no"/"on"/"off"
//   - int* / sint*:  strconv.ParseInt(value, 10, 64)
//   - uint* / fixed: strconv.ParseUint(value, 10, 64)
//   - float* / dbl:  strconv.ParseFloat(value, 64)
//   - empty string for any scalar field: dropped from the map (proto default)
//   - string field:  passthrough (no change)
//   - enum field:    passthrough (protojson handles "ENUM_NAME" + numeric)
//   - message field: passthrough; map values are not recursed because nested
//     scalar coercion can be applied per-call when those fields are decoded.
//
// Unparseable string values (e.g. "maybe" for a bool field) are passed
// through unchanged so the protojson decoder surfaces the canonical
// "invalid value for <type> field" error path — coercion intentionally
// does not mask user typos.
//
// Copy-on-coerce: the caller's original map is not mutated.
func coerceMapScalars(values map[string]any, descriptor protoreflect.MessageDescriptor) map[string]any {
	if values == nil || descriptor == nil {
		return values
	}
	fields := descriptor.Fields()
	fieldByKey := make(map[string]protoreflect.FieldDescriptor, fields.Len()*3)
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		fieldByKey[string(f.Name())] = f
		fieldByKey[f.JSONName()] = f
		fieldByKey[f.TextName()] = f
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
		field, ok := fieldByKey[k]
		if !ok || field.IsList() || field.IsMap() {
			continue
		}
		s, isString := v.(string)
		if !isString {
			continue
		}
		coerced, dropped, ok := coerceStringToScalar(s, field.Kind())
		if !ok {
			continue
		}
		if dropped {
			delete(out, k)
			continue
		}
		out[k] = coerced
	}
	return out
}

// coerceStringToScalar parses s as a Go value matching kind. Returns
// (value, drop=false, ok=true) on success; (nil, drop=true, ok=true) when the
// input is an empty string and the field is numeric/bool (drop yields proto
// default); (nil, false, false) when kind is not a coercible scalar OR parse
// failed (caller passes string through for protojson to error on cleanly).
func coerceStringToScalar(s string, kind protoreflect.Kind) (any, bool, bool) {
	switch kind {
	case protoreflect.BoolKind:
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return nil, true, true
		}
		switch strings.ToLower(trimmed) {
		case "true", "1", "yes", "on":
			return true, false, true
		case "false", "0", "no", "off":
			return false, false, true
		}
		return nil, false, false
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return nil, true, true
		}
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return nil, false, false
		}
		return n, false, true
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return nil, true, true
		}
		n, err := strconv.ParseUint(trimmed, 10, 64)
		if err != nil {
			return nil, false, false
		}
		return n, false, true
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return nil, true, true
		}
		n, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return nil, false, false
		}
		return n, false, true
	}
	return nil, false, false
}

func filterMapToMessageFields(values map[string]any, descriptor protoreflect.MessageDescriptor) map[string]any {
	if values == nil || descriptor == nil {
		return values
	}
	filtered := make(map[string]any)
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		for _, key := range []string{field.JSONName(), field.TextName(), string(field.Name())} {
			if value, ok := values[key]; ok {
				filtered[string(field.Name())] = value
				break
			}
		}
	}
	return filtered
}

func typedAnyToMap(payload *anypb.Any, messageName string, resolver protoregistry.MessageTypeResolver) (map[string]any, error) {
	if payload == nil {
		return nil, nil
	}
	msg, err := newMessageByName(messageName, resolver)
	if err != nil {
		return nil, err
	}
	if err := payload.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("unpack %s typed payload: %w", messageName, err)
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal %s typed payload as JSON: %w", messageName, err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode %s typed JSON as map: %w", messageName, err)
	}
	normalizeTypedJSONMap(values, msg.ProtoReflect().Descriptor())
	return values, nil
}

func normalizeTypedJSONMap(values map[string]any, descriptor protoreflect.MessageDescriptor) {
	if values == nil || descriptor == nil {
		return
	}
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		key := string(field.Name())
		value, ok := values[key]
		if !ok {
			key = field.JSONName()
			value, ok = values[key]
		}
		if !ok {
			continue
		}
		values[key] = normalizeTypedJSONValue(value, field)
	}
}

func normalizeTypedJSONValue(value any, field protoreflect.FieldDescriptor) any {
	if field.IsList() {
		items, ok := value.([]any)
		if !ok {
			return value
		}
		for i := range items {
			items[i] = normalizeTypedJSONScalar(items[i], field)
		}
		return items
	}
	return normalizeTypedJSONScalar(value, field)
}

func normalizeTypedJSONScalar(value any, field protoreflect.FieldDescriptor) any {
	number, ok := value.(float64)
	if !ok {
		return value
	}
	switch field.Kind() {
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if math.Trunc(number) == number && number <= float64(math.MaxInt) && number >= float64(math.MinInt) {
			return int(number)
		}
	}
	return value
}

func newMessageByName(messageName string, resolver protoregistry.MessageTypeResolver) (goproto.Message, error) {
	if resolver != nil {
		if mt, err := resolver.FindMessageByName(protoreflect.FullName(messageName)); err == nil {
			return mt.New().Interface(), nil
		}
	}
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(messageName))
	if err != nil {
		return nil, fmt.Errorf("generated codec for protobuf message %q not found: %w", messageName, err)
	}
	return mt.New().Interface(), nil
}

// protoSchemaToSchema converts a proto ModuleSchema to the workflow schema type.
func protoSchemaToSchema(ps *proto.ModuleSchema) *schema.ModuleSchema {
	if ps == nil {
		return nil
	}
	ms := &schema.ModuleSchema{
		Type:        ps.Type,
		Label:       ps.Label,
		Category:    ps.Category,
		Description: ps.Description,
	}
	for _, inp := range ps.Inputs {
		ms.Inputs = append(ms.Inputs, schema.ServiceIODef{
			Name:        inp.Name,
			Type:        inp.Type,
			Description: inp.Description,
		})
	}
	for _, out := range ps.Outputs {
		ms.Outputs = append(ms.Outputs, schema.ServiceIODef{
			Name:        out.Name,
			Type:        out.Type,
			Description: out.Description,
		})
	}
	for _, cf := range ps.ConfigFields {
		ms.ConfigFields = append(ms.ConfigFields, schema.ConfigFieldDef{
			Key:          cf.Name,
			Label:        cf.Name,
			Type:         schema.ConfigFieldType(cf.Type),
			Description:  cf.Description,
			DefaultValue: cf.DefaultValue,
			Required:     cf.Required,
			Options:      cf.Options,
		})
	}
	return ms
}
