### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-06-12-control-plane-descriptor-validation-design.md`
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- None.

**Findings (Minor):**
- `D1` [Existence/runtime-validity] [Data Flow]: `go list -m -f {{.Dir}}` only resolves the released module after the plan adds a `go.mod` requirement. Recommendation: make the module pin an explicit plan task before writing bundle tests.
- `D2` [Assumptions under attack] [A3]: "downgrade" rejection is shape validation of `downgrade_floor_version`, not semver policy comparison. Recommendation: plan must name this limit and keep semver floor policy deferred to T568 host adapter work.
- `D3` [Multi-component validation] [R3]: `go list -deps ./cmd/wfctl` proves non-test imports only if run without `-test`. Recommendation: plan verification must use the exact non-test command and assert no `workflow-plugin-control-plane` line.

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Design cites equivalent guidance because `docs/design-guidance.md` is absent; it follows `AGENTS.md`/`docs/REPO_LAYOUT.md` fixture and GOWORK rules. |
| Assumptions under attack | Minor | A3 is honest but must stay explicit in the plan; T566 cannot claim full downgrade-policy enforcement. |
| Repo-precedent conflicts | Clean | Reuses `cmd/wfctl/editor_bundle.go`, `plugin.contracts.json`, and `validate-contract` patterns instead of adding a new CLI surface. |
| Artifact-class precedent | Clean | Tests/fixtures stay in `cmd/wfctl` package/testdata or temp dirs, matching existing editor-bundle tests. |
| YAGNI violations | Clean | New subcommand/runtime loader explicitly rejected; scope is fixture and validation proof only. |
| Missing failure modes | Clean | Malformed metadata and invalid registry fields are expected to fail closed. |
| Security/privacy at architecture level | Clean | No auth, secrets, remote execution, or authority movement; released module trust boundary is pinned. |
| Infrastructure impact | Clean | No infra/deploy/migration/secrets impact; only Go module/test/doc changes. |
| Multi-component validation | Minor | Non-test dependency proof must be exact; otherwise a test import could be mistaken for runtime cleanliness. |
| Rollback story | Clean | Revert PR removes test-only pin/docs/tests; no runtime deploy rollback. |
| Simpler alternative not considered | Clean | Copying JSON fixture was considered and rejected because it would not prove released artifact consumption. |
| User-intent drift | Clean | Directly maps T566: Workflow/wfctl bundle validation, negative cases, no runtime/plugin cycles. |
| Existence/runtime-validity | Minor | `go list -m` requires an explicit module pin before the fixture can resolve reliably. |

**Options the author may not have considered:**

1. Use `go mod download -json github.com/GoCodeAlone/workflow-plugin-control-plane@v0.1.0` in tests instead of `go list -m`: avoids permanent `go.mod` pin, but adds network/runtime variability inside tests and makes reproducibility weaker.
2. Vendor a minimal fixture copy: faster and offline, but violates the T566 proof requirement because the copied metadata can drift from the released package.

**Verdict reasoning:** PASS with minor plan constraints. The design is narrow and matches existing wfctl contract paths. The plan must explicitly pin v0.1.0 before module-dir tests, phrase downgrade coverage as input-shape validation only, and verify non-test dependency cleanliness with `go list -deps ./cmd/wfctl`.
