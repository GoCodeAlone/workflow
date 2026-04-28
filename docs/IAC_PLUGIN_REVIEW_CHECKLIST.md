# IaC Plugin Review Checklist

This checklist captures the cross-provider bug-class taxonomy surfaced during
plugin review cycles (initially `workflow-plugin-digitalocean v0.8.0`, P-2
phase). Each bug class is reproducible across every IaC provider plugin (DO,
AWS, GCP, Azure, Tofu, CI-generator). Apply this checklist when reviewing any
plugin PR that touches ResourceDriver implementations, Outputs writers, or
config-field validators.

For executable enforcement, see the test-helper package
`workflow/plugin/sdk/iaclint/`. Each bug class names the matcher that closes
it.

## How to use this checklist

- **As a reviewer:** scan the diff for each bug class. The "Reviewer scan"
  sub-section names the concrete grep / read steps.
- **As a plugin author:** import `github.com/GoCodeAlone/workflow/plugin/sdk/iaclint`
  in your test suite and call the named matcher for every driver/field.
- **As a maintainer auditing existing plugins:** apply each bug-class scan to
  the plugin's `main` HEAD and file one issue per finding.

## BC-1: Plan/Diff cascade gap

**Failure mode:** A driver's `Diff` implementation either always returns nil
(stub) or only compares a subset of fields, so in-place updates silently
no-op or emit spurious changes on every reconcile.

**Repro pattern:** `workflow-plugin-digitalocean` PR #35 round 1
(`AppPlatformDriver.Diff` only compared `image`); PR #36 round 1
(`FirewallDriver.Diff` always returned `NeedsUpdate=false`).

**Fix shape:** `Diff` compares every canonical config field; the matching
`appOutput` / `fwOutput` writer populates `Outputs[*]` for every field `Diff`
reads (see also BC-3).

**Test pattern:** combine `iaclint.AssertDiffPopulatesAllOutputFields` (BC-3
matcher) with explicit `_DetectsXChange` test cases per field. See
`workflow-plugin-digitalocean/internal/drivers/firewall_test.go` for the
canonical structure (8 sub-cases per Diff: each field's positive case +
no-change baseline + reorder/normalization cases).

**Reviewer scan:**

1. `grep -nE 'func.*\bDiff\b' internal/drivers/*.go provider/drivers/*.go`
2. For each Diff, read its body. Does it always return nil? Does it only
   compare one or two fields when the Create/Update accepts more?
3. If the answer is "yes" to either, surface as **BC-1 BLOCKING**.

## BC-2: structpb gRPC boundary (legacy compat plugins only)

**Failure mode:** `Outputs["..."]` stores a typed slice (`[]int`, `[]string`,
`[]godo.X` etc.) that is **rejected** by `structpb.NewStruct` at the
wfctl→plugin gRPC boundary. After the boundary round-trip, reader-side type
assertions fail (`current.Outputs["X"].([]godo.Y)` returns `ok=false`),
treating current state as nil and emitting perpetual spurious changes from
Diff. The whole Diff cascade fix becomes a no-op in production gRPC mode.

**Repro pattern:** `workflow-plugin-digitalocean` PR #36 round 2 (Diff cascade
fix landed but typed-slice Outputs broke under realistic gRPC dispatch; round
3 introduced canonical-shape Outputs to close the gap).

**Constraint reference:** `internal/grpc_dispatch_test.go:30-32` in any
external-dispatch plugin documents the structpb constraint:

> "Slices must be `[]any`; native typed slices (`[]string`, `[]int`, etc.)
> are rejected by `structpb.NewStruct` with 'proto: invalid type'."

**Fix shape:**

- `Outputs[<int-slice key>]` → `[]any` of `float64` (structpb collapses all
  numerics to `float64`; storing as `float64` from the start makes the shape
  symmetric with both pre- and post-roundtrip reads).
- `Outputs[<string-slice key>]` → `[]any` of `string`.
- `Outputs[<struct-slice key>]` → `[]any` of `map[string]any`, with a flatten
  helper per godo struct type.
- Reader-side helpers (`outputsAsIntSlice`, `outputsAsStringSlice`, etc.)
  accept BOTH typed-slice (in-process pre-roundtrip path) AND `[]any` of
  primitive/map (post-roundtrip path).

**Test pattern:** import `iaclint` and call `iaclint.AssertOutputsRoundTripStructpb(t, out.Outputs)`
in the driver's Create/Read/Update tests. For Diff, write a
`_StructpbBoundary_DiffSurvivesRoundTrip` test that builds an Outputs map,
round-trips through `structpb.NewStruct`/`AsMap()`, then calls Diff against a
matching desired and asserts `NeedsUpdate=false`.

**Reviewer scan:**

1. Check `plugin.json` for `mode: strict`. If `strict`, BC-2 doesn't apply.
2. Otherwise: `grep -nE 'Outputs\["[^"]+"\] *= *\[\]' internal/drivers/*.go`
   surfaces typed-slice writes. Each is a **BC-2 BLOCKING** instance.
3. `grep -nE 'current\.Outputs\["[^"]+"\]\.\(\[\]' internal/drivers/*.go`
   surfaces typed-slice reads in Diff. Each is a **BC-2 BLOCKING** instance.

## BC-3: Outputs-vs-Diff invariant

**Failure mode:** `Diff` reads `current.Outputs["X"]` for some field X but no
writer (`Create` / `Update` / `Read`) ever populates X. Diff sees the
zero-value (`""`, `0`, `nil`) and emits a spurious `FieldChange` every
reconcile, even when the live resource matches the desired spec.

**Repro pattern:** `workflow-plugin-digitalocean` PR #35 round 4 (the
`image` gap — `AppPlatformDriver.Diff` compared `image` but `appOutput`
didn't write it, so every reconcile emitted a phantom image change).

**Fix shape:** For every key Diff reads, verify a writer populates it. Add
a `derive*FromAppSpec` (or `deriveOutputsFromCurrent`) helper if the value
can be reconstructed from the live spec when the upstream API doesn't
return it directly. The output writer (`appOutput`, `fwOutput`, etc.) is
the single source of truth — Diff reads only what the writer commits.

**Test pattern:** the driver implements `iaclint.DiffOutputKeyDeclarer`
(returning the static slice of canonical output keys its Diff reads), then
the test calls `iaclint.AssertDiffPopulatesAllOutputFields(t, driver,
sampleSpec)`. The matcher invokes `Create` with `sampleSpec` and asserts
every declared key is present in `out.Outputs`.

**Reviewer scan:**

1. `grep -nE 'current\.Outputs\["[^"]+"\]' internal/drivers/*.go` —
   enumerate every Outputs key Diff reads.
2. `grep -nE '\.Outputs\["[^"]+"\] *=' internal/drivers/*.go` — enumerate
   every Outputs key the writers populate.
3. Compute the set difference. Any key in (1) but not (2) is a **BC-3
   BLOCKING** finding. Cross-link with BC-1 (Diff that reads phantom keys
   often pairs with shallow Diff field coverage).

## BC-4: Validation matrix

**Failure mode:** Field validators check key presence but not value
validity. Common variants observed in the v0.8.0 cycle:

- TCP port: `0` or negative or `> 65535` accepted (PR #35 round 5).
- Float-as-int: `123.9` silently truncated to `123` (PR #36 round 3) — gRPC
  numeric coercion delivers JSON numbers as `float64`, so int-typed config
  keys must reject fractional values explicitly.
- Empty-string slice element: `["", "valid"]` not filtered (PR #36 round
  2) — silently propagated downstream as an invalid CIDR or tag.
- Non-string for string-typed enum: `expose: true` (Go bool) silently
  treated as "omitted" → defaulted to `public` (PR #35 round 3).

**Repro pattern:** PR #35 rounds 3 + 5 and PR #36 rounds 2 + 3 in
`workflow-plugin-digitalocean`.

**Fix shape:** Per-field TDD coverage for each {field, kind} pair —
negative, zero, max, max+1, fractional, empty-string element, wrong-type,
known-good. Validators that reject loudly with a context-bearing error
beat validators that silently coerce or default.

**Test pattern:** `iaclint.AssertValidationMatrix(t, parser, fieldName,
kind)` for each {field, kind} pair the driver accepts. Available kinds:
`KindTCPPort`, `KindNonNegativeInt`, `KindNonEmptyString`,
`KindIntegerOnlyFloat`, and `iaclint.WithStringEnumOptions(allowed)` for
string enums. The matcher exercises the standard probe battery for each
kind and asserts the parser accepts/rejects per the documented contract.

**Reviewer scan:**

1. `grep -nE 'func .*\b(canonical|parse|validate)[A-Z][A-Za-z]*\b' \
   internal/drivers/*.go` — enumerate every field validator.
2. For each validator, identify which `KindX` it should match (port →
   TCPPort, count/replicas → NonNegativeInt, name/identifier →
   NonEmptyString, id/numeric → IntegerOnlyFloat, exposure/visibility →
   StringEnum).
3. Confirm the test suite has `AssertValidationMatrix` coverage for every
   such validator. Missing coverage is a **BC-4** finding (severity scales
   with blast radius — security-relevant validators promote to BLOCKING).

## BC-5: Plan-time vs Apply-time documentation accuracy

**Failure mode:** `plugin.json` description, `CHANGELOG.md` entry, or
package doc-comment claims validation runs "at plan time", but the
validator only fires from `Create` / `Update` (the Apply path) — the IaC
provider's `Plan` doesn't dispatch to the driver for create actions. The
operator reads the docs, expects a plan-time error before any provider
API call, instead observes a partial apply with side effects.

**Repro pattern:** `workflow-plugin-digitalocean` PR #36 round 3 + PR #35
round 5 — both shipped CHANGELOG entries claiming plan-time validation
when the validator only ran inside `applyXxx` helpers on the Apply path.

**Fix shape:** Two acceptable resolutions:

- **Documentation rewording (cheap):** change "fail at plan time" to "fail
  at apply time, before any DigitalOcean API call". Match doc-comments and
  CHANGELOG language to the actual call site.
- **Real plan-time validation (refactor):** call the validators from the
  driver's `Diff` (or a dedicated `Validate` method) so plan output
  surfaces the error before any apply state mutates. Larger surface area;
  defer to v0.9.0 strict-contracts migration when the SDK gains a
  first-class `Validate` hook.

**Test pattern:** No `iaclint` matcher — verify by reviewer scan and
manual test cases. (Future iaclint version may add an `AssertDocClaims`
matcher that compares CHANGELOG/plugin.json claim strings against the
actual call sites; out of scope for v1.)

**Reviewer scan:**

1. `grep -niE 'plan[- ]?time' plugin.json CHANGELOG.md docs/*.md \
   internal/drivers/*.go` — every claim of plan-time behavior.
2. For each hit, trace the named validator. Does it run from
   `Driver.Diff` (or `Driver.Validate` if the SDK supports it), or only
   from `Driver.Create` / `Driver.Update`?
3. Mismatch is a **BC-5** finding. Default severity Minor unless the
   misclaim hides a security validator (then promote to Important).

## BC-6: Diff-side vs Apply-side parity

**Failure mode:** Apply-side has tighter validation (e.g., non-string
`expose` errors loudly via `applyExposeInternal`), but Diff-side accepts
the bad value silently (e.g., `canonicalExpose` defaults to `"public"`).
Plan output misleadingly suggests a successful update; Apply will actually
error. Symmetric variants exist: Diff treats whitespace-only string as
unset, Apply rejects it; Diff sorts a slice for stable comparison, Apply
preserves caller order and rejects unsorted input.

**Repro pattern:** `workflow-plugin-digitalocean` PR #35 round 3
(code-reviewer Observation B — `canonicalExpose` defaulted non-string
input to public while `applyExposeInternal` rejected it).

**Fix shape:** Mirror apply-side validation in Diff-side helpers, OR have
Diff call apply-side validators directly and surface the error via the
plan output. The Diff/Apply split should differ only in side effects, not
in input acceptance.

**Test pattern:** No `iaclint` matcher — verify by reviewer scan and
manual test cases. Pattern: write a parametric test that feeds the same
input through both the Diff-side canonicalizer and the Apply-side
validator and asserts both raise the same error class on bad input.

**Reviewer scan:**

1. `grep -nE 'func .*\b(canonical|normalize|equalize)[A-Z]' \
   internal/drivers/*.go` — every Diff-side canonicalizer.
2. For each, find the matching Apply-side validator (`apply<Field>` /
   `validate<Field>` or inline check in `Create` / `Update`).
3. Compare the input-acceptance contracts. Any silent acceptance on the
   Diff side that errors on the Apply side is a **BC-6** finding.

## BC-7: CIDR widening regression scan (firewall / SG / NSG drivers)

**Failure mode:** An Update path that swaps `inbound_rules.sources` (or
the equivalent — AWS SG `IpRanges`, GCP firewall `sourceRanges`, Azure
NSG `sourceAddressPrefixes`) silently widens CIDR ranges instead of
erroring. Security regression: caller intends to narrow `10.0.0.0/24` to
`10.0.0.0/28`, plugin accepts and applies a wider rule. No diff signal,
no audit trail.

**Repro pattern:** identified in the v5.2.0 adversarial framing of
`workflow-plugin-digitalocean` PR #36 (the v5.0.0 framing missed it on
the round-3 review; v5.2.0 caught it as a regression class). Not
reproduced by the v0.8.0 driver because DO firewall replaces all rules
atomically — but every CIDR-widening Update path in any provider's
firewall/SG/NSG driver is at risk.

**Fix shape:** Fail Update when desired CIDR `sources` ⊊ current CIDR
`sources` (i.e., the desired set is strictly broader than current) unless
an explicit caller flag (`--strict-cidr=false` on `wfctl infra apply`, or
plugin config `allow_cidr_widening: true`) opts out. The default is
deny-on-widen; the opt-out makes the broadening intentional and auditable.

**Test pattern:** No `iaclint` matcher — verify by reviewer scan and
manual test cases. Pattern: build a `current.Outputs` with narrow CIDRs,
build a `desired.Config` with strictly broader CIDRs, call `Update`, and
assert it returns an error citing the widened rule.

**Reviewer scan:**

1. `grep -nE \
   'inbound_rules\.sources|outbound_rules\.destinations|IpRanges|sourceRanges|sourceAddressPrefixes' \
   internal/drivers/*.go` — every CIDR-bearing rule field.
2. For each Update path that mutates such a field, trace whether the path
   compares desired vs current CIDR set membership before applying.
3. Any path that swaps without comparing is a **BC-7 BLOCKING** finding
   (security-class severity).

## BC-8: Schema canonical-key registration

**Failure mode:** Plugin adds a config key (e.g., `http_port_protocol`,
`droplet_pool_strategy`) but doesn't propose adding it to the workflow
framework's `interfaces.canonicalKeySet` or the matching enum in
`schema/iac_canonical_schema.json`. Currently benign because the
canonical-schema enforcement isn't wired into `wfctl validate` for IaC
configs — but when it lands (v0.9.0+), configs using the unregistered
key will be rejected by the validator with a confusing
"unknown-canonical-key" error, even though the plugin accepts the key.

**Repro pattern:** `workflow-plugin-digitalocean` PR #35 round 1
(code-reviewer Finding 2 — `http_port_protocol` added to the App Platform
driver without a matching workflow-side canonical-key registration PR).

**Fix shape:** Either land a small PR against the workflow framework
adding the new key to `canonicalKeySet` + the JSON Schema enum, or
document the plugin-scoped status in `CHANGELOG.md` ("Adds plugin-scoped
config key X; will be promoted to canonical schema in workflow vY.Z."),
so downstream readers know the validator gap is intentional.

**Test pattern:** No `iaclint` matcher — verify by reviewer scan against
`workflow/interfaces/iac_canonical_keys.go`'s `canonicalKeySet` (clone the
workflow repo locally; the set is small enough to grep).

**Reviewer scan:**

1. `grep -nE '"[a-z][a-z0-9_]+":' internal/drivers/*.go internal/canonical*.go` —
   enumerate every config key the plugin reads.
2. For each, check whether the key appears in
   `workflow/interfaces/iac_canonical_keys.go` `canonicalKeySet` or the
   plugin's CHANGELOG documents the plugin-scoped status.
3. Any new key not in either location is a **BC-8** finding.
