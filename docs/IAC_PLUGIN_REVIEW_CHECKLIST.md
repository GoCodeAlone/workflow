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

## (Bug classes BC-3 through BC-8 to follow in Task 8)
