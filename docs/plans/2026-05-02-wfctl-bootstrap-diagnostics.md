# wfctl bootstrap diagnostics + schema validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make wfctl's `infra bootstrap` flow diagnose schema misconfigurations clearly and prevent the most common ones at align time, validated end-to-end by running the core-dump bootstrap+deploy chain entirely on wfctl (no doctl fallback).

**Architecture:** Three sequential, independently-mergeable PRs across two repos. PR 1 fixes core-dump's `infra.yaml` to match the BMW canonical pattern (single `key: SPACES` for the Spaces credential pair; explicit `token:`/`spaces_access_key:`/`spaces_secret_key:` in iac.provider config). PR 2 makes wfctl's external-plugin adapter propagate plugin errors via `errorModule` instead of swallowing as bare nil. PR 3 adds a new `wfctl infra align` rule that flags suspicious `provider_credential` schema patterns (key suffixed with the source's auto-suffix).

**Tech Stack:** Go (workflow + plugin SDK), YAML (infra.yaml), GitHub Actions (deploy workflow), gRPC (plugin protocol), wfctl v0.20.1+ (engine compat), workflow-plugin-digitalocean v0.8.0.

---

### Task 1: PR 1 — core-dump infra.yaml correction (CRITICAL PATH)

Repo: `GoCodeAlone/core-dump`. Branch: `fix/infra-yaml-schema-correct` off origin/main (fresh, NOT layered on `fix/shared-do-registry`). Reference design: `2026-05-02-wfctl-bootstrap-diagnostics-design.md` Section "PR 1".

**Files:**
- Modify: `infra.yaml` (two specific blocks — secrets.generate Spaces entries + iac.provider config)
- Create: `_worktrees/fix-infra-yaml-schema-correct/` (worktree off origin/main)
- Reference: `/Users/jon/workspace/buymywishlist/infra.yaml:11-17` (BMW's canonical SPACES pattern)
- Reference: `/Users/jon/workspace/buymywishlist/infra.yaml:36-42` (BMW's canonical iac.provider config)

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/core-dump
git fetch origin main
git worktree add _worktrees/fix-infra-yaml-schema-correct origin/main -b fix/infra-yaml-schema-correct
cd _worktrees/fix-infra-yaml-schema-correct
```

Expected: worktree created on a fresh branch tracking origin/main.

**Step 2: Make the secrets.generate edit**

Find the existing block in `infra.yaml`:

```yaml
  generate:
    - key: SPACES_access_key
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
    - key: SPACES_secret_key
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
```

Replace with a single entry:

```yaml
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
```

The auto-suffix convention (`providerCredentialSubKeys["digitalocean.spaces"] = ["access_key", "secret_key"]` in `cmd/wfctl/infra_bootstrap.go:251`) produces `SPACES_access_key` and `SPACES_secret_key` from this single declaration.

**Step 3: Make the iac.provider config edit**

Find the existing block:

```yaml
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env
```

Replace with the explicit-form pattern (matches BMW's working config at `/Users/jon/workspace/buymywishlist/infra.yaml:36-42`):

```yaml
  # iac.provider passes token + Spaces creds to the digitalocean plugin's
  # Initialize. `credentials: env` is NOT a wfctl shorthand (this was a P1
  # mistake); the explicit form matches the BMW canonical pattern. See
  # docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md for the full
  # diagnosis. The plugin's NewProvider requires `token` (per
  # workflow-plugin-digitalocean internal/provider.go:51); spaces_access_key
  # and spaces_secret_key are needed by the Spaces backend client used for
  # IaC state.
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
      spaces_access_key: ${SPACES_access_key}
      spaces_secret_key: ${SPACES_secret_key}
```

**Step 4: Static yaml + lint validation**

Run: `python3 -c "import yaml; yaml.safe_load(open('infra.yaml'))"` 
Expected: exit 0; no parse errors.

Run: `actionlint .github/workflows/*.yml` 
Expected: zero findings (no workflow files were touched, but verify nothing regressed).

**Step 5: Commit**

```bash
git add infra.yaml
git commit -m "$(cat <<'EOF'
fix(infra.yaml): correct schema to match BMW canonical pattern

Two P1 schema mistakes hidden behind the diagnostic gap that became the
post-merge deploy failure on 2026-05-02:

1. secrets.generate had two entries for the Spaces key pair — `key:
   SPACES_access_key` + `key: SPACES_secret_key`. wfctl auto-appends sub-key
   suffixes (`_access_key`, `_secret_key` per
   providerCredentialSubKeys["digitalocean.spaces"]) to each entry's key.
   Two entries produced four wrongly-named secrets
   (SPACES_ACCESS_KEY_ACCESS_KEY etc). One entry with `key: SPACES`
   produces the desired SPACES_access_key + SPACES_secret_key pair.
   Matches BMW's canonical pattern.

2. iac.provider config used `credentials: env`, which is not a wfctl
   shorthand. The plugin's NewProvider requires `token: "${DIGITALOCEAN_TOKEN}"`
   explicitly (workflow-plugin-digitalocean internal/provider.go:51).
   spaces_access_key + spaces_secret_key are needed by the Spaces backend
   client. Match BMW's working pattern.

Both mistakes were hard to diagnose because wfctl's external plugin
adapter swallowed the plugin's "missing required config key 'token'"
error as a generic "factory returned nil" message. PR 2 (workflow)
fixes that diagnostic gap; PR 3 (workflow) adds an align rule that
catches the schema pattern at config-validation time.

See docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

**Step 6: Push + open PR**

```bash
git push -u origin fix/infra-yaml-schema-correct
gh pr create --title "fix(infra.yaml): correct schema to match BMW canonical pattern" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Fixes two P1 schema mistakes that surfaced when the post-merge deploy first ran end-to-end (run 25244152181, bootstrap step). Diagnosis in `workflow/docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md`.

- `secrets.generate[]` Spaces entries: collapse two to one `key: SPACES` (auto-suffix produces `SPACES_access_key` + `SPACES_secret_key`)
- `iac.provider` config: replace fictional `credentials: env` with explicit `token:` / `spaces_access_key:` / `spaces_secret_key:` (matches BMW canonical pattern)

Stale GH repo secrets (SPACES_ACCESS_KEY_ACCESS_KEY etc.) cleaned up out-of-band before re-running bootstrap.

## Test plan

- [ ] CI green
- [ ] After merge: clean up stale secrets, re-run bootstrap workflow, verify exactly 2 `SPACES_*` secrets created
- [ ] Next CI completion auto-fires Deploy → deploy-staging reaches plan/align/security-check/apply chain
- [ ] Staging app responds to /healthz

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If `--reviewer "Copilot"` errors with "Could not resolve user with login 'copilot'", retry via API: `gh api -X POST /repos/GoCodeAlone/core-dump/pulls/<N>/requested_reviewers -f 'reviewers[]=Copilot'`.

**Step 7: Verification (post-merge end-to-end runtime check)**

After admin-merge:

```bash
# Clean up the four wrongly-named secrets from prior bootstrap
for s in SPACES_ACCESS_KEY_ACCESS_KEY SPACES_ACCESS_KEY_SECRET_KEY SPACES_SECRET_KEY_ACCESS_KEY SPACES_SECRET_KEY_SECRET_KEY; do
  gh secret delete "$s" --repo GoCodeAlone/core-dump
done

# Re-run bootstrap on staging
gh workflow run bootstrap.yml --repo GoCodeAlone/core-dump --ref main -f environment=staging

# Watch the run
gh run watch <bootstrap-run-id> --repo GoCodeAlone/core-dump
```

Expected post-bootstrap state:
- `gh api repos/GoCodeAlone/core-dump/actions/secrets --jq '.secrets[].name' | grep SPACES` returns EXACTLY two lines: `SPACES_access_key` and `SPACES_secret_key` (case-insensitive — GH uppercases).
- Bootstrap run conclusion: `success`.

Then trigger or wait for a Deploy run on the new main commit:

```bash
# Wait for the next CI completion → workflow_run-triggered Deploy
gh run list --repo GoCodeAlone/core-dump --workflow Deploy --branch main --limit 1
gh run watch <deploy-run-id> --repo GoCodeAlone/core-dump
```

Expected:
- `build-image` job: success (already passing on PR #152's fix).
- `deploy-staging` job: reaches all 4 wfctl steps (`plan`, `align --strict`, `security-check`, `apply --plan`) without `error: static credentials are empty` or `factory returned nil`.
- The job's "Capture STAGING_DATABASE_URL" step succeeds (`doctl databases connection coredump-staging-db --format URI` returns a non-empty URI).
- Final apply step exits 0; staging app reachable on /healthz.

If any of those fail, that's a new finding distinct from this plan's scope — file as follow-up; PR 1's success criterion is "bootstrap produces correctly-named secrets and deploy-staging clears the credentials-resolution gates that previously failed."

**Verification class:** This is a deployment-configuration change (per the writing-plans verification matrix). Runtime-launch-validation is required: post-merge bootstrap + deploy must run cleanly against real staging infra. The class-appropriate evidence is the bootstrap-success + deploy-staging-progresses-past-prior-failure-points captured above, not just unit tests.

---

### Task 2: PR 2 — workflow adapter error propagation

Repo: `GoCodeAlone/workflow`. Branch: `fix/adapter-error-propagation` off origin/main (fresh; do NOT use `design/wfctl-bootstrap-diagnostics`, that branch is for the design doc only). Reference design: `2026-05-02-wfctl-bootstrap-diagnostics-design.md` Section "PR 2".

**Files:**
- Modify: `plugin/external/adapter.go:309-343` (ModuleFactories CreateModule failure path)
- Modify: `plugin/external/adapter.go:354-400` (StepFactories CreateStep failure path — symmetry)
- Modify: `cmd/wfctl/deploy_providers.go:160-164` (caller — detect errorModule, surface its message)
- Test: `plugin/external/adapter_test.go` (new test exercising error propagation)
- Test: `cmd/wfctl/deploy_providers_test.go` (new test for resolveIaCProvider error path)

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow
git worktree add _worktrees/fix-adapter-error-prop origin/main -b fix/adapter-error-propagation
cd _worktrees/fix-adapter-error-prop
```

Expected: worktree on fresh branch off origin/main.

**Step 2: Write failing test for adapter ModuleFactories error propagation**

Edit `plugin/external/adapter_test.go` (create if absent — search for similar tests via `ls plugin/external/*_test.go`). Add:

```go
func TestModuleFactoriesPropagatesPluginError(t *testing.T) {
	// Fake gRPC client that returns a non-empty Error from CreateModule.
	fake := &fakePluginClient{
		moduleTypes: []string{"iac.provider"},
		createModuleResp: &pb.CreateModuleResponse{
			Error: "digitalocean: missing required config key 'token'",
		},
	}
	adapter := &ExternalPluginAdapter{
		client:        &pluginClientWrapper{client: fake},
		contracts:     emptyContracts(),
		contractTypes: nil,
	}

	factories := adapter.ModuleFactories()
	mod := factories["iac.provider"]("test-provider", map[string]any{})

	if mod == nil {
		t.Fatal("expected errorModule, got nil — error was swallowed")
	}
	errMod, ok := mod.(*errorModule)
	if !ok {
		t.Fatalf("expected *errorModule, got %T", mod)
	}
	if errMod.err == nil {
		t.Fatal("errorModule has nil err")
	}
	if !strings.Contains(errMod.err.Error(), "missing required config key 'token'") {
		t.Errorf("expected plugin error message in propagated error, got: %v", errMod.err)
	}
}
```

(`fakePluginClient` and `emptyContracts()` are test helpers — search for existing similar mocks in `plugin/external/*_test.go` and reuse, or add minimal versions in the test file. The existing test suite has prior art.)

**Step 3: Run test to verify it fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./plugin/external/ -run TestModuleFactoriesPropagatesPluginError -v
```

Expected: FAIL with "expected errorModule, got nil — error was swallowed" (current adapter swallows the error and returns bare nil).

**Step 4: Implement the fix in `plugin/external/adapter.go`**

In `ModuleFactories()` (lines 309-343), change the failure-path block from:

```go
createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{
    Type:        tn,
    Name:        name,
    Config:      config,
    TypedConfig: typedConfig,
})
if createErr != nil || createResp.Error != "" {
    return nil
}
```

to:

```go
createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{
    Type:        tn,
    Name:        name,
    Config:      config,
    TypedConfig: typedConfig,
})
if createErr != nil {
    return &errorModule{name: name, err: fmt.Errorf("create remote module %s: %w", tn, createErr)}
}
if createResp.Error != "" {
    return &errorModule{name: name, err: fmt.Errorf("create remote module %s: plugin reported: %s", tn, createResp.Error)}
}
```

Apply the symmetric fix to `StepFactories()` (lines 354+) for the CreateStep error path.

**Step 5: Run adapter test to verify pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./plugin/external/ -run TestModuleFactoriesPropagatesPluginError -v
```

Expected: PASS.

Run the full adapter test suite to catch regressions:

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./plugin/external/...
```

Expected: PASS, no regressions.

**Step 6: Write failing test for caller surfacing**

Edit `cmd/wfctl/deploy_providers_test.go` (create if absent). Add:

```go
func TestResolveIaCProviderSurfacesPluginError(t *testing.T) {
	// Stub a plugin manager that returns a factory which produces an errorModule.
	stubFactory := func(name string, cfg map[string]any) modular.Module {
		return &errorModule{
			name: name,
			err:  errors.New("digitalocean: missing required config key 'token'"),
		}
	}
	// Inject via the package-level resolveIaCProvider hook (mimic test pattern
	// used elsewhere in cmd/wfctl tests for resolveSecretsProvider etc.).
	old := loadPluginAdapterForTest
	loadPluginAdapterForTest = func(pluginDir, pluginName string) (factories map[string]plugin.ModuleFactory, closer io.Closer, err error) {
		return map[string]plugin.ModuleFactory{"iac.provider": stubFactory}, noopCloser{}, nil
	}
	defer func() { loadPluginAdapterForTest = old }()

	_, _, err := discoverAndLoadIaCProvider(context.Background(), "digitalocean", map[string]any{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required config key 'token'") {
		t.Errorf("expected plugin error in wrapped error, got: %v", err)
	}
}
```

(If `loadPluginAdapterForTest` injection seam doesn't exist yet, introduce it in the implementation step. Pattern: extract the `mgr := external.NewExternalPluginManager(...)` + `adapter, loadErr := mgr.LoadPlugin(...)` lines into a helper variable function, mockable via a package-level `var loadPluginAdapterForTest = realLoad`.)

**Step 7: Run test to verify it fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestResolveIaCProviderSurfacesPluginError -v
```

Expected: FAIL with the existing "factory returned nil" message because the caller doesn't yet check for `*errorModule`.

**Step 8: Update caller in `cmd/wfctl/deploy_providers.go`**

In `discoverAndLoadIaCProvider` (lines 143+), modify the post-`factory()` block:

```go
mod := factory("iac-provider", cfg)
if errMod, ok := mod.(*errorModule); ok {
    mgr.Shutdown()
    return nil, nil, fmt.Errorf("plugin %q iac.provider factory failed: %w", pluginName, errMod.err)
}
if mod == nil {
    mgr.Shutdown()
    return nil, nil, fmt.Errorf("plugin %q iac.provider factory returned nil (unexpected — file an issue)", pluginName)
}
```

If the test injects via `loadPluginAdapterForTest`, also extract the mgr/adapter setup as the helper described in Step 6.

**Step 9: Run caller test**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestResolveIaCProviderSurfacesPluginError -v
```

Expected: PASS.

**Step 10: Run full wfctl test suite to verify no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/... ./plugin/external/...
```

Expected: PASS across both packages.

**Step 11: Commit + push + open PR**

```bash
git add plugin/external/adapter.go plugin/external/adapter_test.go cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_test.go
git commit -m "$(cat <<'EOF'
fix(plugin/external): propagate plugin errors instead of swallowing as nil

The external plugin adapter's ModuleFactories returned bare nil on any
CreateModule failure (gRPC error OR plugin-reported error in
createResp.Error). Callers got no signal about WHY the factory failed —
just a generic "iac.provider factory returned nil" message that hid the
actual plugin diagnostic.

This was the diagnostic gap that hid two core-dump P1 schema mistakes
behind a generic message during the 2026-05-02 first-deploy attempt.
The plugin had returned "digitalocean: missing required config key
'token'", but the operator saw "factory returned nil".

Fix: when CreateModule fails (gRPC error OR createResp.Error), return
an *errorModule wrapping the underlying message. Same pattern already
used for configErr a few lines earlier — this just extends it to the
remote-create-fail path. Engine and wfctl callers already handle
errorModule, so this is a uniform convention rather than a contract
change. Symmetric fix in StepFactories.

Caller in cmd/wfctl/deploy_providers.go now detects *errorModule and
surfaces the wrapped error message; bare-nil branch becomes a
defensive backstop with "file an issue" hint.

See docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md PR 2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push -u origin fix/adapter-error-propagation
gh pr create --title "fix(plugin/external): propagate plugin errors instead of swallowing as nil" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Closes the diagnostic gap that hid two core-dump P1 schema mistakes behind a generic "factory returned nil" message during the 2026-05-02 deploy attempt (run 25244152181).

`ExternalPluginAdapter.ModuleFactories()` now returns an `*errorModule` (wrapping the actual plugin / gRPC error) instead of bare nil when `CreateModule` fails. Symmetric fix in `StepFactories()`. Caller in `cmd/wfctl/deploy_providers.go discoverAndLoadIaCProvider` surfaces the wrapped error message.

Engine and wfctl already handle `errorModule` on the configErr branch — this is a uniform convention rather than a contract change.

## Test plan

- [x] Unit test simulating `CreateModule` returning a non-empty `Error` field; assert returned module is `*errorModule` whose error contains the plugin's message.
- [x] Caller-side test: `discoverAndLoadIaCProvider` invoked against an injected factory that returns an `*errorModule`; assert the wrapped error message reaches the return value.
- [x] Existing adapter + wfctl test suites green.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If `--reviewer "Copilot"` fails: retry via `gh api -X POST /repos/GoCodeAlone/workflow/pulls/<N>/requested_reviewers -f 'reviewers[]=Copilot'`.

**Verification class:** internal logic refactor (per matrix). Unit tests green is sufficient evidence. No runtime-launch-validation required (no engine startup paths affected).

---

### Task 3: PR 3 — workflow infra align rule for suspicious provider_credential schema

Repo: `GoCodeAlone/workflow`. Branch: `feat/align-rule-credential-schema` off origin/main (fresh). Reference design: `2026-05-02-wfctl-bootstrap-diagnostics-design.md` Section "PR 3".

**Files:**
- Modify: `cmd/wfctl/infra_align_rules.go` (add `checkRA9` function + register in `runChecks` if there's a registry; otherwise add to the call site)
- Modify: `cmd/wfctl/infra_secrets.go` or wherever `providerCredentialSubKeys` is exposed; expose a getter the rule can call (or duplicate the map — see Step 5 trade-off)
- Test: `cmd/wfctl/infra_align_rules_test.go` (add tests for the new rule)
- Optional: `CHANGELOG.md` entry under "Unreleased"

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow
git worktree add _worktrees/feat-align-rule-cred origin/main -b feat/align-rule-credential-schema
cd _worktrees/feat-align-rule-cred
```

**Step 2: Find the rule registration site + the next R-A* identifier**

```bash
grep -n "checkRA[0-9]\b" cmd/wfctl/infra_align_rules.go cmd/wfctl/infra_align.go 2>&1
grep -n 'Rule:\s*"R-A[0-9]"' cmd/wfctl/infra_align_rules.go 2>&1
```

Expected: see existing R-A1..R-A8. Next sequential is R-A9.

Identify the call site that runs all checks (likely `runChecks` or similar in `infra_align.go`).

**Step 3: Write failing tests for the new rule**

Edit `cmd/wfctl/infra_align_rules_test.go`. Add:

```go
func TestCheckRA9_SuspiciousProviderCredentialKey(t *testing.T) {
	cases := []struct {
		name        string
		gens        []SecretGen
		wantFinding bool
		wantMsgSub  string // substring expected in finding.Message
	}{
		{
			name: "clean — key SPACES with digitalocean.spaces source",
			gens: []SecretGen{{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: false,
		},
		{
			name: "suspicious — key ends with _access_key for digitalocean.spaces",
			gens: []SecretGen{{Key: "SPACES_access_key", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: true,
			wantMsgSub:  "_access_key",
		},
		{
			name: "suspicious — key ends with _secret_key",
			gens: []SecretGen{{Key: "MY_THING_secret_key", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: true,
			wantMsgSub:  "_secret_key",
		},
		{
			name: "not provider_credential — random_hex with _access_key suffix is fine",
			gens: []SecretGen{{Key: "FOO_access_key", Type: "random_hex", Length: 32}},
			wantFinding: false,
		},
		{
			name: "unknown source — no rule applies until source is in providerCredentialSubKeys",
			gens: []SecretGen{{Key: "FOO_access_key", Type: "provider_credential", Source: "aws.s3"}},
			wantFinding: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &alignContext{secretGens: tc.gens}
			findings := checkRA9(ctx)
			if tc.wantFinding && len(findings) == 0 {
				t.Fatal("expected finding, got none")
			}
			if !tc.wantFinding && len(findings) != 0 {
				t.Fatalf("expected no findings, got: %+v", findings)
			}
			if tc.wantFinding && tc.wantMsgSub != "" && !strings.Contains(findings[0].Message, tc.wantMsgSub) {
				t.Errorf("expected message to contain %q, got: %s", tc.wantMsgSub, findings[0].Message)
			}
		})
	}
}
```

Note the `secretGens` field on `alignContext` may not exist yet — adding it is part of the implementation.

**Step 4: Run test to verify it fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestCheckRA9 -v
```

Expected: FAIL with compilation error (`checkRA9` undefined and/or `secretGens` undefined on alignContext).

**Step 5: Implement the rule**

Decision: where does `providerCredentialSubKeys` live? It's currently package-private in `cmd/wfctl/infra_bootstrap.go:251`. The align rule needs read access. Options:
- (a) Export it as `ProviderCredentialSubKeys` and let the align rule import it directly.
- (b) Add a getter `subKeysForSource(source string) ([]string, bool)` in `infra_bootstrap.go`; align rule calls that.
- (c) Duplicate the map in the align rule.

Pick (b) — minimal, encapsulated, single source of truth, no breaking export change. Add to `cmd/wfctl/infra_bootstrap.go`:

```go
// subKeysForSource returns the auto-suffixes wfctl appends to a
// provider_credential entry's key for the given source. Public callers should
// use this rather than reaching into providerCredentialSubKeys directly.
func subKeysForSource(source string) ([]string, bool) {
    subs, ok := providerCredentialSubKeys[source]
    return subs, ok
}
```

Add `secretGens` to `alignContext` in `cmd/wfctl/infra_align_rules.go`:

```go
type alignContext struct {
    modules []config.ModuleConfig
    // ... existing fields ...
    secretKeys map[string]struct{}
    secretGens []SecretGen  // NEW: the parsed secrets.generate[] entries
}
```

Populate `secretGens` in `buildAlignContext` (search for where `secretKeys` is populated; populate alongside it). Use `parseSecretsConfig(cfgFile)` (already exists in `infra_secrets.go`).

Add the rule function:

```go
// checkRA9 flags secrets.generate[] entries whose key ends with one of the
// auto-appended sub-key suffixes for the entry's provider_credential source.
// Such an entry is almost always a misconfiguration: wfctl auto-suffixes
// the sub-keys onto the key, so `key: SPACES_access_key` for source
// digitalocean.spaces produces `SPACES_access_key_access_key` +
// `SPACES_access_key_secret_key`. The intended pattern is `key: SPACES`,
// which produces `SPACES_access_key` + `SPACES_secret_key`.
func checkRA9(ctx *alignContext) []AlignFinding {
    var findings []AlignFinding
    for i, gen := range ctx.secretGens {
        if gen.Type != "provider_credential" {
            continue
        }
        subs, ok := subKeysForSource(gen.Source)
        if !ok {
            continue
        }
        for _, sub := range subs {
            suffix := "_" + sub
            if strings.HasSuffix(gen.Key, suffix) {
                findings = append(findings, AlignFinding{
                    Rule:     "R-A9",
                    Severity: "WARN",
                    Resource: fmt.Sprintf("secrets.generate[%d].key", i),
                    Message: fmt.Sprintf(
                        `key %q ends with %q. For provider_credential source %q wfctl auto-appends sub-key suffixes (%v) onto the key, producing wrongly-named secrets. To bind the credential to env vars matching the sub-key names, declare ONE entry with key set to just the parent prefix (e.g. %q produces %v). See docs/wfctl/secrets-generate.md.`,
                        gen.Key, suffix, gen.Source, subs,
                        strings.TrimSuffix(gen.Key, suffix),
                        prefixedSubKeys(strings.TrimSuffix(gen.Key, suffix), subs),
                    ),
                })
                break // one finding per entry is enough; avoid double-flagging if both suffixes match
            }
        }
    }
    return findings
}

// prefixedSubKeys returns the env-var names that would be produced if the
// user had used `key: <prefix>` for a provider_credential of the given source.
func prefixedSubKeys(prefix string, subs []string) []string {
    out := make([]string, len(subs))
    for i, s := range subs {
        out[i] = prefix + "_" + s
    }
    return out
}
```

Register R-A9 in the call site that runs all checks. Find it via `grep -n "checkRA8\b" cmd/wfctl/`. Append `findings = append(findings, checkRA9(ctx)...)` after the R-A8 invocation.

Strict-mode promotion: WARN → FAIL is handled by the existing `--strict` infrastructure; verify by reading the existing severity-handling code and following the same pattern (look for how R-A2 / R-A4 emit WARN under default and FAIL under `--strict`; mirror that).

**Step 6: Run rule unit tests**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestCheckRA9 -v
```

Expected: all 5 cases PASS.

**Step 7: Add CLI invocation verification (integration smoke)**

Run `wfctl infra align --help` to verify the rule documentation appears (if there's a `--list-rules` or similar; if not, just ensure `--help` exits 0 — no crash from new rule registration).

```bash
GOFLAGS=-mod=mod GOWORK=off go run ./cmd/wfctl infra align --help
```

Expected: exit 0, help text printed.

Also run align against a fixture that triggers R-A9:

```bash
mkdir -p cmd/wfctl/testdata/align
cat > cmd/wfctl/testdata/align/ra9-suspicious-key.yaml <<'EOF'
infra:
  auto_bootstrap: true
secrets:
  provider: github
  config:
    repo: example/test
    token_env: GH_TOKEN
  generate:
    - key: SPACES_access_key
      type: provider_credential
      source: digitalocean.spaces
      name: example-deploy-key
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
EOF

GOFLAGS=-mod=mod GOWORK=off go run ./cmd/wfctl infra align -c cmd/wfctl/testdata/align/ra9-suspicious-key.yaml --env staging
```

Expected: output includes a `R-A9` WARN finding mentioning `_access_key` and recommending `key: SPACES`.

Run again with `--strict`:

```bash
GOFLAGS=-mod=mod GOWORK=off go run ./cmd/wfctl infra align -c cmd/wfctl/testdata/align/ra9-suspicious-key.yaml --env staging --strict
```

Expected: exit non-zero (fails because R-A9 promoted to FAIL).

**Step 8: Run full wfctl test suite**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/...
```

Expected: PASS, no regressions in existing R-A1..R-A8 tests.

**Step 9: Commit + push + open PR**

```bash
git add cmd/wfctl/infra_align_rules.go cmd/wfctl/infra_align_rules_test.go cmd/wfctl/infra_bootstrap.go cmd/wfctl/testdata/align/ra9-suspicious-key.yaml
git commit -m "$(cat <<'EOF'
feat(infra align): add R-A9 to flag suspicious provider_credential keys

When a secrets.generate[] entry uses type: provider_credential with a
source whose sub-keys are known (currently digitalocean.spaces →
[access_key, secret_key]), wfctl auto-appends each sub-key as a suffix
to the entry's key. So `key: SPACES_access_key` for source
digitalocean.spaces produces `SPACES_access_key_access_key` and
`SPACES_access_key_secret_key` — almost certainly a misconfiguration
where the user intended the bare sub-key names.

R-A9 detects this pattern (key ending in `_<sub-key>` for the source's
known sub-keys) and emits a WARN under default mode, FAIL under
--strict. The message points at the canonical fix (use the parent
prefix as the key, e.g. `key: SPACES`).

Caught the original core-dump P1 schema mistake retroactively; would
have prevented it at align-time rather than deploy-time. See
docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md PR 3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push -u origin feat/align-rule-credential-schema
gh pr create --title "feat(infra align): add R-A9 to flag suspicious provider_credential keys" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Adds align rule R-A9 that flags secrets.generate[] entries whose key ends with the auto-suffix wfctl will append for the entry's provider_credential source. Catches the canonical "user wrote `key: SPACES_access_key` instead of `key: SPACES`" mistake at align-time.

WARN under default `wfctl infra align`; FAIL under `--strict`. Backwards-compatible: BMW's working `key: SPACES` pattern passes cleanly. Existing R-A1..R-A8 tests untouched.

## Test plan

- [x] Unit tests cover: clean key SPACES, suspicious _access_key suffix, suspicious _secret_key suffix, non-provider_credential type ignored, unknown source ignored.
- [x] Integration: `wfctl infra align -c testdata/align/ra9-suspicious-key.yaml` emits R-A9 WARN; same with `--strict` exits non-zero.
- [x] Existing wfctl + plugin tests green.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Add Copilot reviewer via API if `--reviewer` flag fails (same pattern as Tasks 1+2).

**Verification class:** CLI command + new rule (per matrix). Class-appropriate evidence: `wfctl infra align --help` exits 0 + the integration invocation against a fixture YAML produces the expected R-A9 finding.

---

### Task 4: Post-merge — bump wfctl pin in core-dump (after PR 2 + PR 3 ship)

Reference: design doc "Cross-repo coordination" section.

**Files:**
- Modify: `core-dump/.github/workflows/deploy.yml` (wfctl version pin line)
- Modify: `core-dump/.github/workflows/bootstrap.yml` (wfctl version pin line)
- Modify: `core-dump/.github/workflows/teardown.yml` (wfctl version pin line)
- Modify: `core-dump/.github/workflows/registry-retention.yml` (wfctl version pin line)
- Modify: `core-dump/infra.yaml` (engine compat header comment)

This task only runs AFTER PR 2 and PR 3 have shipped in a workflow release tag. Pure version-pin bump per `feedback_version_bump_immediate_merge` — fast-track admin-merge once CI is green.

**Step 1: Confirm the new workflow release tag exists**

```bash
gh release list --repo GoCodeAlone/workflow --limit 5
```

Expected: a release tag (e.g. `v0.20.2` or `v0.21.0`) that includes both PR 2 and PR 3's commits.

**Step 2: Set up branch + bump pins**

```bash
cd /Users/jon/workspace/core-dump
git fetch origin main
git worktree add _worktrees/bump-wfctl-pin origin/main -b chore/bump-wfctl-after-diagnostics
cd _worktrees/bump-wfctl-pin

# Replace v0.20.1 with the new tag (substitute NEW_TAG below)
NEW_TAG=v0.X.Y  # fill in from Step 1
for f in .github/workflows/{deploy,bootstrap,teardown,registry-retention}.yml; do
  sed -i.bak "s/version: v0\.20\.1/version: $NEW_TAG/g" "$f" && rm "$f.bak"
done
sed -i.bak "s/wfctl v0\.20\.1+/wfctl $NEW_TAG+/g" infra.yaml && rm infra.yaml.bak
```

**Step 3: Verify diff**

```bash
git diff --stat
```

Expected: 5 files changed, 6 insertions, 6 deletions (or similar — one line per workflow + one in infra.yaml header).

**Step 4: Commit + push + open PR**

```bash
git add .github/workflows/ infra.yaml
git commit -m "chore: bump wfctl pin to <NEW_TAG> for adapter+align diagnostics"
git push -u origin chore/bump-wfctl-after-diagnostics
gh pr create --title "chore: bump wfctl pin to <NEW_TAG>" --reviewer "Copilot" --body "Picks up adapter error propagation (workflow PR <PR2>) and align rule R-A9 (workflow PR <PR3>). See docs/plans/2026-05-02-wfctl-bootstrap-diagnostics-design.md."
```

**Step 5: Admin-merge once CI green**

Per `feedback_version_bump_immediate_merge` — pure version-pin PRs auto-merge in same turn. Admin-merge with `--squash --delete-branch` once CI passes; no Copilot review wait required.

**Verification class:** version pin update (per matrix). Run version-skew audit (per `superpowers:finishing-a-development-branch` Step 1c) + relaunch artifact (next Deploy run on main). Audit confirms no other refs to old tag remain; relaunch confirms the new wfctl version still drives the deploy chain green.

---

## Cross-task notes

**Branch hygiene:** Each task uses a fresh branch off origin/main. The `design/wfctl-bootstrap-diagnostics` branch (where this plan lives) does NOT get implementation commits — it's the design artifact. After all 3 PRs merge, the design branch can be force-pushed to track main if desired, or left as a historical artifact.

**Review discipline:**
- All 3 PRs get Copilot reviewer.
- PR 2 + PR 3: code-reviewer applies the IAC_PLUGIN_REVIEW_CHECKLIST.md 8-bug-class scan plus structpb-boundary scan (per `feedback_workflow_plugin_structpb_boundary` — relevant for any wfctl/plugin code touching contracts).
- PR 1: code-reviewer applies the schema-shape scan (BMW pattern parity, infra.yaml indentation, env-var resolution downstream).
- All ghost-flag verification per `feedback_copilot_ghost_flags_verify_file_content`.
- All admin-merge per `feedback_admin_override_pr_merge`.

**Active monitoring:** per `feedback_active_pr_monitoring` — actively poll PR state on every signal during CI/Copilot windows; do not passively idle.

**No doctl fallback** anywhere in any task. The whole point of this plan is dogfooding wfctl through the bootstrap+deploy chain.

**Auto-chaining:** per `feedback_continuous_autonomous_phases` — after Task 1 PR merges and validation succeeds, immediately spawn the team for Task 2; after Task 2 merges, immediately spawn Task 3; after Task 3 merges, immediately spawn Task 4 (assuming PRs 2+3 land in the same workflow release).

## System Impact

(Carried forward from design doc.)

- auth/authorization: Task 1 changes which env vars the plugin sees for token/spaces creds — same secret material, different env var names. No new auth surface.
- secrets: Task 1 deletes 4 wrongly-named GH repo secrets; recreates 2 correctly. Tasks 2+3+4 do not touch secrets.
- deploy pipeline: Task 1 unblocks the deploy chain that's been broken since P3 merged. Tasks 2+3 only affect diagnostics + validation; no runtime change. Task 4 bumps the wfctl version that drives the deploy chain.
- plugin contract: Task 2 changes the failure-mode response from "factory returns bare nil" to "factory returns errorModule". Engine and wfctl both already handle errorModule on the configErr branch, so this is a uniform convention rather than a new contract.
- align rules: Task 3 adds one new rule. Existing rules unaffected. The new rule is WARN-only by default; only `--strict` mode promotes to fail. core-dump's deploy.yml runs `--strict`, so once core-dump bumps wfctl past Task 3's release (Task 4), the rule is load-bearing for that consumer.
- All other System Impact Matrix categories (anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None — these are wfctl/CI/IaC plumbing changes with no game-runtime surface.
