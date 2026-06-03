package module

import (
	"context"
	"strings"
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
	// Both runners carry a token, so they declare TLS (a token over no-TLS is
	// rejected at Init unless allow_insecure). Here we use allow_insecure to keep
	// the fixture about parsing rather than TLS file loading.
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":           "prod-runner",
				"address":        "agent.prod.example.com:50051",
				"token":          "secret://runner/prod-token",
				"allow_insecure": true,
			},
			map[string]any{
				"name":           "staging-runner",
				"address":        "agent.staging.example.com:50051",
				"token":          "staging-literal-token",
				"allow_insecure": true,
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
	// Both specs are otherwise valid (allow_insecure permits token-over-no-TLS),
	// isolating the duplicate-name failure.
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{"name": "dup", "address": "a:1", "token": "t1", "allow_insecure": true},
			map[string]any{"name": "dup", "address": "b:2", "token": "t2", "allow_insecure": true},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	err := m.Init(nil)
	if err == nil {
		t.Fatal("expected error for duplicate runner name, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-name error, got: %v", err)
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
			map[string]any{"name": "r1", "address": "a:1", "token": "secret://x", "allow_insecure": true},
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

// TestNewSandboxRemoteRunnersModule_TokenNoTLS_Error verifies a runner that sets
// a token but no TLS is rejected at Init (token would travel in cleartext)
// unless allow_insecure is set.
func TestNewSandboxRemoteRunnersModule_TokenNoTLS_Error(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":    "tok-no-tls",
				"address": "agent.example.com:50051",
				"token":   "some-token",
				// no tls, no allow_insecure
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	err := m.Init(nil)
	if err == nil {
		t.Fatal("expected error for token-without-TLS runner, got nil")
	}
	if !strings.Contains(err.Error(), "cleartext") {
		t.Errorf("expected cleartext-token error, got: %v", err)
	}
}

// TestNewSandboxRemoteRunnersModule_TokenNoTLS_AllowInsecure_OK verifies the
// allow_insecure opt-in permits a token-over-no-TLS runner.
func TestNewSandboxRemoteRunnersModule_TokenNoTLS_AllowInsecure_OK(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":           "tok-insecure",
				"address":        "localhost:50051",
				"token":          "some-token",
				"allow_insecure": true,
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("allow_insecure should permit token-over-no-TLS: %v", err)
	}
	if _, ok := m.registry.Get("tok-insecure"); !ok {
		t.Error("expected tok-insecure registered")
	}
}

// TestNewSandboxRemoteRunnersModule_TLSCertWithoutKey_Error verifies that a TLS
// spec with only one of cert/key is rejected at Init (both-or-neither).
func TestNewSandboxRemoteRunnersModule_TLSCertWithoutKey_Error(t *testing.T) {
	for _, tc := range []struct {
		name string
		tls  map[string]any
	}{
		{"cert-only", map[string]any{"cert": "/c.pem"}},
		{"key-only", map[string]any{"key": "/k.pem"}},
	} {
		cfg := map[string]any{
			"remote_runners": []any{
				map[string]any{
					"name":    "partial-tls",
					"address": "agent.example.com:50051",
					"tls":     tc.tls,
				},
			},
		}
		m := NewSandboxRemoteRunnersModule("rn", cfg)
		if err := m.Init(nil); err == nil {
			t.Errorf("%s: expected Init error for partial cert/key, got nil", tc.name)
		}
	}
}

// TestNewSandboxRemoteRunnersModule_CAOnly_OK verifies a TLS spec with only a CA
// (server verification, no client cert) is accepted at Init.
func TestNewSandboxRemoteRunnersModule_CAOnly_OK(t *testing.T) {
	cfg := map[string]any{
		"remote_runners": []any{
			map[string]any{
				"name":    "ca-only",
				"address": "agent.example.com:50051",
				"tls":     map[string]any{"ca": "/ca.pem"},
			},
		},
	}
	m := NewSandboxRemoteRunnersModule("rn", cfg)
	if err := m.Init(nil); err != nil {
		t.Fatalf("CA-only TLS should be accepted at Init: %v", err)
	}
	if _, ok := m.registry.Get("ca-only"); !ok {
		t.Error("expected ca-only registered")
	}
}

// TestBuildTLSConfig_CertKeyMismatch_Error verifies buildTLSConfig rejects a
// lone cert or key (both-or-neither), rather than silently dropping client auth.
func TestBuildTLSConfig_CertKeyMismatch_Error(t *testing.T) {
	if _, err := buildTLSConfig("/cert.pem", "", ""); err == nil {
		t.Error("expected error for cert without key, got nil")
	}
	if _, err := buildTLSConfig("", "/key.pem", ""); err == nil {
		t.Error("expected error for key without cert, got nil")
	}
}

// TestBuildTLSConfig_NeitherCertNorKey_OK verifies buildTLSConfig succeeds with
// no client cert (e.g. server-verification-only via CA-less config).
func TestBuildTLSConfig_NeitherCertNorKey_OK(t *testing.T) {
	cfg, err := buildTLSConfig("", "", "")
	if err != nil {
		t.Fatalf("buildTLSConfig with no cert/key/ca: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if len(cfg.Certificates) != 0 {
		t.Error("expected no client certificates")
	}
}
