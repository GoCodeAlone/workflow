// Package handler hosts the infra.admin handler library — the shared
// business logic dispatched by both the host-side infra.admin
// workflow module's HTTP routes (T15) and the wfctl `infra admin *`
// CLI subcommands (T19-T20). Functions are pure: they take their
// dependencies as parameters (state store, providers, catalog) and
// return typed adminpb outputs. The HTTP transport + audit logging
// happens at the module layer; the CLI transport happens at wfctl.
//
// Design: docs/plans/2026-05-27-infra-admin-dynamic-design.md §Handler library
// Plan:   docs/plans/2026-05-27-infra-admin-dynamic.md (Tasks 5 + 6)
//
// Authz contract (this file): every typed input MUST carry an
// AdminAuthzEvidence whose authz_checked AND authz_allowed are both
// true. The host module attaches admin-auth middleware on every
// registered route; the middleware sets the evidence after running
// authz.Casbin (or whatever the configured authz module is). If the
// evidence is missing or either bit is false, the handler refuses
// the request via the Output.error field (NOT a Go-level error, so
// HTTP transport returns 200 OK with a typed payload that consumers
// must inspect for non-empty error per the proto tag-100 discriminator).
//
// Default-deny semantics: handler refuses unless BOTH bits prove the
// host auth pipeline ran AND approved. A missing evidence means the
// caller bypassed admin auth middleware — likely a wiring bug — and
// must be refused for safety per design §Authz row.
package handler

// **Error-string credential-leak caveat** (per code-reviewer T5 M-5,
// commit 5fe88fe45): every handler in this package returns upstream
// error messages through Output.error verbatim (e.g.
// "list state: " + err.Error()). Current upstream errors come from
// os.ReadFile / json.Marshal / fake stores in tests — none carry
// credentials. But future backends (e.g. a Postgres-backed state
// store that errors with a DSN-in-message) could leak secrets
// through this channel. Scrub well-known credential-bearing
// patterns (URLs with userinfo, etc.) at the backend boundary OR
// in this package before concatenating, OR pin the no-credential
// upstream assumption per backend at integration-test time (T17).
// Not in T5 scope; flagged here so a future contributor sees the
// risk before extending the handler family.

import adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"

// authzError returns the operator-facing rejection string when the
// supplied evidence does not meet default-deny criteria. Returns ""
// when evidence is acceptable. Callers funnel the non-empty return
// into Output.error and short-circuit further work.
//
// Per design §Authz: read endpoints require
// authz_checked && authz_allowed. The "authz" substring in the
// message is load-bearing — operator-grep convention and pinned by
// TestListResources_DenyMessageMentionsAuthz.
func authzError(ev *adminpb.AdminAuthzEvidence) string {
	if ev == nil {
		return "authz evidence missing — admin middleware did not attach to this route (host wiring bug)"
	}
	if !ev.AuthzChecked {
		return "authz check did not run — evidence.authz_checked=false (admin middleware bypassed or misconfigured)"
	}
	if !ev.AuthzAllowed {
		return "authz denied — evidence.authz_allowed=false"
	}
	return ""
}
