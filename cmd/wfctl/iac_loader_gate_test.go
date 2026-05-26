package main

import (
	"errors"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestAssertIaCPluginAdvertisesRequiredService_TypedRegistryAccepts asserts
// that a ContractRegistry containing a SERVICE-kind descriptor for
// IaCProviderRequired passes the gate silently. Mirrors the
// post-cutover happy path: DO plugin v1.0.0 registers
// IaCProviderRequired via sdk.RegisterAllIaCProviderServices, then
// GetContractRegistry returns the typed-service descriptor that this
// gate looks for.
func TestAssertIaCPluginAdvertisesRequiredService_TypedRegistryAccepts(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
				ServiceName: iacServiceRequired,
				Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	if err := AssertIaCPluginAdvertisesRequiredService("workflow-plugin-digitalocean", "v1.0.0", registry); err != nil {
		t.Fatalf("expected nil for typed registry; got %v", err)
	}
}

// TestAssertIaCPluginAdvertisesRequiredService_LegacyRegistryRejects asserts
// the gate fires for a legacy plugin whose ContractRegistry advertises
// only Module/Step/Trigger contracts (no IaCProviderRequired service).
// The error MUST: name the plugin + version, include the migration
// instructions, point at the runbook, and wrap errLegacyIaCPlugin.
func TestAssertIaCPluginAdvertisesRequiredService_LegacyRegistryRejects(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:       pb.ContractKind_CONTRACT_KIND_MODULE,
				ModuleType: "do.spaces_bucket",
				Mode:       pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	err := AssertIaCPluginAdvertisesRequiredService("workflow-plugin-digitalocean", "v0.14.2", registry)
	if err == nil {
		t.Fatalf("expected legacy-plugin error; got nil")
	}
	if !IsLegacyIaCPluginErr(err) {
		t.Fatalf("error must wrap errLegacyIaCPlugin; got %v", err)
	}
	for _, want := range []string{
		"workflow-plugin-digitalocean",
		"v0.14.2",
		".wfctl-lock.yaml",
		"docs/runbooks/iac-typed-cutover.md",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got %q", want, err.Error())
		}
	}
}

// TestAssertIaCPluginAdvertisesRequiredService_NilRegistryRejects asserts
// the gate treats a nil registry as legacy (defensive — a plugin that
// fails to return a registry at all is also pre-cutover).
func TestAssertIaCPluginAdvertisesRequiredService_NilRegistryRejects(t *testing.T) {
	err := AssertIaCPluginAdvertisesRequiredService("plugin-x", "v0.0.1", nil)
	if err == nil {
		t.Fatalf("expected error for nil registry")
	}
	if !IsLegacyIaCPluginErr(err) {
		t.Errorf("nil-registry error must wrap errLegacyIaCPlugin")
	}
}

// TestAssertIaCPluginAdvertisesRequiredService_EmptyContractsRejects
// asserts a registry with no contracts is treated as legacy. Catches
// the post-RPC path where GetContractRegistry returns successfully but
// empty (e.g., a plugin that built against typed proto but forgot to
// wire BuildContractRegistry into its ContractProvider hook).
func TestAssertIaCPluginAdvertisesRequiredService_EmptyContractsRejects(t *testing.T) {
	registry := &pb.ContractRegistry{}
	err := AssertIaCPluginAdvertisesRequiredService("plugin-y", "v1.0.0-rc0", registry)
	if err == nil {
		t.Fatalf("expected error for empty contracts slice")
	}
	if !IsLegacyIaCPluginErr(err) {
		t.Errorf("empty-contracts error must wrap errLegacyIaCPlugin")
	}
}

// TestAssertIaCPluginAdvertisesRequiredService_WrongKindRejects asserts
// that a descriptor naming IaCProviderRequired but with the wrong
// CONTRACT_KIND (e.g., MODULE instead of SERVICE) does NOT satisfy the
// gate. Guards against a plugin author copy-pasting a service name
// into the wrong descriptor kind.
func TestAssertIaCPluginAdvertisesRequiredService_WrongKindRejects(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:        pb.ContractKind_CONTRACT_KIND_MODULE,
				ServiceName: iacServiceRequired, // wrong kind
				Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	err := AssertIaCPluginAdvertisesRequiredService("plugin-z", "v0.5.0", registry)
	if err == nil {
		t.Fatalf("expected error: SERVICE-kind required (CONTRACT_KIND_MODULE seen)")
	}
}

// TestAssertIaCPluginAdvertisesRequiredService_EmptyMetadataDefaults
// asserts the error formats unknown plugin metadata gracefully when
// the loader didn't populate name/version (defensive — the gate
// should still surface a reasonable message).
func TestAssertIaCPluginAdvertisesRequiredService_EmptyMetadataDefaults(t *testing.T) {
	err := AssertIaCPluginAdvertisesRequiredService("", "", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "<unknown>") {
		t.Errorf("expected <unknown> placeholder in error; got %q", err.Error())
	}
}

// TestIsLegacyIaCPluginErr_NoFalsePositives asserts the sentinel does
// not match unrelated errors. Critical because dispatch sites use this
// to decide between "exit-with-runbook-message" and "exit with the
// generic plugin-load failure path."
func TestIsLegacyIaCPluginErr_NoFalsePositives(t *testing.T) {
	if IsLegacyIaCPluginErr(nil) {
		t.Errorf("nil should not match")
	}
	if IsLegacyIaCPluginErr(errors.New("some other error")) {
		t.Errorf("unrelated error should not match")
	}
	if IsLegacyIaCPluginErr(errors.New("plugin uses legacy something")) {
		t.Errorf("string-similar error should not match (we wrap a typed sentinel)")
	}
}
