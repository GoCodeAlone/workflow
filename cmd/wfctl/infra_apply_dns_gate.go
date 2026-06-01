package main

import (
	"context"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/dns/gate"
	"github.com/GoCodeAlone/workflow/dns/policy"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// dnsGateHook returns a wfctlhelpers.ApplyPlanHooks-compatible
// OnBeforeAction function that enforces DNS policy on infra.dns resources
// during `wfctl infra apply`. Wires the workflow/dns/gate package as a
// FATAL pre-apply gate per design §Phase 3a.
//
// Behavior:
//
//   - Non-infra.dns actions: pass-through (nil error).
//   - WORKFLOW_DNS_OWNER env unset: log a warning and pass-through. Gate
//     cannot be enforced without an owner identity; explicit skip is
//     better than silently breaking applies that haven't yet adopted the
//     policy model.
//   - infra.dns action: build a gate.CachingGate (one TXT-read per zone
//     for the lifetime of this apply), iterate records in
//     action.Resource.Config["records"], and gate-check each
//     (record_name, record_type, owner) tuple. ANY denial aborts the
//     action (FATAL); the rest of the records under that action are not
//     checked because OnBeforeAction itself is fatal and aborts the
//     whole apply per the design's hard-stop semantics.
//
// The hook is constructed per-apply so its CachingGate's memo table is
// scoped to one apply invocation — no cross-apply policy bleed.
func dnsGateHook(provider interfaces.IaCProvider) func(context.Context, interfaces.PlanAction) error {
	owner := os.Getenv("WORKFLOW_DNS_OWNER")
	cachingGate := gate.NewCachingGate()
	return func(ctx context.Context, action interfaces.PlanAction) error {
		if action.Resource.Type != "infra.dns" {
			return nil
		}
		if owner == "" {
			// Surface to stderr so operators see the explicit skip — the
			// alternative (silently allow every action) would mask config
			// errors; the alternative (block every infra.dns action) would
			// break legitimate applies that pre-date Phase 3a.
			fmt.Fprintf(os.Stderr, "warning: WORKFLOW_DNS_OWNER not set; skipping DNS policy gate for %s/%s\n", action.Resource.Type, action.Resource.Name)
			return nil
		}
		zone, _ := action.Resource.Config["domain"].(string)
		if zone == "" {
			// infra.dns ResourceSpec carries the zone in Config["domain"]
			// (per DO/CF/NC/Hover plugin configSchema). No fallback because
			// ResourceSpec has no ProviderID field; ProviderID lives on
			// ResourceState — only available post-apply.
			return fmt.Errorf("dns-gate: action %s has no Config.domain; cannot read policy", action.Resource.Name)
		}
		driver, err := provider.ResourceDriver("infra.dns")
		if err != nil {
			return fmt.Errorf("dns-gate: resolve infra.dns driver for %s: %w", zone, err)
		}
		reader := &gate.DriverReader{Driver: driver, Zone: zone}
		records := extractDNSRecords(action.Resource.Config["records"])
		for _, rec := range records {
			recName, _ := rec["name"].(string)
			recType, _ := rec["type"].(string)
			if recName == "" || recType == "" {
				continue
			}
			if err := cachingGate.Check(ctx, reader, zone, recName, recType, owner); err != nil {
				return fmt.Errorf("dns-gate: zone=%s record=%s/%s owner=%s: %w", zone, recName, recType, owner, err)
			}
		}
		return nil
	}
}

// extractDNSRecords normalises both concrete-type variants of the records
// slice ([]map[string]any and []any-of-map) returned by config parsers.
// Returns nil-safe empty slice when records is missing or has an
// unexpected shape — the gate-check loop then iterates zero times and the
// action passes.
func extractDNSRecords(records any) []map[string]any {
	switch v := records.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if rec, ok := raw.(map[string]any); ok {
				out = append(out, rec)
			}
		}
		return out
	}
	return nil
}

// wireDNSGateIntoHooks attaches the DNS gate as OnBeforeAction on the
// hooks struct returned by statePersistenceHooks. Caller must invoke this
// AFTER constructing the hooks via statePersistenceHooks so the OnBefore
// closure shares the same provider reference.
func wireDNSGateIntoHooks(hooks *wfctlhelpers.ApplyPlanHooks, provider interfaces.IaCProvider) {
	appendOnBeforeActionHook(hooks, dnsGateHook(provider))
}

// Compile-time guard that the policy + gate packages stay in dependency
// reach for this file even if the body changes — catches accidental
// import-path drift early. The `_ = policy.HeritageV1` line references a
// stable constant in the policy package; if policy gets refactored to
// move HeritageV1, this line breaks at compile time.
var _ = policy.HeritageV1
