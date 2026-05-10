package main

import (
	"strings"
	"testing"
)

// TestAuditPluginStrictContracts_IaCServiceMethodsAreNotRequired asserts
// that after the strict-contracts force-cutover, plugin manifests that
// list IaC service methods (e.g., "IaCProvider.EnumerateAll",
// "ResourceDriver.Create") are NOT flagged for missing strict-contract
// descriptors when audit runs with StrictContracts:true. Per Task 19 of
// the strict-contracts force-cutover plan: IaC interfaces are now
// compile-time enforced via Go interface satisfaction
// (sdk.RegisterAllIaCProviderServices) — the manifest-side strict-
// contract advertisement is redundant for those methods.
//
// The audit MUST continue to flag non-IaC service methods that lack a
// descriptor (e.g., "StrictService/Call") so the Module/Step/Trigger
// migration tracker is unaffected.
func TestAuditPluginStrictContracts_IaCServiceMethodsAreNotRequired(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-iac-only", `{
  "name": "workflow-plugin-iac-only",
  "version": "1.0.0",
  "capabilities": {
    "serviceMethods": [
      "IaCProvider.Initialize",
      "IaCProvider.Plan",
      "IaCProvider.Apply",
      "IaCProvider.EnumerateAll",
      "ResourceDriver.Create",
      "ResourceDriver.Read"
    ]
  }
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	for _, finding := range result.Findings {
		if finding.Code == "missing_service_method_contract_descriptor" {
			t.Errorf("audit must NOT flag IaC service methods as needing "+
				"strict-contract descriptors (now compile-time enforced); "+
				"got finding: %+v", finding)
		}
	}
	// ContractCoverage.ServiceMethods.Total must be 0 (every advertised
	// method was IaC, so they all got filtered).
	if result.ContractCoverage.ServiceMethods.Total != 0 {
		t.Errorf("expected ServiceMethods.Total=0 after IaC filter; got %+v",
			result.ContractCoverage.ServiceMethods)
	}
}

// TestAuditPluginStrictContracts_NonIaCServiceMethodsStillRequire asserts
// the IaC filter is narrow: a non-IaC service method (Module/Step/
// Trigger / SecurityScanner / ad-hoc) still gets the missing-descriptor
// finding when no contract is advertised. Guards against the filter
// silently dropping every service method.
func TestAuditPluginStrictContracts_NonIaCServiceMethodsStillRequire(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-mixed", `{
  "name": "workflow-plugin-mixed",
  "version": "1.0.0",
  "capabilities": {
    "serviceMethods": [
      "IaCProvider.Plan",
      "SecurityScanner/Scan"
    ]
  }
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	var nonIaCFinding bool
	for _, finding := range result.Findings {
		if finding.Code != "missing_service_method_contract_descriptor" {
			continue
		}
		if !strings.Contains(finding.Message, "SecurityScanner/Scan") {
			t.Errorf("non-IaC missing-descriptor finding must name the offending method; got %q",
				finding.Message)
		}
		if strings.Contains(finding.Message, "IaCProvider.Plan") {
			t.Errorf("audit must NOT flag IaCProvider.Plan; got %q", finding.Message)
		}
		nonIaCFinding = true
	}
	if !nonIaCFinding {
		t.Errorf("expected missing_service_method_contract_descriptor for "+
			"SecurityScanner/Scan; findings=%v", result.Findings)
	}
	if result.ContractCoverage.ServiceMethods.Total != 1 {
		t.Errorf("expected ServiceMethods.Total=1 (IaC filtered, SecurityScanner/Scan kept); got %+v",
			result.ContractCoverage.ServiceMethods)
	}
}

// TestIsIaCServiceMethod_Cases asserts the classifier accepts every
// IaCProvider.* and ResourceDriver.* shape (with or without trailing
// method names; canonical pkg-prefixed and bare). New IaC service
// methods added in iac.proto should be covered by this matcher.
func TestIsIaCServiceMethod_Cases(t *testing.T) {
	cases := map[string]bool{
		// IaCProvider methods (legacy InvokeService dispatch shape).
		"IaCProvider.Initialize":           true,
		"IaCProvider.Plan":                 true,
		"IaCProvider.Apply":                true,
		"IaCProvider.EnumerateAll":         true,
		"IaCProvider.RepairDirtyMigration": true,
		// ResourceDriver methods (legacy InvokeService dispatch shape).
		"ResourceDriver.Create":        true,
		"ResourceDriver.SensitiveKeys": true,
		"ResourceDriver.Troubleshoot":  true,
		// Typed-proto package-qualified service names (post-cutover).
		"workflow.plugin.external.iac.IaCProviderRequired/Plan":           true,
		"workflow.plugin.external.iac.IaCProviderEnumerator/EnumerateAll": true,
		"workflow.plugin.external.iac.ResourceDriver/Create":              true,
		// Non-IaC service methods that must NOT match.
		"SecurityScanner/Scan":      false,
		"StrictService/Call":        false,
		"PluginService/GetManifest": false,
		"":                          false,
	}
	for in, want := range cases {
		if got := isIaCServiceMethod(in); got != want {
			t.Errorf("isIaCServiceMethod(%q) = %v; want %v", in, got, want)
		}
	}
}
