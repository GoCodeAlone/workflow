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
// silently degrades to the safe legacy path. Schema validation in
// plugin/sdk.ParseManifest catches unknown values at parse time so
// this branch only sees "v1", "v2", or empty in practice.
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
// keeps the dispatch decision in one place.
func DispatchVersionFor(p ComputePlanVersionDeclarer) string {
	if p == nil {
		return "v1"
	}
	if v := p.ComputePlanVersion(); v == DispatchVersionV2 {
		return DispatchVersionV2
	}
	return "v1"
}
