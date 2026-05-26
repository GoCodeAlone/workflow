# verify_capabilities test fixtures

Fixtures for `plugin_verify_capabilities_test.go` (workflow#765).

Each scenario directory is a self-contained Go module. Tests build in-place
with `go build -mod=readonly`; binary emitted to `t.TempDir()`.

## Maintenance

When workflow SDK adds a new transitive dep that fixtures pick up, regenerate
each fixture's `go.sum`:

```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go mod tidy)
done
git add cmd/wfctl/testdata/verify_capabilities/*/go.sum
```

The `replace github.com/GoCodeAlone/workflow => ../../../../..` directive
resolves 5-ups from each scenario directory to the workflow repo root.
DO NOT use an absolute path — it diverges across developer machines.

## Scenarios

- `good/` — plugin.json version=0.0.0, ldflag injects v0.1.0 → PASS (CI artifact case)
- `release-good/` — plugin.json version=1.2.3, ldflag injects v1.2.3 → PASS (release case)
- `missing-ldflag/` — plugin.json version=0.0.0, no ldflag (Version="dev" → ResolveBuildVersion returns "(devel) [@ sha]") → FAIL
- `version-drift/` — plugin.json version=1.2.3, ldflag injects v0.9.0 → FAIL
- `name-drift/` — plugin.json name="foo", binary advertises Name="bar" (see Task 6 — created separately) → FAIL

## SDK semantics

`sdk.ResolveBuildVersion` returns its argument unchanged UNLESS the arg is one
of `{"", "dev", "(devel)"}`, in which case it consults `debug.ReadBuildInfo()`
and returns `"(devel) [@ <sha>[.dirty]]"`. So:

- Initial `var Version = "dev"` + no ldflag → wire Version is `"(devel) [@ sha]"`
- Initial `var Version = "dev"` + ldflag `-X .Version=v1.2.3` → wire Version is `"v1.2.3"`
- Initial `var Version = "0.0.0"` + no ldflag → wire Version is `"0.0.0"` (NOT a build-info fallback; `"0.0.0"` is NOT in the SDK's reset set)

The `missing-ldflag` fixture uses `Version = "dev"` deliberately so it exercises
the `(devel)` fallback path, not the `"0.0.0"` pass-through.
