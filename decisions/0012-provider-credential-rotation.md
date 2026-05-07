# ADR 0012 — provider_credential rotation: mint-new-then-revoke-old via ProviderCredentialRevoker

**Status:** Accepted  
**Date:** 2026-05-07  
**Context:** TC2 staging cutover blocked on DO Spaces `SignatureDoesNotMatch` because bootstrap
detected existing keys and skipped regeneration. Operator requires `--force-rotate SPACES` to
replace compromised or drifted credentials without manual cloud-console intervention.

---

## Problem

`wfctl infra bootstrap --force-rotate SPACES` previously hard-rejected `provider_credential`
types with:

> must be rotated via the upstream provider; cannot regenerate locally

This forced operators to manually mint new keys in the DO console and update GH secrets, which
is error-prone, undocumented, and not audited by wfctl.

---

## Decision

Enable `--force-rotate` for `provider_credential` secrets with the following ordering guarantee:

1. **Read** the OLD `access_key` (stored as `<name>_access_key`) from the secrets store before any deletion.
2. **Delete** all existing sub-keys from the secrets store.
3. **Mint** new credentials via `secrets.GenerateSecret` (calls the provider API).
4. **Store** new sub-keys in the secrets store.
5. **Revoke** the OLD credential at the upstream provider via `ProviderCredentialRevoker`.

This is **mint-new-then-revoke-old** — not revoke-then-mint. The window between steps 4 and 5
is very short, but the critical property is:

> At no point is there a period where no valid credential exists for the dependent service.

---

## Interfaces

A new optional interface is added to `interfaces/iac_provider.go`:

```go
type ProviderCredentialRevoker interface {
    RevokeProviderCredential(ctx context.Context, source string, credentialID string) error
}
```

Provider plugins that issue credentials (e.g. `workflow-plugin-digitalocean` for
`digitalocean.spaces`) implement this interface. Core wfctl type-asserts at the call site;
plugins that do not implement it are valid — revocation just doesn't happen automatically, and
a warning is logged asking the operator to revoke manually.

---

## Revoke-failure handling

If `RevokeProviderCredential` returns an error:

- The new credential stored in GH secrets is **NOT rolled back** — it is the valid active key.
- A warning is logged to stderr: `warn: revoke old credential <name> (id=<id>): <err> — revoke manually`
- The operator must manually revoke the old key via the cloud console or API.

Rolling back the GH secret on revoke failure would leave the dependent service with no valid
credential, which is a worse failure mode than having two simultaneously-valid credentials
temporarily.

---

## Force-rotate semantics

- Requires explicit `--force-rotate <name>` flag — never triggered automatically.
- Validates against `secrets.generate[]` first (fast-fail on typos).
- `infra_output` types are still rejected (they are derived from apply state, not generated).
- The OLD `access_key` (sub-key `<name>_access_key`) is captured from the secrets store before
  deletion. If the store doesn't expose Get (write-only provider like GitHub Actions), revocation
  is skipped with a warning.

---

## Alternatives considered

**Revoke-then-mint:** Rejected. Creates a window where no valid credential exists, causing
immediate service disruption for anything using the old key.

**Operator-manual rotation only:** Rejected. No audit trail, error-prone, requires console
access that may not be available in CI/CD pipelines.

**Separate wfctl subcommand (`wfctl infra rotate-credential`):** Deferred. The existing
`--force-rotate` flag is sufficient for the TC2 use case and avoids a new UX surface.

---

## Consequences

- `workflow-plugin-digitalocean` gains `RevokeProviderCredential` for `digitalocean.spaces`
  (DELETE /v2/spaces/keys/{access_key_id}).
- Other provider plugins are unaffected (optional interface).
- Bootstrap audit log gains explicit rotation + revocation lines on stderr.
- TC2 cutover can proceed: `wfctl infra bootstrap -c infra.yaml --env staging --force-rotate SPACES`
  replaces the stale DO Spaces keys and revokes the old ones.
