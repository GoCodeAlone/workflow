package handler

import (
	"context"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type providerRegionLister interface {
	ListProviderRegions(context.Context, string) ([]string, error)
}

// ListProviders implements InfraAdminService.ListProviders by
// walking the provided `providers` map (keyed by host YAML module
// name) and emitting one AdminProviderSummary per entry. The
// summary carries the YAML-config provider_type string from the
// caller-supplied providerTypeByModule map (NOT provider.Name() —
// see invariant below), the catalogued region + engine lists for
// that provider type, and the full catalog type list as
// supported_types.
//
// **provider_type MUST come from the YAML config string, not
// provider.Name()** — per spec-reviewer T6 F1 (commit 1ea231fdd) +
// design cycle-5/6 backports:
//   - interfaces.IaCProvider.Name() returns the plugin's DISPLAY
//     name (e.g. "DigitalOcean Provider"). This is operator-facing
//     decoration, not a stable identifier.
//   - The YAML-config provider: field (e.g. "digitalocean") is the
//     stable identifier the region + engine catalogs key against.
//   - The host module (T15) reads each iac.provider module's
//     config at Init time and populates providerTypeByModule
//     keyed by module-name → provider-type-string.
//   - If providerTypeByModule[modName] is missing (e.g. a stale
//     module loaded without re-Init), provider_type stays empty
//     and SupportedRegions + SupportedEngines come back empty —
//     UI degrades gracefully rather than rendering wrong dropdowns.
//
// Signature deviation from design §Handler library (informational —
// not blocking): the design listed
//
//	ListProviders(ctx, providers, regionCat, in)
//
// The proto's AdminProviderSummary requires supported_engines +
// supported_types (so fieldCat + engineCat are needed) AND the F1
// fix requires providerTypeByModule. Final shape is 7 params;
// design line 233 was underspecified.
//
// regions_source is "provider-lister" when the provider advertises and
// successfully serves IaCProviderRegionLister; otherwise it remains the
// literal "local-catalog" fallback.
//
// Per design §Authz: default-deny via the shared authz guard.
func ListProviders(
	ctx context.Context,
	providers map[string]interfaces.IaCProvider, //nolint:revive // reserved for symmetry + future per-provider RPCs (e.g. live capability probe)
	providerTypeByModule map[string]string,
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
		providerType := providerTypeByModule[modName] // may be "" if Init didn't populate
		supportedRegions := regionCat.For(providerType)
		regionsSource := "local-catalog"
		if lister, ok := providers[modName].(providerRegionLister); ok {
			if regions, err := lister.ListProviderRegions(ctx, in.GetEnvName()); err == nil {
				supportedRegions = normalizeProviderRegions(regions)
				regionsSource = "provider-lister"
			}
		}
		summary := &adminpb.AdminProviderSummary{
			ModuleName:       modName,
			ProviderType:     providerType,
			SupportedRegions: supportedRegions,
			SupportedEngines: engineCat.For(providerType),
			SupportedTypes:   append([]string(nil), allTypes...),
			RegionsSource:    regionsSource,
		}
		out.Providers = append(out.Providers, summary)
	}
	return out, nil
}

func normalizeProviderRegions(regions []string) []string {
	out := make([]string, 0, len(regions))
	seen := make(map[string]bool, len(regions))
	for _, region := range regions {
		region = strings.TrimSpace(region)
		if region == "" || seen[region] {
			continue
		}
		seen[region] = true
		out = append(out, region)
	}
	sort.Strings(out)
	return out
}
