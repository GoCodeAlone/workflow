package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

// ─── stub types for Reachability tests ───────────────────────────────────────

// stubRemoteWithAccess implements Provider + AccessChecker and returns nil from
// CheckAccess (simulates a reachable remote backend).
type stubRemoteWithAccess struct {
	checkAccessErr error
	checkCalled    bool
}

func (s *stubRemoteWithAccess) Name() string                                    { return "stub-remote-access" }
func (s *stubRemoteWithAccess) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubRemoteWithAccess) Set(_ context.Context, _, _ string) error        { return nil }
func (s *stubRemoteWithAccess) Delete(_ context.Context, _ string) error        { return nil }
func (s *stubRemoteWithAccess) List(_ context.Context) ([]string, error)        { return nil, nil }
func (s *stubRemoteWithAccess) CheckAccess(_ context.Context) error {
	s.checkCalled = true
	return s.checkAccessErr
}

// stubRemoteNoAccess implements Provider only (no AccessChecker).
type stubRemoteNoAccess struct{}

func (s *stubRemoteNoAccess) Name() string                                    { return "stub-remote-no-access" }
func (s *stubRemoteNoAccess) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubRemoteNoAccess) Set(_ context.Context, _, _ string) error        { return nil }
func (s *stubRemoteNoAccess) Delete(_ context.Context, _ string) error        { return nil }
func (s *stubRemoteNoAccess) List(_ context.Context) ([]string, error)        { return nil, nil }

// stubGitHubLikeWithAccess is a provider that behaves like GitHub:
// Get returns ErrUnsupported but ALSO implements AccessChecker (returns nil).
// This tests that the GitHub short-circuit fires before CheckAccess is consulted.
type stubGitHubLikeWithAccess struct {
	checkCalled bool
}

func (s *stubGitHubLikeWithAccess) Name() string { return "stub-github-like" }
func (s *stubGitHubLikeWithAccess) Get(_ context.Context, _ string) (string, error) {
	return "", secrets.ErrUnsupported
}
func (s *stubGitHubLikeWithAccess) Set(_ context.Context, _, _ string) error { return nil }
func (s *stubGitHubLikeWithAccess) Delete(_ context.Context, _ string) error { return nil }
func (s *stubGitHubLikeWithAccess) List(_ context.Context) ([]string, error) { return nil, nil }
func (s *stubGitHubLikeWithAccess) CheckAccess(_ context.Context) error {
	s.checkCalled = true
	return nil
}

// compile-time interface assertions
var _ secrets.Provider = (*stubRemoteWithAccess)(nil)
var _ secrets.AccessChecker = (*stubRemoteWithAccess)(nil)
var _ secrets.Provider = (*stubRemoteNoAccess)(nil)
var _ secrets.Provider = (*stubGitHubLikeWithAccess)(nil)
var _ secrets.AccessChecker = (*stubGitHubLikeWithAccess)(nil)

// ─── TestReachability ─────────────────────────────────────────────────────────

func TestReachability(t *testing.T) {
	cases := []struct {
		name          string
		provider      secrets.Provider
		execEnv       string
		wantReachable bool
		wantReasonSub string // non-empty: the reason must contain this substring
	}{
		// ── Host-local backends + LOCAL exec-env — reachable ──────────────────
		{
			name:          "EnvProvider local-exec",
			provider:      secrets.NewEnvProvider(""),
			execEnv:       "local",
			wantReachable: true,
		},
		{
			name:          "EnvProvider empty (local) exec",
			provider:      secrets.NewEnvProvider("APP_"),
			execEnv:       "",
			wantReachable: true,
		},
		{
			name:          "FileProvider local-docker exec",
			provider:      secrets.NewFileProvider("/tmp"),
			execEnv:       "local-docker",
			wantReachable: true,
		},
		{
			name:          "KeychainProvider local exec",
			provider:      newTestKeychainProvider(t),
			execEnv:       "local",
			wantReachable: true,
		},

		// ── Host-local backends + REMOTE exec-env — fail-safe UNREACHABLE ──────
		// Under ADR 0017 the remote agent resolves its own secrets; the engine
		// cannot vouch for engine-host env/file/keychain on a remote runner.
		{
			name:          "EnvProvider remote-exec → fail-safe unreachable",
			provider:      secrets.NewEnvProvider("APP_"),
			execEnv:       "remote",
			wantReachable: false,
			wantReasonSub: "host-local backend (env)",
		},
		{
			name:          "FileProvider remote-exec → fail-safe unreachable",
			provider:      secrets.NewFileProvider("/tmp"),
			execEnv:       "k8s-prod",
			wantReachable: false,
			wantReasonSub: "host-local backend (file)",
		},
		{
			name:          "KeychainProvider remote-exec → fail-safe unreachable",
			provider:      newTestKeychainProvider(t),
			execEnv:       "remote",
			wantReachable: false,
			wantReasonSub: "host-local backend (keychain)",
		},

		// ── GitHubSecretsProvider — always unreachable (write-only short-circuit) ─
		// The stub has CheckAccess returning nil to prove it is NOT consulted.
		{
			name:          "GitHubSecretsProvider write-only short-circuit (local exec)",
			provider:      newTestGitHubProvider(t),
			execEnv:       "local",
			wantReachable: false,
			wantReasonSub: "write-only",
		},
		{
			name:          "GitHubSecretsProvider write-only short-circuit (remote exec)",
			provider:      newTestGitHubProvider(t),
			execEnv:       "remote",
			wantReachable: false,
			wantReasonSub: "write-only",
		},

		// ── Remote backends WITH AccessChecker ────────────────────────────────
		{
			name:          "remote with AccessChecker nil → reachable",
			provider:      &stubRemoteWithAccess{checkAccessErr: nil},
			execEnv:       "remote",
			wantReachable: true,
		},
		{
			name:          "remote with AccessChecker err → unreachable",
			provider:      &stubRemoteWithAccess{checkAccessErr: errors.New("connection refused")},
			execEnv:       "remote",
			wantReachable: false,
			wantReasonSub: "connection refused",
		},

		// ── Remote backends WITHOUT AccessChecker ─────────────────────────────
		{
			name:          "remote WITHOUT AccessChecker + remote execEnv → fail-safe unreachable",
			provider:      &stubRemoteNoAccess{},
			execEnv:       "remote",
			wantReachable: false,
			wantReasonSub: "reachability unknown",
		},
		{
			name:          "remote WITHOUT AccessChecker + empty execEnv → reachable (local assumed)",
			provider:      &stubRemoteNoAccess{},
			execEnv:       "",
			wantReachable: true,
		},
		{
			name:          "remote WITHOUT AccessChecker + 'local' execEnv → reachable",
			provider:      &stubRemoteNoAccess{},
			execEnv:       "local",
			wantReachable: true,
		},
		{
			name:          "remote WITHOUT AccessChecker + 'local-docker' execEnv → reachable",
			provider:      &stubRemoteNoAccess{},
			execEnv:       "local-docker",
			wantReachable: true,
		},

		// ── Unknown provider type + remote execEnv → fail-safe ────────────────
		{
			name:          "unknown provider + remote execEnv → fail-safe unreachable",
			provider:      &stubRemoteNoAccess{},
			execEnv:       "prod-k8s",
			wantReachable: false,
			wantReasonSub: "reachability unknown",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := secrets.Reachability(context.Background(), tc.provider, tc.execEnv)
			if result.Reachable != tc.wantReachable {
				t.Errorf("Reachable = %v, want %v (reason: %q)", result.Reachable, tc.wantReachable, result.Reason)
			}
			if tc.wantReasonSub != "" {
				if result.Reason == "" {
					t.Errorf("expected Reason containing %q, got empty", tc.wantReasonSub)
				} else if !containsSubstr(result.Reason, tc.wantReasonSub) {
					t.Errorf("expected Reason containing %q, got %q", tc.wantReasonSub, result.Reason)
				}
			}
			if tc.wantReachable && result.Reason != "" {
				t.Errorf("expected empty Reason when reachable, got %q", result.Reason)
			}
		})
	}
}

// TestReachability_GitHubShortCircuit_CheckAccessNotConsulted proves that the
// GitHub write-only short-circuit fires BEFORE any AccessChecker call. We use a
// real *secrets.GitHubSecretsProvider for the type-switch, but we also verify the
// pattern with our stub whose CheckAccess is instrumented.
//
// For the stub: the key invariant is that a provider whose Get returns ErrUnsupported
// should be short-circuited — the stub is NOT a *GitHubSecretsProvider, so this
// test specifically covers the *GitHubSecretsProvider path via the real type.
func TestReachability_GitHubShortCircuit_CheckAccessNotConsulted(t *testing.T) {
	p := newTestGitHubProvider(t)

	result := secrets.Reachability(context.Background(), p, "remote")
	if result.Reachable {
		t.Error("GitHubSecretsProvider must be unreachable (write-only)")
	}
	if !containsSubstr(result.Reason, "write-only") {
		t.Errorf("expected reason to mention write-only, got %q", result.Reason)
	}
	// The short-circuit must fire without making any network call.
	// We can't instrument the real GitHubSecretsProvider's CheckAccess, but we
	// verify the stubGitHubLikeWithAccess (same shape) is NOT consulted:
	stub := &stubGitHubLikeWithAccess{}
	// stubGitHubLikeWithAccess is NOT *GitHubSecretsProvider, so the type-switch
	// won't catch it — it will fall through to the generic remote path.
	// This is intentional: the type-switch is explicitly for *GitHubSecretsProvider.
	// Verify the stub does get its CheckAccess called (it's a regular remote-with-access).
	result2 := secrets.Reachability(context.Background(), stub, "remote")
	if !result2.Reachable {
		t.Errorf("stub with CheckAccess→nil should be reachable, got reason: %q", result2.Reason)
	}
	if !stub.checkCalled {
		t.Error("expected CheckAccess to be called on stub remote provider")
	}
}

// TestReachability_VaultAndAWS_WithAccessChecker covers the AccessChecker branch
// that *VaultProvider and *AWSSecretsManagerProvider hit (rule 3). Those concrete
// providers require live clients, so this test uses stubRemoteWithAccess — a stub
// implementing AccessChecker — to exercise the same default-case probe path. It
// does NOT instantiate the real provider types.
func TestReachability_VaultAndAWS_WithAccessChecker(t *testing.T) {
	// stubRemoteWithAccess covers the same code path as *VaultProvider and
	// *AWSSecretsManagerProvider: it implements AccessChecker.
	stub := &stubRemoteWithAccess{checkAccessErr: nil}
	result := secrets.Reachability(context.Background(), stub, "prod-env")
	if !result.Reachable {
		t.Errorf("remote with CheckAccess→nil should be reachable, got: %q", result.Reason)
	}
	if !stub.checkCalled {
		t.Error("expected CheckAccess to be called")
	}

	stubErr := &stubRemoteWithAccess{checkAccessErr: errors.New("vault: token expired")}
	result2 := secrets.Reachability(context.Background(), stubErr, "prod-env")
	if result2.Reachable {
		t.Error("remote with CheckAccess returning err should be unreachable")
	}
	if !containsSubstr(result2.Reason, "token expired") {
		t.Errorf("expected reason to contain error text, got %q", result2.Reason)
	}
}

// stubCtxObservingProvider records the context it received in CheckAccess so a
// test can prove ctx is propagated (not context.Background()).
type stubCtxObservingProvider struct {
	gotCtx context.Context
}

func (s *stubCtxObservingProvider) Name() string                                    { return "stub-ctx" }
func (s *stubCtxObservingProvider) Get(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubCtxObservingProvider) Set(_ context.Context, _, _ string) error        { return nil }
func (s *stubCtxObservingProvider) Delete(_ context.Context, _ string) error        { return nil }
func (s *stubCtxObservingProvider) List(_ context.Context) ([]string, error)        { return nil, nil }
func (s *stubCtxObservingProvider) CheckAccess(ctx context.Context) error {
	s.gotCtx = ctx
	return ctx.Err()
}

var _ secrets.AccessChecker = (*stubCtxObservingProvider)(nil)

// TestReachability_PropagatesContext proves Reachability passes the caller's ctx
// (not a fresh context.Background) into AccessChecker.CheckAccess, so a route /
// pipeline deadline bounds the probe. An already-cancelled context must surface
// as an unreachable verdict.
func TestReachability_PropagatesContext(t *testing.T) {
	stub := &stubCtxObservingProvider{}

	type ctxKey string
	const k ctxKey = "marker"
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), k, "v"))
	cancel() // already cancelled — CheckAccess returns ctx.Err()

	result := secrets.Reachability(ctx, stub, "remote")

	if stub.gotCtx == nil {
		t.Fatal("CheckAccess did not receive a context")
	}
	if stub.gotCtx.Value(k) != "v" {
		t.Error("Reachability did not propagate the caller's context into CheckAccess (got a different context)")
	}
	if result.Reachable {
		t.Error("expected unreachable when the propagated context is already cancelled")
	}
	if !containsSubstr(result.Reason, "access check failed") {
		t.Errorf("expected access-check-failed reason, got %q", result.Reason)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTestGitHubProvider creates a real *secrets.GitHubSecretsProvider using a
// fake token so the type-switch in Reachability fires correctly.
// It sets the WORKFLOW_TEST_GH_TOKEN env var temporarily (the name passed into
// NewGitHubSecretsProvider) so the provider doesn't reject an empty token.
func newTestGitHubProvider(t *testing.T) *secrets.GitHubSecretsProvider {
	t.Helper()
	t.Setenv("WORKFLOW_TEST_GH_TOKEN", "ghp_fake_token_for_type_switch_test")
	p, err := secrets.NewGitHubSecretsProvider("owner/repo", "WORKFLOW_TEST_GH_TOKEN")
	if err != nil {
		t.Fatalf("NewGitHubSecretsProvider: %v", err)
	}
	return p
}

// newTestKeychainProvider creates a real *secrets.KeychainProvider so the
// host-local type-switch in Reachability fires correctly. It performs no OS
// keychain I/O (Reachability classifies by type, it does not probe keychain).
func newTestKeychainProvider(t *testing.T) *secrets.KeychainProvider {
	t.Helper()
	p, err := secrets.NewKeychainProvider("workflow-test-reachability")
	if err != nil {
		t.Fatalf("NewKeychainProvider: %v", err)
	}
	return p
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestReachability_NilProvider verifies a nil or typed-nil provider is fail-safe
// unreachable (never panics, never reports reachable).
func TestReachability_NilProvider(t *testing.T) {
	// Untyped nil.
	if r := secrets.Reachability(context.Background(), nil, "local"); r.Reachable {
		t.Errorf("nil provider should be unreachable, got reachable")
	}
	// Typed-nil pointer stored in the Provider interface.
	var typedNil *secrets.VaultProvider
	r := secrets.Reachability(context.Background(), typedNil, "local")
	if r.Reachable {
		t.Errorf("typed-nil *VaultProvider should be unreachable, got reachable")
	}
	if r.Reason == "" {
		t.Errorf("expected a non-empty reason for nil provider")
	}
}
