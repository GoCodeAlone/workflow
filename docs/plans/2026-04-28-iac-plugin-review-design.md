---
status: approved
area: plugins
owner: workflow
external_refs: []
verification:
  last_checked: 2026-04-28
  result: pending
supersedes: []
superseded_by: []
---

# IaC Plugin Cross-Provider Review Discipline & Audit

**Date:** 2026-04-28
**Author:** Jon Langevin
**Status:** Approved (design)

## Summary

Extract the bug-class taxonomy surfaced by the workflow-plugin-digitalocean v0.8.0 (P-2 phase) review cycle into cross-provider discipline that benefits all IaC provider plugins (DO, AWS, GCP, Azure, Tofu, CI-generator) and prevents per-provider rediscovery. Four deliverables: a markdown checklist, an executable Go test-helper package, an extension to the existing strict-contracts migration tracker, and a generic plugin-pattern-reviewer skill.

## Goals

1. **Capture P-2 learnings as transferable discipline** so the same bug classes don't have to be rediscovered on every IaC provider plugin.
2. **Make the discipline executable** — markdown for human reviewers + Go test helpers for CI so regressions surface automatically, not in production gRPC dispatch.
3. **Auto-apply via reviewer skill** — code-reviewer agents reviewing any workflow plugin pick up the matching pattern checklist via plugin.json field detection, without per-prompt re-explanation.
4. **Close the audit→migration loop** — fold pre-migration findings into the v0.9.0 strict-contracts migration so downstream readers see the bug-class fix lineage.

## Non-Goals

- Strict-contracts migration of IaC plugins themselves (that's tracked in [strict-grpc-plugin-contracts](2026-04-26-strict-grpc-plugin-contracts.md) and scheduled for v0.9.0+; this design feeds findings INTO that migration).
- Reviewer-skill content for non-IaC patterns (auth/sso/payment/agent etc.) — pattern dispatch IS pre-allocated; only IaC content populated in v1.
- Cross-cloud feature parity (e.g., bringing AWS to feature parity with DO) — out of scope.

## Bug-class taxonomy (the deliverable's content)

Distilled from the P-2 review cycle, every class shipped at least once in v0.8.0:

### BC-1: Plan/Diff cascade gap
**Failure mode:** A driver's `Diff` implementation either always returns nil (stub) or only compares a subset of fields, so in-place updates silently no-op or emit spurious changes.
**Repro pattern:** P-3 round 1 of F4 (`AppPlatformDriver.Diff` only compared `image`); F7 round 1 (`FirewallDriver.Diff` always returned `NeedsUpdate=false`).
**Fix shape:** Diff compares every canonical config field; the matching `appOutput` / `fwOutput` populates `Outputs[*]` for every field Diff reads.
**Retroactive applicability:** every IaC plugin's resource drivers — high.

### BC-2: structpb gRPC boundary (legacy compat plugins only)
**Failure mode:** `Outputs["..."]` stores a typed slice (`[]int`/`[]string`/`[]godo.X`) that's rejected by `structpb.NewStruct` at the wfctl→plugin gRPC boundary. Reader-side type assertions fail post-roundtrip → assertions return nil → Diff emits perpetual spurious changes.
**Repro pattern:** F7 round 2 (whole Diff cascade fix was a no-op in production gRPC mode until canonical-shape Outputs landed in round 3).
**Fix shape:** Convert all typed slices to `[]any` of structpb-compatible primitives or `map[string]any` BEFORE storing; readers handle both pre- and post-roundtrip shapes via tolerant coercer helpers.
**Retroactive applicability:** every IaC plugin still on legacy compat (DO/AWS/GCP/Azure/Tofu — strict-mode plugins are immune). Becomes obsolete after v0.9.0 strict-contracts migration of each plugin.

### BC-3: Outputs-vs-Diff invariant
**Failure mode:** Diff reads `current.Outputs["X"]` for some field X but no writer (Create/Update/Read) ever populates X. Diff sees `""` and emits a spurious FieldChange every reconcile.
**Repro pattern:** F4 round 4 (image gap — Diff compared image but `appOutput` didn't write it).
**Fix shape:** For every key Diff reads, verify a writer populates it. Add a `derive*FromAppSpec` helper if the value can be reconstructed from the live spec.
**Retroactive applicability:** every IaC plugin — high.

### BC-4: Validation matrix
**Failure mode:** Field validators check key presence but not value validity. Common variants:
- TCP port: `0` or negative or `> 65535` accepted (F4 round 5).
- Float-as-int: `123.9` silently truncated to `123` (F7 round 3).
- Empty-string slice element: `["", "valid"]` not filtered (F7 round 2).
- Non-string for string-typed enum: `expose: true` (Go bool) silently treated as "omitted" → defaults to public (F4 round 3).
**Fix shape:** TDD coverage for each {field, kind} pair: negative, zero, max, max+1, fractional, empty-string element, wrong-type.
**Retroactive applicability:** every IaC plugin — high.

### BC-5: Plan-time vs Apply-time documentation accuracy
**Failure mode:** `plugin.json` description, CHANGELOG entry, or doc-comment claims validation runs "at plan time" but the validator only fires from `Create`/`Update` (the Apply path) — `DOProvider.Plan` doesn't call the driver for create actions. User reads the docs, expects a plan-time error, gets an apply-time error instead.
**Repro pattern:** F7 round 3 + F4 round 5.
**Fix shape:** Reword to "fail at apply time, before any [provider] API call" (or implement true plan-time validation by extending Diff/Validate paths — bigger refactor).
**Retroactive applicability:** every IaC plugin's CHANGELOG/doc-comments — moderate.

### BC-6: Diff-side vs Apply-side parity
**Failure mode:** Apply-side has tighter validation (e.g., non-string `expose` errors loudly via `applyExposeInternal`), but Diff-side accepts the bad value silently (e.g., `canonicalExpose` defaults to `"public"`). Plan output misleadingly suggests a successful update when Apply will actually error.
**Repro pattern:** F4 round 3 (code-reviewer Observation B).
**Fix shape:** Mirror apply-side validation in Diff-side helpers, OR have Diff call apply-side validators and surface the error in Plan output.
**Retroactive applicability:** every IaC plugin where Diff and Create/Update read the same config keys — high.

### BC-7: CIDR widening regression scan (firewall/SG/NSG drivers)
**Failure mode:** An Update path that swaps `inbound_rules.sources` (or equivalent) silently widens CIDR ranges instead of erroring. Security regression: caller intends to narrow, plugin silently accepts a wider rule.
**Repro pattern:** identified in P-2 F7 review prompt (the v5.0 framing missed this in workflow's earlier P-3 review; v5.2.0 framing caught it).
**Fix shape:** Fail Update when desired CIDR sources ⊊ current CIDR sources unless explicit `--strict-cidr=false` flag.
**Retroactive applicability:** AWS SG, GCP firewall, Azure NSG, DO firewall — high.

### BC-8: Schema canonical-key registration
**Failure mode:** Plugin adds a config key (e.g., `http_port_protocol`) but doesn't propose adding it to workflow's `interfaces.canonicalKeySet`. Currently benign because the canonical-schema enforcement isn't wired into `wfctl validate`, but when it is, configs using the key will be rejected by the validator.
**Repro pattern:** F5 round 1 (code-reviewer Finding 2).
**Fix shape:** PR against workflow framework to add the key to `canonicalKeySet` + `iac_canonical_schema.json` enum, or document plugin-scoped status in CHANGELOG.
**Retroactive applicability:** every IaC plugin that has added schema keys post-canonical-schema landing — moderate.

## Architecture (4 deliverables)

### D-1: Cross-provider review checklist (markdown)

**Path:** `workflow/docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`

Single document, ~400-600 lines. Each bug class above gets a section with:
- Failure mode
- Repro pattern (cite the v0.8.0 review round + commit if applicable)
- Fix shape
- Test pattern (link to D-2 helper)
- Reviewer scan procedure (concrete grep / code-read steps)

References from:
- `workflow/CONTRIBUTING.md` (top-level link)
- Each IaC plugin's `CONTRIBUTING.md` (added in retroactive Phase C, one PR per plugin)

### D-2: Executable Go test-helper package

**Path:** `workflow/plugin/sdk/iaclint/`

Public API (initial):

```go
package iaclint

import (
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

// AssertOutputsRoundTripStructpb verifies that every value in outputs survives
// structpb.NewStruct → AsMap() round-trip without type degradation that breaks
// downstream type assertions. Closes BC-2.
func AssertOutputsRoundTripStructpb(t *testing.T, outputs map[string]any)

// AssertDiffPopulatesAllOutputFields verifies that for every key the Diff
// implementation reads from current.Outputs, the matching writer (Create/
// Update/Read) populates it. Closes BC-3.
func AssertDiffPopulatesAllOutputFields(
    t *testing.T,
    driver interfaces.ResourceDriver,
    sampleSpec interfaces.ResourceSpec,
)

// ValidationKind enumerates the standard {field, value-class} probes.
type ValidationKind int
const (
    KindTCPPort ValidationKind = iota  // probes: 0, -1, 1, 65535, 65536
    KindNonNegativeInt                  // probes: 0, -1, max
    KindNonEmptyString                  // probes: "", "  ", "x"
    KindStringEnum                      // probes: each value, "" (absent), random string, non-string
    KindIntegerOnlyFloat                // probes: 1.0, 1.9, max int float, NaN, Inf
)

// AssertValidationMatrix runs the standard battery for the named field
// against the parser. Closes BC-4. Caller passes a parser closure that
// extracts and validates one config field.
func AssertValidationMatrix(
    t *testing.T,
    parser func(cfg map[string]any) (any, error),
    fieldName string,
    kind ValidationKind,
)
```

Each IaC plugin imports this in its test suite and calls the matchers for every driver/field. CI catches regressions per-PR.

### D-3: Strict-contracts tracker extension

**Modify:** `workflow/docs/plans/2026-04-26-strict-grpc-plugin-contracts.md` and its companion design doc.

Add to the `implementation_refs` and the per-plugin migration table:

| Plugin | Strict migration status (target v0.9.0) | Pre-migration audit findings |
|---|---|---|
| workflow-plugin-aws | active | (Phase C audit fills this in) |
| workflow-plugin-azure | verified locally, awaiting PR | (Phase C audit fills this in) |
| workflow-plugin-ci-generator | merged | n/a (already shipped) |
| **workflow-plugin-digitalocean** | **pending (v0.8.0 ships legacy compat)** | **F4/F5/F7 cycle: BC-1, BC-2, BC-3, BC-4, BC-5, BC-6, BC-8 closed in v0.8.0; BC-7 not applicable. Issue #37 (BC-5 Update naming consistency) deferred to v0.8.x** |
| **workflow-plugin-gcp** | **pending** | (Phase C audit fills this in) |
| **workflow-plugin-tofu** | **pending** | (Phase C audit fills this in) |

Each IaC plugin's v0.9.0 strict-migration PR adds a "Pre-migration findings closed" CHANGELOG sub-section that references the Phase C audit issues and the bug classes addressed in the migration.

### D-4: Generic plugin-pattern-reviewer skill

**Path:** `~/.claude/skills/workflow-plugin-reviewer/SKILL.md` (workspace-local; not pushed upstream to superpowers as a contribution unless cross-team adoption emerges).

**Skill content shape:**

```markdown
---
name: workflow-plugin-reviewer
description: Use when reviewing a PR in any GoCodeAlone/workflow-plugin-* repository — auto-detects the provider pattern from plugin.json and applies the matching cross-provider bug-class checklist.
---

# Workflow Plugin Review Discipline

When reviewing a PR in a workflow plugin repo:

1. Read `plugin.json` to identify which provider pattern the plugin implements.
2. Load the matching checklist from the workflow framework docs:
   - `iacProvider` → `workflow/docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`
   - `authProvider` → (TBD — checklist pending)
   - `paymentProvider` → (TBD — checklist pending)
   - `agentProvider` → (TBD — checklist pending)
   - `auditProvider` → (TBD — checklist pending)
   - `module-step` → (TBD — generic step-driver checklist pending)
3. Apply the matched checklist as part of the bug-class scan, in addition to the standard adversarial-review framing.
4. If the plugin is on legacy compat dispatch (no `internal/contracts/` package, plugin.json `mode` ≠ `strict`), include the structpb-boundary scan from BC-2.

(Pattern dispatch table is pre-allocated. Only IaC has populated content as of 2026-04-28.)
```

The skill auto-loads when the code-reviewer (or any reviewer agent) is invoked on a plugin repo. Future provider-pattern checklists slot into the dispatch table without rewriting the skill.

## Sequencing

### Phase A — Author the discipline (this cycle)

- D-1 (markdown checklist): 1 implementer, ~4 hours.
- D-2 (Go test-helper package): 1 implementer, ~6 hours including matcher API design + tests for the helpers themselves.
- D-3 (tracker extension): doc-only edit, ~30 min.
- D-4 (generic skill): ~1 hour.

Single team, 2 implementers split D-1+D-3+D-4 vs D-2.

### Phase B — P-1 BMW cleanup (existing plan)

Per `core-dump/_worktrees/docs-iac-design/docs/plans/2026-04-27-iac-do-staging-implementation.md` Task P-1.BMW. The Phase A checklist is reviewer input — when BMW runs `wfctl infra plan/align/security-check` against real config, any new findings feed back into the checklist via Phase A revision.

### Phase C — Retroactive fix-forward across IaC plugins

For each IaC plugin (DO/AWS/GCP/Azure/Tofu/CI-generator):
1. Run the checklist against the current main HEAD.
2. File one issue per bug-class instance found.
3. Open per-plugin per-finding PRs (matches `feedback_per_agent_worktree_per_task_pr.md`).
4. Each PR's CHANGELOG entry references the bug class it closes.
5. After all plugin audit PRs land, the strict-migration tracker rows for that plugin show "Pre-migration findings closed" — feeds into v0.9.0 strict migration cleanly.

Estimated 4-5 findings per plugin × 6 plugins = 25-30 small PRs. Multi-team execution; can split across multiple cycles.

## System Impact

Per `core-dump/CLAUDE.md` matrix (this design doc dated 2026-04-28+ requires the section):

- **auth, authorization, anti-cheat, malware/ecology, sandbox/code execution, network/hacking, filesystem, process/OS, email/social/comms, NPC sim, factions/orgs, economy/stocks/crypto, infrastructure/IoT/ICS, media/gossip, legal/heat/agency, forensics/IR/defense, assistant/VERA, achievements/Steam, client desktop apps, shell/terminal, world history, content/narrative, telemetry:** None — this design touches only the workflow framework + IaC plugin repos. Core-dump game systems are unaffected.
- **tests:** Phase A D-2 ships a public `workflow/plugin/sdk/iaclint/` test-helper package with its own unit tests. Phase C audit PRs add per-plugin tests via the helpers. Tests run via each plugin's existing CI.

## Risks & Mitigations

- **D-2 helper API churn during Phase C** — if Phase C audit findings reveal the helpers don't cover a class, helper API expands. Mitigation: ship D-2 as `v0.x.x` with explicit "API may evolve until cross-plugin adoption is complete" CHANGELOG note; tag stable when all 6 IaC plugins have adopted.
- **D-4 skill adoption depends on reviewer agents auto-loading it** — if the skill isn't picked up, the discipline lives in docs only. Mitigation: include explicit "if reviewing a workflow plugin, invoke `workflow-plugin-reviewer`" line in the team-conventions reviewer prompts (P-2 used `agents/team-conventions.md`; we update that to chain).
- **Phase C surge on AWS/GCP/Azure** — these plugins are larger than DO; per-plugin audit could surface 8-12 findings each (vs DO's 4-5). Mitigation: prioritize highest-severity bug class (BC-2 structpb boundary) first across all plugins, then BC-1/BC-3 as the next pass; defer cosmetic classes to a later cycle.
- **v0.9.0 strict migration may obsolete some bug classes mid-Phase-C** — if strict migration lands first for a plugin, the BC-2 audit becomes moot. Mitigation: each Phase-C audit PR is small enough to land or close cleanly regardless of migration timing.

## Acceptance

- D-1: `workflow/docs/IAC_PLUGIN_REVIEW_CHECKLIST.md` committed to `main`. CONTRIBUTING.md links to it. Each bug class has the four sub-sections (failure / repro / fix / scan).
- D-2: `workflow/plugin/sdk/iaclint/` package committed; matchers covered by unit tests; usage example in package godoc. Demonstrate by importing in workflow-plugin-digitalocean's test suite as a smoke check (no audit PR yet, just import + run).
- D-3: Strict-contracts tracker shows DO/GCP/Tofu rows; DO row's "Pre-migration audit findings" cell populated with the v0.8.0 closures.
- D-4: Skill file at `~/.claude/skills/workflow-plugin-reviewer/SKILL.md` with pattern-dispatch table; reviewer agents on a workflow-plugin-* PR successfully load + reference the IaC checklist.

After Phase A merges to main, Phase B (P-1 BMW) auto-dispatches per the autonomous mandate.
