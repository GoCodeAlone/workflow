# Source Go Amendment Review

**Phase:** plan amendment
**Status:** PASS after bounded concrete P1/P2 corrections
**Reviewer:** independent Bohr, read-only; lead authored amendment.
**Authority:** ADR0057, same three tasks/one existing PR1022.

## Findings And Resolutions

- P1 Important: record-fixture does not prove generated plugin output. Task2C
  now runs actual built-wfctl generation, normalizes local Workflow replacement
  with structured go mod edit, native builds generated binary, places both
  generated sidecars in valid loader layout and asserts typed uppercase host
  call/discovery/lifecycle. Reviewer confirmed loader/name; final requested
  relative-to-absolute integration-only normalization applied literally.
- P2 Important: existing build-binary compile test accepts failure. Task2C now
  exercises actual default factory/Execute emitted output, independently checks
  module1.27.1, resolves unreleased source explicitly, builds/runs HTTP identity
  and shutdown. Reviewer PASS; no published v0.0.0 resolution claim.
- P3 Minor: historical1.26.8 commands retained. Explicit amendment states new
  A-E supersede final commands; old evidence remains historical, not Go27 proof.

## Scan Transcript

| Required class | Result / inspected boundary |
|---|---|
| Project guidance | Clean: existing tooling/ownership |
| Assumptions | P1/P2 resolved: generated consumers not adjacent tests |
| Repo precedent | Clean: generator owner/separate CI |
| Artifact class | Clean: external temporary output/package-local tests |
| YAGNI | Clean: no new API/fleet migration |
| Failure modes | P2 resolved: no accepted build-error proof |
| Security/privacy | Clean: protected bytes/no policy bypass |
| Infrastructure | Clean: local resources, no deploy |
| Multi-component | P1/P2 resolved: real generated consumer gates |
| Declared integration | P1/P2 resolved: generated runtime proof |
| UI contribution | Clean: none new |
| Rollback | Clean: reviewed revert/rebuild/release hold |
| Simpler alternative | Clean: root-only rejected |
| User intent | Clean: latestGo/default alignment |
| Existence/runtime | P1/P2 resolved: actual commands/runtime layout |
| Decomposition | Clean: three tasks/one PR |
| Verification class | P1/P2 resolved |
| Auth chain | Clean: admin ordinary review only, all checks first |
| Serial dependencies | Clean: stabilize then frozen-head proof |
| Rollback wiring | Clean: Task2E |
| Integration proof/matrix | P1/P2 resolved, Task2C/D and existing matrix |
| UI route proof | Clean: no new route |
| Infrastructure verification | Clean: maintained-image build/launch |
| Plugin loader | P1 resolved: matching runtime name/layout/sidecars |
| Config schema | Clean: existing API modules/routes/trigger |
| Identifiers | Clean: registered commands/override keys |
| Embedded code compile | Clean: no new compiled snippets |

Alignment: S1/S6→Task2; S2→Task3; S3→Task1; S4→all three; S5→Task2/3.
Reverse: Task1 S3/S4, Task2 S1/S4/S5/S6, Task3 S2/S4/S5. All task IDs occur
once in sole existing PR row. Programmatic strict manifest check PASS before
restamp. No source code/test success or CI completion claimed by this report.
