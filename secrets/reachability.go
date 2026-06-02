package secrets

import (
	"context"
	"fmt"
)

// Result holds the outcome of a Reachability check.
//
// Reachable is true when the secrets provider can be reached from the given
// execution environment. Reason is empty when Reachable is true, and contains a
// human-readable (non-credential-leaking) explanation when Reachable is false.
type Result struct {
	Reachable bool
	Reason    string
}

// isLocalExecEnv returns true for execution environments that are considered
// local — i.e., the same host where the engine (and its configured secrets
// providers) run. Host-local secrets backends (env, file, keychain) are only
// verifiably reachable from these environments.
func isLocalExecEnv(execEnv string) bool {
	return execEnv == "" || execEnv == "local" || execEnv == "local-docker"
}

// hostLocalKind returns a short label for a host-local backend type, or "" if p
// is not a host-local backend. Used to classify and to build the fail-safe
// reason string.
func hostLocalKind(p Provider) string {
	switch p.(type) {
	case *EnvProvider:
		return "env"
	case *FileProvider:
		return "file"
	case *KeychainProvider:
		return "keychain"
	default:
		return ""
	}
}

// Reachability determines whether the secrets referenced by a plan can be read
// from the given execution environment (execEnv). It is designed to be
// fail-safe: when the answer is uncertain and the exec-env is remote, it
// returns unreachable rather than failing open.
//
// ctx bounds any backend probe (AccessChecker.CheckAccess). Callers MUST pass a
// deadline-bearing context (e.g. the pipeline/route ctx) so a slow or
// unreachable remote backend cannot hang the pre-flight (vault default ~60s,
// aws ~10s) past the request deadline.
//
// Classification uses a concrete type-switch rather than Name() string-matching.
// Name() is a config-assigned identifier that operators may override; the
// concrete type is the authoritative signal for capability detection.
//
// Rules (in order):
//
//  1. *EnvProvider, *FileProvider, *KeychainProvider — host-local backends.
//     Reachable ONLY when execEnv is local ("" | "local" | "local-docker").
//     For a REMOTE exec-env these are fail-safe UNREACHABLE: under the
//     agent-side-resolution model (ADR 0017), a remote exec-env resolves
//     secrets with the remote agent's OWN provider, so the engine cannot vouch
//     that the engine-host's env vars / files / OS keychain exist on the remote
//     runner (e.g. a macOS keychain entry is not present on a remote Linux
//     runner). This is intentionally conservative: it is the safe default until
//     the agent-probe hardening filed in ADR 0017 lands.
//
//  2. *GitHubSecretsProvider — short-circuited to unreachable BEFORE any
//     AccessChecker call.  GitHub secrets are write-only: Get() returns
//     ErrUnsupported and values are only injected at CI-job startup. No
//     exec-env can read them at runtime. CheckAccess on the GitHub provider
//     would succeed (it probes the public key endpoint, not Get), so probing
//     it would give a false "reachable" verdict.
//
//  3. All other providers that implement AccessChecker — call CheckAccess(ctx).
//     Reachable iff CheckAccess returns nil.
//
//  4. Providers that do NOT implement AccessChecker + remote execEnv — fail-safe
//     unreachable ("reachability unknown for remote exec-env; assuming unreachable").
//     We never fail open for an unknown/unclassified backend in a remote context.
//
//  5. Providers that do NOT implement AccessChecker + local execEnv — treat as
//     reachable (local operation; the runtime will surface access errors if any).
func Reachability(ctx context.Context, p Provider, execEnv string) Result {
	if kind := hostLocalKind(p); kind != "" {
		// Host-local backend — verifiable only from a local exec-env.
		if isLocalExecEnv(execEnv) {
			return Result{Reachable: true}
		}
		return Result{
			Reachable: false,
			Reason:    fmt.Sprintf("host-local backend (%s) is not verifiable from a remote exec-env; the remote agent resolves its own secrets (ADR 0017)", kind),
		}
	}

	switch p.(type) {
	case *GitHubSecretsProvider:
		// Write-only short-circuit: GitHub secrets inject at CI time only.
		// CheckAccess is intentionally NOT called — it would return a false
		// positive (probing the public key endpoint, not read capability).
		return Result{
			Reachable: false,
			Reason:    "write-only — secrets inject at CI time only; no exec-env can read them",
		}

	default:
		// For all remaining providers: check whether they implement AccessChecker.
		if ac, ok := p.(AccessChecker); ok {
			if err := ac.CheckAccess(ctx); err != nil {
				return Result{
					Reachable: false,
					Reason:    fmt.Sprintf("access check failed: %s", err.Error()),
				}
			}
			return Result{Reachable: true}
		}

		// No AccessChecker implementation:
		//  - Local exec-env: treat as reachable (the runtime will catch errors).
		//  - Remote exec-env: fail-safe unreachable.
		if isLocalExecEnv(execEnv) {
			return Result{Reachable: true}
		}
		return Result{
			Reachable: false,
			Reason:    "reachability unknown for remote exec-env; assuming unreachable",
		}
	}
}
