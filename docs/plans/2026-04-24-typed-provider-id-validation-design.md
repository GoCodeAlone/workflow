# Typed ProviderID Validation at wfctl ↔ Plugin Boundaries — Design

**Status:** Approved (autonomous pipeline, 2026-04-24)

**Target release:** workflow v0.18.11 + workflow-plugin-digitalocean v0.7.9

**Related:**
- Tonight's BMW failure — "PUT /v2/apps/bmw-staging: 400 invalid uuid" (root cause: unvalidated raw `string` ProviderID across wfctl ↔ plugin boundary)
- DO plugin v0.7.7 (task #58) — silent-failure empty-ID guard (band-aid 1)
- DO plugin v0.7.8 (task #67) — reactive heal in Update/Delete (band-aid 2)
- Task #41 (v0.19.0) — typed-args refactor for IaCProvider gRPC boundary (Level 2-3 follow-up)
- Task #42 (v0.19.0) — plugin manifest + lockfile split (Level 4 follow-up for ResourceSpec.Config schema)

---

## Problem

BMW's IaC state contained `ProviderID="bmw-staging"` (a resource name) instead of a UUID. When `wfctl infra apply` tried to UPDATE that resource, DO's REST API rejected the request because UUID-shaped identifiers are required in the URL path. The failure surfaced after 10 minutes of polling "no deployment found."

Root cause is not the specific mis-substitution that produced the bad state — it's that **raw `string` flows through the wfctl ↔ plugin boundary with zero validation**. Any driver can return any string as a ProviderID, wfctl persists whatever it gets, and bad state can stick around indefinitely.

v0.7.7 and v0.7.8 both tried to fix this inside the DO plugin. Both were band-aids: v0.7.7 guarded only the Create path (not existing state), v0.7.8 reactively heals stale state on Update but doesn't prevent bad writes. The proper fix is **preventive validation at the boundary**.

---

## Approach — Level 1

Declare ID format PER driver. Enforce at wfctl's apply path on BOTH input (before driver call) and output (before state write). Optional interface — drivers that don't opt in get today's behavior.

### Why not Level 2 (typed wrapper) or higher

Level 2 (`type ProviderID string` with constructor validation) is Go-idiomatic but requires every call site across wfctl and every plugin to adopt the new type — a large diff across two repos. Level 3 (proto oneof) is a schema break and blocks on task #41's typed-args refactor. Level 4 (JSON Schema on Config) is a whole separate track (task #42 plugin-manifest).

**Level 1 buys the 80% outcome (bad state can't be written) with a 20% diff that ships in one pair of PRs and doesn't require breaking changes.**

### Interface addition (`workflow/interfaces/iac_resource_driver.go`)

```go
// ProviderIDFormat identifies the shape of provider-specific resource
// identifiers so wfctl can validate them at the driver boundary without
// knowing provider-specific semantics.
type ProviderIDFormat int

const (
    IDFormatUnknown    ProviderIDFormat = iota // no validation (default)
    IDFormatUUID                               // 36-char canonical UUID
    IDFormatDomainName                         // RFC 1035 domain name
    IDFormatARN                                // AWS-style ARN
    IDFormatFreeform                           // driver allows any non-empty string
)

// ProviderIDValidator is an optional interface ResourceDriver implementations
// may provide to declare the shape of their ProviderIDs. wfctl uses the
// declaration to validate ProviderIDs before calling Update/Delete (input) and
// before persisting ResourceOutput to state (output). Drivers that do not
// implement the interface get today's behavior (no validation).
type ProviderIDValidator interface {
    ProviderIDFormat() ProviderIDFormat
}
```

Separate interface (not a method on `ResourceDriver`) keeps backward compat: existing drivers work unchanged.

### Validators (`workflow/interfaces/idformat.go`)

```go
// ValidateProviderID reports whether s matches format. Unknown and Freeform
// always return true (no constraint). Implementations are pure functions —
// no allocations in the hot path.
func ValidateProviderID(s string, format ProviderIDFormat) bool {
    switch format {
    case IDFormatUUID:
        return validateUUID(s)
    case IDFormatDomainName:
        return validateDomainName(s)
    case IDFormatARN:
        return validateARN(s)
    case IDFormatFreeform:
        return s != ""
    case IDFormatUnknown:
        return true // no constraint
    default:
        return true // forward-compat: unknown formats accepted
    }
}

func validateUUID(s string) bool {
    if len(s) != 36 {
        return false
    }
    if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
        return false
    }
    for i, r := range s {
        if i == 8 || i == 13 || i == 18 || i == 23 {
            continue
        }
        if !isHex(r) {
            return false
        }
    }
    return true
}

func validateDomainName(s string) bool {
    // RFC 1035 relaxed: non-empty, ≤253 chars, labels separated by '.',
    // labels 1-63 chars, [a-zA-Z0-9-], not starting/ending with '-'.
    // (Details spelled out in the plan's test table.)
    return len(s) > 0 && len(s) <= 253 && hasValidLabels(s)
}

func validateARN(s string) bool {
    // arn:<partition>:<service>:<region>:<account>:<resource>
    // Require 6 colon-separated segments; segments 3/4 may be empty.
    return hasARNShape(s)
}
```

Implementation detail: positional checks only (no regex) for `ValidateUUID` to keep the hot path allocation-free. `ValidateDomainName` and `ValidateARN` can use scan-based parsing — neither runs in a hot path.

### wfctl enforcement (`workflow/cmd/wfctl/infra_apply.go`)

Two enforcement points:

**Input side** — before `driver.Update(ctx, ref, spec)` and `driver.Delete(ctx, ref)`:

```go
if v, ok := driver.(interfaces.ProviderIDValidator); ok {
    if !interfaces.ValidateProviderID(ref.ProviderID, v.ProviderIDFormat()) {
        log.Printf("warn: wfctl: %s %q has non-conformant ProviderID %q "+
            "(expected %s). Driver will attempt self-heal if supported.",
            ref.Type, ref.Name, ref.ProviderID, v.ProviderIDFormat())
    }
}
```

Soft warn, not fail: the driver may have a heal path (v0.7.8 AppPlatformDriver.Update does). Hard-failing here would regress tonight's BMW unblock.

**Output side** — before `state.Save(ResourceState{ProviderID: r.ProviderID})` in `applyWithProviderAndStore`:

```go
if v, ok := driver.(interfaces.ProviderIDValidator); ok {
    format := v.ProviderIDFormat()
    if format != interfaces.IDFormatUnknown && format != interfaces.IDFormatFreeform {
        if !interfaces.ValidateProviderID(r.ProviderID, format) {
            return nil, fmt.Errorf(
                "driver %q returned malformed ProviderID %q for resource %q; "+
                "expected %s — state not persisted",
                providerType, r.ProviderID, r.Name, format)
        }
    }
}
```

Hard fail on output. This is the guardrail that would have prevented BMW's state from ever containing the name. The assumption: if a driver claims "UUID" and returns something that isn't, it's a plugin bug — fail loudly so it surfaces in CI output, not as a mystery deploy failure hours later.

### DO plugin adoption (`workflow-plugin-digitalocean` v0.7.9)

Each existing driver gets a one-line `ProviderIDFormat()` method:

| Driver | Format |
|---|---|
| `AppPlatformDriver` | `IDFormatUUID` |
| `ApiGatewayDriver` | `IDFormatUUID` |
| `DatabaseDriver` | `IDFormatUUID` |
| `CacheDriver` | `IDFormatUUID` |
| `CertificateDriver` | `IDFormatUUID` |
| `DropletDriver` | `IDFormatUUID` |
| `LoadBalancerDriver` | `IDFormatUUID` |
| `VPCDriver` | `IDFormatUUID` |
| `FirewallDriver` | `IDFormatUUID` |
| `ReservedIPDriver` | `IDFormatUUID` (if it exists in the plugin) |
| `DNSDriver` | `IDFormatDomainName` |
| `SpacesDriver` | `IDFormatFreeform` (bucket name, arbitrary shape) |

After this lands, the `isUUIDLike` check inside `AppPlatformDriver.resolveProviderID` (v0.7.8) becomes redundant for drivers whose UPDATE path gets called after wfctl's input-side validation — but we keep the heal logic because it recovers from pre-v0.7.9 bad state. Removing the inline check in the DO plugin post-v0.7.9 is a cleanup task, not this workstream.

---

## Data flow (post-v0.18.11 + v0.7.9, BMW-like failure)

Before:
```
wfctl infra apply → driver.Update(ref{ProviderID:"bmw-staging"}, spec)
  → DO API: PUT /v2/apps/bmw-staging → 400 "invalid uuid"
  → wfctl fails after 10 min health poll
```

After (bad state already in state store from pre-v0.7.9):
```
wfctl infra apply → input validation → WARN "non-conformant ProviderID; driver will self-heal"
  → driver.Update(...) → v0.7.8 resolveProviderID heals via findAppByName → succeeds with real UUID
  → state write validation → UUID confirmed → state rewritten with healed UUID
  → future applies pass input validation cleanly
```

After (hypothetical future plugin bug that returns a malformed ProviderID):
```
wfctl infra apply → driver.Create(...) → ResourceOutput{ProviderID:"bmw-staging"}
  → state write validation: FAIL with "driver digitalocean returned malformed ProviderID 
    \"bmw-staging\" for resource bmw-staging; expected UUID — state not persisted"
  → operator sees the bug within one apply cycle, not after state is corrupted
```

---

## Testing

**Unit tests (`workflow/interfaces/idformat_test.go`):**
- `TestValidateUUID` — canonical UUIDs, too short, too long, missing hyphens at each position, non-hex chars, upper/lower case
- `TestValidateDomainName` — valid names, IP addresses (reject), empty, leading/trailing hyphen, label too long, total too long, consecutive dots
- `TestValidateARN` — canonical ARNs, missing segments, empty partition, too many segments
- `TestValidateProviderID_Dispatch` — each `IDFormatXxx` routes to the correct validator; unknown/freeform pass

**Integration tests (`workflow/cmd/wfctl/infra_apply_validation_test.go`):**
- `TestInfraApply_InputValidation_Warns` — fake driver declares UUID format, state has a name-shaped ProviderID; assert wfctl logs WARN but lets the Update proceed (driver can heal)
- `TestInfraApply_OutputValidation_RejectsBadProviderID` — fake driver returns bad UUID in ResourceOutput; assert wfctl returns an error containing driver name, resource name, offending value, expected format; assert state is NOT written
- `TestInfraApply_OutputValidation_SkipsFreeform` — fake driver declares Freeform format and returns arbitrary string; assert wfctl passes it through to state without error
- `TestInfraApply_NoValidator_BackwardCompat` — fake driver doesn't implement `ProviderIDValidator`; assert wfctl behaves identically to pre-v0.18.11 (no validation, no warn)

**Regression verification:**
- Temporarily change `AppPlatformDriver.ProviderIDFormat()` to return `IDFormatUUID` in test, then force `Create` to return a name — assert the integration test fails loudly with the expected error text.

---

## Backward compatibility

- `ProviderIDValidator` is an optional interface. Drivers that don't implement it get today's behavior. No existing plugin breaks on upgrade.
- `ProviderIDFormat` enum includes `IDFormatUnknown` as the zero value. A driver can return that to explicitly opt out without removing the method.
- DO plugin v0.7.9 adopts the interface across all drivers but the `IDFormatFreeform` / `IDFormatUnknown` cases let it opt out per-driver if needed during rollout.

---

## Rollout

**Phase 1 — workflow v0.18.11:**
- Commit interface + validators + wfctl enforcement + integration tests.
- Unit-only coverage at interface layer; integration coverage at wfctl layer.
- Open PR on GoCodeAlone/workflow (feat/v0.18.11-typed-provider-id-validation).
- Copilot + review cycle. Merge. Team-lead tags v0.18.11.

**Phase 2 — workflow-plugin-digitalocean v0.7.9 (bundles with state-heal replication):**
- Bump workflow dep to v0.18.11.
- Every DO driver adds `ProviderIDFormat() ProviderIDFormat`.
- Audit drivers for any `isUUIDLike`-style inline checks that duplicate wfctl's new boundary check. Keep them if they protect the driver from legacy bad state (that's heal, not validation) — the interfaces are complementary, not redundant.
- Integration test pass across the driver matrix asserting declared format matches observed DO API responses.
- PR, review, merge, tag v0.7.9.

**Phase 3 — BMW bump:**
- Single PR: setup-wfctl v0.18.10.1 → v0.18.11 + workflow-plugin-digitalocean v0.7.8 → v0.7.9.
- Purely additive. No deploy.yml or infra.yaml changes.

---

## Observability tie-in (v0.18.10 Troubleshooter)

When output-validation fails, the error is structured enough that the existing `Troubleshooter` interface can surface it as a `Diagnostic`. Future work (not this workstream): extend the error path so the diagnostic includes the attempted ProviderID and the expected format as first-class fields, not just in the error message string. That's a v0.18.12+ polish item.

---

## Success criteria

- `workflow v0.18.11` merges with the interface, validators, enforcement, and tests.
- `workflow-plugin-digitalocean v0.7.9` merges with per-driver `ProviderIDFormat` declarations + state-heal replication (covered in separate design doc).
- Running the full integration-test suite against a fake driver that misbehaves produces a specific, actionable error naming the driver + expected format + offending string.
- BMW's state — still containing the stale name-as-ProviderID — is *transparently healed* on next apply with v0.18.11 + v0.7.9 because the DO driver's Update path heals, and wfctl's output validation confirms the healed UUID before persisting.
- Future hypothetical driver bugs that would have silently corrupted state now fail loudly at the state-write boundary within one apply cycle.

---

## Non-goals

- Level 2 typed `ProviderID` wrapper (task #41 / v0.19.0 scope).
- Level 3 proto oneof validation at the gRPC wire (v0.19.0 scope alongside typed args).
- Level 4 JSON Schema validation of `ResourceSpec.Config` (task #42 / v0.19.0 scope alongside plugin-manifest).
- Validation of ResourceOutput fields *other than* ProviderID (e.g., Outputs map, Status) — ProviderID is the one field whose malformed value directly causes state corruption. Other fields are informational; bugs there surface differently.
- wfctl generic self-heal path triggered by validation failure. Heal stays inside drivers that know how to look up by name.
- Retroactive state-file repair. Already handled by v0.7.8 reactive heal.
- AWS / GCP / Azure plugin adoption. Each provider's plugin adopts in its own repo on its own schedule; the interface is additive and safe for non-adopters.
