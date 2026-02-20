package external

import (
	"github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/schema"
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
