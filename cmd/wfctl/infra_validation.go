package main

import (
	"fmt"
	"log"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// validateInputProviderIDs iterates a plan's update/delete actions, looks up
// each driver, probes for ProviderIDValidator, and logs a WARN when the
// ProviderID in state does not match the driver's declared format. Input
// validation is soft-warn (not fail): the driver may have a self-heal
// path that recovers from stale state.
func validateInputProviderIDs(provider interfaces.IaCProvider, plan *interfaces.IaCPlan) {
	for i := range plan.Actions {
		act := &plan.Actions[i]
		if act.Action != "update" && act.Action != "delete" {
			continue
		}
		rd, err := provider.ResourceDriver(act.Resource.Type)
		if err != nil {
			continue
		}
		v, ok := rd.(interfaces.ProviderIDValidator)
		if !ok {
			continue
		}
		format := v.ProviderIDFormat()
		if format == interfaces.IDFormatUnknown {
			continue
		}
		// The ProviderID in current state is what the driver will receive; validate it.
		if act.Current == nil {
			continue
		}
		if !interfaces.ValidateProviderID(act.Current.ProviderID, format) {
			log.Printf(
				"warn: wfctl: %s %q has non-conformant ProviderID %q "+
					"(expected %s). Driver will attempt self-heal if supported.",
				act.Resource.Type, act.Resource.Name,
				act.Current.ProviderID, format,
			)
		}
	}
}

// validateOutputProviderID probes the driver for ProviderIDValidator and
// rejects malformed ProviderIDs for strict formats (UUID, DomainName, ARN).
// Freeform and Unknown formats pass through. Returns a detailed error on
// violation so operators can identify the buggy driver immediately.
func validateOutputProviderID(provider interfaces.IaCProvider, providerType string, r *interfaces.ResourceOutput) error {
	rd, err := provider.ResourceDriver(r.Type)
	if err != nil {
		log.Printf("warn: wfctl: cannot probe ResourceDriver for validation of %s %q: %v", r.Type, r.Name, err)
		return nil
	}
	v, ok := rd.(interfaces.ProviderIDValidator)
	if !ok {
		return nil
	}
	format := v.ProviderIDFormat()
	if format == interfaces.IDFormatUnknown || format == interfaces.IDFormatFreeform {
		return nil
	}
	if !interfaces.ValidateProviderID(r.ProviderID, format) {
		return fmt.Errorf(
			"driver %q returned malformed ProviderID %q for resource %q (type %s); "+
				"expected %s — state not persisted",
			providerType, r.ProviderID, r.Name, r.Type, format,
		)
	}
	return nil
}
