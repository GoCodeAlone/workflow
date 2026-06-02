// Package applydispatch re-exports wfctlhelpers.ApplyPlanWithHooks and its
// companion ApplyPlanHooks type so packages that cannot directly import
// wfctlhelpers (specifically module/, which would create a cycle because
// wfctlhelpers/state.go imports module/) can still use the v2 IaC apply
// dispatch path.
//
// This package contains no logic of its own: it is a thin re-export shim.
// All implementation lives in wfctlhelpers.
package applydispatch

import (
	"context"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ApplyPlanHooks re-exports wfctlhelpers.ApplyPlanHooks so callers of this
// package don't need to import wfctlhelpers directly.
type ApplyPlanHooks = wfctlhelpers.ApplyPlanHooks

// ApplyPlanWithHooks dispatches each plan action to the matching
// ResourceDriver on the provider. It is a direct delegate to
// wfctlhelpers.ApplyPlanWithHooks — see that function's documentation for
// the full contract (best-effort per-action errors, context cancellation,
// OnPlanComplete semantics, etc.).
func ApplyPlanWithHooks(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan, hooks ApplyPlanHooks) (*interfaces.ApplyResult, error) {
	return wfctlhelpers.ApplyPlanWithHooks(ctx, p, plan, hooks)
}
