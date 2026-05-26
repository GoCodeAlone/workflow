# ADR 0021 — Use rewriteTransport (not DIGITALOCEAN_API_URL env var) for DO API test stubbing

**Status:** Accepted
**Date:** 2026-05-09
**Context:** Code review of PR4a (Task 8 commit 1f03a7d9) flagged a Critical
credential-exfiltration vector in `secrets/generators.go`. Resolved by switching
the test stub mechanism in Task 7 (`secrets/generators_test.go`) and removing the
production hook from `generateDOSpacesKey`.

---

## Problem

The `spaces-key-iac-resource` plan (commit 316559f7) prescribed an
`os.Getenv("DIGITALOCEAN_API_URL")` override in `generateDOSpacesKey` so that
the Task 7 failing test could redirect the DO API call at an `httptest.Server`
via `t.Setenv("DIGITALOCEAN_API_URL", srv.URL)`. The hook was honored
**unconditionally** in production code:

```go
apiURL := "https://api.digitalocean.com"
if v := os.Getenv("DIGITALOCEAN_API_URL"); v != "" {
    apiURL = v
}
req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/v2/spaces/keys", ...)
req.Header.Set("Authorization", "Bearer "+os.Getenv("DIGITALOCEAN_TOKEN"))
```

A process whose environment includes `DIGITALOCEAN_API_URL` — set by a
malicious `.env` checked into a repo, a hostile CI step, a multi-tenant runner,
or a compromised dependency — would silently redirect the
`Authorization: Bearer <DIGITALOCEAN_TOKEN>` POST to an attacker-controlled
server. The attacker captures the production token in the request header
without any authentication challenge and without any indication to the
operator. **Credential exfiltration with no detection signal.**

This is a textbook "innocuous test hook becomes a security backdoor" pattern.
Copilot independently flagged it on PR #582 as a Critical security finding.

---

## Decision

1. **Remove** the `DIGITALOCEAN_API_URL` env-var override from
   `secrets/generators.go`. The production code path uses the hardcoded DO
   endpoint `https://api.digitalocean.com/v2/spaces/keys`.

2. **Switch** `TestGenerateDOSpacesKey_IncludesCreatedAt` (Task 7) to use the
   package's existing `rewriteTransport` helper (defined in
   `secrets/github_provider_test.go` line 30, used by the three sister tests
   `TestGenerateSecret_ProviderCredential_DOSpaces*` at lines 107-109,
   156-158, 211-213):

   ```go
   orig := http.DefaultClient.Transport
   http.DefaultClient.Transport = rewriteTransport{base: srv.URL}
   defer func() { http.DefaultClient.Transport = orig }()
   ```

   This pattern is hermetic — it mutates `http.DefaultClient` in-test only,
   requires explicit Go code in the test to take effect, and has zero
   production attack surface. It matches the convention already established
   in this exact file for the same provider.

3. **No production behavior change** beyond removing the env-var lookup.
   The hardcoded URL is the same one Task 7's commit had before Task 8
   modified it.

---

## Alternatives considered

- **Constructor-injection of a `Transport` (or full `*http.Client`) into
  `generateDOSpacesKey`.** Would also be safe, but is a larger refactor (the
  generator is a free function, not a struct method) and would diverge from
  the existing `rewriteTransport`+`http.DefaultClient` convention used by the
  other DO Spaces tests in the same file. Not worth the churn for the same
  end-state safety property.

- **Honor the env var only when an in-process build tag is set** (e.g.
  `//go:build testhook`). Workable, but adds a parallel build configuration
  to the repo for one feature and still doesn't match the existing sister
  pattern. Same churn-cost objection as constructor injection.

- **Keep the env-var override but only honor it when a sentinel like
  `WFCTL_TEST=1` is also set.** Defense-in-depth, but the attacker who can
  set one env var can typically set another. Doesn't actually close the
  vector — just adds one cheap step to the exploit. Rejected.

---

## Plan deviation

The original plan (`docs/plans/2026-05-08-spaces-key-iac-resource.md`,
Task 7 step 1 + Task 8 step 1) literally prescribed:

> `t.Setenv("DIGITALOCEAN_API_URL", srv.URL) // hook used by generateDOSpacesKey for tests`
>
> Plus add a test hook for `DIGITALOCEAN_API_URL` env var if not present:
> `apiURL := "https://api.digitalocean.com"; if v := os.Getenv("DIGITALOCEAN_API_URL"); v != "" { apiURL = v }`

The plan was wrong on this. Per workspace memory
`feedback_proper_fixes_over_workarounds`, fixing the security flaw at the
mechanism level (rewriteTransport) is preferred over patching around the
env var. Per `feedback_no_invented_interfaces`, switching to an already-existing
helper in the package (rather than introducing a new test hook) is also
preferred.

The plan author has been notified. Task 8's review-cycle catch is exactly
what the 2-stage spec/code review process exists for.

---

## Consequences

- **Positive:** DO API URL is no longer overridable from process environment
  in production. Test stub mechanism matches the three sibling DO Spaces
  tests. No new test infrastructure introduced.

- **Negative:** None. The `rewriteTransport` helper already existed in this
  package, so this is a strictly subtractive change to production code +
  a pattern-conforming change to the test.

- **Follow-up:** Plan-as-written should not be re-used as a template for
  HTTP-stub patterns in other generators. The `secrets/generators_test.go`
  sibling tests are the canonical example to copy.
