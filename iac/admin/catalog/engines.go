// engines.go (T8) hosts the per-provider database/cache engine
// catalog used by the new-resource form-builder for enum_dynamic +
// EnumSource="engines" fields (infra.database.engine, etc.) that
// depend on the chosen provider.
//
// Like the region catalog, v1 is local-only. Same refresh cadence —
// review on every minor provider-plugin release.
//
// Last reviewed: 2026-05-27.

package catalog

import "sort"

// EngineCatalog maps provider-type strings to their supported
// database engines. Cache engines are handled separately via a fixed
// catalog entry (Kind="enum" with EnumValues=[redis, memcached,
// valkey]) — see fields.go infra.cache.engine — because the engine
// matrix there is uniform across providers in v1.
//
// The provider-type string convention matches RegionCatalog: comes
// from the iac.provider module's `provider:` config field
// ("digitalocean", "aws", "gcp", "azure", "stub").
type EngineCatalog struct {
	byProviderType map[string][]string
}

// NewEngineCatalog returns the v1 local engine catalog. Coverage
// reflects the typed drivers shipped by each cloud provider plugin
// as of 2026-05-27. AWS adds dynamodb + aurora atop the common
// postgres/mysql/mongodb/redis set per the design table.
func NewEngineCatalog() *EngineCatalog {
	return &EngineCatalog{byProviderType: map[string][]string{
		"digitalocean": {"postgres", "mysql", "mongodb", "redis"},
		"aws":          {"postgres", "mysql", "mongodb", "redis", "dynamodb", "aurora"},
		"gcp":          {"postgres", "mysql", "redis", "spanner"},
		"azure":        {"postgres", "mysql", "redis", "cosmos"},
		"stub":         {"postgres"},
	}}
}

// For returns a defensive copy of catalogued engines for the given
// provider type, or nil when uncatalogued. Defensive copy parallels
// RegionCatalog.For semantics.
func (e *EngineCatalog) For(providerType string) []string {
	if e == nil {
		return nil
	}
	src, ok := e.byProviderType[providerType]
	if !ok {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// Providers returns the sorted list of catalogued provider-type
// keys. Symmetric with RegionCatalog.Providers.
func (e *EngineCatalog) Providers() []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.byProviderType))
	for k := range e.byProviderType {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
