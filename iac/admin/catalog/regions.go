// regions.go (T8) hosts the per-provider region catalog used by the
// new-resource form-builder UI when populating the region dropdown for
// enum_dynamic+depends_on=provider fields.
//
// v1 is local-only — the design's IaCProviderRegionLister gRPC service
// extension is filed as a follow-up issue (per scope manifest §Out of
// scope). When a provider plugin lands runtime region listing, swap
// this constant table for the gRPC client.
//
// Refresh cadence: review on every minor upstream provider-plugin
// release. Add the new region codes here; the form-builder picks up
// the change after a host restart.
//
// Last reviewed: 2026-05-27.

package catalog

import "sort"

// RegionCatalog maps provider-type strings (the YAML config
// `provider:` field on iac.provider modules) to their supported
// region codes. The provider-type string comes from
// workflow.plugins.infra.v1.InfraResourceConfig.provider — i.e.
// "digitalocean", "aws", "gcp", "azure", "stub" — NOT the host
// module name.
type RegionCatalog struct {
	byProviderType map[string][]string
}

// NewRegionCatalog returns the v1 local region catalog. Per design
// §FieldSpec Catalog the lists cover the regions surfaced by each
// provider plugin's typed driver as of 2026-05-27. Stub provider is
// included so unit + integration tests have deterministic options.
func NewRegionCatalog() *RegionCatalog {
	return &RegionCatalog{byProviderType: map[string][]string{
		"digitalocean": {
			"nyc1", "nyc3", "sfo3", "ams3", "sgp1",
			"lon1", "fra1", "tor1", "blr1", "syd1",
		},
		"aws": {
			"us-east-1", "us-east-2", "us-west-1", "us-west-2",
			"eu-west-1", "eu-central-1",
			"ap-northeast-1", "ap-southeast-1", "ap-southeast-2",
		},
		"gcp": {
			"us-central1", "us-east1", "us-west1",
			"europe-west1", "asia-east1",
		},
		"azure": {
			"eastus", "westus2", "westeurope", "southeastasia",
		},
		"stub": {
			"test-region-1", "test-region-2",
		},
	}}
}

// For returns a defensive copy of the catalogued regions for the
// given provider type, or nil when the provider is uncatalogued.
// The defensive copy prevents callers from mutating the catalog
// (the slices are otherwise shared across invocations).
func (r *RegionCatalog) For(providerType string) []string {
	if r == nil {
		return nil
	}
	src, ok := r.byProviderType[providerType]
	if !ok {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// Providers returns the sorted list of provider-type keys this
// catalog knows about. Useful for tests + diagnostics; not used by
// the form-builder which iterates AdminProviderSummary entries.
func (r *RegionCatalog) Providers() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.byProviderType))
	for k := range r.byProviderType {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
