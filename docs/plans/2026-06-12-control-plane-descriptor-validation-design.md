# Control Plane Descriptor Validation Design

## Goal

Complete T566 by adding Workflow/wfctl descriptor-bundle validation fixtures
that consume the released `workflow-plugin-control-plane` v0.1.0 contracts,
reject invalid schema/provenance/downgrade/revocation inputs, and prove
descriptor-only loading without runtime/plugin-loading cycles.

## Global Design Guidance

Source: `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`,
`docs/REPO_LAYOUT.md`, `decisions/0030-plugin-conformance-evidence-index.md`,
`docs/PLUGIN_RELEASE_GATES.md`.

| guidance | design response |
|---|---|
| use `GOWORK=off` | all Go verification commands use `GOWORK=off` |
| keep examples under `example/`; no scratch roots | fixtures stay under `cmd/wfctl/testdata` and tests synthesize temp plugin dirs |
| update docs/tests for CLI/config behavior | add focused wfctl/schema tests plus `docs/WFCTL.md` note for control-plane message bundle validation |
| centralize plugin compatibility in wfctl gates | reuse `editor-bundle`, `plugin.contracts.json`, and `validate-contract`; no new registry service |
| release evidence must use real artifacts | consume released module `github.com/GoCodeAlone/workflow-plugin-control-plane v0.1.0` instead of copied success payloads |

## Requirements

| id | requirement | acceptance |
|---|---|---|
| R1 | consume released control-plane contracts | tests locate v0.1.0 module dir via `go list -m`, run `runEditorBundle --registry=false --plugin-dir <moduleDir>`, and assert the three control-plane message contracts |
| R2 | reject invalid descriptor registry inputs | tests import released `registry`, `descriptors`, and `envelopes` packages and assert invalid schema digest, provenance ref, downgrade-floor version, and revocation freshness fail |
| R3 | descriptor-only loading | `go list -deps ./cmd/wfctl` must not include `github.com/GoCodeAlone/workflow-plugin-control-plane`; only tests import it |
| R4 | no plugin-loading/runtime cycle | no runtime code imports, executes, or discovers `workflow-plugin-control-plane`; validation uses JSON/protobuf descriptors plus pure validators |
| R5 | preserve current editor bundle shape | existing message/descriptor-set tests remain green; new tests assert descriptor set refs and message names for released control-plane packages |

## Approach

Recommended: add test-only release-fixture coverage around the existing
`wfctl editor-bundle` and `plugin.contracts.json` path.

| option | verdict | reason |
|---|---|---|
| extend existing editor-bundle tests with released module fixture | choose | smallest real boundary: released plugin metadata → wfctl parser/exporter → editor bundle JSON |
| add new `wfctl control-plane validate` subcommand | reject | creates user-facing API before T568 host consumption and duplicates `validate-contract`/`editor-bundle` |
| add runtime plugin loader conformance | reject | violates T566 descriptor-only scope; T568/T569 own host/provider/scenario adoption |

## Data Flow

1. test pins `github.com/GoCodeAlone/workflow-plugin-control-plane v0.1.0`;
2. helper runs `go list -m -f {{.Dir}} github.com/GoCodeAlone/workflow-plugin-control-plane`;
3. `runEditorBundle --registry=false --plugin-dir <moduleDir>` loads released
   `plugin.json`, `plugin.contracts.json`, and descriptor-set refs;
4. test asserts control-plane descriptors are present in bundle JSON;
5. validator tests construct released protobuf messages and mutate one field per
   negative class.

## Security Review

- Auth/authz: none added; T566 is local CLI/test validation only.
- Secrets: no credentials, tokens, or private plugin registries are read.
- Trust boundary: the released module is an external dependency; pin exact
  v0.1.0 and verify non-test binaries do not import it.
- Abuse cases: reject raw/network provenance refs, malformed digests, stale
  revocation freshness, and invalid downgrade-floor versions through released
  validators.
- Authority: no route binding, persistence, provider dispatch, credentials,
  trust roots, deployment approval, private keys, or rollout state moves into
  Workflow core.

## Infrastructure Impact

No cloud resources, deployments, migrations, secrets, queues, storage, IAM, or
staging changes. The only dependency change is a Go module pin used by tests.
No production deploy approval is required.

## Multi-Component Validation

| boundary | proof |
|---|---|
| released plugin package → wfctl | `runEditorBundle` reads v0.1.0 module metadata and emits bundle JSON |
| released validators → Workflow tests | negative cases call public package validators with real protobuf messages |
| Workflow runtime → no plugin dependency | `go list -deps ./cmd/wfctl` excludes `workflow-plugin-control-plane` |
| docs → CLI behavior | `docs/WFCTL.md` describes descriptor-only bundle validation for control-plane contracts |

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | CI can download public `workflow-plugin-control-plane v0.1.0` | module proxy/network issue | use direct GitHub fallback already supported by Go env; do not vendor or copy generated code |
| A2 | `go list -m -f {{.Dir}}` is acceptable in integration tests | slower than pure unit test | keep one focused test and run package-level tests in CI |
| A3 | released validators cover "downgrade" by validating downgrade-floor shape, not semver comparison | host may later need floor comparison | T568 host adapter can add policy comparison; T566 only proves invalid downgrade input rejection |
| A4 | no public runtime API is needed in T566 | downstream wants a command | T568/T569 can add host/scenario commands after descriptor loading is proven |

## Self-Challenge

1. Laziest solution: copy `plugin.contracts.json` into a fixture. Rejected:
   copied snippets would not prove released package consumption.
2. Fragile assumption: module-cache lookup in tests. Mitigation: helper fails
   with explicit `go list -m` context; no hard-coded module cache paths.
3. YAGNI risk: new CLI command. Avoided; existing `editor-bundle` is enough.
4. Partial failure: malformed released metadata should fail bundle export, not
   silently omit contracts. Existing editor-bundle error behavior remains
   fail-closed and is covered by malformed descriptor tests.
5. Repo-precedent conflict: fixtures belong under `cmd/wfctl/testdata` or temp
   dirs; no root scratch fixture directory is introduced.

## Rollback

Revert the T566 PR. This removes the test-only module pin, fixtures/tests, and
docs. Since no runtime binary imports the public package and no deployment is
changed, rollback requires no migration or staging action.
