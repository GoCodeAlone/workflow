# Managed agent token issuance

`module.M2MAuthModule.IssueManagedToken(ctx, module.ManagedTokenRequest)` is a
typed signing boundary for grants already approved by an authenticated host.
Workflow remains the issuer. The reusable auth integration owns grant storage,
current grant version and expiry checks, approved scopes, exchange replay
protection, tenant authorization, revocation and audit.

Use a dedicated module instance with an exact canonical HTTPS issuer passed to
`NewM2MAuthModule`, a stable P-256 PEM private key loaded by `SetECDSAKey`, and
`SetManagedOnly(true)` before serving requests. The managed API refuses generated
keys, HS256, incomplete key configuration and pending initialization errors.
Do not share this key with an issuer exposing generic grants or turn managed-only
mode off after activation. Key configuration belongs to startup; no key, grant,
credential or persistent permission is created by this API.

`ManagedTokenRequest` has these fields:

| Field | Type | Required binding |
|---|---|---|
| `Subject` | `string` | Approved stable agent subject |
| `Audience` | `string` | One exact canonical HTTPS resource audience |
| `GrantID` | `string` | Current approved grant identity |
| `GrantVersion` | `int64` | Positive current grant version |
| `TenantID` | `int64` | Positive approved tenant ID |
| `Scopes` | `[]string` | Nonempty, unique approved scope tokens |
| `IssuedAt` | `time.Time` | UTC whole seconds, not future or over 30 seconds old |
| `ExpiresAt` | `time.Time` | UTC whole seconds, future, at most 15 minutes after issuance |

The expiry must also be capped by the grant's current expiry by the caller.
Subjects/grant IDs are bounded printable ASCII without whitespace or wildcards.
Scopes use the RFC 6749 scope-token character range, forbid wildcards, and are
limited to 64 tokens of at most 256 bytes each. The issuer and audience reject
userinfo, queries, fragments, host case aliases, encoded paths and path-cleaning
aliases. No trailing slash or audience spelling is normalized. API and preview
audiences must be separately approved and compared exactly.

The signer fixes `iss`, `sub`, `aud`, `iat`, `exp`, an internally generated random
`jti`, `token_use=agent_access`, `grant_id`, `grant_version`, `tenant_id` and the
space-separated `scope`. There is no arbitrary extra-claim map or inheritance
from legacy registered clients. ES256 uses the same public key ID as this module's
JWKS. Errors do not echo caller claims or key material.

Managed-only mode denies the generic token, revoke and introspection HTTP
handlers with 403 before parsing their inputs, for default or custom endpoint
paths. JWKS remains readable. The legacy default, signing algorithms, expiry,
generic provider behavior and revocation policy remain unchanged. The legacy
`Authenticate` helper is not a managed grant authorizer: resource use requires
the auth integration's strict algorithm/issuer/audience/token-use verifier and
fresh grant/policy checks, including fail-closed revocation handling.

This source change does not activate an identity, create a signing key, configure
secrets, register an endpoint, alter dependency pins or deploy a service. An owner
must separately approve the scoped grant and activate its reviewed host adapter.
