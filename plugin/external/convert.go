package external

import (
	"encoding/json"
	"fmt"

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
// Returns nil if the input is nil.
func mapToStruct(m map[string]any) *structpb.Struct {
	if m == nil {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		// Fall back to empty struct on conversion error
		return &structpb.Struct{}
	}
	return s
}

// structToMap converts a protobuf Struct to a Go map.
// Returns nil if the input is nil.
func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
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
	return values, nil
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
