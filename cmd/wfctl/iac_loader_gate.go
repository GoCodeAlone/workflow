package main

import (
	"errors"
	"fmt"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// iacRequiredServiceName is the fully-qualified name of the typed
// IaCProviderRequired gRPC service emitted by iac.proto. The pre-flight
// gate looks for exactly this string in the plugin's GetContractRegistry
// response.
const iacRequiredServiceName = "workflow.plugin.external.iac.IaCProviderRequired"

// errLegacyIaCPlugin is the typed sentinel returned when a pinned IaC
// plugin does not advertise IaCProviderRequired in its
// ContractRegistry. Wrapped errors that errors.Is on this sentinel
// surface the install-time mitigation step.
//
// The dispatch sites (wfctl deploy, wfctl infra plan/apply) catch this
// and exit with the actionable message documented in
// docs/runbooks/iac-typed-cutover.md.
var errLegacyIaCPlugin = errors.New("iac: plugin uses legacy InvokeService dispatch removed in workflow v1.0.0")

// AssertIaCPluginAdvertisesRequiredService inspects a *pb.ContractRegistry
// (the response from GetContractRegistry) and returns nil iff the plugin
// advertises a CONTRACT_KIND_SERVICE descriptor for
// workflow.plugin.external.iac.IaCProviderRequired.
//
// The error names the offending plugin (pluginName + pluginVersion) and
// points operators at docs/runbooks/iac-typed-cutover.md, plus wraps
// errLegacyIaCPlugin for errors.Is dispatch.
//
// Per Task 18 of the strict-contracts force-cutover plan: workflow
// v1.0.0 refuses to start a deploy if any pinned IaC plugin doesn't
// expose pb.IaCProviderServer registration. The check happens at the
// plugin-loader boundary (after GetContractRegistry succeeds) so the
// failure surfaces with a typed mitigation rather than as a generic
// "method not found" gRPC status at the first IaC RPC.
//
// pluginName + pluginVersion are forwarded to the error message; pass
// the values from the plugin's manifest (Manifest.Name + Version).
// They are advisory — the function still rejects a registry that
// lacks the typed service even when called with empty strings.
func AssertIaCPluginAdvertisesRequiredService(pluginName, pluginVersion string, registry *pb.ContractRegistry) error {
	if !registryAdvertisesIaCRequired(registry) {
		name := pluginName
		if name == "" {
			name = "<unknown>"
		}
		version := pluginVersion
		if version == "" {
			version = "<unknown>"
		}
		return fmt.Errorf(
			"plugin %q v%s uses legacy InvokeService dispatch removed in workflow v1.0.0. "+
				"Migration: edit .wfctl-lock.yaml to pin v1.0.0+, then re-run "+
				"`wfctl plugin install`. See docs/runbooks/iac-typed-cutover.md: %w",
			name, version, errLegacyIaCPlugin,
		)
	}
	return nil
}

// registryAdvertisesIaCRequired returns true iff registry contains a
// CONTRACT_KIND_SERVICE descriptor naming
// workflow.plugin.external.iac.IaCProviderRequired. Treats nil
// registry / nil contracts slice as "not advertised."
func registryAdvertisesIaCRequired(registry *pb.ContractRegistry) bool {
	if registry == nil {
		return false
	}
	for _, c := range registry.Contracts {
		if c == nil {
			continue
		}
		if c.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE && c.ServiceName == iacRequiredServiceName {
			return true
		}
	}
	return false
}

// IsLegacyIaCPluginErr reports whether err signals a failed IaC
// plugin pre-flight gate. Dispatch sites can use this to attach
// runbook-specific exit codes / messages without inspecting the
// error message string.
func IsLegacyIaCPluginErr(err error) bool {
	return errors.Is(err, errLegacyIaCPlugin)
}
