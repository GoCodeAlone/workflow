### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-06-16-plugin-lock-and-domain-migration-design.md
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- `D1` [Assumptions under attack] [Plugin Lock Semantics]: Digest fields were named but canonical input was underspecified; different YAML formatting or ignored manifest fields could create false drift. Recommendation: define typed sorted canonical JSON scope. _Resolution: design now defines manifest and lock digest field scope._
- `D2` [Security / privacy] [Blackorchid Multisite Migration]: Source capture did not explicitly constrain scraping to operator-owned public assets. Recommendation: add ownership/access guard and forbid credential bypass. _Resolution: design now limits capture to owned/rights-cleared public material._
- `D3` [Missing failure modes] [Plugin Lock Semantics]: Existing lockfiles without digest fields could make CI fail across repos without a repair path. Recommendation: local one-time digest bootstrap; CI emits exact repair command. _Resolution: design now includes bootstrap + CI repair behavior._

**Findings (Minor):**
- `D4` [Infrastructure impact] [Domain Migration Order]: `gigbagg.com` still needs a separate content/intent decision because DigitalOcean has an A record while live NS is Hover. Recommendation: keep as inspect-before-cutover. _Resolution: already present in action table._

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Uses core for bootstrap CLI behavior; provider DNS stays in plugins. |
| Assumptions under attack | Finding | D1/D3 tightened digest and legacy-lock assumptions. |
| Repo-precedent conflicts | Clean | `docs/WFCTL.md`, `cmd/wfctl`, and provider plugin ownership match existing layout. |
| Artifact-class precedent | Clean | Design follows existing ADR + `docs/plans/*-design.md` shape. |
| YAGNI violations | Clean | No new broad package manager; scope limited to plugin lock/install. |
| Missing failure modes | Finding | D3 legacy lockfile path fixed. |
| Security / privacy | Finding | D2 content capture rights guard fixed. |
| Infrastructure impact | Clean | Lists GitHub repo, DNS, plugins, workflow, and multisite effects. |
| Multi-component validation | Clean | Requires CLI fixture, plugin-host, DNS generated YAML, GH Actions, and route proofs. |
| Rollback story | Clean | Each runtime/plugin/DNS path has rollback. |
| Simpler alternative not considered | Clean | Minimal `plugin lock && plugin install` documented and rejected. |
| User-intent drift | Clean | Addresses requested lock CI behavior, DNS migration, and Blackorchid hosting route. |
| Existence / runtime-validity | Clean | Existing multisite content contract, workflow plugin paths, and DNS runbooks were inspected. |

**Options the author may not have considered:**
1. `wfctl plugin check` only: smaller than `plugin ci`, but less discoverable for CI installs because users still need a second install command.
2. Store lock provenance in sidecar `.wfctl-lock.meta`: avoids schema churn but splits atomicity; rejected because lock and provenance must review together.
3. Keep Blackorchid in Wix until a manual rewrite: safer for dynamic widgets but delays DNS consolidation; design keeps Wix until preview proof, so migration is reversible.

**Verdict reasoning:** PASS. The tangible issues found in cycle 1 were resolved in the design before this report was committed. Remaining risk is execution complexity across repos, which belongs in the implementation plan's task grouping and verification steps.
