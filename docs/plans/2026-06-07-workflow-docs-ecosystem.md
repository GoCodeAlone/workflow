# Workflow Docs Ecosystem Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Build a coherent Workflow documentation system with 0.8 config-quirk cleanup, released-version Go API docs, plugin API docs, and website navigation that serves YAML assemblers and Go extenders.

**Architecture:** Workflow owns behavior fixes and `wfctl docs generate`; the website consumes generated Markdown and version metadata during `sync-docs`. Public docs are reorganized into reader paths, and defect-ledger docs are removed only after verified fixes ship.

**Tech Stack:** Go 1.26.4, `go/parser`, `go/doc`, `go/ast`, `go list`, Node 24, Astro Starlight, existing `scripts/sync-docs.mjs`, GitHub releases.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 3
**Tasks:** 9
**Estimated Lines of Change:** ~2200

**Out of scope:**
- Full pkg.go.dev UI parity.
- Executing plugin code during docs generation.
- Route-to-pipeline compatibility syntax.
- Third-party non-GoCodeAlone plugin repo ingestion.
- Removing alias support before v1.0.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | fix: normalize config authoring quirks | Task 1, Task 2, Task 3, Task 4 | fix/config-quirks-075 |
| 2 | feat: generate Go API docs with wfctl | Task 5, Task 6 | feat/wfctl-go-docs |
| 3 | docs: reorganize website docs and API reference | Task 7, Task 8, Task 9 | feat/website-go-api-docs |

**Status:** Draft

## Global Design Guidance Mapping

| Guidance | Plan wiring |
|---|---|
| `GOWORK=off` for Go commands | Every Go verification command includes `GOWORK=off`. |
| Prefer `wfctl` for lifecycle tooling | Task 5 creates `wfctl docs generate`; Task 7 dogfoods it from website sync. |
| Keep examples under `example/` | No new root examples; tests use temp dirs or existing fixtures. |
| Plugin/provider boundaries | Task 6 reads plugin registry metadata; generator does not execute plugin code. |
| Fix known product foibles before docs normalize them | Tasks 1-4 fix and verify quirks before Task 9 removes the public quirks page. |

## Task 1: HTTP Server Port Alias And Registry Metadata

**Files:**
- Modify: `plugins/http/modules.go`
- Modify: `plugins/http/schemas.go`
- Modify: `cmd/wfctl/type_registry.go`
- Test: `plugins/http/plugin_test.go`
- Test: `cmd/wfctl/validate_test.go`

**Step 1: Write failing tests**

Add tests proving `http.server` accepts:

```yaml
config:
  port: 8080
```

Expected behavior:
- factory creates `module.NewStandardHTTPServer(name, ":8080")`;
- `address` still wins when both `address` and `port` exist;
- `wfctl validate` accepts `port` and type registry lists it as an alias.

**Step 2: Verify RED**

Run:

```bash
GOWORK=off go test ./plugins/http ./cmd/wfctl -run 'TestHTTPServerPortAlias|TestValidateHTTPServerPortAlias' -count=1
```

Expected: FAIL because `port` is ignored or validation still requires `address`.

**Step 3: Implement**

Add a small HTTP config normalizer in `plugins/http/factories.go`:
- if `address` is a non-empty string, use it;
- else if `port` is `int`, `int64`, `float64`, or numeric string, convert to `":<port>"`;
- reject invalid ports with a clear factory error.

Update schema/type registry metadata so docs/completions know `port` is accepted but `address` is canonical.

**Step 4: Verify GREEN**

Run:

```bash
GOWORK=off go test ./plugins/http ./cmd/wfctl -run 'TestHTTPServerPortAlias|TestValidateHTTPServerPortAlias' -count=1
```

Expected: PASS.

**Step 5: Regression proof**

Temporarily revert only the production normalizer. Re-run the RED command.

Expected: FAIL on `port` alias. Restore fix and re-run; Expected: PASS.

**Rollback:** revert this task commit; configs using canonical `address` remain valid.

## Task 2: Database Step Alias Normalization

**Files:**
- Modify: `module/pipeline_step_db_query.go`
- Modify: `module/pipeline_step_db_exec.go`
- Modify: `module/pipeline_step_db_query_cached.go` if it shares the same authoring surface.
- Test: `module/pipeline_step_db_query_test.go`
- Test: `module/pipeline_step_db_exec_test.go`
- Test: `cmd/wfctl/modernize_test.go`

**Step 1: Write failing tests**

Add tests proving:
- `module` aliases `database` for `step.db_query`, `step.db_exec`, and cached query if applicable;
- `args` aliases `params`;
- `mode: many` normalizes to `list`;
- `mode: one` normalizes to `single`;
- canonical keys win when both forms exist.

**Step 2: Verify RED**

Run:

```bash
GOWORK=off go test ./module ./cmd/wfctl -run 'TestDB(Query|Exec).*Alias|TestModernize.*DB' -count=1
```

Expected: FAIL on missing `database`, ignored `args`, or invalid mode.

**Step 3: Implement**

Create unexported helpers near DB step code:

```go
func configStringAlias(cfg map[string]any, canonical string, aliases ...string) string
func configListAlias(cfg map[string]any, canonical string, aliases ...string) []string
func normalizeDBMode(mode string) string
```

Use helpers from query/exec factories. Keep runtime fields canonical.

Extend `modernize.AllRules()` so `wfctl modernize --fix` rewrites `module` to `database`, `args` to `params`, and `many`/`one` to `list`/`single`.

**Step 4: Verify GREEN**

Run:

```bash
GOWORK=off go test ./module ./cmd/wfctl -run 'TestDB(Query|Exec).*Alias|TestModernize.*DB' -count=1
```

Expected: PASS.

**Step 5: Regression proof**

Revert DB production helper usage, keep tests, re-run targeted command.

Expected: FAIL on alias tests. Restore and re-run; Expected: PASS.

**Rollback:** revert this task commit; canonical DB configs remain valid.

## Task 3: Request, Response, And Conditional Ergonomics

**Files:**
- Modify: `module/pipeline_step_request_parse.go`
- Modify: `module/pipeline_step_json_response.go`
- Modify: `module/pipeline_step_conditional.go`
- Modify: `plugins/pipelinesteps/plugin.go`
- Modify: `handlers/testhelpers_test.go`
- Modify: `cmd/wfctl/type_registry.go`
- Test: `module/pipeline_step_request_parse_test.go`
- Test: `module/pipeline_step_json_response_test.go`
- Test: `module/pipeline_step_conditional_test.go`
- Test: `plugins/pipelinesteps/plugin_test.go`

**Step 1: Write failing tests**

Add tests proving:
- `step.request_parse` with `format: json` sets `parseBody=true`;
- `step.response` is registered and behaves like `step.json_response`;
- template body strings resolving to JSON arrays/objects encode as arrays/objects, not quoted JSON strings;
- `step.conditional` supports:

```yaml
config:
  if: "{{ .status == \"active\" }}"
  then: proceed
  else: reject
```

Expected conditional output includes `next_step`.

**Step 2: Verify RED**

Run:

```bash
GOWORK=off go test ./module ./plugins/pipelinesteps -run 'TestRequestParseFormatAlias|TestJSONResponseTemplateRawJSON|TestConditionalIfThenElse|TestStepResponseAlias' -count=1
```

Expected: FAIL because aliases/mode are absent.

**Step 3: Implement**

Implementation rules:
- `format: json` and `format: form` imply body parsing; JSON remains content-type/JSON parsing behavior.
- `step.response` maps to `NewJSONResponseStepFactory()` in plugin manifest/factory/type registry.
- JSON response template output: if resolved string is valid JSON object/array, unmarshal to `any` before encoding; leave scalars and invalid JSON as strings.
- Conditional: add mode fields `ifExpr`, `thenStep`, `elseStep`; if `if` exists, evaluate template/expr result as bool/string truthy; route to then/else.

**Step 4: Verify GREEN**

Run:

```bash
GOWORK=off go test ./module ./plugins/pipelinesteps -run 'TestRequestParseFormatAlias|TestJSONResponseTemplateRawJSON|TestConditionalIfThenElse|TestStepResponseAlias' -count=1
```

Expected: PASS.

**Step 5: Regression proof**

Revert production changes for request/response/conditional, keep tests.

Expected: targeted command FAIL. Restore and re-run; Expected: PASS.

**Rollback:** revert this task commit; canonical pipeline configs remain valid.

## Task 4: Retire Public Quirks Doc In Workflow Source

**Files:**
- Delete: `docs/config-field-quirks.md`
- Modify: `docs/dsl-reference.md`
- Modify: `docs/tutorials/building-apps-with-workflow.md`
- Modify: `docs/WFCTL.md`
- Test: `cmd/wfctl/modernize_test.go`

**Step 1: Write failing doc-sync guard**

Add/extend a test that fails when `config-field-quirks.md` is still considered a manual public doc.

Run:

```bash
GOWORK=off go test ./cmd/wfctl -run TestModernizeConfigQuirkAliases -count=1
```

Expected: FAIL until modernize rules and docs cleanup are in place.

**Step 2: Update docs**

Replace defect-ledger content with:
- canonical examples in `dsl-reference.md`;
- a short `WFCTL.md` section for `wfctl modernize` aliases;
- tutorial explanation that inline pipeline triggers are intentional.

Delete `docs/config-field-quirks.md`.

**Step 3: Verify**

Run:

```bash
GOWORK=off go test ./cmd/wfctl ./module ./plugins/http ./plugins/pipelinesteps -count=1
GOWORK=off go test ./... -count=1
```

Expected: PASS.

**Rollback:** restore the deleted doc and revert docs edits if the behavior release is reverted.

## Task 5: Add `wfctl docs generate` Command Skeleton And Workflow Packages

**Files:**
- Create: `cmd/wfctl/docs_generate.go`
- Create: `cmd/wfctl/docs_generate_test.go`
- Modify: `cmd/wfctl/main.go`
- Modify: `docs/WFCTL.md`

**Step 1: Write failing CLI tests**

Test:
- `wfctl docs generate --help` exposes flags;
- running against the current repo with `--source . --out <tmp> --module github.com/GoCodeAlone/workflow --version v0.75.0 --packages plugin,plugin/sdk,plugin/external/sdk` writes Markdown and `versions.json`;
- output contains import path, package synopsis, exported types/functions, source link, and warning list.

**Step 2: Verify RED**

Run:

```bash
GOWORK=off go test ./cmd/wfctl -run TestDocsGenerate -count=1
```

Expected: FAIL because command is absent.

**Step 3: Implement minimal generator**

Use Go stdlib:
- `go list -json` for package dirs/import paths;
- `parser.ParseDir` + `doc.New` for docs;
- Markdown renderer with stable headings;
- metadata JSON with `schemaVersion`, `generatedAt`, `subject`, `versions`, `packages`, `warnings`.

**Step 4: Verify GREEN**

Run:

```bash
GOWORK=off go test ./cmd/wfctl -run TestDocsGenerate -count=1
GOWORK=off go run ./cmd/wfctl docs generate --source . --out "$TMPDIR/workflow-go-docs" --module github.com/GoCodeAlone/workflow --version v0.75.0 --packages plugin,plugin/sdk,plugin/external/sdk
```

Expected: PASS; output dir contains Markdown and metadata.

**Rollback:** remove command and docs; existing `wfctl docs` behavior remains unchanged.

## Task 6: Add Released-Tag Plugin API Generation

**Files:**
- Modify: `cmd/wfctl/docs_generate.go`
- Modify: `cmd/wfctl/docs_generate_test.go`
- Create: `cmd/wfctl/testdata/docs-registry.json`

**Step 1: Write failing registry tests**

Test with two local fixture repos or testdata:
- registry entries discover `repository`, `source`, `version`, `name`;
- generated routes are `plugins/<slug>/latest/`;
- missing matching tag creates warning but does not fail whole generation;
- non-GoCodeAlone or non-GitHub repos are skipped with a trust-boundary warning.

**Step 2: Verify RED**

Run:

```bash
GOWORK=off go test ./cmd/wfctl -run TestDocsGenerateRegistryPlugins -count=1
```

Expected: FAIL because registry mode is absent.

**Step 3: Implement**

Add flags:
- `--registry <path-or-url>`
- `--cache-dir <dir>`
- `--subjects workflow,plugins`
- `--max-version-lines 3`

Use non-shell `git` invocations via `exec.CommandContext`. Clone depth 1 when
possible; checkout exact release tag. Restrict first implementation to
`https://github.com/GoCodeAlone/<repo>`.

**Step 4: Verify GREEN**

Run:

```bash
GOWORK=off go test ./cmd/wfctl -run TestDocsGenerateRegistryPlugins -count=1
```

Expected: PASS with warnings asserted.

**Rollback:** keep workflow-only generation; disable plugin registry flag in website sync.

## Task 7: Website Sync Invokes `wfctl docs generate`

**Files in `gocodealone-website`:**
- Modify: `scripts/sync-docs.mjs`
- Modify: `package.json`
- Modify: `.github/workflows/sync-docs.yml`
- Modify: `.github/workflows/ci.yml`
- Test: `test/sync-docs.test.mjs`

**Step 1: Write failing website tests**

Test:
- generated Go docs are expected under `docs/site/src/content/docs/reference/go/...`;
- sync removes stale generated API docs outside retention;
- `config-field-quirks.md` is not synced.

**Step 2: Verify RED**

Run:

```bash
npm test -- test/sync-docs.test.mjs
```

Expected: FAIL because no Go-doc generation exists and quirks doc is still allowed.

**Step 3: Implement**

Update `sync-docs.mjs` to:
- call `wfctl docs generate` after Markdown sync;
- pass `src/data/plugins.json` as registry input;
- write generated API docs under `reference/go`;
- tolerate generator warnings but fail if workflow core generation emits no pages;
- exclude `config-field-quirks.md` from `WORKFLOW_MANUAL_DOCS`.

Update workflows to install/use the newly released `wfctl` version.

**Step 4: Verify GREEN**

Run:

```bash
GITHUB_TOKEN=$(gh auth token) npm run sync-docs
npm test -- test/sync-docs.test.mjs
npm run build
```

Expected: PASS; generated reference pages are present and Starlight builds.

**Rollback:** revert website sync invocation; committed generated docs remain last-known-good.

## Task 8: Website Navigation And Version Metadata

**Files in `gocodealone-website`:**
- Modify: `docs/site/astro.config.mjs`
- Create/Modify: `docs/site/src/content/docs/reference/index.md`
- Create/Modify: `docs/site/src/content/docs/start-here/index.md` if needed
- Modify: generated docs metadata location.
- Test: `test/sync-docs.test.mjs`

**Step 1: Write failing navigation tests**

Assert docs contain sections/routes for:
- Start Here
- Build Applications
- Extend With Code
- Ecosystem
- Reference
- Go API reference version metadata

**Step 2: Verify RED**

Run:

```bash
npm test -- test/sync-docs.test.mjs
```

Expected: FAIL because sidebar/content is still flat.

**Step 3: Implement**

Use Starlight sidebar groups rather than one flat autogenerate bucket. Keep old
routes where possible; add landing pages that link both wfctl-assisted and
manual paths.

Render version links from generated metadata in Markdown or a small docs data
file. Do not build a heavy custom dropdown unless route metadata proves stable.

**Step 4: Verify GREEN**

Run:

```bash
npm test -- test/sync-docs.test.mjs
npm run build
```

Expected: PASS; Pagefind indexes generated reference pages.

**Rollback:** restore previous sidebar and keep generated pages reachable by direct URL.

## Task 9: Regenerate Website Docs, Release Website

**Files in `gocodealone-website`:**
- Generated Markdown under `docs/site/src/content/docs/`
- `src/data/plugins.json` if registry changed

**Step 1: Regenerate**

Run:

```bash
GITHUB_TOKEN=$(gh auth token) npm run sync-plugins
GITHUB_TOKEN=$(gh auth token) npm run sync-docs
npm run lint
npm test
npm run validate-configs
npm run build
npm run package:site
```

Expected:
- no `docs/site/src/content/docs/workflow/config-field-quirks.md`;
- Go API pages exist under `reference/go`;
- build exits 0.

**Step 2: Open and monitor PR 3**

Create website PR, add `copilot-pull-request-reviewer`, invoke `autodev:pr-monitoring`, fix feedback, merge after green checks.

**Step 3: Release website**

Tag next website patch release after merge and monitor release workflow.

Expected:
- GitHub release contains `site.tar.gz`;
- multisite repository dispatch succeeds.

**Rollback:** delete/revert website tag only before release completes; after publish, release a patch restoring previous docs snapshot.

## Post-Merge Release Order

1. Merge PR 1 (config cleanup), verify CI.
2. Merge PR 2 (docs generator), verify CI.
3. Release Workflow minor (`v0.75.0` if latest remains `v0.74.7`).
4. Merge PR 3 (website docs/navigation), verify CI.
5. Release website patch and verify multisite dispatch.
