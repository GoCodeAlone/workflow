package wfctlhelpers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ValidateAllowReplaceProtected gates dispatch on the per-resource
// `protected: true` annotation. It walks every replace or delete
// action in plan and aggregates ALL blockers (resources protected and
// not in `allow`) into a single error before returning, so the
// operator sees the full set of names in one apply attempt and can
// authorize them with one round-trip via the pre-formatted
// --allow-replace=<csv> value.
//
// Format (W-6/T6.2):
//
//	plan would require destructive action on N protected resource(s):
//	  <name1> (replace)
//	  <name2> (delete)
//	  ...
//	to authorize, re-run with:
//	  --allow-replace=<name1>,<name2>,...
//
// Both names and the csv preserve plan-action declaration order so
// the output is deterministic across runs.
//
// `protected: true` is sourced from PlanAction.Resource.Config for
// replace actions (where Resource carries the desired spec) and from
// PlanAction.Current.AppliedConfig for delete actions (where
// platform.differ leaves Resource.Config empty and the protected
// status is preserved on the previously-applied state).
//
// Promoted from cmd/wfctl in W-7/T7.9 so the iac/conformance suite
// can exercise the gate contract without importing package main. The
// cmd/wfctl validateAllowReplaceProtected wrapper now delegates here;
// behavior and error format are byte-identical with the pre-W-7
// implementation, so all existing tests in cmd/wfctl continue to
// pass without modification.
func ValidateAllowReplaceProtected(plan interfaces.IaCPlan, allow map[string]struct{}) error {
	type blocker struct {
		name   string
		action string
	}
	var blockers []blocker
	for i := range plan.Actions {
		a := &plan.Actions[i]
		if a.Action != "replace" && a.Action != "delete" {
			continue
		}
		if !planActionIsProtected(a) {
			continue
		}
		if _, ok := allow[a.Resource.Name]; ok {
			continue
		}
		blockers = append(blockers, blocker{name: a.Resource.Name, action: a.Action})
	}
	if len(blockers) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "plan would require destructive action on %d protected resource(s):", len(blockers))
	names := make([]string, 0, len(blockers))
	for _, blk := range blockers {
		fmt.Fprintf(&b, "\n  %s (%s)", blk.name, blk.action)
		names = append(names, blk.name)
	}
	fmt.Fprintf(&b, "\nto authorize, re-run with:\n  --allow-replace=%s", strings.Join(names, ","))
	return errors.New(b.String())
}

// planActionIsProtected reports whether the action targets a resource
// annotated `protected: true`. Replace actions carry the desired
// Config on Resource; delete actions (built by platform.differ from
// current state) carry the protected flag on Current.AppliedConfig.
// Returning true if either source declares protected covers both
// classes.
//
// Kept package-private: callers should reach the gate via
// ValidateAllowReplaceProtected so the error-format contract stays
// owned by one function.
func planActionIsProtected(a *interfaces.PlanAction) bool {
	if a.Resource.Config != nil {
		if p, ok := a.Resource.Config["protected"].(bool); ok && p {
			return true
		}
	}
	if a.Current != nil && a.Current.AppliedConfig != nil {
		if p, ok := a.Current.AppliedConfig["protected"].(bool); ok && p {
			return true
		}
	}
	return false
}
