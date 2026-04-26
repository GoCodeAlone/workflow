package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// TestPluginVerifyConfig_JSONUnmarshal verifies that the Verify block in a
// PluginRequirement round-trips through JSON correctly.
func TestPluginVerifyConfig_JSONUnmarshal(t *testing.T) {
	raw := `{
		"name": "supply-chain",
		"version": "0.3.0",
		"verify": {
			"signature": "required",
			"sbom": "allow-missing",
			"vuln_policy": "block-critical"
		}
	}`
	var req config.PluginRequirement
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if req.Verify == nil {
		t.Fatal("Verify should not be nil")
	}
	if req.Verify.Signature != "required" {
		t.Errorf("Signature: got %q, want required", req.Verify.Signature)
	}
	if req.Verify.SBOM != "allow-missing" {
		t.Errorf("SBOM: got %q, want allow-missing", req.Verify.SBOM)
	}
	if req.Verify.VulnPolicy != "block-critical" {
		t.Errorf("VulnPolicy: got %q, want block-critical", req.Verify.VulnPolicy)
	}
}

// TestPluginVerifyConfig_YAMLUnmarshal verifies that the Verify block parses
// correctly from YAML (as found in app.yaml requires.plugins[].verify).
func TestPluginVerifyConfig_YAMLUnmarshal(t *testing.T) {
	raw := `
name: supply-chain
version: "0.3.0"
verify:
  signature: required
  sbom: off
  vuln_policy: warn
`
	var req config.PluginRequirement
	if err := yaml.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if req.Verify == nil {
		t.Fatal("Verify should not be nil after YAML unmarshal")
	}
	if req.Verify.Signature != "required" {
		t.Errorf("Signature: got %q, want required", req.Verify.Signature)
	}
	if req.Verify.SBOM != "off" {
		t.Errorf("SBOM: got %q, want off", req.Verify.SBOM)
	}
	if req.Verify.VulnPolicy != "warn" {
		t.Errorf("VulnPolicy: got %q, want warn", req.Verify.VulnPolicy)
	}
}

// TestPluginVerifyConfig_Nil verifies that a PluginRequirement without a
// verify block has a nil Verify field.
func TestPluginVerifyConfig_Nil(t *testing.T) {
	raw := `{"name":"authz","version":"0.1.0"}`
	var req config.PluginRequirement
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if req.Verify != nil {
		t.Errorf("Verify should be nil when not specified, got %+v", req.Verify)
	}
}

// TestRegistryCapabilities_NewFields verifies that RegistryCapabilities
// accepts BuildHooks, CLICommands, MigrationDrivers, PortIntrospect fields.
func TestRegistryCapabilities_NewFields(t *testing.T) {
	raw := `{
		"buildHooks": [{"event":"post_container_build","priority":500}],
		"cliCommands": [{"name":"supply-chain","description":"Supply chain tools"}],
		"migrationDrivers": ["golang-migrate","goose"],
		"serviceMethods": ["StrictService/Call"],
		"portIntrospect": true
	}`
	var caps RegistryCapabilities
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(caps.BuildHooks) != 1 {
		t.Errorf("BuildHooks: got %d, want 1", len(caps.BuildHooks))
	}
	if caps.BuildHooks[0].Event != "post_container_build" {
		t.Errorf("BuildHooks[0].Event: got %q", caps.BuildHooks[0].Event)
	}
	if len(caps.CLICommands) != 1 {
		t.Errorf("CLICommands: got %d, want 1", len(caps.CLICommands))
	}
	if caps.CLICommands[0].Name != "supply-chain" {
		t.Errorf("CLICommands[0].Name: got %q", caps.CLICommands[0].Name)
	}
	if len(caps.MigrationDrivers) != 2 {
		t.Errorf("MigrationDrivers: got %d, want 2", len(caps.MigrationDrivers))
	}
	if len(caps.ServiceMethods) != 1 {
		t.Errorf("ServiceMethods: got %d, want 1", len(caps.ServiceMethods))
	}
	if !caps.PortIntrospect {
		t.Error("PortIntrospect should be true")
	}
}

// TestInstallVerifyHookEmission verifies that when Verify is configured,
// the install_verify hook is emitted after tarball download and before extraction.
func TestInstallVerifyHookEmission(t *testing.T) {
	var emittedEvent interfaces.HookEvent
	var emittedPayload interfaces.InstallVerifyPayload

	fakeTarball := filepath.Join(t.TempDir(), "plugin.tar.gz")
	if err := os.WriteFile(fakeTarball, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	verify := &config.PluginVerifyConfig{
		Signature:  "required",
		SBOM:       "allow-missing",
		VulnPolicy: "block-critical",
	}

	var hookCalled bool
	hookFn := func(_ context.Context, event interfaces.HookEvent, p interfaces.InstallVerifyPayload) error {
		hookCalled = true
		emittedEvent = event
		emittedPayload = p
		return nil
	}

	emitInstallVerifyHook(context.Background(), fakeTarball, verify, hookFn)

	if !hookCalled {
		t.Fatal("install_verify hook was not called")
	}
	if emittedEvent != interfaces.HookEventInstallVerify {
		t.Errorf("event: got %q, want %q", emittedEvent, interfaces.HookEventInstallVerify)
	}
	if emittedPayload.TarballPath != fakeTarball {
		t.Errorf("TarballPath: got %q, want %q", emittedPayload.TarballPath, fakeTarball)
	}
	if emittedPayload.VulnPolicy != "block-critical" {
		t.Errorf("VulnPolicy: got %q, want block-critical", emittedPayload.VulnPolicy)
	}
}

// TestInstallVerifyHookEmission_AbortOnFailure verifies that a non-nil error
// from the hook function is returned to the caller.
func TestInstallVerifyHookEmission_AbortOnFailure(t *testing.T) {
	fakeTarball := filepath.Join(t.TempDir(), "plugin.tar.gz")
	_ = os.WriteFile(fakeTarball, []byte("x"), 0644)

	hookFn := func(_ context.Context, _ interfaces.HookEvent, _ interfaces.InstallVerifyPayload) error {
		return os.ErrPermission // simulate cosign failure
	}

	err := emitInstallVerifyHook(context.Background(), fakeTarball, &config.PluginVerifyConfig{
		Signature: "required",
	}, hookFn)
	if err == nil {
		t.Error("expected error when hook fails, got nil")
	}
}

// TestInstallVerifyHookEmission_SkipWhenNilVerify verifies that the hook is
// NOT emitted when verify config is nil.
func TestInstallVerifyHookEmission_SkipWhenNilVerify(t *testing.T) {
	called := false
	hookFn := func(_ context.Context, _ interfaces.HookEvent, _ interfaces.InstallVerifyPayload) error {
		called = true
		return nil
	}
	_ = emitInstallVerifyHook(context.Background(), "/tmp/foo.tar.gz", nil, hookFn)
	if called {
		t.Error("hook should not be called when verify is nil")
	}
}
