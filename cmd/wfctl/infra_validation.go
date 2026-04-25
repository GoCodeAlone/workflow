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
// rejects malformed ProviderIDs for strict formats and empty Freeform IDs.
// Unknown formats pass through. Returns a detailed error on
// violation so operators can identify the buggy driver immediately.
func validateOutputProviderID(provider interfaces.IaCProvider, providerType string, r *interfaces.ResourceOutput) error {
	return validateProviderID(provider, providerType, r.Type, r.Name, r.ProviderID)
}

func validateStateProviderID(provider interfaces.IaCProvider, providerType string, r interfaces.ResourceState) error {
	return validateProviderID(provider, providerType, r.Type, r.Name, r.ProviderID)
}

func validateProviderID(provider interfaces.IaCProvider, providerType, resourceType, resourceName, providerID string) error {
	rd, err := provider.ResourceDriver(resourceType)
	if err != nil {
		log.Printf("warn: wfctl: cannot probe ResourceDriver for validation of %s %q: %v", resourceType, resourceName, err)
		return nil
	}
	v, ok := rd.(interfaces.ProviderIDValidator)
	if !ok {
		return nil
	}
	format := v.ProviderIDFormat()
	if format == interfaces.IDFormatUnknown {
		return nil
	}
	if !interfaces.ValidateProviderID(providerID, format) {
		return fmt.Errorf(
			"driver %q returned malformed ProviderID %q for resource %q (type %s); "+
				"expected %s — state not persisted",
			providerType, providerID, resourceName, resourceType, format,
		)
	}
	return nil
}
