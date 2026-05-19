# Multi-Repo OSS-Readiness QoL Sweep Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring `workflow` + `workflow-registry` + 51 public plugin repos to a uniform OSS-readiness baseline (READMEs, examples, license, experimental-status markers) so a new external user has consistent docs across the entire ecosystem.

**Architecture:** Coordinated 3-PR schema-feature group (workflow Go struct + workflow-registry schema + workflow-registry manifest population) gates the registry `status` field. P0 deep-treat 7 verified plugins + workflow engine; P1 deep-treat 5 user-called-out unverified plugins (aws/gcp/azure/tofu/ci-generator); P2 mass-marker 39 remaining plugins; P3 non-plugin license sweep across 6 repos.

**Tech Stack:** Go (RegistryManifest struct), JSON Schema 2020-12 (ajv-cli validation), Markdown (READMEs/banners), GitHub CLI `gh` for PR creation, `wfctl validate --skip-unknown-types` for example validation.

**Base branch:** `main` (per-repo; each repo's default branch)

**Design doc:** `docs/plans/2026-05-19-multi-repo-qol-sweep-design.md`
**ADR:** `decisions/0041-multi-repo-qol-sweep-experimental-marker.md`

---

## Scope Manifest

**PR Count:** 19
**Tasks:** 19
**Estimated Lines of Change:** ~6,000 (informational)

Note on PR Count: 19 logical-task rows. The plan ships **≈62 GitHub PRs** in total because some tasks ship a batch of small per-repo PRs (Task 17 = 39 PRs split A-M / M-Z internally, Task 18 = 6 PRs). The 19-row count is the alignment-check contract; the 62-PR count is captured in each task's "Ships PRs" annotation.

**Out of scope:**
- New features, module types, step types, triggers.
- Live-deployment validation of examples (only schema validation via `wfctl validate --skip-unknown-types`).
- Touching upstream forks (`genkit`, `v8go`, `voxtral-tts`, `wgpu`, `yaegi`, `go-plugin`).
- Touching private plugins (security cluster: waf, security, sandbox, supply-chain, data-protection; authz-ui; cardgame/dnd; cloud-ui).
- Deep documentation for the 39 P2 plugins (tracking issues filed instead).
- `wfctl plugin verify` subcommand or any new CLI tooling.
- Translation / i18n.
- GitHub topic tagging.

**PR Grouping:**

| PR # | Title | Tasks | Ships PRs | Branch | Repo |
|------|-------|-------|-----------|--------|------|
| 1 | feat(wfctl): add status + private to RegistryManifest + PluginSummary | Task 1 | 1 | feat/registrymanifest-status-field | workflow |
| 2 | feat(schema): add optional status enum to registry-schema | Task 2 | 1 | chore/qol-sweep-2026-05-19 | workflow-registry |
| 3 | docs: workflow main README polish + examples index + plugin templates | Task 3 | 1 | chore/qol-sweep-2026-05-19 (umbrella PR #714) | workflow |
| 4 | docs(do): add CONTRIBUTING + examples; verified banner | Task 4 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-digitalocean |
| 5 | docs(payments): add CONTRIBUTING + examples; verified banner | Task 5 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-payments |
| 6 | docs(agent): add CONTRIBUTING + examples; verified banner | Task 6 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-agent |
| 7 | docs(audit-chain): add CONTRIBUTING + examples; verified banner | Task 7 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-audit-chain |
| 8 | docs(auth): add CONTRIBUTING + examples; verified banner | Task 8 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-auth |
| 9 | docs(eventbus): add CONTRIBUTING + examples; verified banner | Task 9 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-eventbus |
| 10 | docs(twilio): add CONTRIBUTING + examples; verified banner | Task 10 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-twilio |
| 11 | docs(aws): add README + examples; experimental banner | Task 11 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-aws |
| 12 | docs(gcp): add README + examples; experimental banner | Task 12 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-gcp |
| 13 | docs(azure): add README + examples; experimental banner | Task 13 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-azure |
| 14 | docs(tofu): add README + examples; experimental banner | Task 14 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-tofu |
| 15 | docs(ci-generator): add README + examples; experimental banner | Task 15 | 1 | chore/qol-sweep-2026-05-19 | workflow-plugin-ci-generator |
| 16 | feat(registry): populate status field across registry manifests | Task 16 | 1 | chore/qol-sweep-2026-05-19-manifests | workflow-registry |
| 17 | docs(<plugin>): experimental banner (P2 mass-marker × 39 repos) | Task 17 | 39 | chore/qol-sweep-2026-05-19 | 39 P2 plugins (dispatched in two parallel halves A-M, M-Z) |
| 18 | docs: add MIT LICENSE (P3 non-plugin × 6 repos) | Task 18 | 6 | chore/qol-sweep-2026-05-19 | 6 P3 non-plugin repos |
| 19 | docs: post-sweep retro | Task 19 | 1 | committed to workflow main after PRs land | workflow |

**Total GitHub PRs ≈ 62** (sum of "Ships PRs" column).

**Status:** Draft

---

## Pre-Task Setup (one-shot)

**Step S0.1: Create one worktree per non-workflow repo.**

For each non-workflow repo in the scope, the implementer agent first creates an isolated worktree:

```sh
cd /Users/jon/workspace/<repo>
git fetch origin
git worktree add -b chore/qol-sweep-2026-05-19 _worktrees/qol-sweep origin/<default-branch>
cd _worktrees/qol-sweep
```

`<default-branch>` is `main` for most repos; the implementer reads `git symbolic-ref refs/remotes/origin/HEAD` to confirm.

**Step S0.2: Establish shared templates in workflow main — folded into Task 3.**

The templates that Tasks 4–17 reference (`docs/templates/CONTRIBUTING-plugin.md`, banner snippets, issue templates, PR template) are created and committed as part of **Task 3** (which always runs first among the per-plugin tasks). Tasks 4–17 must wait for Task 3 to have its commit pushed to PR #714 before they begin so the template files exist on disk in the implementer's workspace clone.

If Tasks 4–17 are dispatched in parallel with Task 3, the implementer must verify the templates exist before starting:

```sh
test -d /Users/jon/workspace/workflow/_worktrees/qol-sweep/docs/templates || {
  echo "ERROR: templates missing — Task 3 must complete first"; exit 1
}
```

This is the only cross-task dependency in the plan; everything else is fully parallel.

---

### Task 1: Add `status` + `private` fields to RegistryManifest Go struct (Step B)

**Repo:** `workflow`
**Branch:** `feat/registrymanifest-status-field` (NEW branch, not the umbrella `chore/qol-sweep-2026-05-19`; this PR must be independently mergeable so Task 16 can gate on it).
**Change class:** internal logic refactor + struct field addition + CLI output extension (Go); verification = unit tests + `wfctl plugin list` smoke test.

**Files:**
- Modify: `cmd/wfctl/plugin_registry.go:26-49` (RegistryManifest struct)
- Modify: `cmd/wfctl/registry_validate.go:34, 61-66` (add validPluginStatuses + validation block)
- Modify: `cmd/wfctl/multi_registry_test.go` (add test cases for new fields)
- Modify: `cmd/wfctl/plugin_install.go` or wherever `PluginSummary` is defined + `SearchPlugins` callsites + `wfctl plugin list` output format (find via grep: `grep -rn "type PluginSummary\|func.*SearchPlugins" cmd/wfctl/`).

**Rollback:** revert this PR; existing manifests with `status` fields continue parsing because Go's `encoding/json` ignores unknown fields, but `ValidateManifest` will accept invalid status values until the validation block returns. If Task 16 (manifest population) has already merged, revert Task 16's PR FIRST (so ajv-cli CI in the registry stays green), then revert this PR.

**Step 1.1: Write failing tests first (TDD).**

Add to `cmd/wfctl/multi_registry_test.go`:

```go
func TestValidateManifest_StatusEnum(t *testing.T) {
    cases := []struct {
        name    string
        status  string
        wantErr bool
    }{
        {"empty allowed", "", false},
        {"verified", "verified", false},
        {"experimental", "experimental", false},
        {"deprecated", "deprecated", false},
        {"invalid value", "bogus", true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            m := validBaseManifest()
            m.Status = tc.status
            errs := ValidateManifest(m, ValidationOptions{})
            hasStatusErr := false
            for _, e := range errs {
                if e.Field == "status" {
                    hasStatusErr = true
                }
            }
            if hasStatusErr != tc.wantErr {
                t.Fatalf("status=%q wantErr=%v got errs=%v", tc.status, tc.wantErr, errs)
            }
        })
    }
}

func TestRegistryManifest_PrivateField(t *testing.T) {
    raw := []byte(`{"name":"x","version":"1.0.0","author":"a","description":"d","type":"external","tier":"community","license":"MIT","private":true}`)
    var m RegistryManifest
    if err := json.Unmarshal(raw, &m); err != nil { t.Fatal(err) }
    if !m.Private {
        t.Fatalf("expected Private=true, got %v", m.Private)
    }
}
```

If `validBaseManifest()` helper doesn't exist, inline a literal manifest.

**Step 1.2: Run tests, confirm they fail.**

Run: `go test ./cmd/wfctl -run 'TestValidateManifest_StatusEnum|TestRegistryManifest_PrivateField' -v`
Expected: FAIL — `m.Status undefined`, `m.Private undefined`.

**Step 1.3: Add struct fields.**

Edit `cmd/wfctl/plugin_registry.go:26-49` — add to `RegistryManifest` struct:

```go
Status  string `json:"status,omitempty"`  // verified | experimental | deprecated
Private bool   `json:"private,omitempty"` // mirrors manifest.json `private` field
```

**Step 1.4: Add enum-validation block.**

Edit `cmd/wfctl/registry_validate.go`:

After line 34 (alongside `validPluginTiers`):
```go
var validPluginStatuses = map[string]bool{"verified": true, "experimental": true, "deprecated": true}
```

After the tier-validation block (around line 66) in `ValidateManifest`:
```go
if m.Status != "" && !validPluginStatuses[m.Status] {
    errs = append(errs, ValidationError{Field: "status", Message: fmt.Sprintf("must be one of: verified, experimental, deprecated (got %q)", m.Status)})
}
```

**Step 1.5: Update `PluginSummary` + `SearchPlugins` callsites + `wfctl plugin list` output.**

This is the design-mandated user-visible surface for the `status` field (round-2 plan-review C-1). Without it the manifest-tagging exercise produces no user-visible change.

```sh
grep -rn "type PluginSummary" cmd/wfctl/
grep -rn "func.*SearchPlugins" cmd/wfctl/
grep -rn "fmt.Printf.*plugin list\|wfctl plugin list" cmd/wfctl/
```

Locate the `PluginSummary` struct (likely `cmd/wfctl/plugin_install.go` or `cmd/wfctl/registry_source.go`). Add a `Status string` field:

```go
type PluginSummary struct {
    Name        string
    Description string
    Tier        string
    Source      string
    Status      string // verified | experimental | deprecated; "" if not set
}
```

Propagate the field in every `SearchPlugins()` implementation (likely `GitHubRegistrySource.SearchPlugins()` in `plugin_registry.go` ~line 201 and `StaticRegistrySource.SearchPlugins()` in `registry_source.go` ~line 177 + 281 — confirm with grep). Each construction site that builds a `PluginSummary` from a `RegistryManifest` must add:

```go
PluginSummary{
    // ... existing fields
    Status: m.Status,
}
```

Then update the `wfctl plugin list` output (column header + per-row format) and `wfctl marketplace search` output to include the status column. If a `Status` is empty, render as `-` to maintain table alignment.

Add a new test in `multi_registry_test.go`:

```go
func TestPluginSummary_StatusPropagation(t *testing.T) {
    m := RegistryManifest{
        Name: "test", Version: "1.0.0", Author: "a", Description: "d",
        Type: "external", Tier: "community", License: "MIT",
        Status: "experimental",
    }
    src := &StaticRegistrySource{manifests: []RegistryManifest{m}}
    summaries, err := src.SearchPlugins(context.Background(), "")
    if err != nil { t.Fatal(err) }
    if len(summaries) != 1 || summaries[0].Status != "experimental" {
        t.Fatalf("expected status=experimental in summary, got %+v", summaries)
    }
}
```

(Adapt `StaticRegistrySource` field/constructor name to whatever actually exists — confirm via grep first.)

**Step 1.6: Run tests, confirm they pass.**

Run: `go test ./cmd/wfctl -run 'TestValidateManifest_StatusEnum|TestRegistryManifest_PrivateField|TestPluginSummary_StatusPropagation' -v`
Expected: PASS — all cases green.

**Step 1.7: Run full wfctl tests for regression.**

Run: `go test ./cmd/wfctl/...`
Expected: PASS across all wfctl tests (regression sentinel).

**Step 1.8: Run vet for hygiene.**

Run: `go vet ./cmd/wfctl/...`
Expected: no output.

**Step 1.9: Smoke test the CLI surface.**

```sh
go install ./cmd/wfctl
wfctl marketplace search '' --registry-url=file://path/to/local/registry 2>/dev/null || true
# Or — if local marketplace setup unavailable — just confirm the help text is unchanged:
wfctl plugin list --help
```

Expected: `--help` exit 0; format unchanged or includes Status column header. Smoke is informational only — the real signal is unit-test pass.

**Step 1.10: Commit + open PR.**

```sh
git add cmd/wfctl/plugin_registry.go cmd/wfctl/registry_validate.go cmd/wfctl/multi_registry_test.go \
        cmd/wfctl/plugin_install.go cmd/wfctl/registry_source.go
git commit -m "feat(wfctl): add status + private to RegistryManifest + PluginSummary

- Adds optional Status (verified|experimental|deprecated) to RegistryManifest aligning with workflow-registry schema extension.
- Adds Private bool to RegistryManifest mirroring existing manifest.json field (was silently discarded).
- Propagates Status through PluginSummary + SearchPlugins callsites so wfctl plugin list and wfctl marketplace search surface the verification status.
- Adds enum validation in ValidateManifest.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin feat/registrymanifest-status-field
gh pr create --title "feat(wfctl): add status + private fields to RegistryManifest" --body "<see template>"
```

**Verification (change class = struct + validation):**
- Unit tests green: `go test ./cmd/wfctl/...` exit 0.
- No runtime-launch-validation trigger (struct field is optional; no startup change).

---

### Task 2: Add optional `status` enum to workflow-registry schema (PR-R1)

**Repo:** `workflow-registry`
**Worktree:** `/Users/jon/workspace/workflow-registry/_worktrees/qol-sweep`
**Branch:** `chore/qol-sweep-2026-05-19`
**Change class:** schema migration (additive); verification = ajv-cli validation across all existing manifests.

**Files:**
- Modify: `schema/registry-schema.json` (add optional `status` property to per-plugin entry schema)
- Modify: `README.md` (note the new field in the schema-overview section if one exists)

**Step 2.1: Inspect schema to find correct insertion point.**

```sh
cd /Users/jon/workspace/workflow-registry/_worktrees/qol-sweep
grep -n "tier\|additionalProperties" schema/registry-schema.json | head -20
```

Identify the per-plugin entry's properties block (where `tier` is defined). The `status` property is added alongside.

**Step 2.2: Write failing validation check first.**

```sh
# Create a probe manifest that uses status (should fail before schema update)
cat > /tmp/probe-manifest.json <<EOF
{
  "name": "workflow-plugin-test",
  "version": "1.0.0",
  "author": "test",
  "description": "probe",
  "type": "external",
  "tier": "community",
  "license": "MIT",
  "status": "experimental"
}
EOF

ajv validate --spec=draft2020 -s schema/registry-schema.json -d /tmp/probe-manifest.json 2>&1 | head -5
```

Expected (before fix): validation error mentioning `additionalProperties` rejecting `status`.

**Step 2.3: Add `status` property to schema.**

Edit `schema/registry-schema.json` — inside the per-plugin entry's `properties` block (where `tier` is defined), insert:

```json
"status": {
  "type": "string",
  "enum": ["verified", "experimental", "deprecated"],
  "description": "Active-usage verification status. 'verified' = pinned in a merged main-branch wfctl.yaml of an active GoCodeAlone project; 'experimental' = compiles + unit-tests pass but no verified production usage; 'deprecated' = scheduled removal."
}
```

Do NOT add `status` to the `required` array — it must remain optional so existing manifests keep validating.

**Step 2.4: Re-validate probe + all existing manifests.**

```sh
ajv validate --spec=draft2020 -s schema/registry-schema.json -d /tmp/probe-manifest.json
# Expected: valid

ajv validate --spec=draft2020 -s schema/registry-schema.json -d 'plugins/*/manifest.json'
# Expected: all 58 manifests valid (no status field present = optional = OK)
```

**Step 2.5: Run the registry's own CI validation script if present.**

```sh
ls scripts/ 2>/dev/null
test -f scripts/validate.sh && bash scripts/validate.sh
```

Expected: exit 0.

**Step 2.6: Commit + open PR.**

```sh
git add schema/registry-schema.json
git commit -m "feat(schema): add optional status enum to registry-schema

Adds optional 'status' property (verified|experimental|deprecated) to the
per-plugin manifest schema. Backward-compatible: status is optional;
existing manifests without the field continue to validate.

This is PR-R1 of the workflow-registry schema-feature group. PR-R2 will
populate the field across all 51 manifests once this PR + the workflow
RegistryManifest struct PR both merge.

See: workflow-registry/schema/registry-schema.json
See: workflow/docs/plans/2026-05-19-multi-repo-qol-sweep-design.md

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin chore/qol-sweep-2026-05-19
gh pr create --title "feat(schema): add optional status enum to registry-schema" --body "<template>"
```

**Verification (change class = schema migration):**
- `ajv validate` across all existing 58 manifests: exit 0.
- Test manifest with `status` field validates cleanly.
- Test manifest with invalid status value (e.g. `"bogus"`) gets rejected by ajv (enum check).

**Rollback:** revert PR commit; existing manifests are unaffected (status field was never required).

---

### Task 3: Workflow main README polish + examples index + docs/templates/

**Repo:** `workflow` (this worktree)
**Branch:** `chore/qol-sweep-2026-05-19` (already open as #714)
**Change class:** documentation only.

**Files:**
- Modify: `README.md` (add a "Plugin Status" section linking the registry + experimental-marker explanation)
- Create: `docs/EXAMPLES.md` (index of `example/` configs with one-line descriptions)
- Create: `docs/templates/CONTRIBUTING-plugin.md`
- Create: `docs/templates/README-plugin-banner-verified.md`
- Create: `docs/templates/README-plugin-banner-experimental.md`
- Create: `docs/templates/issue-templates/bug_report.md`
- Create: `docs/templates/issue-templates/feature_request.md`
- Create: `docs/templates/PULL_REQUEST_TEMPLATE.md`

**Step 3.1: Audit existing README links for staleness.**

```sh
cd /Users/jon/workspace/workflow/_worktrees/qol-sweep
grep -oE '\[[^]]+\]\([^)]+\.md[^)]*\)' README.md | sort -u
# For each linked path, confirm the file exists:
for link in $(grep -oE '\(([^)]+\.md)' README.md | tr -d '('); do
  test -f "$link" || echo "BROKEN: $link"
done
```

Fix any broken links found.

**Step 3.2: Add "Plugin Status" section to README.**

Insert (after the Features section, before the Getting Started section):

```markdown
## Plugin Ecosystem

The workflow engine has a [registry of 51 public plugins](https://github.com/GoCodeAlone/workflow-registry). Each plugin in the registry carries a `status` field:

- **✅ Verified** — pinned in a merged main-branch `wfctl.yaml` of an active GoCodeAlone production project. Production miles.
- **⚠️ Experimental** — compiles and passes its unit tests, but no validated production deployment. Use with caution.
- **🚫 Deprecated** — scheduled removal.

See the [Plugin Authoring Guide](docs/PLUGIN_AUTHORING.md) to build your own.
```

**Step 3.3: Create docs/EXAMPLES.md.**

```sh
mkdir -p docs
cat > docs/EXAMPLES.md <<'EOF'
# Examples Index

Each `example/*.yaml` is a runnable config. Validate with:

```sh
wfctl validate example/<name>.yaml
```

| Example | Description |
|---------|-------------|
EOF

# Auto-generate the table:
for f in example/*.yaml; do
  name=$(basename "$f")
  desc=$(head -3 "$f" | grep -oE '^#.+' | head -1 | sed 's/^# *//')
  echo "| $name | ${desc:-(see file)} |" >> docs/EXAMPLES.md
done
```

**Step 3.4: Create shared templates.**

Create `docs/templates/CONTRIBUTING-plugin.md`:

```markdown
# Contributing to workflow-plugin-<name>

This plugin is part of the [GoCodeAlone/workflow](https://github.com/GoCodeAlone/workflow) ecosystem.

## Before contributing

Read the [upstream CONTRIBUTING.md](https://github.com/GoCodeAlone/workflow/blob/main/CONTRIBUTING.md) for general conventions, signing, and review expectations.

## Local development

```sh
git clone https://github.com/GoCodeAlone/workflow-plugin-<name>.git
cd workflow-plugin-<name>
go build ./...
go test ./...
```

## Pull requests

- One feature or bugfix per PR.
- Update CHANGELOG.md with a Keep-a-Changelog entry.
- Add tests covering new behavior.
- Run `go vet ./...` before pushing.

## Reporting issues

See the issue templates under `.github/ISSUE_TEMPLATE/`.
```

Create `docs/templates/README-plugin-banner-verified.md`:

```markdown
> ✅ **Verified** — used in production at <PROJECTS>. This plugin has been validated end-to-end in a merged main-branch wfctl.yaml of an active GoCodeAlone project.
```

Create `docs/templates/README-plugin-banner-experimental.md`:

```markdown
> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-<NAME>/issues/new) if you adopt it so we can promote it to **verified** status.
```

Create `docs/templates/issue-templates/bug_report.md` + `feature_request.md` + `docs/templates/PULL_REQUEST_TEMPLATE.md` per standard GitHub conventions.

**Step 3.5: Validate links.**

```sh
# Quick eyeball: every md link target file exists.
grep -roE '\]\(\.?\.?/docs/[^)]+\)' README.md docs/EXAMPLES.md docs/templates/ | \
  awk -F'[()]' '{print $2}' | sort -u | while read link; do
    full="${link#./}"
    test -e "$full" || test -e "docs/$full" || echo "BROKEN: $link"
done
```

**Step 3.6: Commit on the existing umbrella branch.**

```sh
git add README.md docs/EXAMPLES.md docs/templates/
git commit -m "docs: workflow README + examples index + plugin templates

- Adds Plugin Ecosystem section with verified/experimental status legend
- Adds docs/EXAMPLES.md index of example/*.yaml configs
- Adds docs/templates/ shared templates for plugin repos (CONTRIBUTING,
  banner snippets, issue templates, PR template)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Verification (change class = documentation):**
- Every internal `[label](path)` link's target file exists.
- `markdown-link-check README.md docs/EXAMPLES.md` if installed; otherwise eyeball.

---

### Task 4: workflow-plugin-digitalocean — full P0 checklist

Repo: `workflow-plugin-digitalocean`. Apply the shared P0-verified-plugin checklist below with `<plugin>=digitalocean` and `<project-list>=buymywishlist, core-dump, workflow-compute`.

### Task 5: workflow-plugin-payments — full P0 checklist

Repo: `workflow-plugin-payments`. Apply checklist below with `<plugin>=payments` and `<project-list>=buymywishlist`.

### Task 6: workflow-plugin-agent — full P0 checklist

Repo: `workflow-plugin-agent`. Apply checklist below with `<plugin>=agent` and `<project-list>=ratchet`.

### Task 7: workflow-plugin-audit-chain — full P0 checklist

Repo: `workflow-plugin-audit-chain`. Apply checklist below with `<plugin>=audit-chain` and `<project-list>=buymywishlist`.

### Task 8: workflow-plugin-auth — full P0 checklist

Repo: `workflow-plugin-auth`. Apply checklist below with `<plugin>=auth` and `<project-list>=buymywishlist`.

### Task 9: workflow-plugin-eventbus — full P0 checklist

Repo: `workflow-plugin-eventbus`. Apply checklist below with `<plugin>=eventbus` and `<project-list>=buymywishlist`.

### Task 10: workflow-plugin-twilio — full P0 checklist

Repo: `workflow-plugin-twilio`. Apply checklist below with `<plugin>=twilio` and `<project-list>=buymywishlist`.

### Shared P0 verified-plugin checklist (used by Tasks 4–10)

Per-task verification is identical to the steps below; each task independently runs the full sequence in its own worktree.

**Per-plugin sub-checklist:** (identical for tasks 4–10; substitute `<plugin>` and `<project-list>`)

**Files (per repo):**
- Verify/polish: `README.md`
- Create if missing: `CHANGELOG.md`, `CONTRIBUTING.md`, `examples/minimal/config.yaml`, `.github/ISSUE_TEMPLATE/{bug_report,feature_request}.md`, `.github/PULL_REQUEST_TEMPLATE.md`

**Step T.1: Set up worktree.**
```sh
cd /Users/jon/workspace/workflow-plugin-<plugin>
git fetch origin
git worktree add -b chore/qol-sweep-2026-05-19 _worktrees/qol-sweep origin/main
cd _worktrees/qol-sweep
```

**Step T.2: Inspect current state.**
```sh
ls README.md CHANGELOG.md CONTRIBUTING.md LICENSE 2>&1
ls examples/ .github/ 2>&1
cat plugin.json 2>/dev/null | jq -r '.name, .version, .description, .capabilities' 2>/dev/null
```
Identify present-vs-missing.

**Step T.3: README polish — add verified banner.**

Prepend (or replace existing banner) at top of `README.md`:
```markdown
> ✅ **Verified** — used in production at <project-list>. This plugin has been validated end-to-end in a merged main-branch wfctl.yaml of an active GoCodeAlone project.
```

Where `<project-list>` comes from the design's verified-set table:
- digitalocean → buymywishlist, core-dump, workflow-compute
- payments → buymywishlist
- agent → ratchet
- audit-chain → buymywishlist
- auth → buymywishlist
- eventbus → buymywishlist
- twilio → buymywishlist

Ensure README has: title, banner, build badge (if CI exists), license badge, 60-second quickstart, install, link to upstream workflow docs.

**Step T.4: Create CHANGELOG.md if missing.**

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [<latest-tag>] - <release-date>

Initial CHANGELOG entry tracking the current release. See git tags for prior versions.
```

Latest tag: `git describe --tags --abbrev=0 2>/dev/null`. Release date: `git log -1 --format=%cd --date=short $(git describe --tags --abbrev=0) 2>/dev/null`.

**Step T.5: Create CONTRIBUTING.md from template.**

Copy from `workflow/docs/templates/CONTRIBUTING-plugin.md`, substitute `<name>`.

**Step T.6: Create `examples/minimal/config.yaml`.**

Derive from `plugin.json` capabilities. For step-only plugins (payments/twilio/audit-chain/auth):
```yaml
modules:
  - name: <required-module>
    type: <step-only-or-bridge-module>
    config: {}

workflows:
  pipeline:
    steps:
      - name: example
        type: <step-from-plugin>
        config: {}
```

For IaC plugins (digitalocean): keep this task at the per-plugin verified-set level (digitalocean already has examples upstream — check `examples/` first; if present, only refresh the banner + manifest).

For eventbus: minimal config publishing one event.

For agent: minimal config with one tool.

**Step T.7: Validate the example.**

```sh
wfctl validate --skip-unknown-types examples/minimal/config.yaml
```
Expected: exit 0.

If the binary is not installed locally:
```sh
go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
```

**Step T.8: Create .github/ files from templates.**

```sh
mkdir -p .github/ISSUE_TEMPLATE
cp /Users/jon/workspace/workflow/_worktrees/qol-sweep/docs/templates/issue-templates/bug_report.md .github/ISSUE_TEMPLATE/
cp /Users/jon/workspace/workflow/_worktrees/qol-sweep/docs/templates/issue-templates/feature_request.md .github/ISSUE_TEMPLATE/
cp /Users/jon/workspace/workflow/_worktrees/qol-sweep/docs/templates/PULL_REQUEST_TEMPLATE.md .github/
```

**Step T.9: Build + vet sanity.**

```sh
go build ./...
go vet ./...
```
Expected: both exit 0. If pre-existing failures, note in PR description; do not fix.

**Step T.10: Commit + push + open PR.**

```sh
git add README.md CHANGELOG.md CONTRIBUTING.md examples/ .github/
git commit -m "docs(<plugin>): add CONTRIBUTING + examples; verified banner (QoL sweep)

Part of 2026-05-19 multi-repo OSS-readiness QoL sweep.
See: https://github.com/GoCodeAlone/workflow/pull/714

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin chore/qol-sweep-2026-05-19
gh pr create --title "docs(<plugin>): add CONTRIBUTING + examples; verified banner (QoL sweep)" --body "<template referencing #714>"
gh pr edit --add-reviewer "@copilot"
```

**Verification (change class = documentation):**
- `go build ./...` exit 0
- `go vet ./...` exit 0
- `wfctl validate --skip-unknown-types examples/**/*.yaml` exit 0
- README/CHANGELOG/CONTRIBUTING all present and pass markdown-link sanity (eyeball or `markdown-link-check`)

**Rollback:** revert merge SHA; no runtime effect.

---

### Task 11: workflow-plugin-aws — P1 deep-treat with new README

Repo: `workflow-plugin-aws`. README absent — author from scratch using the scaffold below. Banner = experimental. Apply the shared P1 checklist that follows.

### Task 12: workflow-plugin-gcp — P1 deep-treat with new README

Repo: `workflow-plugin-gcp`. README absent — author from scratch. Banner = experimental.

### Task 13: workflow-plugin-azure — P1 deep-treat with new README

Repo: `workflow-plugin-azure`. README absent — author from scratch. Banner = experimental.

### Task 14: workflow-plugin-tofu — P1 deep-treat with new README

Repo: `workflow-plugin-tofu`. README absent — author from scratch. Banner = experimental.

### Task 15: workflow-plugin-ci-generator — P1 deep-treat with new README

Repo: `workflow-plugin-ci-generator`. README absent — author from scratch. Banner = experimental.

### Shared P1 unverified-plugin checklist (used by Tasks 11–15)

Same as Task 4–10 with two differences:

1. **README is missing entirely.** Implementer must author from scratch using `plugin.json` capabilities + scaffold template (see below).
2. **Banner is experimental** (not verified):
   ```markdown
   > ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-<name>/issues/new) if you adopt it so we can promote it to **verified** status.
   ```

**Scaffold README template (used for P1 + P2 when README missing):**

```markdown
# workflow-plugin-<name>

> ⚠️ **Experimental** — <as above>

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/GoCodeAlone/workflow-plugin-<name>.svg)](https://pkg.go.dev/github.com/GoCodeAlone/workflow-plugin-<name>)

<one-line description from plugin.json>

## What it provides

<modules/steps/triggers from plugin.json capabilities>

## Install

```yaml
# In your wfctl.yaml
version: 1
plugins:
  - name: workflow-plugin-<name>
    version: <latest-tag>
    source: github.com/GoCodeAlone/workflow-plugin-<name>
```

Then:
```sh
wfctl plugin install
```

## Minimal example

See `examples/minimal/config.yaml`.

## Documentation

- [Plugin authoring guide (upstream)](https://github.com/GoCodeAlone/workflow/blob/main/docs/PLUGIN_AUTHORING.md)
- [Workflow engine docs](https://github.com/GoCodeAlone/workflow)

## License

MIT. See [LICENSE](LICENSE).
```

**Examples for IaC plugins (aws/gcp/azure/tofu) and ci-generator:** the `examples/minimal/config.yaml` includes a single `infra.*` module declaration referencing the provider type but no real credentials (use `${ENV_VAR}` substitution). Document required env vars in the README.

**Per-task verification is identical to Task 4–10 with `--skip-unknown-types` mandatory.**

---

### Task 16: workflow-registry manifest population (PR-R2)

**Repo:** `workflow-registry`
**Worktree:** `/Users/jon/workspace/workflow-registry/_worktrees/qol-sweep-manifests`
**Branch:** `chore/qol-sweep-2026-05-19-manifests` (new, separate branch from PR-R1)
**Gating:** **DO NOT START** until PR-R1 (Task 2) AND Task 1 (workflow Go struct) are both merged.
**Change class:** data migration (manifest field addition).

**Files:** `workflow-registry/plugins/<name>/manifest.json` for every `type=external` manifest, **except** the explicit private-cluster skip-list (waf, security, sandbox, supply-chain, data-protection, authz-ui, cloud-ui) which are out-of-scope per the design.

**Authoritative count (derived via jq, not hardcoded):**

```sh
# At time of plan writing: registry has 42 type=external manifests.
# In-scope = 42 minus 7-item private-cluster skip-list = 35 manifests to update.
# Of those 35: 7 marked verified, 28 marked experimental.
# Counts derived live by the Step 16.2 script; the plan does not enforce a literal number.
```

**Verified-set names as they appear in registry directories** (verify with `ls plugins/<name>/manifest.json` before running script):

- `agent`, `audit-chain`, `digitalocean`, `eventbus`, `payments`, `twilio` — directory names match plugin short-name
- `workflow-plugin-auth` — directory is name-prefixed; the short-name `auth` is a different builtin

**Private-cluster skip-list (do NOT touch):**

- `waf`, `security`, `sandbox`, `supply-chain`, `data-protection`, `authz-ui`, `cloud-ui` (some have registry entries, some don't; skip whichever exist)

**Step 16.1: Set up worktree off main (post-PR-R1 merge).**

```sh
cd /Users/jon/workspace/workflow-registry
git fetch origin main
git worktree add -b chore/qol-sweep-2026-05-19-manifests _worktrees/qol-sweep-manifests origin/main
cd _worktrees/qol-sweep-manifests
```

**Step 16.2: Script the manifest update.**

```sh
# In-scope verified plugin set (directory names matching workflow-registry/plugins/<dir>/)
VERIFIED=(agent audit-chain digitalocean eventbus payments twilio workflow-plugin-auth)

# Private cluster — never touched by this sweep
SKIP_PRIVATE=(waf security sandbox supply-chain data-protection authz-ui cloud-ui workflow-plugin-supply-chain)

# Build a lookup set for fast contains-check
declare -A IS_VERIFIED IS_SKIP
for v in "${VERIFIED[@]}"; do IS_VERIFIED["$v"]=1; done
for s in "${SKIP_PRIVATE[@]}"; do IS_SKIP["$s"]=1; done

for manifest in plugins/*/manifest.json; do
  dir=$(basename "$(dirname "$manifest")")
  type=$(jq -r '.type' "$manifest")

  # Skip builtins — out of scope (status is only for external plugins)
  [ "$type" != "external" ] && continue

  # Skip private-cluster
  [ -n "${IS_SKIP[$dir]}" ] && { echo "SKIP private-cluster: $dir"; continue; }

  # Assign status
  if [ -n "${IS_VERIFIED[$dir]}" ]; then
    status="verified"
  else
    status="experimental"
  fi

  jq --arg s "$status" '.status = $s' "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
done
```

**Step 16.3: Validate.**

```sh
ajv validate --spec=draft2020 -s schema/registry-schema.json -d 'plugins/*/manifest.json'
```
Expected: all manifests valid.

**Step 16.4: Negative-assertion — confirm private-cluster manifests untouched.**

```sh
git diff --name-only | grep -E "plugins/(waf|security|sandbox|supply-chain|data-protection|authz-ui|cloud-ui|workflow-plugin-supply-chain)/" && {
  echo "ERROR: private-cluster manifest was modified"; exit 1
} || echo "OK: no private-cluster manifest modified"
```

Expected: "OK" — exit 0.

**Step 16.5: Run any repo-specific CI scripts.**

```sh
test -f scripts/validate.sh && bash scripts/validate.sh
test -f scripts/build-static.sh && bash scripts/build-static.sh
```

**Step 16.6: Spot-check.**

```sh
for d in digitalocean payments aws gcp workflow-plugin-auth; do
  test -f plugins/$d/manifest.json && jq -r '"\(.name) (dir=\($dir)): status=\(.status // "MISSING")"' --arg dir "$d" plugins/$d/manifest.json
done
```

Expected:
- digitalocean: status=verified
- payments: status=verified
- workflow-plugin-auth: status=verified
- aws: status=experimental
- gcp: status=experimental

**Step 16.7: Commit + push + open PR.**

```sh
N_VERIFIED=$(grep -l '"status": "verified"' plugins/*/manifest.json | wc -l | tr -d ' ')
N_EXPERIMENTAL=$(grep -l '"status": "experimental"' plugins/*/manifest.json | wc -l | tr -d ' ')

git add plugins/*/manifest.json
git commit -m "feat(registry): populate status field across external plugin manifests

Sets status=verified on $N_VERIFIED plugins exercised by merged main-branch
usage in active GoCodeAlone projects; status=experimental on $N_EXPERIMENTAL
others. Private-cluster manifests (security/waf/sandbox/supply-chain/
data-protection/authz-ui/cloud-ui) are explicitly skipped per scope.

Gated on workflow-registry PR-R1 (schema) + workflow RegistryManifest
struct PR. Part of 2026-05-19 multi-repo OSS-readiness QoL sweep.

See: workflow#714

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin chore/qol-sweep-2026-05-19-manifests
gh pr create --title "feat(registry): populate status field across external plugin manifests" --body "<template>"
```

**Verification (change class = data migration):**
- ajv-cli validation across all manifests: exit 0.
- Negative-assertion: zero private-cluster manifests in diff.
- Spot-check: 7 verified entries match expected set.

**Rollback:** revert the merge commit; manifests revert to no-status state which is still valid against the schema (status is optional).

---

### Task 17: P2 Mass-Marker Sweep (39 plugins)

The plugin set is dispatched in two parallel halves to bound wall-clock time. Both halves run the identical per-repo checklist below; the split is for parallel-agent scheduling, not for distinct plan tasks.

**Plugin set (39):** all public workflow-plugin-* repos NOT in verified-set (7) and NOT in P1 (5). Authoritative list:

```
actors admin analytics approval audit authz bento broker cicd crm
data-engineering datadog deployment discord erp github gitlab infra
launchdarkly marketplace mcp messaging-core migrations monday okta
openlms platform rooms salesforce security-scanner slack sso steam
teams template turnio vectorstore websocket ws-auth
```

**Pre-flight (per round-2 plan-review I-2):** 13 of these 39 repos have NO entry in workflow-registry (`actors`, `analytics`, `broker`, `deployment`, `infra`, `marketplace`, `mcp`, `messaging-core`, `rooms`, `security-scanner`, `steam`, `template`, `ws-auth`). For those: README banner + LICENSE check still apply; the registry-manifest population in Task 16 cannot mark them experimental because there's no manifest. The implementer files a tracking issue per missing manifest (titled `qol-sweep: create registry manifest for workflow-plugin-<plugin>`) and proceeds with the banner/LICENSE work in the repo.

**Split for parallelism:**
- **Task 17a (20 plugins, A–M alphabetical):** actors, admin, analytics, approval, audit, authz, bento, broker, cicd, crm, data-engineering, datadog, deployment, discord, erp, github, gitlab, infra, launchdarkly, marketplace
- **Task 17b (19 plugins, M–Z alphabetical):** mcp, messaging-core, migrations, monday, okta, openlms, platform, rooms, salesforce, security-scanner, slack, sso, steam, teams, template, turnio, vectorstore, websocket, ws-auth

Each batch is dispatched to a separate Haiku implementer agent so total wall-clock stays bounded.

**Repo-existence pre-flight (per round-2 plan-review I-2) — required first step for each batch:**

```sh
for p in <batch list>; do
  if ! gh repo view "GoCodeAlone/workflow-plugin-$p" >/dev/null 2>&1; then
    echo "SKIP missing repo: workflow-plugin-$p"; continue
  fi
done
```

A repo that is missing from GitHub (e.g. if a name was wrong) is logged and skipped — never silently fails.

**Branch:** `chore/qol-sweep-2026-05-19` in each repo.
**Change class:** documentation banner + LICENSE check (one-liner).

**Per-repo steps (template-driven, executed by doc-impl-5 in a loop):**

**Step 17.1: For each plugin, set up worktree.**
```sh
cd /Users/jon/workspace/workflow-plugin-<plugin>
git fetch origin
DEFAULT=$(git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/origin/@@')
git worktree add -b chore/qol-sweep-2026-05-19 _worktrees/qol-sweep "origin/$DEFAULT"
cd _worktrees/qol-sweep
```

**Step 17.2: LICENSE check (add MIT if missing).**

```sh
if [ ! -f LICENSE ]; then
  # Copy MIT from workflow main and update copyright year/holder
  cat > LICENSE <<'EOF'
MIT License

Copyright (c) 2026 GoCodeAlone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
EOF
fi
```

Special case: `workflow-plugin-migrations` — run Apache-audit grep before changing. If audit finds vendored Apache code, **skip LICENSE change** and file a tracking issue.

```sh
if [ "<plugin>" = "migrations" ]; then
  hits=$(git log --diff-filter=A --name-only --pretty= -- '*.go' | sort -u | \
    xargs grep -l 'Copyright.*Apache\|github.com/golang-migrate/migrate' 2>/dev/null | head -1)
  if [ -n "$hits" ]; then
    echo "ABORT migrations relicense: found Apache-licensed inline code"
    # File issue and skip
    gh issue create --title "qol-sweep: workflow-plugin-migrations Apache audit found inline code" \
      --body "Audit found Apache-2.0 inline code in $hits. Cannot relicense to MIT. Original Apache-2.0 retained."
  fi
fi
```

**Step 17.3: Add experimental banner to README.**

If README.md exists, prepend the banner block (above first heading or after first heading line, before any badge row):

```sh
if [ -f README.md ]; then
  # Insert banner after the first heading line
  awk -v plugin="<plugin>" 'NR==1 && /^#/ {
    print;
    print "";
    print "> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-" plugin "/issues/new) if you adopt it so we can promote it to **verified** status.";
    next
  }
  { print }
  ' README.md > README.md.tmp && mv README.md.tmp README.md
else
  # Create minimal README from scaffold (Task 11-15 template)
  cat > README.md <<EOF
# workflow-plugin-<plugin>

> ⚠️ **Experimental** — <as above>

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Part of the [GoCodeAlone/workflow](https://github.com/GoCodeAlone/workflow) ecosystem.

## Install

\`\`\`yaml
# In your wfctl.yaml
version: 1
plugins:
  - name: workflow-plugin-<plugin>
    version: <latest-tag>
    source: github.com/GoCodeAlone/workflow-plugin-<plugin>
\`\`\`

## Documentation

- [Plugin authoring guide (upstream)](https://github.com/GoCodeAlone/workflow/blob/main/docs/PLUGIN_AUTHORING.md)
- [Workflow engine docs](https://github.com/GoCodeAlone/workflow)

## License

MIT. See [LICENSE](LICENSE).
EOF
fi
```

**Step 17.4: File deep-docs tracking issue in workflow-registry.**

```sh
cd /Users/jon/workspace/workflow-registry
gh issue create --title "qol-sweep: deep docs/examples for workflow-plugin-<plugin>" \
  --body "QoL sweep (2026-05-19) shipped experimental banner + LICENSE check for this plugin. Deep documentation and examples follow-up tracked here.

Checklist:
- [ ] CHANGELOG.md (Keep-a-Changelog)
- [ ] CONTRIBUTING.md
- [ ] examples/minimal/config.yaml (validated with \`wfctl validate --skip-unknown-types\`)
- [ ] .github/ISSUE_TEMPLATE/{bug_report,feature_request}.md
- [ ] .github/PULL_REQUEST_TEMPLATE.md
- [ ] README polish (60-second quickstart)

See: workflow#714 (umbrella design + ADR-0041)"
```

**Step 17.5: Commit + push + open PR.**

```sh
cd /Users/jon/workspace/workflow-plugin-<plugin>/_worktrees/qol-sweep
git add README.md LICENSE 2>/dev/null
git commit -m "docs(<plugin>): experimental banner + MIT LICENSE (QoL sweep)

Part of 2026-05-19 multi-repo OSS-readiness QoL sweep.
See: workflow#714

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin chore/qol-sweep-2026-05-19
gh pr create --title "docs: experimental banner + MIT LICENSE (QoL sweep)" --body "<template>"
```

**Verification (change class = documentation):**
- README has banner at top
- LICENSE present (MIT) or, for migrations special case, documented exception
- Tracking issue filed in workflow-registry

**Rollback:** revert each merge SHA individually.

---

### Task 18: P3 License-Only Sweep (6 non-plugin repos)

**Repos:** `homebrew-tap`, `superpowers-marketplace`, `ratchet`, `ratchet-cli`, `claude-skills`, `rover`.

**Per-repo steps (executed by doc-impl-6):**

**Step 18.1: Set up worktree.**
```sh
cd /Users/jon/workspace/<repo>
git fetch origin
DEFAULT=$(git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/origin/@@')
git worktree add -b chore/qol-sweep-2026-05-19 _worktrees/qol-sweep "origin/$DEFAULT"
cd _worktrees/qol-sweep
```

**Step 18.2: Add MIT LICENSE.**

If LICENSE absent, add MIT (same boilerplate as Step 17.2).

**Step 18.3: Commit + push + open PR.**
```sh
git add LICENSE
git commit -m "docs: add MIT LICENSE (QoL sweep)

Part of 2026-05-19 multi-repo OSS-readiness QoL sweep. All
public GoCodeAlone-owned non-fork repos should carry MIT.

See: workflow#714

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push -u origin chore/qol-sweep-2026-05-19
gh pr create --title "docs: add MIT LICENSE (QoL sweep)" --body "<template>"
```

**Verification:**
- LICENSE present and content is canonical MIT with "Copyright (c) 2026 GoCodeAlone"

**Rollback:** revert merge SHA.

---

### Task 19: Post-Sweep Retrospective

**Repo:** `workflow`
**Branch:** any branch off main (committed directly to main after all sweep PRs merge)
**Change class:** documentation only.

**Files:**
- Create: `docs/retros/2026-05-19-multi-repo-qol-sweep.md`

**Step 19.1: Compile retro.**

After Task 17 completes (or runs out of time), draft the retro. Required sections:

- Summary (1 paragraph)
- Final tally: PRs opened / merged / open / blocked
- What went well
- What didn't (gaps, retries, surprises)
- What we'd change next time
- Follow-up issues filed (link the tracking issues from Task 17.4)
- Verification of success criteria from the design

**Step 19.2: Commit.**

```sh
git add docs/retros/2026-05-19-multi-repo-qol-sweep.md
git commit -m "docs(retro): multi-repo OSS-readiness QoL sweep (2026-05-19)

Closes the loop on the cross-repo doc + license + experimental-marker
sweep authored at #714. Records what went well, what didn't, and
follow-up tracking issues filed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

**Verification:** retro file present and references the design + ADR.

---

## Cross-cutting concerns

### Worktree-isolated execution

Every per-repo task runs in a `_worktrees/qol-sweep` worktree inside the target repo. Implementers MUST NOT operate on the canonical checkout in `/Users/jon/workspace/<repo>` directly; the working-tree is reserved for the operator's interactive work.

Per `feedback_implementer_taskclaim_before_work`: implementer agents claim their task in TaskList with `owner` set BEFORE creating the worktree, to prevent racing.

### Local validation before push (per `feedback_local_image_launch_validation` + `feedback_no_speculative_remote_ci`)

Every PR has its build + vet + example validation run **locally** in the worktree before push. No PR uses CI to discover failures.

### Per-priority review tier

| Priority | Pre-merge gate |
|----------|----------------|
| P0 (Tasks 1, 2, 3, 4-10, 16) | Reviewer-agent + Copilot review pass + CI green; then admin-merge |
| P1 (Tasks 11-15) | Reviewer-agent + Copilot review pass + CI green; then admin-merge |
| P2 (Task 17) | Reviewer-agent + CI green; admin-merge |
| P3 (Task 18) | Reviewer-agent + CI green; admin-merge |

Per `feedback_copilot_review_settle_window` — wait ~10 minutes after `gh pr edit --add-reviewer "@copilot"` before admin-merge to allow Copilot's async pipeline to surface findings.

Per `feedback_copilot_reviewer_at_handle` — use `"@copilot"` literal, not display name.

### Coordinator policy

- The team-lead (main session) dispatches implementer agents in parallel per the team topology in the design.
- ScheduleWakeup every 20 minutes while team is active (per `feedback_team_watchdog_wakeup_required`).
- Implementers commit + push + open PR + add reviewer; team-lead admin-merges per the per-priority gate after Copilot settles.
- Stale or wedged worktree → fast-forward + re-attempt (per `feedback_worktree_agents_must_ff_before_commit`).

---

## Risks + Mitigations

1. **Schema PR-R1 fails ajv in CI even though local ajv passes.** Mitigation: PR-R1 is small; if it fails, fix-forward in the same PR before Task 16 starts.
2. **Banner-PR conflicts with concurrent maintainer work on the same repo's default branch.** Mitigation: branch off the latest origin/<default>; if conflict at merge time, rebase + re-push.
3. **`wfctl` not installed locally on implementer machines.** Mitigation: each task's setup runs `go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest` if `which wfctl` fails.
4. **Apache-2.0 audit for workflow-plugin-migrations finds vendored code.** Mitigation: file tracking issue + retain Apache; no MIT conversion.
5. **Worktree cap on the underlying repo.** Mitigation: clean up worktrees after each repo's PR merges (`git worktree remove _worktrees/qol-sweep`).
6. **doc-impl-5 (39-repo loop) times out.** Mitigation: split into two halves (19 + 20); first half checkpoints, second half resumes.

---

## Done When

All 5 success-criteria bullets from the design's "Success Criteria" section are checked off, AND Task 19 retrospective is committed.
