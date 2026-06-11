### Adversarial Review Report

**Phase:** plan
**Artifact:** docs/plans/2026-06-11-capability-matrix.md
**Design:** docs/plans/2026-06-11-capability-matrix-design.md
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**

| id | class | loc | issue | recommendation | resolution |
|---|---|---|---|---|---|
| P1 | Existence / runtime-validity | Task 6 Step 3 | The first plan used `cmd/wfctl/wfctl.yaml` as both `--manifest` and `--workflow` for app smoke testing. That file is wfctl's embedded Workflow config, not an application `wfctl.yaml`, so the representative command could test the wrong artifact class or fail for the wrong reason. | Add dedicated app fixture files and use them for CLI app/check tests and final smoke invocation. | Resolved: Task 4 now creates `cmd/wfctl/testdata/capability/app/*`; Task 6 uses that fixture. |

**Findings (Minor):**

| id | class | loc | issue | recommendation |
|---|---|---|---|---|
| P2 | Verification scope | Task 5 | `schema.json` may be manually maintained in the first PR. | Keep schema small and add a decode/field-presence test so drift is caught until a generator exists. |
| P3 | Naming semantics | Task 4 | `capability check` sounds enforcement-oriented while first implementation exits 0. | Help/docs/tests should explicitly say "warning-only in this release". |

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Plan uses `GOWORK=off`, clean worktree, docs/tests with CLI behavior, and Workflow-owned extraction. |
| Assumptions under attack | Clean | Taxonomy and inference uncertainty are tested and warning-oriented. |
| Repo-precedent conflicts | Clean | Command shape follows `audit`/`docs`; config loading reuses `config.NewFileSource`. |
| Artifact-class precedent | Finding | App smoke fixture originally used wrong artifact class; resolved. |
| YAGNI violations | Clean | Strict mode, runtime plugin execution, website renderer, and source scanning are out of scope. |
| Missing failure modes | Clean | Missing providers, uncategorized types, malformed manifests, and policy-risk warnings are planned. |
| Security / privacy | Clean | Plan verifies no secret values; commands are read-only and do not execute plugin binaries. |
| Infrastructure impact | Clean | No external resource mutation; generated docs only. |
| Multi-component validation | Clean | Plan covers registry+local plugin, app config+lockfile+plugin manifests, CLI, JSON, and Markdown. |
| Rollback story | Clean | Single additive PR revert, no migration. |
| Simpler alternative not considered | Clean | ADR rejects docs-only and website-only alternatives. |
| User-intent drift | Clean | Tasks map to ecosystem matrix, app profile, agent/CI check, and website-ready artifacts. |
| Existence / runtime-validity | Finding | Representative app command fixed to use real app fixture files. |
| Over-decomposition / under-decomposition | Clean | Six tasks map to independently testable slices. |
| Verification-class mismatch | Clean | CLI commands have help/invocation tests; docs artifacts have decode/render checks; generated artifacts are tested. |
| Auth/authz chain composition | Clean | No runtime auth chain is introduced; authz is only inventoried as evidence. |
| Hidden serial dependencies | Clean | Single PR, sequential commits; no parallel hidden shared-file work. |
| Missing rollback wiring | Clean | Runtime-affecting CLI addition has revert-only rollback in plan. |
| Missing integration proof | Clean | CLI smoke exercises real command dispatch and fixture data. |
| Infrastructure verification mismatch | Clean | No infra changes. |
| Plugin-loader runtime layout | Clean | Plan reads manifests only; no external plugin process loading. |
| Config-validation schema rules | Clean | Fixture app config is required for app command tests; no generated app config is introduced. |
| Identifier / naming-convention match | Minor | `check` warning-only semantics must be explicit. |

**Options the author may not have considered:**

1. Put `capability` under `wfctl audit`: fewer top-level commands, but conflates quality findings with product/application inventory. Current split is clearer.
2. Skip generated snapshots and only emit CLI output on demand: less repo churn, but fails the website-consumption requirement and weakens agent discoverability.

**Verdict reasoning:** PASS. One important runtime-validity issue was fixed before this report was committed. Remaining findings are minor and should be enforced through help text and artifact tests during execution.
