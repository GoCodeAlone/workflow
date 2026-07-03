# wfctl Install Lifecycle Design

## Global Design Guidance

Source: workspace `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/PORTFOLIO.md`, `docs/FOLLOWUPS.md`

| guidance | design response |
|---|---|
| Prefer existing tool/plugin capabilities over rebuilding | Document existing `wfctl update`, release assets, Homebrew formula automation, `wfctl plugin add/lock/install/ci/update/remove`, and global plugin flags instead of adding installer code. |
| Update docs when behavior or commands change | Treat `README.md`, `docs/WFCTL.md`, and generated website inputs as canonical docs surfaces. |
| Use real Workflow app/tool boundaries for scenario-like proof | Validate docs against actual release assets, current CLI help, Homebrew formula metadata, and local command behavior. |
| Keep examples under documented repo layout | Add docs under `docs/`; no new examples or scratch roots. |

Portfolio inventory: `docs/PORTFOLIO.md` lists `workflow`, `homebrew-tap`, and `gocodealone-website`; `docs/FOLLOWUPS.md` has no existing wfctl install-doc follow-up. Existing release automation and tap publication are reused.

## Goal

Make wfctl installation, update, and plugin lifecycle documentation accurate across Workflow, the GoCodeAlone Homebrew tap, and the website surfaces that advertise wfctl.

## Current Evidence

- Latest Workflow release checked on 2026-07-03: `v0.84.3`, with raw `wfctl-{darwin,linux}-{amd64,arm64}`, `wfctl-windows-amd64.exe`, and `checksums.txt` assets.
- Workflow release workflow publishes checksums and dispatches `update-wfctl` to `GoCodeAlone/homebrew-tap` for stable tags.
- `homebrew-tap` `origin/main` has `Formula/wfctl.rb` at `0.84.3`; local brew cache may lag until `brew update`.
- `homebrew-tap` `main` has no branch protection as of 2026-07-03, so generated formula PRs are not blocked by required review settings. PR #59 (`automation/update-wfctl-v0.84.3`) merged after CodeQL checks with only a Copilot comment.
- `wfctl` already supports `update`, `update --check`, project plugin manifest/lock commands, `plugin ci`, and `-g/--global` plugin installs.

## Recommended Approach

Use a documentation-first repair.

1. Workflow owns canonical wfctl installation and plugin lifecycle docs.
2. Homebrew tap owns concise tap/formula docs and update/troubleshooting instructions.
3. Website hardcoded marketing copy points to the maintained install surfaces and stops pinning stale wfctl versions.
4. Releases and generated docs flow through existing Workflow release and website sync-docs automation.

Alternative considered: add a shell installer script. Rejected for now because current published assets, checksums, `wfctl update`, and Homebrew formula already cover install/update needs; a script would add another maintenance and trust surface.

Alternative considered: make Homebrew the only recommended path. Rejected because Linux CI, Windows users, and browser/manual downloads need a documented non-Homebrew path.

## Scope

In scope:

- Replace stale README install one-liners.
- Add a dedicated wfctl installation/lifecycle doc covering Homebrew, terminal release download, browser/manual download, source install, self-update, PATH, and verification.
- Expand plugin lifecycle docs: project manifest, lockfile, CI install, global install, update, remove, and compatibility/checksum notes.
- Expand the tap README with tap setup, formulas, install/upgrade examples, and automation notes.
- Verify that generated Homebrew formula PRs can merge without mandatory reviews; if required reviews are later enabled, remove that branch requirement or add an explicit generated-PR bypass before relying on auto-merge.
- Update website hardcoded wfctl install snippets away from stale `v0.60.17`.
- After PRs merge, release Workflow and the website as needed, then verify downstream repository dispatches ran: Homebrew tap formula update, workflow-registry sync, workflow-scenarios bump, workflow-editor notification, website docs sync, and multisite ingest.

Out of scope:

- New installer binaries or install scripts.
- Changing release asset names.
- Changing plugin installer behavior.
- Automatically refreshing all generated website docs before Workflow docs are released.

## Security Review

The docs must not encourage unauthenticated blind execution. Non-Homebrew terminal installs include downloading `checksums.txt` and verifying SHA-256 before moving the binary into `PATH`. Homebrew uses formula SHA-256 verification. `go install` is documented as a source/developer path, not the preferred release path. Plugin docs call out checksum verification, lockfiles for CI, and the risk of `--skip-checksum`.

## Infrastructure Impact

No runtime infrastructure changes. Release impact is limited to normal Workflow and website release pipelines. Homebrew tap updates continue through existing repository dispatch. Generated PR auto-merge depends on repository settings not requiring human review for bot-created formula PRs.

## Multi-Component Validation

Validation must cover:

- GitHub release asset existence and checksums for latest stable Workflow release.
- Local CLI help for documented `wfctl update` and plugin lifecycle commands.
- Homebrew tap formula version and formula syntax/test where available.
- Website build or at least type/build validation after copy changes.
- Post-merge release and downstream automation: GitHub Actions release jobs complete successfully; generated tap PR merges; website docs sync PR and release/deploy workflows complete.

## Assumptions

- Workflow `v0.84.3` is still the latest stable release at implementation time; if a newer stable release appears, docs use non-pinned `latest` commands where possible and cite the exact checked version only as evidence.
- `gocodealone-website` generated docs consume Workflow docs after Workflow release/sync; hardcoded website copy must be fixed separately.
- Homebrew users may need `brew update` before seeing a just-merged formula.

## Rollback

Docs-only rollback: revert the documentation PRs. Release rollback: cut a new Workflow/website patch release from the reverted state if bad install docs were published. Tap README rollback: revert tap docs PR; formulas are unchanged by this design. If a downstream dispatch fails after a release, rerun the failed workflow after fixing the blocker; do not edit generated formula checksums by hand.

## Self-Challenge

1. The laziest solution is only patching `README.md`; rejected because stale instructions also exist in `docs/WFCTL.md`, website copy, and tap docs.
2. Fragile assumption: Homebrew freshness. Mitigation: document `brew update` and validate `origin/main` formula separately from local brew cache.
3. Potential YAGNI: extensive plugin lifecycle prose. Kept because the user explicitly asked for global install, update/upgrade plugins, and lifecycle parity with plugin managers.
