package external

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
)

// coerceTestMessageDescriptor builds a synthetic MessageDescriptor with one
// field per coercible kind so coerceMapScalars + the mapToTypedAny integration
// path can be exercised without depending on a generated plugin proto.
func coerceTestMessageDescriptor(t *testing.T) (protoreflect.MessageDescriptor, *protoregistry.Types) {
	t.Helper()
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	boolType := descriptorpb.FieldDescriptorProto_TYPE_BOOL
	int32Type := descriptorpb.FieldDescriptorProto_TYPE_INT32
	int64Type := descriptorpb.FieldDescriptorProto_TYPE_INT64
	uint32Type := descriptorpb.FieldDescriptorProto_TYPE_UINT32
	uint64Type := descriptorpb.FieldDescriptorProto_TYPE_UINT64
	floatType := descriptorpb.FieldDescriptorProto_TYPE_FLOAT
	doubleType := descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		{
			Name:    stringPtr("coerce_test.proto"),
			Package: stringPtr("workflow.plugins.test.coerce"),
			Syntax:  stringPtr("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: stringPtr("CoerceTestConfig"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: stringPtr("password_enabled"), JsonName: stringPtr("passwordEnabled"), Number: int32Ptr(1), Label: &label, Type: &boolType},
						{Name: stringPtr("max_retries"), JsonName: stringPtr("maxRetries"), Number: int32Ptr(2), Label: &label, Type: &int32Type},
						{Name: stringPtr("offset_64"), JsonName: stringPtr("offset64"), Number: int32Ptr(3), Label: &label, Type: &int64Type},
						{Name: stringPtr("count_32"), JsonName: stringPtr("count32"), Number: int32Ptr(4), Label: &label, Type: &uint32Type},
						{Name: stringPtr("count_64"), JsonName: stringPtr("count64"), Number: int32Ptr(5), Label: &label, Type: &uint64Type},
						{Name: stringPtr("rate_f32"), JsonName: stringPtr("rateF32"), Number: int32Ptr(6), Label: &label, Type: &floatType},
						{Name: stringPtr("rate_f64"), JsonName: stringPtr("rateF64"), Number: int32Ptr(7), Label: &label, Type: &doubleType},
						{Name: stringPtr("env"), JsonName: stringPtr("env"), Number: int32Ptr(8), Label: &label, Type: &stringType},
					},
				},
			},
		},
	}}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}
	file, err := files.FindFileByPath("coerce_test.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}
	msg := file.Messages().ByName("CoerceTestConfig")
	if msg == nil {
		t.Fatal("CoerceTestConfig not found in file descriptor")
	}
	types := new(protoregistry.Types)
	if err := registerFileMessages(types, file.Messages()); err != nil {
		t.Fatalf("registerFileMessages: %v", err)
	}
	return msg, types
}

func TestCoerceMapScalars_BoolStrings(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	cases := []struct {
		in   string
		want bool
	}{
		{"true", true}, {"false", false},
		{"True", true}, {"FALSE", false},
		{"1", true}, {"0", false},
		{"yes", true}, {"no", false},
		{"on", true}, {"off", false},
		{"  true  ", true},
	}
	for _, tc := range cases {
		out := coerceMapScalars(map[string]any{"password_enabled": tc.in}, msg)
		got, ok := out["password_enabled"].(bool)
		if !ok || got != tc.want {
			t.Errorf("input %q: got %T(%v), want bool(%v)", tc.in, out["password_enabled"], out["password_enabled"], tc.want)
		}
	}
}

func TestCoerceMapScalars_BoolEmptyStringDropped(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	out := coerceMapScalars(map[string]any{"password_enabled": "", "env": "production"}, msg)
	if _, present := out["password_enabled"]; present {
		t.Errorf("empty-string bool should be dropped (proto default), got %v", out["password_enabled"])
	}
	if got := out["env"]; got != "production" {
		t.Errorf("string field should passthrough, got %v", got)
	}
}

func TestCoerceMapScalars_BoolInvalidPassthrough(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	out := coerceMapScalars(map[string]any{"password_enabled": "maybe"}, msg)
	got, ok := out["password_enabled"].(string)
	if !ok || got != "maybe" {
		t.Errorf("invalid bool string should passthrough to surface protojson error, got %T(%v)", out["password_enabled"], out["password_enabled"])
	}
}

func TestCoerceMapScalars_IntStrings(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	out := coerceMapScalars(map[string]any{
		"max_retries": "42",
		"offset_64":   "-9000000000",
		"count_32":    "7",
		"count_64":    "18446744073709551615",
		"rate_f32":    "1.5",
		"rate_f64":    "3.14159",
	}, msg)
	if got, ok := out["max_retries"].(int64); !ok || got != 42 {
		t.Errorf("max_retries: got %T(%v), want int64(42)", out["max_retries"], out["max_retries"])
	}
	if got, ok := out["offset_64"].(int64); !ok || got != -9000000000 {
		t.Errorf("offset_64: got %T(%v), want int64(-9000000000)", out["offset_64"], out["offset_64"])
	}
	if got, ok := out["count_32"].(uint64); !ok || got != 7 {
		t.Errorf("count_32: got %T(%v), want uint64(7)", out["count_32"], out["count_32"])
	}
	if got, ok := out["count_64"].(uint64); !ok || got != 18446744073709551615 {
		t.Errorf("count_64: got %T(%v), want uint64(max)", out["count_64"], out["count_64"])
	}
	if got, ok := out["rate_f32"].(float64); !ok || got != 1.5 {
		t.Errorf("rate_f32: got %T(%v), want float64(1.5)", out["rate_f32"], out["rate_f32"])
	}
	if got, ok := out["rate_f64"].(float64); !ok || got != 3.14159 {
		t.Errorf("rate_f64: got %T(%v), want float64(3.14159)", out["rate_f64"], out["rate_f64"])
	}
}

func TestCoerceMapScalars_NonStringPassthrough(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	in := map[string]any{
		"password_enabled": true,     // already bool
		"max_retries":      int64(5), // already int
		"env":              "prod",
	}
	out := coerceMapScalars(in, msg)
	if !reflect.DeepEqual(out["password_enabled"], true) {
		t.Errorf("non-string bool should passthrough, got %v", out["password_enabled"])
	}
	if !reflect.DeepEqual(out["max_retries"], int64(5)) {
		t.Errorf("non-string int should passthrough, got %v", out["max_retries"])
	}
}

func TestCoerceMapScalars_UnknownFieldPassthrough(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	out := coerceMapScalars(map[string]any{"not_a_field": "false"}, msg)
	if got := out["not_a_field"]; got != "false" {
		t.Errorf("unknown field should passthrough, got %T(%v)", got, got)
	}
}

func TestCoerceMapScalars_JSONNameLookup(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	// Field is declared as password_enabled (proto name) with JSON name
	// passwordEnabled — coercion must work for both keying conventions
	// since callers can populate the map from either YAML (proto name) or
	// JSON (json name).
	out := coerceMapScalars(map[string]any{"passwordEnabled": "true"}, msg)
	if got, ok := out["passwordEnabled"].(bool); !ok || !got {
		t.Errorf("JSON-name keyed bool should coerce, got %T(%v)", out["passwordEnabled"], out["passwordEnabled"])
	}
}

func TestCoerceMapScalars_NilInputs(t *testing.T) {
	if got := coerceMapScalars(nil, nil); got != nil {
		t.Errorf("nil/nil should return nil, got %v", got)
	}
	msg, _ := coerceTestMessageDescriptor(t)
	if got := coerceMapScalars(nil, msg); got != nil {
		t.Errorf("nil values with descriptor should return nil, got %v", got)
	}
	if got := coerceMapScalars(map[string]any{}, msg); got == nil || len(got) != 0 {
		t.Errorf("empty values map should return empty map, got %v", got)
	}
}

func TestCoerceMapScalars_DoesNotMutateInput(t *testing.T) {
	msg, _ := coerceTestMessageDescriptor(t)
	in := map[string]any{"password_enabled": "true", "env": "prod"}
	_ = coerceMapScalars(in, msg)
	if got, ok := in["password_enabled"].(string); !ok || got != "true" {
		t.Errorf("input map should not be mutated, got %T(%v)", in["password_enabled"], in["password_enabled"])
	}
}

// TestMapToTypedAny_CoercesStringBoolBeforeProtojson is the regression test
// for BMW PR 278 image-launch failure: workflow template emits "false" for
// a bool proto field; without coercion the protojson decode fails with
// "invalid value for bool field passwordEnabled".
//
// Round-trip behavior: protojson MarshalOptions{UseProtoNames: true} (see
// typedAnyToMap) omits proto3 default values, so a bool field with the
// false default is absent from the round-trip map. The presence-asserting
// case uses a non-default bool value to exercise the full pipeline.
func TestMapToTypedAny_CoercesStringBoolBeforeProtojson(t *testing.T) {
	_, types := coerceTestMessageDescriptor(t)
	any1, err := mapToTypedAny(
		"workflow.plugins.test.coerce.CoerceTestConfig",
		map[string]any{
			"password_enabled": "true", // non-default so it round-trips
			"max_retries":      "3",
			"rate_f64":         "0.5",
			"env":              "production",
		},
		types,
	)
	if err != nil {
		t.Fatalf("mapToTypedAny returned error on coerced scalars: %v", err)
	}
	if any1 == nil {
		t.Fatal("expected typed Any, got nil")
	}
	// Round-trip back to map to verify decoded field values.
	values, err := typedAnyToMap(any1, "workflow.plugins.test.coerce.CoerceTestConfig", types)
	if err != nil {
		t.Fatalf("typedAnyToMap: %v", err)
	}
	if got, ok := values["password_enabled"].(bool); !ok || !got {
		t.Errorf("password_enabled round-trip: got %T(%v), want bool(true)", values["password_enabled"], values["password_enabled"])
	}
	if got, ok := values["max_retries"].(int); !ok || got != 3 {
		t.Errorf("max_retries round-trip: got %T(%v), want int(3)", values["max_retries"], values["max_retries"])
	}
}

// TestMapToTypedAny_StringFalseAcceptedAsDefault verifies the BMW-specific
// path where the template emits "false" for a bool field whose proto3
// default is also false. Without coercion this errored at decode; with
// coercion the protojson decoder sees the bool literal and accepts it
// (then drops the default-value field on Marshal, which is fine).
func TestMapToTypedAny_StringFalseAcceptedAsDefault(t *testing.T) {
	_, types := coerceTestMessageDescriptor(t)
	if _, err := mapToTypedAny(
		"workflow.plugins.test.coerce.CoerceTestConfig",
		map[string]any{"password_enabled": "false"},
		types,
	); err != nil {
		t.Fatalf("mapToTypedAny('false' for bool) must accept the coerced value: %v", err)
	}
}

// Ensure unused import in test stays referenced (anypb is reachable via
// mapToTypedAny in TestMapToTypedAny_*).
var _ = anypb.Any{}
