# Retro — surface cigen info in wfctl MCP output (#854)

**Date:** 2026-06-04
**PR:** GoCodeAlone/workflow#854 (merged b482dff2b)
**Design:** docs/plans/2026-06-04-mcp-cigen-info-design.md
**Plan:** docs/plans/2026-06-04-mcp-cigen-info.md

## What shipped

Informational accuracy fix: wfctl's MCP output now describes the config-derived **cigen** CI generation. 5 surfaces + a `Contains` test:
- `mcp/server.go` Instructions (extracted to a `serverInstructions` const so it's testable), `mcp/docs.go` `docsOverview`, `mcp/wfctl_tools.go` (`ci_plan`/`generate_github_actions` descriptions), `mcp/setup_guide.go` CI/CD flow, `docs/mcp-tools-reference.md` (added the absent `ci_plan`/`generate_github_actions` entries + corrected the stale `scaffold_ci` entry).
- No new tools, no handler/behavior change, no release. The render path was already cigen-backed; only the *information* was stale.

## What the gates caught (and it was a lot, for a "doc change")

- **Design adversarial (2 cycles)** killed a self-inflicted factual error: the design claimed "Jenkins/CircleCI are template-based" — contradicting `docs/WFCTL.md:2142` (all four platforms are config-derived from the same CIPlan; workflow#810/ADR 0044). Also surfaced a **missed surface**: the `workflow://docs/setup-guide` resource + `scaffold_ci`/`generate_bootstrap` route the AI to a *different*, non-cigen CI flow — left unmentioned, the MCP would self-contradict on the very topic being fixed. Both fixed (all-four-config-derived; setup-guide "two CI paths" note).
- **Plan adversarial** verified the mcp-go API types (`ListTools()` → `map[string]*server.ServerTool`, `.Tool.Description`) and **ran the Task 7 runtime handshake live** before approving.
- **Copilot review** (functional again) found **4 real accuracy bugs** in the descriptions — ironic for an accuracy PR, but correct: the MCP `generate_github_actions` tool **re-analyzes from `yaml_content`** (it does NOT accept a pre-built/edited CIPlan) and renders **GitHub Actions only** (not GitLab); editing-then-rendering is the CLI `--from-plan` path. The descriptions implied otherwise. Also: the test *claimed* to lock the Instructions but never asserted them. All 4 addressed (clarified render-tool semantics; extracted `serverInstructions` const + asserted it).

## Lessons

- **"Informational" changes still need the full pipeline.** Three independent gates (design adversarial, Copilot, and the runtime check) each caught a distinct factual error that would have shipped *wrong* documentation — the worst outcome for an accuracy fix. The design self-challenge alone would not have caught the Jenkins/CircleCI error (I wrote it confidently).
- **Describe what the MCP tool *actually does*, not what the CLI does.** The cigen CLI (`wfctl ci generate`) supports `--from-plan` and all four platforms; the MCP `generate_github_actions` tool is a narrower surface (re-analyze, GHA-only). Conflating the two is the exact error class. Verify tool descriptions against the *handler*, not the CLI docs. → [[feedback_verify_artifact_exists_not_just_shape]] applies to tool *semantics*, not just existence.
- **Make claimed test coverage real.** A test comment that says "locks the Instructions" while asserting nothing about them is a latent gap Copilot rightly flagged; extracting the string to a package const made the assertion possible.

## Follow-ups (logged in plan out-of-scope)

- `generate_github_actions` handler reads `phase_config_yaml`/`wfctl_version` but the tool def does NOT declare them (latent schema gap) — clients can't discover them. Worth a small schema PR.
- `scaffold_environment`/`scaffold_infra` entries in `docs/mcp-tools-reference.md` also drift from their defs (not CI-related).

## Gate tally

design adversarial 2 cycles (C1 platform-coverage error, I1 missed setup-guide surface, I2 ref-doc format/stale scaffold_ci) + plan adversarial 1 cycle PASS (ran runtime handshake) + alignment PASS + scope-lock + code review + Copilot 4 findings addressed. 21/21 CI green → admin-merge.
