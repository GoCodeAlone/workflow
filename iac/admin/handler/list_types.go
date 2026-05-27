package handler

import (
	"context"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ListResourceTypes implements InfraAdminService.ListResourceTypes by
// walking the FieldSpecCatalog and emitting one
// AdminResourceTypeMetadata per registered type. Each metadata
// entry carries the catalog's full FieldSpec list so the new-resource
// form-builder UI can render the right inputs without an extra RPC
// per type. Per plan §Task 6 + design §Handler library.
//
// The providers parameter is reserved for symmetry with the other
// handler functions; v1 of this endpoint does not filter types by
// live providers (the FieldSpecCatalog is the authoritative type
// list and assumes every registered type is supportable by every
// known provider — see TestListProviders_PopulatesRegionsAndEngines
// AndTypes for the cross-task assumption).
//
// Per design §Authz: default-deny via the shared authz guard.
func ListResourceTypes(
	ctx context.Context,
	fieldCat *catalog.FieldSpecCatalog,
	providers map[string]interfaces.IaCProvider, //nolint:revive // reserved for symmetry + future use
	in *adminpb.AdminListResourceTypesInput,
) (*adminpb.AdminListResourceTypesOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminListResourceTypesOutput{Error: msg}, nil
	}

	out := &adminpb.AdminListResourceTypesOutput{}
	for _, typeName := range fieldCat.AllTypes() {
		fields, ok := fieldCat.Get(typeName)
		if !ok {
			continue
		}
		out.Types = append(out.Types, &adminpb.AdminResourceTypeMetadata{
			Type:               typeName,
			ConfigMessageFqn:   typeNameToConfigFQN(typeName),
			Fields:             projectFieldSpecs(fields),
			SupportedProviders: nil, // v1: empty = "any catalogued provider"; see godoc on this function.
			Description:        "",  // populated by future catalog enhancement.
		})
	}
	return out, nil
}

// projectFieldSpecs converts the host-side FieldSpec slice into the
// wire-typed AdminFieldSpec slice field-for-field. Single-sourced
// here so the projection cannot drift across handlers.
func projectFieldSpecs(in []catalog.FieldSpec) []*adminpb.AdminFieldSpec {
	out := make([]*adminpb.AdminFieldSpec, 0, len(in))
	for i := range in {
		f := in[i]
		out = append(out, &adminpb.AdminFieldSpec{
			Name:           f.Name,
			Label:          f.Label,
			Kind:           f.Kind,
			Required:       f.Required,
			EnumValues:     append([]string(nil), f.EnumValues...),
			EnumSource:     f.EnumSource,
			Description:    f.Description,
			DefaultValue:   f.DefaultValue,
			Sensitive:      f.Sensitive,
			ElementKind:    f.ElementKind,
			MinCount:       f.MinCount,
			MaxCount:       f.MaxCount,
			DependsOnField: f.DependsOnField,
		})
	}
	return out
}

// typeNameToConfigFQN maps a catalog type name (e.g. "infra.vpc")
// to its fully-qualified proto message name in workflow-plugin-infra/
// internal/contracts/infra.proto (e.g.
// "workflow.plugin.infra.v1.VPCConfig"). Used by the
// AdminResourceTypeMetadata.config_message_fqn field so cross-language
// consumers can correlate against the vendored proto descriptor.
//
// The mapping is purely a string transform: strip "infra." prefix,
// snake_case → PascalCase, append "Config". Centralised here so
// future per-type FQN overrides can be added in one place.
func typeNameToConfigFQN(typeName string) string {
	const prefix = "workflow.plugin.infra.v1."
	const suffix = "Config"
	const trim = "infra."
	if len(typeName) <= len(trim) || typeName[:len(trim)] != trim {
		return ""
	}
	short := typeName[len(trim):]
	// snake_case → PascalCase
	var b []byte
	upperNext := true
	for i := 0; i < len(short); i++ {
		c := short[i]
		if c == '_' {
			upperNext = true
			continue
		}
		if upperNext && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b = append(b, c)
		upperNext = false
	}
	return prefix + string(b) + suffix
}
