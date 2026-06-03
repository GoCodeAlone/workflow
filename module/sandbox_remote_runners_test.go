package module

import (
	"context"
	"testing"
)

func TestNewSandboxRemoteRunnersModule_Empty(t *testing.T) {
	m := NewSandboxRemoteRunnersModule("runners", map[string]any{})
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if _, ok := m.registry.Get("anything"); ok {
		t.Error("expected empty registry to return false for any lookup")
	}
}

func TestNewSandboxRemoteRunnersModule_ParsesRunners(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":    "prod-runner",
				"address": "agent.prod.example.com:50051",
				"token":   "secret://runner/prod-token",
			},
			map[string]any{
				"name":    "staging-runner",
				"address": "agent.staging.example.com:50051",
				"token":   "staging-literal-token",
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("my-runners", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec, ok := m.registry.Get("prod-runner")
	if !ok {
		t.Fatal("expected prod-runner to be found")
	}
	if spec.Address != "agent.prod.example.com:50051" {
		t.Errorf("Address: got %q, want %q", spec.Address, "agent.prod.example.com:50051")
	}
	if spec.Token != "secret://runner/prod-token" {
		t.Errorf("Token: got %q, want %q", spec.Token, "secret://runner/prod-token")
	}

	spec2, ok := m.registry.Get("staging-runner")
	if !ok {
		t.Fatal("expected staging-runner to be found")
	}
	if spec2.Address != "agent.staging.example.com:50051" {
		t.Errorf("Address: got %q, want %q", spec2.Address, "agent.staging.example.com:50051")
	}

	// Non-existent name.
	if _, ok := m.registry.Get("missing"); ok {
		t.Error("expected missing name to return false")
	}
}

func TestNewSandboxRemoteRunnersModule_MissingAddress_Error(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":  "no-address",
				"token": "tok",
				// address intentionally absent
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("bad", cfg)
	if err := m.Init(nil); err == nil {
		t.Error("expected error for runner with missing address, got nil")
	}
}

// TestNewSandboxRemoteRunnersModule_ReservedName_Error verifies that a runner
// named with a reserved exec_env value (empty/local-docker/ephemeral) is rejected
// at Init time — otherwise it would be silently unreachable.
func TestNewSandboxRemoteRunnersModule_ReservedName_Error(t *testing.T) {
	for _, name := range []string{"", "local-docker", "ephemeral"} {
		cfg := map[string]any{
			"remote_runners": []any{
				map[string]any{
					"name":    name,
					"address": "agent.example.com:50051",
					"token":   "tok",
				},
			},
		}
		m := NewSandboxRemoteRunnersModule("rn", cfg)
		if err := m.Init(nil); err == nil {
			t.Errorf("reserved name %q: expected Init error, got nil", name)
		}
	}
}

// TestNewSandboxRemoteRunnersModule_DuplicateName_Error verifies a duplicate
// runner name is rejected rather than silently overwriting the first.
func TestNewSandboxRemoteRunnersModule_DuplicateName_Error(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{"name": "dup", "address": "a:1", "token": "t1"},
			map[string]any{"name": "dup", "address": "b:2", "token": "t2"},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err == nil {
		t.Error("expected error for duplicate runner name, got nil")
	}
}

// TestNewSandboxRemoteRunnersModule_NoAuthNoTLS_Error verifies a runner with
// neither a token nor TLS is rejected unless allow_insecure is set.
func TestNewSandboxRemoteRunnersModule_NoAuthNoTLS_Error(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":    "naked",
				"address": "agent.example.com:50051",
				// no token, no tls, no allow_insecure
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err == nil {
		t.Error("expected error for no-auth-no-tls runner, got nil")
	}
}

// TestNewSandboxRemoteRunnersModule_NoAuthNoTLS_AllowInsecure_OK verifies the
// explicit allow_insecure opt-in permits a no-auth-no-tls runner.
func TestNewSandboxRemoteRunnersModule_NoAuthNoTLS_AllowInsecure_OK(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":           "dev-runner",
				"address":        "localhost:50051",
				"allow_insecure": true,
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("allow_insecure should permit no-auth-no-tls: %v", err)
	}
	spec, ok := m.registry.Get("dev-runner")
	if !ok {
		t.Fatal("expected dev-runner registered")
	}
	if !spec.AllowInsecure {
		t.Error("expected AllowInsecure=true")
	}
}

func TestSandboxRemoteRunnersModule_ProvidesServices(t *testing.T) {
	m := NewSandboxRemoteRunnersModule("runners", map[string]any{})
	_ = m.Init(nil)
	svcProviders := m.ProvidesServices()
	if len(svcProviders) == 0 {
		t.Fatal("expected at least one service provider")
	}
	found := false
	for _, sp := range svcProviders {
		if sp.Name == SandboxRemoteRunnerServiceName {
			found = true
		}
	}
	if !found {
		t.Errorf("expected service %q in ProvidesServices, got %v", SandboxRemoteRunnerServiceName, svcProviders)
	}
}

func TestSandboxRemoteRunnersModule_StartStop(t *testing.T) {
	m := NewSandboxRemoteRunnersModule("runners", map[string]any{})
	_ = m.Init(nil)
	if err := m.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestNewSandboxRemoteRunnersModule_ParsesTLS(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":    "secure-runner",
				"address": "agent.secure.example.com:50051",
				"tls": map[string]any{
					"cert": "/etc/certs/client.crt",
					"key":  "/etc/certs/client.key",
					"ca":   "/etc/certs/ca.crt",
				},
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("tls-runners", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	spec, ok := m.registry.Get("secure-runner")
	if !ok {
		t.Fatal("expected secure-runner")
	}
	if spec.TLS == nil {
		t.Fatal("expected non-nil TLS spec")
	}
	if spec.TLS.Cert != "/etc/certs/client.crt" {
		t.Errorf("TLS.Cert: got %q", spec.TLS.Cert)
	}
	if spec.TLS.Key != "/etc/certs/client.key" {
		t.Errorf("TLS.Key: got %q", spec.TLS.Key)
	}
	if spec.TLS.CA != "/etc/certs/ca.crt" {
		t.Errorf("TLS.CA: got %q", spec.TLS.CA)
	}
}

// TestNewSandboxRemoteRunnersModule_ParsesSecretsProvider verifies the
// secrets_provider config key is captured and exposed on the registry.
func TestNewSandboxRemoteRunnersModule_ParsesSecretsProvider(t *testing.T) {
	cfg := map[string]any{
		"secrets_provider": "my-vault",
		"remote_runners": []any{
			map[string]any{"name": "r1", "address": "a:1", "token": "secret://x"},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := m.registry.SecretsProvider(); got != "my-vault" {
		t.Errorf("SecretsProvider: got %q, want %q", got, "my-vault")
	}
}

func TestRemoteRunnerRegistry_NilSafe(t *testing.T) {
	var r *RemoteRunnerRegistry
	if _, ok := r.Get("anything"); ok {
		t.Error("nil registry Get should return false")
	}
	if r.SecretsProvider() != "" {
		t.Error("nil registry SecretsProvider should return empty")
	}
}
