### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-06-07-workflow-docs-ecosystem-design.md
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**

| id | class | loc | issue | recommendation | resolution |
|---|---|---|---|---|---|
| D1 | Missing failure modes / infra impact | `## Versioning` | Versioned API docs could grow unbounded if every Workflow/plugin version is committed to the website snapshot. | Add retention/pruning policy and keep first implementation bounded. | Resolved: design now publishes `latest` plus bounded recent Workflow lines; plugin version lines require reliable release metadata. |
| D2 | Assumptions / user-intent drift | `## Defect-Ledger Docs And 0.8 Cleanup` | Alias fixes could become permanent undocumented alternate schemas or be removed too quickly, either preserving quirks or breaking users. | Add explicit compatibility policy: canonical docs, modernize rewrite, warnings where practical, aliases supported through v1.0. | Resolved: design now includes compatibility policy. |

**Findings (Minor):**

| id | class | loc | issue | recommendation |
|---|---|---|---|---|
| D3 | Security / privacy | `## Security Review` | Registry repo URLs are trusted too broadly for first implementation. | Restrict first generator to public GoCodeAlone GitHub repos; defer wider third-party support to a separate trust-boundary review. |

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Design cites `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, ecosystem audit, and CI boundary ADR; no conflict found. |
| Assumptions under attack | Finding | Alias support lifetime was underspecified; fixed by compatibility policy. |
| Repo-precedent conflicts | Clean | `wfctl` ownership matches existing lifecycle/audit precedent. |
| Artifact-class precedent | Clean | Docs sync already commits generated Markdown snapshots; design extends that artifact class. |
| YAGNI violations | Clean | Version dropdown is metadata-first; no custom UI required initially. |
| Missing failure modes | Finding | Version retention/pruning was missing; fixed. |
| Security / privacy | Finding | Repo trust boundary needed tightening; fixed for first implementation. |
| Infrastructure impact | Finding | Site-size growth needed bounded retention; fixed. |
| Multi-component validation | Clean | Design requires `wfctl docs generate` + website sync + Starlight build + representative config execution. |
| Rollback story | Clean | Design has rollback paths for generator, website sync, and 0.8 behavior changes. |
| Simpler alternative not considered | Clean | Design rejects website-only scripts and pkg.go.dev-only links with concrete trade-offs. |
| User-intent drift | Clean | Design covers holistic docs, released Go API docs, versioning, quirks cleanup, and doc removal. |
| Existence / runtime-validity | Clean | Plan must verify `wfctl` command surfaces and consumed website routes before relying on generated output. |

**Options the author may not have considered:**
1. `pkgsite` static mirror: closer visual parity with pkg.go.dev, but heavier and less compatible with Starlight/navigation/version metadata. Keep as future option if stdlib Markdown output proves too thin.
2. Split docs IA and 0.8 cleanup into unrelated initiatives: lower PR risk, but it would leave a public defect doc in place while adding better API docs. Current phased design keeps order explicit.

**Verdict reasoning:** PASS. The review found no Critical issues. Important issues were resolved in the design before this report was committed; remaining concerns are plan-level sizing and execution sequencing.
