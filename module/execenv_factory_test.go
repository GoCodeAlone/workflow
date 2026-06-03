package module

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/sandbox"
	"github.com/GoCodeAlone/workflow/secrets"
)

// TestExecEnvFactory_DefaultLocalDocker verifies that an empty execEnv or
// "local-docker" both resolve to a local Docker runner (non-nil SandboxRunner).
func TestExecEnvFactory_DefaultLocalDocker(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}

	for _, execEnv := range []string{"", "local-docker"} {
		runner, err := resolveSandboxRunner(context.Background(), nil, execEnv, cfg, "", "")
		if err != nil {
			// Runner creation uses the Docker client env (DOCKER_HOST/TLS); a
			// failure here is a Docker-availability issue, not an exec_env-routing
			// regression. Skip rather than flake (matches sandbox/docker_test.go).
			t.Skipf("execEnv=%q: docker client unavailable: %v", execEnv, err)
		}
		if runner == nil {
			t.Errorf("execEnv=%q: expected non-nil runner", execEnv)
			continue
		}
		_ = runner.Close()
	}
}

// TestExecEnvFactory_UnknownExecEnv_Error verifies that unknown exec_env values
// return a clear error rather than silently falling through.
//
// As of PR8, exec_env values other than "" / "local-docker" / "ephemeral" are
// treated as named remote runner names. Without an application context (nil app),
// they all return a "no application context" error.
//
// As of PR9, "ephemeral" is fully wired: with a nil app it returns a clear
// "requires an application context" error (not "deferred to PR9").
func TestExecEnvFactory_UnknownExecEnv_Error(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}

	tests := []struct {
		execEnv     string
		errContains string
	}{
		// "ephemeral" now returns a clear "no application context" error (PR9 wired).
		{"ephemeral", "application context"},
		// Named runner names fail with "no application context" when app is nil.
		{"remote", "not configured"},
		{"nope", "not configured"},
		{"argo", "not configured"},
	}

	for _, tt := range tests {
		runner, err := resolveSandboxRunner(context.Background(), nil, tt.execEnv, cfg, "", "")
		if err == nil {
			t.Errorf("execEnv=%q: expected error, got nil runner=%v", tt.execEnv, runner)
			if runner != nil {
				_ = runner.Close()
			}
			continue
		}
		if !strings.Contains(err.Error(), tt.errContains) {
			t.Errorf("execEnv=%q: expected error to contain %q, got: %v", tt.execEnv, tt.errContains, err)
		}
	}
}

// TestSandboxExec_ExecEnvAbsent_Unchanged verifies that a SandboxExecStep
// constructed without exec_env still uses the factory path and produces a
// local runner (identical behaviour to before this PR).
func TestSandboxExec_ExecEnvAbsent_Unchanged(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("check-exec-env", map[string]any{
		"command": []any{"echo", "hello"},
		// exec_env intentionally absent
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)

	// execEnv should be the zero value — the factory path will default to local-docker.
	if s.execEnv != "" {
		t.Errorf("expected empty execEnv for absent config key, got %q", s.execEnv)
	}

	// Confirm the factory resolves it to a local runner without error.
	cfg := s.buildSandboxConfig()
	runner, err := resolveSandboxRunner(context.Background(), s.app, s.execEnv, cfg, s.argoModule, s.provider)
	if err != nil {
		t.Skipf("resolveSandboxRunner with empty execEnv: docker client unavailable: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner for empty execEnv")
	}
	_ = runner.Close()
}

// TestSandboxExec_ExecEnvLocalDocker_ExplicitlySet verifies that setting
// exec_env: local-docker explicitly is accepted and behaves identically.
func TestSandboxExec_ExecEnvLocalDocker_ExplicitlySet(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("explicit-local", map[string]any{
		"command":  []any{"echo", "hello"},
		"exec_env": "local-docker",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.execEnv != "local-docker" {
		t.Errorf("expected execEnv=local-docker, got %q", s.execEnv)
	}

	cfg := s.buildSandboxConfig()
	runner, err := resolveSandboxRunner(context.Background(), s.app, s.execEnv, cfg, s.argoModule, s.provider)
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	_ = runner.Close()
}

// TestSandboxExec_Factory_ValidExecEnv verifies the step factory accepts exec_env
// values at construction time. As of PR8, named remote runner names are allowed at
// construction time (validation is deferred to Execute time, when the service registry
// is consulted). Only "local-docker" and the empty string are local runners; all
// other non-empty strings are accepted as potential remote runner names.
func TestSandboxExec_Factory_ValidExecEnv(t *testing.T) {
	app := NewMockApplication()
	factory := NewSandboxExecStepFactory()

	// All of these must now succeed at construction time.
	// "remote", "ephemeral", and arbitrary names are accepted — errors appear at Execute time.
	for _, ee := range []string{"local-docker", "", "remote", "ephemeral", "nope", "prod-runner"} {
		if _, err := factory("sb", map[string]any{"image": "alpine", "exec_env": ee}, app); err != nil {
			t.Errorf("exec_env %q: unexpected factory error: %v", ee, err)
		}
	}
}

// mapSecretsProvider is an in-memory secrets.Provider for token-resolution tests.
type mapSecretsProvider struct {
	vals map[string]string
}

func (p *mapSecretsProvider) Name() string { return "map" }
func (p *mapSecretsProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := p.vals[key]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (p *mapSecretsProvider) Set(_ context.Context, _, _ string) error { return secrets.ErrUnsupported }
func (p *mapSecretsProvider) Delete(_ context.Context, _ string) error { return secrets.ErrUnsupported }
func (p *mapSecretsProvider) List(_ context.Context) ([]string, error) {
	return nil, secrets.ErrUnsupported
}

// TestResolveRunnerToken_SecretRefResolvesThroughProvider is the CRITICAL test:
// a spec token "secret://x" + a configured provider must resolve to the literal
// secret value — NOT pass the "secret://x" string through verbatim.
func TestResolveRunnerToken_SecretRefResolvesThroughProvider(t *testing.T) {
	app := NewMockApplication()
	provider := &mapSecretsProvider{vals: map[string]string{"runner/prod-token": "RESOLVED-SECRET-VALUE"}}
	if err := app.RegisterService("my-vault", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	got, err := resolveRunnerToken(context.Background(), app, "my-vault", "secret://runner/prod-token", "prod-runner")
	if err != nil {
		t.Fatalf("resolveRunnerToken: %v", err)
	}
	if got != "RESOLVED-SECRET-VALUE" {
		t.Errorf("token: got %q, want %q (must NOT pass secret:// through verbatim)", got, "RESOLVED-SECRET-VALUE")
	}
}

// TestResolveRunnerToken_LiteralPassesThrough verifies a literal (non-secret://)
// token is returned unchanged even when no provider is configured.
func TestResolveRunnerToken_LiteralPassesThrough(t *testing.T) {
	app := NewMockApplication()
	got, err := resolveRunnerToken(context.Background(), app, "", "literal-token", "r1")
	if err != nil {
		t.Fatalf("resolveRunnerToken: %v", err)
	}
	if got != "literal-token" {
		t.Errorf("token: got %q, want %q", got, "literal-token")
	}
}

// TestResolveRunnerToken_Empty returns empty without error.
func TestResolveRunnerToken_Empty(t *testing.T) {
	app := NewMockApplication()
	got, err := resolveRunnerToken(context.Background(), app, "", "", "r1")
	if err != nil {
		t.Fatalf("resolveRunnerToken: %v", err)
	}
	if got != "" {
		t.Errorf("token: got %q, want empty", got)
	}
}

// TestResolveRunnerToken_SecretRefNoProvider_Error verifies a secret:// token
// with NO configured provider is a hard error — we must NOT send the literal
// "secret://..." string as the Bearer header.
func TestResolveRunnerToken_SecretRefNoProvider_Error(t *testing.T) {
	app := NewMockApplication()
	_, err := resolveRunnerToken(context.Background(), app, "", "secret://runner/token", "r1")
	if err == nil {
		t.Fatal("expected error for secret:// token with no provider, got nil")
	}
}

// TestResolveRunnerToken_ProviderMissingRef_Error verifies a non-leaky error
// when the provider cannot resolve the reference.
func TestResolveRunnerToken_ProviderMissingRef_Error(t *testing.T) {
	app := NewMockApplication()
	provider := &mapSecretsProvider{vals: map[string]string{}}
	if err := app.RegisterService("my-vault", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	_, err := resolveRunnerToken(context.Background(), app, "my-vault", "secret://missing", "r1")
	if err == nil {
		t.Fatal("expected error for unresolvable secret ref, got nil")
	}
	// The error must not echo the secret reference value.
	if strings.Contains(err.Error(), "missing") {
		t.Errorf("error leaks the secret key/ref: %v", err)
	}
}

// TestResolveNamedRemoteRunner_SecretTokenReachesConfig is the end-to-end CRITICAL
// check: a registered runner with a secret:// token resolves the token through the
// provider before the RemoteRunner dials (allow_insecure lets the no-TLS+token build
// succeed in-test). We assert the runner builds without error, proving the resolved
// (non-secret://) value reached RemoteRunnerConfig.Token — a verbatim secret:// would
// not change the build outcome here, so we additionally cover the value path via the
// resolveRunnerToken unit test above.
func TestResolveNamedRemoteRunner_SecretTokenBuilds(t *testing.T) {
	app := NewMockApplication()
	provider := &mapSecretsProvider{vals: map[string]string{"runner/tok": "REAL-TOKEN"}}
	if err := app.RegisterService("vault", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	mod := NewSandboxRemoteRunnersModule("rn", map[string]any{
		"secrets_provider": "vault",
		"remote_runners": []any{
			map[string]any{
				"name":           "prod",
				"address":        "localhost:50051",
				"token":          "secret://runner/tok",
				"allow_insecure": true, // permit token over no-TLS in-test
			},
		},
	})
	if err := mod.Init(app); err != nil {
		t.Fatalf("module Init: %v", err)
	}
	if err := app.RegisterService(SandboxRemoteRunnerServiceName, mod.registry); err != nil {
		t.Fatalf("RegisterService(registry): %v", err)
	}

	runner, err := resolveSandboxRunner(context.Background(), app, "prod", sandbox.SandboxConfig{Image: "alpine", Profile: "standard"}, "", "")
	if err != nil {
		t.Fatalf("resolveSandboxRunner: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	_ = runner.Close()
}

// TestResolveNamedRemoteRunner_SecretTokenNoProvider_Errors verifies the
// end-to-end path refuses to build a runner when a secret:// token cannot be
// resolved (no provider) — instead of silently sending the literal ref.
func TestResolveNamedRemoteRunner_SecretTokenNoProvider_Errors(t *testing.T) {
	app := NewMockApplication()
	mod := NewSandboxRemoteRunnersModule("rn", map[string]any{
		// no secrets_provider configured
		"remote_runners": []any{
			map[string]any{
				"name":           "prod",
				"address":        "localhost:50051",
				"token":          "secret://runner/tok",
				"allow_insecure": true,
			},
		},
	})
	if err := mod.Init(app); err != nil {
		t.Fatalf("module Init: %v", err)
	}
	if err := app.RegisterService(SandboxRemoteRunnerServiceName, mod.registry); err != nil {
		t.Fatalf("RegisterService(registry): %v", err)
	}

	_, err := resolveSandboxRunner(context.Background(), app, "prod", sandbox.SandboxConfig{Image: "alpine"}, "", "")
	if err == nil {
		t.Fatal("expected error: secret:// token with no provider must not build a runner")
	}
}

// ─── ephemeral (PR9) tests ────────────────────────────────────────────────────

// TestExecEnvFactory_Ephemeral_WithArgoModule verifies that exec_env: ephemeral
// with a registered *ArgoWorkflowsModule returns a non-nil ArgoEphemeralRunner.
func TestExecEnvFactory_Ephemeral_WithArgoModule(t *testing.T) {
	app := NewMockApplication()

	// Register a mock argo.workflows module.
	argoMod := NewArgoWorkflowsModule("my-argo", map[string]any{
		"backend":   "mock",
		"namespace": "argo",
	})
	if err := argoMod.Init(app); err != nil {
		t.Fatalf("argo module Init: %v", err)
	}

	cfg := sandbox.SandboxConfig{Image: "alpine:3.19", Profile: "standard"}

	// Explicit name.
	runner, err := resolveSandboxRunner(context.Background(), app, "ephemeral", cfg, "my-argo", "")
	if err != nil {
		t.Fatalf("resolveSandboxRunner ephemeral (explicit name): %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil ArgoEphemeralRunner")
	}
	_ = runner.Close()

	// Auto-detect (empty argoModuleName) — exactly one argo module registered.
	runner2, err := resolveSandboxRunner(context.Background(), app, "ephemeral", cfg, "", "")
	if err != nil {
		t.Fatalf("resolveSandboxRunner ephemeral (auto-detect): %v", err)
	}
	if runner2 == nil {
		t.Fatal("expected non-nil ArgoEphemeralRunner (auto-detect)")
	}
	_ = runner2.Close()
}

// TestExecEnvFactory_Ephemeral_NoArgoModule verifies that exec_env: ephemeral
// with no registered argo module returns a clear error.
func TestExecEnvFactory_Ephemeral_NoArgoModule(t *testing.T) {
	app := NewMockApplication()
	// No argo module registered.

	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	_, err := resolveSandboxRunner(context.Background(), app, "ephemeral", cfg, "", "")
	if err == nil {
		t.Fatal("expected error when no argo module is registered for ephemeral exec_env")
	}
	if !strings.Contains(err.Error(), "argo.workflows") {
		t.Errorf("expected error to mention argo.workflows, got: %v", err)
	}
}

// TestExecEnvFactory_Ephemeral_ExplicitNameNotFound verifies a clear error when
// the explicitly named argo_module is not in the registry.
func TestExecEnvFactory_Ephemeral_ExplicitNameNotFound(t *testing.T) {
	app := NewMockApplication()
	// No module named "missing-argo" registered.

	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	_, err := resolveSandboxRunner(context.Background(), app, "ephemeral", cfg, "missing-argo", "")
	if err == nil {
		t.Fatal("expected error for unknown argo_module name")
	}
	if !strings.Contains(err.Error(), "missing-argo") {
		t.Errorf("expected error to mention module name, got: %v", err)
	}
}

// ─── provider-ephemeral (#840) tests ───────────────────────────────────────

func TestExecEnvFactory_ProviderEphemeral_WithRunnerProvider(t *testing.T) {
	app := NewMockApplication()
	fake := &fakeRunnerProvider{runner: &fakeJobRunner{
		statuses: []interfaces.JobState{interfaces.JobStateSucceeded},
		logs:     []interfaces.LogChunk{{Data: []byte("done\n"), Source: "stdout"}},
	}}
	if err := app.RegisterService("cloud", fake); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	cfg := sandbox.SandboxConfig{Image: "alpine:3.19", Env: map[string]string{"A": "B"}}
	runner, err := resolveSandboxRunner(context.Background(), app, "provider-ephemeral", cfg, "", "cloud")
	if err != nil {
		t.Fatalf("resolveSandboxRunner provider-ephemeral: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil ProviderEphemeralRunner")
	}
	result, err := runner.Exec(context.Background(), []string{"echo", "done"})
	if err != nil {
		t.Fatalf("provider runner Exec: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "done\n" {
		t.Fatalf("result = %+v, want exit 0 stdout done", result)
	}
	if fake.runner.lastSpec.Kind != interfaces.JobKindEphemeral {
		t.Fatalf("RunJob spec kind = %q, want %q", fake.runner.lastSpec.Kind, interfaces.JobKindEphemeral)
	}
	if fake.runner.lastSpec.Image != "alpine:3.19" || fake.runner.lastSpec.RunCommand != "echo done" {
		t.Fatalf("RunJob spec = %+v", fake.runner.lastSpec)
	}
}

func TestProviderEphemeralRunner_NilStatusErrors(t *testing.T) {
	runner := newProviderEphemeralRunner(&nilStatusJobRunner{}, "cloud", sandbox.SandboxConfig{Image: "alpine"}, time.Millisecond)
	_, err := runner.Exec(context.Background(), []string{"true"})
	if err == nil {
		t.Fatal("expected nil JobStatusReply to return an error")
	}
	if !strings.Contains(err.Error(), "nil job status") {
		t.Fatalf("error = %v, want mention nil job status", err)
	}
}

func TestExecEnvFactory_ProviderEphemeral_MissingProviderName(t *testing.T) {
	_, err := resolveSandboxRunner(context.Background(), NewMockApplication(), "provider-ephemeral", sandbox.SandboxConfig{Image: "alpine"}, "", "")
	if err == nil {
		t.Fatal("expected provider-ephemeral without provider name to error")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error = %v, want mention provider", err)
	}
}

type fakeRunnerProvider struct {
	runner *fakeJobRunner
}

func (p *fakeRunnerProvider) Runner() interfaces.IaCProviderRunner { return p.runner }

var _ providerclient.RunnerProvider = (*fakeRunnerProvider)(nil)

type fakeJobRunner struct {
	lastSpec interfaces.JobSpec
	statuses []interfaces.JobState
	logs     []interfaces.LogChunk
}

func (r *fakeJobRunner) RunJob(_ context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	r.lastSpec = spec
	return &interfaces.JobHandle{ID: "job-1", Name: spec.Name, Provider: "fake"}, nil
}

func (r *fakeJobRunner) JobStatus(_ context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	state := interfaces.JobStateSucceeded
	if len(r.statuses) > 0 {
		state = r.statuses[0]
		r.statuses = r.statuses[1:]
	}
	exitCode := 0
	if state != interfaces.JobStateSucceeded {
		exitCode = 1
	}
	return &interfaces.JobStatusReply{Handle: handle, State: state, ExitCode: exitCode}, nil
}

func (r *fakeJobRunner) JobLogs(_ context.Context, _ interfaces.JobHandle, sink interfaces.LogCaptureSink) error {
	for _, chunk := range r.logs {
		if err := sink.WriteLogChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

type nilStatusJobRunner struct{}

func (r *nilStatusJobRunner) RunJob(_ context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	return &interfaces.JobHandle{ID: "job-1", Name: spec.Name, Provider: "fake"}, nil
}

func (r *nilStatusJobRunner) JobStatus(context.Context, interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	return nil, nil
}

func (r *nilStatusJobRunner) JobLogs(context.Context, interfaces.JobHandle, interfaces.LogCaptureSink) error {
	return nil
}
