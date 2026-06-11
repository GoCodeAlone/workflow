### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-06-11-capability-matrix-design.md
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**

| id | class | loc | issue | recommendation | resolution |
|---|---|---|---|---|---|
| D1 | Assumptions / missing failure modes | `## Ecosystem Capability Matrix` | The original design required a curated taxonomy but did not say where it lives or how unmapped raw plugin types are handled. Without this, website routes and agent references can drift between generator runs. | Add explicit taxonomy ownership, versioning, aliases, policy tags, and `uncategorized` handling. | Resolved in `## Taxonomy Ownership`. |
| D2 | Artifact-class precedent / existence-validity | `## Application Capability Profile` | The original `--workflow workflow.yaml` example risked implying a single-file app model. Existing apps can compose `wfctl.yaml`, `*.wfctl.yaml`, imports, provider fragments, and lockfiles. | Require reuse of existing config discovery/import/merge behavior before adding new repeatable flags. | Resolved in `## Application Capability Profile`. |
| D3 | Multi-component validation / website handoff | `## Website Documentation` | The original design left website consumption split between release artifacts and checked-in snapshots, with no source metadata. That makes docs freshness and version provenance ambiguous. | First implementation should commit Workflow-owned snapshots and include generator/source/taxonomy metadata; release-artifact fetching can follow after website renderer exists. | Resolved in `## Website Documentation` and assumptions. |

**Findings (Minor):**
- `D4` [YAGNI] [`## Application Consistency Check`]: Strict CI failure mode is deferred, which is correct, but the command name `check` may imply failure semantics too early. Recommendation: plan should make default warning-only behavior explicit in help text and tests.

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Design cites repo guidance, ecosystem audit, plugin contract design, and ADR 0048; no conflict found. |
| Assumptions under attack | Finding | Taxonomy stability and app config discovery were underspecified; both resolved. |
| Repo-precedent conflicts | Clean | `wfctl audit plugins`, `wfctl docs generate`, and registry commands already own related extraction/metadata surfaces. |
| Artifact-class precedent | Finding | Existing app/config tooling is not single-file-only; resolved by requiring existing discovery/import/merge behavior. |
| YAGNI violations | Minor | Strict policy failure is deferred; plan should preserve warning-only default. |
| Missing failure modes | Finding | Unmapped types and stale website provenance were missing; resolved by taxonomy `uncategorized` and source metadata. |
| Security / privacy | Clean | Design is read-only and explicitly avoids secret values; app profiles remain local unless committed by app repo. |
| Infrastructure impact | Clean | No cloud/resource mutations; generated docs/tests only. |
| Multi-component validation | Finding | Website handoff needed source/version metadata; resolved. |
| Rollback story | Clean | Additive commands/artifacts can be reverted without runtime migration. |
| Simpler alternative not considered | Clean | Design rejects docs-only and website-only generation with durable ADR 0049. |
| User-intent drift | Clean | Covers both requested use cases: ecosystem discovery and per-app capability enforcement hints. |
| Existence / runtime-validity | Clean | Plan must verify `wfctl` command dispatch/help, config loaders, JSON schema output, and generated Markdown consumers. |

**Options the author may not have considered:**

1. Extend `wfctl audit plugins` instead of adding `wfctl capability`: cheaper command surface, but it would overload audit health with product discovery and app profiling. Keep `audit` for quality findings; use `capability` for inventory/profile/check.
2. Generate website docs directly from `data/registry`: simpler for released plugins, but misses local plugin repos, app profiles, lockfiles, and Workflow config analysis.

**Verdict reasoning:** PASS. The review found no Critical issues. Important findings were resolved in the design before this report was committed. Remaining risk is plan-level: keep the first implementation warning-only for app policy checks and avoid broad source-code scanning.
