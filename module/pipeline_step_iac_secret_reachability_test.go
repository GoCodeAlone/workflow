package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/secrets"
)

// ─── secrets provider stubs for reachability step tests ──────────────────────

// stubSecretsProviderWithAccess implements secrets.Provider + secrets.AccessChecker.
// It simulates a reachable (or unreachable, via checkErr) remote backend.
type stubSecretsProviderWithAccess struct {
	checkErr    error
	checkCalled bool
	checkCount  int
}

func (s *stubSecretsProviderWithAccess) Name() string { return "stub-vault" }
func (s *stubSecretsProviderWithAccess) Get(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubSecretsProviderWithAccess) Set(_ context.Context, _, _ string) error { return nil }
func (s *stubSecretsProviderWithAccess) Delete(_ context.Context, _ string) error { return nil }
func (s *stubSecretsProviderWithAccess) List(_ context.Context) ([]string, error) { return nil, nil }
func (s *stubSecretsProviderWithAccess) CheckAccess(_ context.Context) error {
	s.checkCalled = true
	s.checkCount++
	return s.checkErr
}

// stubSecretsProviderNoAccess implements secrets.Provider only (no AccessChecker).
type stubSecretsProviderNoAccess struct{}

func (s *stubSecretsProviderNoAccess) Name() string { return "stub-no-access" }
func (s *stubSecretsProviderNoAccess) Get(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubSecretsProviderNoAccess) Set(_ context.Context, _, _ string) error { return nil }
func (s *stubSecretsProviderNoAccess) Delete(_ context.Context, _ string) error { return nil }
func (s *stubSecretsProviderNoAccess) List(_ context.Context) ([]string, error) { return nil, nil }

// stubSecretsModuleAccessor wraps a secrets.Provider behind a Provider() accessor,
// mirroring how SecretsVaultModule / SecretsAWSModule work in production.
type stubSecretsModuleAccessor struct {
	underlying secrets.Provider
}

func (m *stubSecretsModuleAccessor) Provider() secrets.Provider { return m.underlying }

// compile-time assertions
var _ secrets.Provider = (*stubSecretsProviderWithAccess)(nil)
var _ secrets.AccessChecker = (*stubSecretsProviderWithAccess)(nil)
var _ secrets.Provider = (*stubSecretsProviderNoAccess)(nil)

// ─── TestSecretReachabilityStep ───────────────────────────────────────────────

// TestSecretReachabilityStep_VaultWithAccess verifies case (a):
// specs with a vault secret:// ref + stub vault provider whose CheckAccess→nil
// + execEnv "remote" → all_reachable true.
func TestSecretReachabilityStep_VaultWithAccess(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderWithAccess{checkErr: nil}
	// Register via Provider() accessor (mirrors production SecretsVaultModule wiring).
	if err := app.RegisterService("my-vault", &stubSecretsModuleAccessor{underlying: stub}); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-vault",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "my-db",
				"type": "infra.database",
				"config": map[string]any{
					"password": "secret://vault/my-db-password",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// The AccessChecker probe must have been consulted (remote backend path).
	if !stub.checkCalled {
		t.Error("expected CheckAccess to be called on the vault provider")
	}

	allReachable, ok := result.Output["all_reachable"].(bool)
	if !ok {
		t.Fatalf("expected bool all_reachable, got %T: %v", result.Output["all_reachable"], result.Output["all_reachable"])
	}
	if !allReachable {
		t.Errorf("expected all_reachable=true, got false")
	}

	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 1 {
		t.Errorf("expected 1 secret entry, got %d", len(secretsList))
	}
	if secretsList[0]["ref"] != "secret://vault/my-db-password" {
		t.Errorf("expected ref=secret://vault/my-db-password, got %v", secretsList[0]["ref"])
	}
	if secretsList[0]["reachable"] != true {
		t.Errorf("expected reachable=true for secret entry, got %v", secretsList[0]["reachable"])
	}
}

// TestSecretReachabilityStep_GitHubProvider verifies case (b):
// a github-backed provider → all_reachable false with the write-only reason.
func TestSecretReachabilityStep_GitHubProvider(t *testing.T) {
	app := module.NewMockApplication()
	t.Setenv("WORKFLOW_TEST_GH_TOKEN2", "ghp_fake_token_step_test")
	ghProvider, err := secrets.NewGitHubSecretsProvider("owner/repo", "WORKFLOW_TEST_GH_TOKEN2")
	if err != nil {
		t.Fatalf("NewGitHubSecretsProvider: %v", err)
	}
	if err := app.RegisterService("my-gh", &stubSecretsModuleAccessor{underlying: ghProvider}); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-gh",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "deploy-key",
				"type": "infra.secret",
				"config": map[string]any{
					"token": "secret://gh/DEPLOY_TOKEN",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	allReachable, _ := result.Output["all_reachable"].(bool)
	if allReachable {
		t.Error("expected all_reachable=false for github-backed provider")
	}

	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 1 {
		t.Errorf("expected 1 secret entry, got %d", len(secretsList))
	}
	reason, _ := secretsList[0]["reason"].(string)
	if !containsString(reason, "write-only") {
		t.Errorf("expected reason to contain 'write-only', got %q", reason)
	}
}

// TestSecretReachabilityStep_VaultNoAccessChecker_Remote verifies case (c):
// a vault provider without AccessChecker + execEnv "remote" → all_reachable false (fail-safe).
func TestSecretReachabilityStep_VaultNoAccessChecker_Remote(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderNoAccess{}
	if err := app.RegisterService("my-vault-noaccess", &stubSecretsModuleAccessor{underlying: stub}); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-vault-noaccess",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "infra-key",
				"type": "infra.database",
				"config": map[string]any{
					"key": "secret://unknown/backend-key",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	allReachable, _ := result.Output["all_reachable"].(bool)
	if allReachable {
		t.Error("expected all_reachable=false for fail-safe (no AccessChecker + remote env)")
	}

	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 1 {
		t.Errorf("expected 1 secret entry, got %d", len(secretsList))
	}
	reason, _ := secretsList[0]["reason"].(string)
	if !containsString(reason, "reachability unknown") {
		t.Errorf("expected reason to contain 'reachability unknown', got %q", reason)
	}
}

// TestSecretReachabilityStep_NoSecretRefs verifies case (d):
// specs with NO secret refs → all_reachable true, empty list.
func TestSecretReachabilityStep_NoSecretRefs(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderWithAccess{checkErr: nil}
	if err := app.RegisterService("my-vault", &stubSecretsModuleAccessor{underlying: stub}); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-vault",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "plain-resource",
				"type": "infra.database",
				"config": map[string]any{
					"size":   "small",
					"region": "us-east-1",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	allReachable, ok := result.Output["all_reachable"].(bool)
	if !ok {
		t.Fatalf("expected bool all_reachable, got %T", result.Output["all_reachable"])
	}
	if !allReachable {
		t.Error("expected all_reachable=true when no secret refs present")
	}

	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 0 {
		t.Errorf("expected empty secrets list, got %d entries", len(secretsList))
	}
}

// TestSecretReachabilityStep_MultipleRefs_SameProvider verifies that multiple
// distinct secret:// refs in specs are all reported individually but the provider
// check fires only once (provider-level verdict).
func TestSecretReachabilityStep_MultipleRefs_SameProvider(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderWithAccess{checkErr: errors.New("unreachable vault")}
	if err := app.RegisterService("my-vault", &stubSecretsModuleAccessor{underlying: stub}); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-vault",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "db",
				"type": "infra.database",
				"config": map[string]any{
					"password": "secret://vault/db-pass",
					"username": "secret://vault/db-user",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	allReachable, _ := result.Output["all_reachable"].(bool)
	if allReachable {
		t.Error("expected all_reachable=false when vault is unreachable")
	}

	// The verdict is provider-level: CheckAccess must fire EXACTLY ONCE even
	// though two distinct refs are reported.
	if stub.checkCount != 1 {
		t.Errorf("expected CheckAccess called exactly once (provider-level verdict), got %d", stub.checkCount)
	}

	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 2 {
		t.Errorf("expected 2 secret entries (one per distinct ref), got %d", len(secretsList))
	}
	for _, entry := range secretsList {
		if entry["reachable"] != false {
			t.Errorf("expected all entries unreachable when vault is down, got %v", entry["reachable"])
		}
	}
}

// TestSecretReachabilityStep_Factory_RequiresProvider verifies factory validation.
func TestSecretReachabilityStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCSecretReachabilityStepFactory()
	_, err := factory("reach-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected factory error when 'provider' missing")
	}
	if !containsString(err.Error(), "provider") {
		t.Errorf("expected error to mention 'provider', got: %v", err)
	}
}

// TestSecretReachabilityStep_DirectProvider verifies that the step works when
// the service directly implements secrets.Provider (not via Provider() accessor).
func TestSecretReachabilityStep_DirectProvider(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderWithAccess{checkErr: nil}
	// Register the provider directly (not wrapped).
	if err := app.RegisterService("direct-provider", stub); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "direct-provider",
		"exec_env": "remote",
		"specs": []any{
			map[string]any{
				"name": "my-service",
				"type": "infra.service",
				"config": map[string]any{
					"api_key": "secret://direct/api-key",
				},
			},
		},
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	allReachable, _ := result.Output["all_reachable"].(bool)
	if !allReachable {
		t.Error("expected all_reachable=true for direct provider with CheckAccess→nil")
	}
}

// TestSecretReachabilityStep_SpecsFromContext verifies the dynamic specs_from
// path: specs are resolved from the pipeline context at Execute time, and their
// secret:// refs are collected and checked.
func TestSecretReachabilityStep_SpecsFromContext(t *testing.T) {
	app := module.NewMockApplication()
	stub := &stubSecretsProviderWithAccess{checkErr: nil}
	if err := app.RegisterService("my-vault", &stubSecretsModuleAccessor{underlying: stub}); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCSecretReachabilityStepFactory()
	step, err := factory("reach-step", map[string]any{
		"provider":   "my-vault",
		"exec_env":   "remote",
		"specs_from": "steps.parse-request.body.specs",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"parse-request": {
				"body": map[string]any{
					"specs": []any{
						map[string]any{
							"name": "vault-db",
							"type": "infra.database",
							"config": map[string]any{
								"password": "secret://vault/dynamic-pass",
							},
						},
					},
				},
			},
		},
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !stub.checkCalled {
		t.Error("expected CheckAccess to be called for specs_from path")
	}
	allReachable, _ := result.Output["all_reachable"].(bool)
	if !allReachable {
		t.Error("expected all_reachable=true for reachable vault via specs_from")
	}
	secretsList, ok := result.Output["secrets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any secrets, got %T", result.Output["secrets"])
	}
	if len(secretsList) != 1 || secretsList[0]["ref"] != "secret://vault/dynamic-pass" {
		t.Errorf("expected one ref secret://vault/dynamic-pass from specs_from, got %#v", secretsList)
	}
}

// TestSecretReachabilityStep_Factory_SpecsAndSpecsFromMutualExclusion verifies
// that setting both 'specs' and 'specs_from' is rejected at factory time.
func TestSecretReachabilityStep_Factory_SpecsAndSpecsFromMutualExclusion(t *testing.T) {
	factory := module.NewIaCSecretReachabilityStepFactory()
	_, err := factory("reach-step", map[string]any{
		"provider":   "my-vault",
		"specs_from": "steps.parse-request.body.specs",
		"specs": []any{
			map[string]any{"name": "x", "type": "infra.database"},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected factory error when both specs and specs_from are set")
	}
	if !containsString(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error, got: %v", err)
	}
}
