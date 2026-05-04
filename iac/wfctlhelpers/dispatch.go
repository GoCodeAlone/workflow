package wfctlhelpers

// ComputePlanVersionDeclarer is the optional interface a loaded
// interfaces.IaCProvider satisfies when wfctl has read its plugin.json
// SDK manifest's iacProvider.computePlanVersion field. wfctl's apply
// path type-asserts this interface to choose between the v1 (legacy
// in-provider Apply) and v2 (wfctlhelpers.ApplyPlan + drift
// postcondition) dispatch paths.
//
// The dispatch contract is rev2/rev3-locked: there is NO
// WFCTL_USE_V2_APPLY env var, NO operator-flippable gate. The v1/v2
// routing is plugin-author-controlled via the manifest field. A
// provider that does not satisfy this interface defaults to v1 (legacy
// dispatch); a provider that returns "v2" routes through ApplyPlan; any
// other return value is treated as "v1" so a typo in the manifest
// silently degrades to the safe legacy path.
//
// NOTE on validation: when the manifest is loaded via plugin/sdk.ParseManifest,
// schema validation rejects unknown values at parse time. However, some
// loader paths in wfctl (e.g. cmd/wfctl/deploy_providers.go's
// findIaCPluginDir) currently use a minimal json.Unmarshal without
// schema validation, so unknown values CAN reach DispatchVersionFor at
// runtime. The default-to-v1 behavior is the safety net for those
// paths — DO NOT rely on the manifest-validation guarantee in callers.
type ComputePlanVersionDeclarer interface {
	ComputePlanVersion() string
}

// DispatchVersionV2 is the manifest value that routes apply through
// wfctlhelpers.ApplyPlan. Exported so callers don't string-literal it
// at every dispatch site.
const DispatchVersionV2 = "v2"

// DispatchVersionFor returns the apply-time dispatch version for p.
// Providers that don't implement ComputePlanVersionDeclarer, or that
// return anything other than "v2", get "v1" (the legacy
// provider.Apply path). Centralizing the type assertion + default
// keeps the dispatch decision in one place — call sites pass the raw
// provider value (typed as interfaces.IaCProvider or any concrete
// provider type) rather than type-asserting at every dispatch site.
//
// Param is `any` rather than interfaces.IaCProvider so this package
// stays import-free of the engine's interfaces package (and so
// non-engine call sites such as tests can pass concrete provider
// stubs without an extra adapter). The contract is identical: pass
// the loaded provider; receive "v1" or "v2".
func DispatchVersionFor(p any) string {
	if p == nil {
		return "v1"
	}
	d, ok := p.(ComputePlanVersionDeclarer)
	if !ok {
		return "v1"
	}
	if v := d.ComputePlanVersion(); v == DispatchVersionV2 {
		return DispatchVersionV2
	}
	return "v1"
}
