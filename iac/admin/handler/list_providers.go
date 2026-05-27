package handler

import (
	"context"
	"sort"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ListProviders implements InfraAdminService.ListProviders by
// walking the provided `providers` map (keyed by host YAML module
// name) and emitting one AdminProviderSummary per entry. The
// summary carries the live provider type (from provider.Name()),
// the catalogued region + engine lists for that provider type,
// and the full catalog type list as supported_types.
//
// Signature deviation from design §Handler library (informational —
// not blocking): the design listed
//
//	ListProviders(ctx, providers, regionCat, in)
//
// but the proto's AdminProviderSummary fields supported_engines +
// supported_types require engineCat + fieldCat as parameters.
// Adding them keeps the function pure (no hidden RPC fan-out for
// engine/type lookup); the design's shorter signature was an
// underspecification rather than an intentional minimal surface.
// Spec-reviewer was DM'd at T6 commit time with this rationale.
//
// regions_source is the literal "local-catalog" per design §FieldSpec
// Catalog so consumers can distinguish v1's local lookup from a
// future v1.1 IaCProviderRegionLister gRPC service.
//
// Per design §Authz: default-deny via the shared authz guard.
func ListProviders(
	ctx context.Context,
	providers map[string]interfaces.IaCProvider,
	fieldCat *catalog.FieldSpecCatalog,
	regionCat *catalog.RegionCatalog,
	engineCat *catalog.EngineCatalog,
	in *adminpb.AdminListProvidersInput,
) (*adminpb.AdminListProvidersOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminListProvidersOutput{Error: msg}, nil
	}

	// Sort module names so the output ordering is deterministic.
	// Downstream snapshot tests + the form-builder dropdown order
	// both benefit from a stable iteration order; map iteration is
	// random in Go.
	moduleNames := make([]string, 0, len(providers))
	for name := range providers {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)

	// supported_types is catalog-derived and uniform across providers
	// in v1 (every typed Config can be applied to every iac.provider
	// per the design's FieldSpec table). Cache the sorted list once
	// rather than re-deriving per provider.
	allTypes := fieldCat.AllTypes()

	out := &adminpb.AdminListProvidersOutput{}
	for _, modName := range moduleNames {
		p := providers[modName]
		providerType := ""
		if p != nil {
			providerType = p.Name()
		}
		summary := &adminpb.AdminProviderSummary{
			ModuleName:       modName,
			ProviderType:     providerType,
			SupportedRegions: regionCat.For(providerType),
			SupportedEngines: engineCat.For(providerType),
			SupportedTypes:   append([]string(nil), allTypes...),
			RegionsSource:    "local-catalog",
		}
		out.Providers = append(out.Providers, summary)
	}
	return out, nil
}
