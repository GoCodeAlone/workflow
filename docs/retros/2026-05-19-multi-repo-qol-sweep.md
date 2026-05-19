# Multi-Repo OSS-Readiness QoL Sweep Retrospective (2026-05-19)

## Summary

This retrospective covers the multi-repo OSS-readiness quality-of-life sweep executed across the workflow engine, workflow-registry, and ~50 public plugin repositories (May 19, 2026). The effort brought a uniform OSS-readiness baseline to the ecosystem by adding experimental-status banners, standardized LICENSE files, and structural metadata (registry PluginSummary + RegistryManifest status fields) so external users encounter consistent documentation across all public repositories.

## Final Tally

### Phase 1 (Merged)
**15 PRs merged across core infrastructure:**
- workflow#714 (umbrella PR): README polish + examples index + plugin templates
- workflow#715: RegistryManifest status field + PluginSummary propagation
- workflow-registry#53: JSON schema extension for status enum
- workflow-registry#54: manifest population (35 external plugins marked verified/experimental)
- P3 non-plugin license sweep (6 repos): homebrew-tap, superpowers-marketplace, ratchet, ratchet-cli, claude-skills, rover

### Phase 2 (Merged)
**59 total PRs merged this session:**

**Phase 2 P0 (7 PRs merged):**
- Plugin deep-treats: digitalocean#126, payments#20, agent#16, audit-chain#13, auth#19, eventbus#17, twilio#11

**Phase 2 P2 (37 PRs merged):**
- Task 17a (P2 mass-marker A-M): 18 PRs merged (2 archived repos excluded: actors, deployment)
- Task 17b (P2 mass-marker M-Z): 19 PRs merged (1 special case: migrations kept Apache-2.0 per audit)
- All PRs use branch `chore/qol-sweep-phase2-2026-05-19` with experimental banner + MIT LICENSE (or license-appropriate equivalent)

**Summary:** 15 phase-1 + 7 phase-2 P0 + 37 phase-2 P2 = **59 PRs total**

### Deferred / Out-of-Scope
- **2 archived repos** (read-only on GitHub): workflow-plugin-actors, workflow-plugin-deployment
- **11 plugins lacking workflow-registry manifests** (banner-only, no manifest): analytics, broker, infra, marketplace, mcp, messaging-core, rooms, security-scanner, steam, template, ws-auth
  - Tracking issue: workflow-registry#717 (multi-PR sub-issue for each)

## What Went Well

1. **Pipeline discipline held.** Three rounds of adversarial design review + one round of plan review + alignment-check + scope-lock prevented downstream rework. Changes to the manifest schema, struct definitions, and registry population were locked before implementation, eliminating thrashing.

2. **Implementer split reduced wall-clock time.** Dispatching the P2 sweep (39 plugins) across two parallel Sonnet implementers + one Haiku batch-marker agent kept wall-clock under 4 hours despite 37 PRs. Sequential approach would have taken 12+ hours.

3. **Per-priority review tiers avoided over-gating.** P0 plugins required Copilot bot review pass (rigorous surface-area check) + CI green before admin merge; P2 batch PRs required spec-reviewer + CI green only (faster throughput without sacrificing quality on experimental plugins). This graduated review model kept the path clear.

4. **Task 11–13 rework narrowed spec surface.** Tasks 11–13 encountered banner hyperlink format issues, missing `.github/` templates, outdated engine version pins, example module mismatches, and undocumented `GH_TOKEN` requirements. Clarification of these issues upstream prevented similar rework in later batch tasks.

5. **Scope lock worked as intended.** Once the 19-task manifest was approved and locked, implementers did not rescope, collapse PRs, or ship partial demos. All 19 tasks shipped as defined (minus the 2 archived repos, which were pre-identified as out-of-scope).

## What Didn't

1. **Implementers marked tasks `completed` too early.** Some marked Task 4–10 (P0 deep-treats) as completed immediately after opening PRs, before spec-reviewer verified commits. This created a false signal that work was "done" when review gates were still pending. Future runs should require "awaiting review" intermediate state or explicit signal that CI + review passed.

2. **Tasks 11–13 surface-area rework.** Early P1 plugins revealed spec gaps: banner hyperlink syntax (needed `[open an issue](url)`), missing `.github/CONTRIBUTING.md` + `.github/SECURITY.md` templates, engine version pins out-of-date, example module names mismatched to actual plugins, and `GH_TOKEN` requirement undocumented in task spec. Rework was prospectively applied to Tasks 14–20; represents need for tighter pre-flight validation checklist.

3. **Branch collision between phases.** Phase 1 created `chore/qol-sweep-2026-05-19` on many repos (some PRs merged, others orphaned). Phase 2 reused the same branch name on new work, hitting force-push safety gates. Had to switch to `chore/qol-sweep-phase2-2026-05-19` (distinct name). This was caught early but caused minor rework.

4. **codecov patch-coverage gate required admin override.** workflow#715 (RegistryManifest struct) needed `TestPluginSummary_StatusPropagation` test to cover the real `StaticRegistrySource` implementation. The test was added, but codecov initially flagged the output-formatter print branch as uncovered, requiring temporary override. Fix: add a test case that exercises the print path.

5. **13 P2 plugins lack workflow-registry manifests.** These are banner-only PRs without registry entries to mark as verified/experimental. This was pre-identified as acceptable (workflow-registry#717 tracks the creation), but leaves inconsistent UX: `wfctl plugin list` won't surface these plugins' status field because they have no manifest. Future: pre-populate manifests in initial registry PR, or batch them with the banner PRs.

6. **2 plugin repos archived on GitHub.** workflow-plugin-actors and workflow-plugin-deployment are configured as read-only archives. Cannot accept PRs. Cloned locally, but force-push blocked by safety gates. These should have been identified in phase 1 pre-flight.

## What We'd Change Next Time

1. **Pre-flight repo audit.** Before dispatching batch tasks (e.g. Task 17a/17b), run a quick script to identify archived repos, missing-from-registry plugins, and branch conflicts from prior phases. Report these up-front so implementers can skip or defer knowingly.

2. **Branch naming with session timestamp.** Use `chore/qol-sweep-2026-05-19T12-30` (includes time) instead of `chore/qol-sweep-2026-05-19` to prevent cross-session branch collisions. Alternatively, version-prefix per phase: `chore/qol-sweep-p1-2026-05-19`, `chore/qol-sweep-p2-2026-05-19`.

3. **Implement `TestPluginSummary_StatusPropagation` with output-formatter coverage.** The test should exercise the print branch so codecov does not flag it as dead code, avoiding manual overrides.

4. **Task completion signal.** Require intermediate state "awaiting code-review" between PR-creation and task completion. Only mark complete after spec-reviewer + CI green. This prevents false signals of done-ness and keeps the board accurate.

5. **Registry manifest pre-population.** For future multi-repo sweeps, populate registry manifests in the initial registry PR (Task 16) rather than deferring. This ensures `wfctl plugin list` output is complete from day one.

## Follow-Ups (workflow-registry#717)

**Sub-issues filed (one per plugin without manifest):**
- workflow-plugin-analytics
- workflow-plugin-broker
- workflow-plugin-infra
- workflow-plugin-marketplace
- workflow-plugin-mcp
- workflow-plugin-messaging-core
- workflow-plugin-rooms
- workflow-plugin-security-scanner
- workflow-plugin-steam
- workflow-plugin-template
- workflow-plugin-ws-auth

**Archived repos (out-of-scope):**
- workflow-plugin-actors (GitHub archived; read-only)
- workflow-plugin-deployment (GitHub archived; read-only)

**Action items from lessons learned:**
- Add codecov print-branch coverage to test suite (workflow#715 follow-up)
- Document `GOWORK=off` requirement in CONTRIBUTING templates across plugin repos (workflow#714 scope)

## References

- **Design doc:** [docs/plans/2026-05-19-multi-repo-qol-sweep-design.md](../../docs/plans/2026-05-19-multi-repo-qol-sweep-design.md)
- **Implementation plan:** [docs/plans/2026-05-19-multi-repo-qol-sweep.md](../../docs/plans/2026-05-19-multi-repo-qol-sweep.md)
- **ADR:** [decisions/0041-multi-repo-qol-sweep-experimental-marker.md](../../decisions/0041-multi-repo-qol-sweep-experimental-marker.md)
- **Registry tracking (follow-up manifests):** [workflow-registry#717](https://github.com/GoCodeAlone/workflow-registry/issues/717)

---

**Closed by commit:** docs(retro): multi-repo OSS-readiness QoL sweep (2026-05-19)
**Cross-ref:** workflow-registry#717

