# Capability Matrix Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add generated Workflow ecosystem and application capability inventories so humans, agents, and website docs can cross-reference existing functionality and app-level capability usage.

**Architecture:** Add a Go-native inventory package with a checked-in taxonomy, then expose it through `wfctl capability ecosystem|app|check`. The ecosystem path reads registry/local plugin manifests and emits released/local capability rows; the app path reads wfctl/app config, lockfiles, installed plugin manifests, and inferred cross-cutting usage with confidence/evidence.

**Tech Stack:** Go stdlib, existing `config`, `manifest`, `plugin`, `cmd/wfctl` command patterns, YAML via existing module dependency, Markdown rendering via `text/tabwriter`/strings.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~1800

**Out of scope:**
- Website renderer changes in `gocodealone-website`.
- Runtime plugin execution or RPC capability verification.
- Strict CI-failing app policy mode.
- Arbitrary application source-code scanning beyond Workflow-owned config/manifests.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): add capability matrix inventory | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/capability-matrix |

**Status:** Draft

## Global Design Guidance

Source: `docs/plans/2026-06-11-capability-matrix-design.md`

| guidance | plan response |
|---|---|
| Workflow owns extraction semantics | Implement Go package + `wfctl capability` in this repo. |
| Generated evidence over hand-maintained claims | JSON rows carry source kind/path/json path/detail and finding codes. |
| Respect plugin boundaries | Read manifests/contracts/lockfiles; do not execute plugin binaries. |
| Website consumes artifacts | Generate `docs/generated/capabilities/*` JSON/Markdown snapshot. |

## Task 1: Inventory Model And Taxonomy Loader

**Files:**
- Create: `capability/inventory/types.go`
- Create: `capability/inventory/taxonomy.go`
- Create: `capability/inventory/taxonomy_test.go`
- Create: `capability/inventory/testdata/taxonomy.yaml`
- Create: `capability/inventory/testdata/taxonomy-duplicate.yaml`
- Create: `data/capabilities/taxonomy.yaml`

**Step 1: Write failing taxonomy tests**

Create `capability/inventory/taxonomy_test.go` with tests:

```go
func TestLoadTaxonomyMapsAliases(t *testing.T) {
    tax, err := LoadTaxonomy("testdata/taxonomy.yaml")
    if err != nil { t.Fatalf("LoadTaxonomy: %v", err) }
    got, ok := tax.MatchType("module", "http.server")
    if !ok || got.ID != "http.server" { t.Fatalf("got %#v ok=%v", got, ok) }
}

func TestLoadTaxonomyRejectsDuplicateIDs(t *testing.T) {
    _, err := LoadTaxonomy("testdata/taxonomy-duplicate.yaml")
    if err == nil || !strings.Contains(err.Error(), "duplicate capability id") {
        t.Fatalf("expected duplicate id error, got %v", err)
    }
}
```

Also create test fixtures under `capability/inventory/testdata/`.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./capability/inventory -run TestLoadTaxonomy -count=1`

Expected: FAIL because package/functions do not exist.

**Step 3: Implement model and taxonomy loader**

Add structs with JSON tags:

```go
type Capability struct {
    ID          string     `json:"id"`
    Category    string     `json:"category"`
    Name        string     `json:"name"`
    Description string     `json:"description,omitempty"`
    Lifecycle   string     `json:"lifecycle,omitempty"`
    Tags        []string   `json:"tags,omitempty"`
    Providers   []Provider `json:"providers,omitempty"`
    Evidence    []Evidence `json:"evidence,omitempty"`
    Findings    []Finding  `json:"findings,omitempty"`
}
```

Implement `LoadTaxonomy(path string) (*Taxonomy, error)` using `gopkg.in/yaml.v3`.
Validate duplicate IDs and duplicate aliases. Add `MatchType(kind, value string)`.

Seed `data/capabilities/taxonomy.yaml` with initial categories for:
`auth.authn`, `auth.authz`, `auth.sso`, `tenancy.scope`, `secrets.management`,
`platform.github`, `platform.gitlab`, `platform.aws`, `platform.azure`,
`platform.gcp`, `platform.digitalocean`, `ci.generation`, `iac.provider`,
`iac.state-backend`, `observability.health`, `observability.tracing`,
`migrations.schema`, `featureflags.flags`, `payments.processing`,
`messaging.broker`, `storage.object`, `storage.database`, `docs.api`.

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./capability/inventory -run TestLoadTaxonomy -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add capability/inventory data/capabilities/taxonomy.yaml
git commit -m "feat(capability): add inventory taxonomy model"
```

## Task 2: Ecosystem Inventory Collector

**Files:**
- Create: `capability/inventory/ecosystem.go`
- Create: `capability/inventory/ecosystem_test.go`
- Test fixtures: `capability/inventory/testdata/ecosystem/`

**Step 1: Write failing ecosystem tests**

Test fixture shape:

```text
testdata/ecosystem/
  registry/plugins/auth/manifest.json
  registry/plugins/github/manifest.json
  repos/workflow-plugin-authz/plugin.json
  repos/workflow-plugin-local-only/plugin.json
```

Tests:

```go
func TestCollectEcosystemMarksReleasedAndLocal(t *testing.T) {
    inv, err := CollectEcosystem(EcosystemOptions{
        RegistryDir: "testdata/ecosystem/registry",
        RepoRoot: "testdata/ecosystem/repos",
        TaxonomyPath: "testdata/taxonomy.yaml",
        GeneratedAt: fixedTime,
    })
    if err != nil { t.Fatalf("CollectEcosystem: %v", err) }
    assertProviderStatus(t, inv, "workflow-plugin-authz", "local-only")
    assertProviderStatus(t, inv, "auth", "released")
}

func TestCollectEcosystemUncategorizedRawTypes(t *testing.T) {
    inv, err := CollectEcosystem(...)
    if err != nil { t.Fatal(err) }
    assertFinding(t, inv, "uncategorized", "needs-review")
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./capability/inventory -run TestCollectEcosystem -count=1`

Expected: FAIL because `CollectEcosystem` does not exist.

**Step 3: Implement collector**

Implement:

```go
type EcosystemOptions struct {
    RegistryDir string
    RepoRoot string
    TaxonomyPath string
    GeneratedAt time.Time
    WorkflowVersion string
}

func CollectEcosystem(opts EcosystemOptions) (*Inventory, error)
```

Behavior:
- Read registry manifests from `<registry>/plugins/*/manifest.json` when present.
- Read local plugin manifests from `<repoRoot>/workflow-plugin-*/plugin.json`.
- Reuse `plugin.PluginManifest` for plugin.json parsing where possible.
- Convert registry `capabilities.{moduleTypes,stepTypes,triggerTypes,workflowHandlers}` and plugin manifest top-level promoted type fields into capability evidence.
- Match type aliases through taxonomy.
- Emit `uncategorized` capabilities/findings for raw types with no taxonomy match.
- Include metadata: generator name, generated timestamp, workflow version, taxonomy digest, registry path, local plugin count, released row count, local row count.

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./capability/inventory -run 'TestCollectEcosystem|TestLoadTaxonomy' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add capability/inventory
git commit -m "feat(capability): collect ecosystem inventory"
```

## Task 3: Application Profile And Policy Findings

**Files:**
- Create: `capability/inventory/app.go`
- Create: `capability/inventory/app_test.go`
- Test fixtures: `capability/inventory/testdata/app/`

**Step 1: Write failing app profile tests**

Fixtures:

```text
testdata/app/
  wfctl.yaml
  .wfctl-lock.yaml
  workflow.yaml
  plugins/workflow-plugin-authz/plugin.json
```

Test expectations:
- Declared plugin usage from `wfctl.yaml` and lockfile.
- Declared module usage from `config.NewFileSource(workflow.yaml).Load`.
- Inferred authz/tenancy/secrets rows have confidence and source evidence.
- Missing provider finding when a type is used but no plugin/registry/local provider declares it.
- Policy risk finding when tenancy is inferred but a database/storage route lacks tenant evidence.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./capability/inventory -run 'TestCollectApp|TestCheckApp' -count=1`

Expected: FAIL because app collector does not exist.

**Step 3: Implement app collector**

Implement:

```go
type AppOptions struct {
    ManifestPath string
    WorkflowPaths []string
    PluginDir string
    LockfilePath string
    TaxonomyPath string
    GeneratedAt time.Time
}

func CollectApp(ctx context.Context, opts AppOptions) (*AppProfile, error)
func CheckApp(profile *AppProfile) []Finding
```

Use existing loaders:
- `config.LoadWfctlManifest` for `wfctl.yaml`.
- `config.LoadWfctlLockfile` when present.
- `config.NewFileSource(path).Load(ctx)` for each workflow config, preserving imports/merge behavior.
- Existing plugin manifest loading patterns from `cmd/wfctl/docs.go` or `plugin.LoadManifest`.
- `manifest.Analyze(cfg)` to seed service/storage/database/external API facts.

Inference rules for first pass:
- Authn/authz: module/step/plugin/type names containing `auth`, `authz`, `rbac`, `jwt`, `sso`, `oidc`, `passkey`, `okta`, `auth0`, `ory`, `scalekit`.
- Tenancy: module/step/config keys containing `tenant`, `tenantID`, `tenantKey`, `tenantScoped`, `tenant_id`.
- Secrets: `secrets`, `secretStores`, `env`, `${...}`, plugin required secrets where present.
- Provider usage: plugin names and provider config keys for GitHub/GitLab/AWS/Azure/GCP/DigitalOcean/Vault.

Every inferred row must carry `mode=inferred`, `confidence`, and evidence.

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./capability/inventory -run 'TestCollectApp|TestCheckApp|TestCollectEcosystem' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add capability/inventory
git commit -m "feat(capability): profile application usage"
```

## Task 4: wfctl capability Commands And Renderers

**Files:**
- Create: `cmd/wfctl/capability.go`
- Create: `cmd/wfctl/capability_test.go`
- Create: `cmd/wfctl/testdata/capability/app/wfctl.yaml`
- Create: `cmd/wfctl/testdata/capability/app/.wfctl-lock.yaml`
- Create: `cmd/wfctl/testdata/capability/app/workflow.yaml`
- Create: `cmd/wfctl/testdata/capability/app/plugins/workflow-plugin-authz/plugin.json`
- Modify: `cmd/wfctl/main.go`
- Modify: `cmd/wfctl/wfctl.yaml`
- Modify: `docs/WFCTL.md`

**Step 1: Write failing CLI tests**

Tests in `cmd/wfctl/capability_test.go`:

```go
func TestRunCapabilityUsage(t *testing.T) {
    var out bytes.Buffer
    err := runCapabilityWithOutput([]string{}, &out)
    if err == nil || !strings.Contains(out.String(), "Usage: wfctl capability") {
        t.Fatalf("expected usage, got err=%v out=%s", err, out.String())
    }
}

func TestRunCapabilityEcosystemJSON(t *testing.T) { ... }
func TestRunCapabilityAppJSON(t *testing.T) { ... }
func TestRunCapabilityCheckWarnOnly(t *testing.T) { ... }
func TestEmbeddedCLIRegistersCapability(t *testing.T) { ... }
```

Use the `cmd/wfctl/testdata/capability/app/` fixture for CLI app/check tests.
Do not use `cmd/wfctl/wfctl.yaml` as a project manifest; it is the embedded
Workflow config for the CLI itself, not an application `wfctl.yaml`.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunCapability -count=1`

Expected: FAIL because command does not exist.

**Step 3: Implement CLI**

Add `commands["capability"] = runCapability` in `main.go`.

Add `capability` command and `cmd-capability` pipeline to `cmd/wfctl/wfctl.yaml`.

Implement subcommands:

```text
wfctl capability ecosystem [--repo-root ..] [--registry data/registry] [--taxonomy data/capabilities/taxonomy.yaml] [--format json|md] [--output path]
wfctl capability app [--manifest wfctl.yaml] [--workflow path] [--plugin-dir dir] [--lock-file .wfctl-lock.yaml] [--taxonomy data/capabilities/taxonomy.yaml] [--format json|md] [--output path]
wfctl capability check [same app flags] [--format text|json]
```

Renderers:
- JSON uses `json.Encoder` with indentation.
- Markdown ecosystem renders summary and category/provider tables.
- Markdown app renders declared/inferred/missing-provider/policy-risk tables.
- `check` exits 0 in first implementation even with warnings; document warning-only behavior.

**Step 4: Run CLI tests**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunCapability|TestEmbeddedCLIRegistersCapability' -count=1`

Expected: PASS.

**Step 5: Update docs**

Add `capability` to `docs/WFCTL.md` command overview and a section documenting:
- ecosystem/app/check subcommands
- warning-only `check`
- public vs private output guidance
- website artifact consumption path

**Step 6: Commit**

```bash
git add cmd/wfctl docs/WFCTL.md
git commit -m "feat(wfctl): add capability inventory commands"
```

## Task 5: Generated Capability Artifacts

**Files:**
- Create: `docs/generated/capabilities/schema.json`
- Create: `docs/generated/capabilities/ecosystem.json`
- Create: `docs/generated/capabilities/ecosystem.md`
- Create: `docs/generated/capabilities/README.md`
- Test: `cmd/wfctl/capability_test.go`

**Step 1: Write failing generated artifact test**

Add a test that runs the same code path as the command against `data/registry`
and asserts:
- `ecosystem.json` decodes into `inventory.Inventory`.
- metadata has workflow version or `dev`, taxonomy digest, and row counts.
- Markdown contains `# Workflow Capability Matrix` and at least one known category.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestCapabilityGeneratedArtifacts -count=1`

Expected: FAIL because generated files do not exist.

**Step 3: Generate artifacts**

Run:

```bash
GOWORK=off go run ./cmd/wfctl capability ecosystem \
  --repo-root .. \
  --registry data/registry \
  --taxonomy data/capabilities/taxonomy.yaml \
  --format json \
  --output docs/generated/capabilities/ecosystem.json

GOWORK=off go run ./cmd/wfctl capability ecosystem \
  --repo-root .. \
  --registry data/registry \
  --taxonomy data/capabilities/taxonomy.yaml \
  --format md \
  --output docs/generated/capabilities/ecosystem.md
```

Write `schema.json` from the Go struct shape manually or via a small generator
only if the repo already has an obvious schema helper. Keep it Draft 2020-12 and
focused on fields emitted by the command.

**Step 4: Run artifact tests**

Run: `GOWORK=off go test ./cmd/wfctl ./capability/inventory -run 'TestCapabilityGeneratedArtifacts|TestCollect' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add docs/generated/capabilities cmd/wfctl/capability_test.go
git commit -m "docs: generate capability matrix artifacts"
```

## Task 6: Final Verification And PR Prep

**Files:**
- Modify as needed based on test/lint findings only.

**Step 1: Run focused package tests**

Run:

```bash
GOWORK=off go test ./capability/inventory ./cmd/wfctl ./manifest ./plugin -count=1
```

Expected: all packages PASS.

**Step 2: Run broader smoke tests**

Run:

```bash
GOWORK=off go test ./capability/... ./cmd/wfctl -count=1
```

Expected: PASS.

**Step 3: Verify CLI help and representative invocations**

Run:

```bash
GOWORK=off go run ./cmd/wfctl capability -h
GOWORK=off go run ./cmd/wfctl capability ecosystem --registry data/registry --repo-root .. --format json
GOWORK=off go run ./cmd/wfctl capability app \
  --manifest cmd/wfctl/testdata/capability/app/wfctl.yaml \
  --workflow cmd/wfctl/testdata/capability/app/workflow.yaml \
  --plugin-dir cmd/wfctl/testdata/capability/app/plugins \
  --lock-file cmd/wfctl/testdata/capability/app/.wfctl-lock.yaml \
  --format json
```

Expected:
- Help prints usage and exits without "subcommand is required" after `-h`.
- Ecosystem JSON includes `metadata`, `capabilities`, and at least one provider.
- App JSON includes declared/inferred usage or clean no-finding output without secret values.

**Step 4: Run formatting and diff checks**

Run:

```bash
gofmt -w capability/inventory cmd/wfctl/capability.go cmd/wfctl/capability_test.go
git diff --check
```

Expected: no output from `git diff --check`.

**Step 5: Commit any final fixes**

```bash
git status --short
git add <changed-files>
git commit -m "test: verify capability inventory"
```

Commit only if there are final changes.

**Step 6: Open PR**

```bash
git push -u origin feat/capability-matrix
gh pr create --repo GoCodeAlone/workflow \
  --title "feat(wfctl): add capability matrix inventory" \
  --body-file <generated-pr-body>
```

PR body must list:
- design and ADR paths
- commands run
- generated artifact paths
- website follow-up explicitly out of scope

## Verification Matrix

| surface | command | expected |
|---|---|---|
| taxonomy/model | `GOWORK=off go test ./capability/inventory -run TestLoadTaxonomy -count=1` | PASS |
| ecosystem collector | `GOWORK=off go test ./capability/inventory -run TestCollectEcosystem -count=1` | PASS |
| app profile/check | `GOWORK=off go test ./capability/inventory -run 'TestCollectApp|TestCheckApp' -count=1` | PASS |
| CLI | `GOWORK=off go test ./cmd/wfctl -run 'TestRunCapability|TestEmbeddedCLIRegistersCapability' -count=1` | PASS |
| generated artifacts | `GOWORK=off go test ./cmd/wfctl -run TestCapabilityGeneratedArtifacts -count=1` | PASS |
| integrated smoke | `GOWORK=off go test ./capability/... ./cmd/wfctl -count=1` | PASS |

## Rollback

Rollback is a single-PR revert. Because commands are additive and read-only,
there is no data migration. Reverting removes `wfctl capability`, taxonomy data,
generated artifacts, tests, and docs. Existing runtime, plugin install, docs
generate, and audit commands continue to work.
