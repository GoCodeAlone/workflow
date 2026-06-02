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
// local — i.e., the same host where the engine/agent runs. Local secrets
// backends (env, file, keychain) are always reachable from these environments.
func isLocalExecEnv(execEnv string) bool {
	return execEnv == "" || execEnv == "local" || execEnv == "local-docker"
}

// Reachability determines whether the secrets referenced by a plan can be read
// from the given execution environment (execEnv). It is designed to be
// fail-safe: when the answer is uncertain and the exec-env is remote, it
// returns unreachable rather than failing open.
//
// Classification uses a concrete type-switch rather than Name() string-matching.
// Name() is a config-assigned identifier that operators may override; the
// concrete type is the authoritative signal for capability detection.
//
// Rules (in order):
//
//  1. *EnvProvider, *FileProvider, *KeychainProvider — local backends. Always
//     reachable regardless of execEnv.  For KeychainProvider specifically: the
//     OS keychain is only meaningful on the engine host, but for the gate's
//     purpose we treat it as local-reachable because the engine (wherever it
//     runs) is the process that started the pipeline. If a remote exec-env truly
//     cannot access the keychain, the runtime will surface an error when the
//     secret is actually read; the gate does not second-guess that.
//
//  2. *GitHubSecretsProvider — short-circuited to unreachable BEFORE any
//     AccessChecker call.  GitHub secrets are write-only: Get() returns
//     ErrUnsupported and values are only injected at CI-job startup. No
//     exec-env can read them at runtime. CheckAccess on the GitHub provider
//     would succeed (it probes the public key endpoint, not Get), so probing
//     it would give a false "reachable" verdict.
//
//  3. All other providers that implement AccessChecker — call CheckAccess.
//     Reachable iff CheckAccess returns nil.
//
//  4. Providers that do NOT implement AccessChecker + remote execEnv — fail-safe
//     unreachable ("reachability unknown for remote exec-env; assuming unreachable").
//     We never fail open for an unknown/unclassified backend in a remote context.
//
//  5. Providers that do NOT implement AccessChecker + local execEnv — treat as
//     reachable (local operation; the runtime will surface access errors if any).
func Reachability(p Provider, execEnv string) Result {
	switch p.(type) {
	case *EnvProvider, *FileProvider, *KeychainProvider:
		// Local backends — always reachable from any exec-env.
		return Result{Reachable: true}

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
			if err := ac.CheckAccess(context.Background()); err != nil {
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
