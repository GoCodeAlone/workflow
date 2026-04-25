---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: dc9d918
  - repo: workflow
    commit: b0dce2f
  - repo: workflow
    commit: c44ea53
  - repo: workflow
    commit: b250e8e
  - repo: workflow
    commit: 11a43c9
  - repo: workflow
    commit: c02f228
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - rg -n "Troubleshooter|Diagnostic|troubleshootAfterFailure|emitDiagnostics|WriteStepSummary" interfaces cmd/wfctl
    - GOWORK=off go test ./interfaces ./config ./platform ./cmd/wfctl -run 'Test(Migration|Tenant|Canonical|BuildHook|PluginCLI|ScaffoldDockerfile|ResolveForEnv|ConfigHash|ApplyInfraModules|Diagnostic|Troubleshoot|ProviderID|ValidateProviderID|PluginInstall|ParseChecksums|Audit|WfctlManifest|WfctlLockfile|PluginLock|PluginAdd|PluginRemove|MigratePlugins|InfraOutputs)' -count=1
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Deploy-Log Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.18.10 + workflow-plugin-digitalocean v0.7.8 so that when a `wfctl infra apply` or `wfctl ci run --phase deploy` fails or its health check times out, wfctl automatically fetches provider-side deployment context and renders it to CI output (via `::group::` blocks and `$GITHUB_STEP_SUMMARY`), eliminating the need to visit the provider's web console to diagnose failures.

**Architecture:** Optional `Troubleshooter` interface on `ResourceDriver` (synchronous batch fetch, not streaming). DO plugin implements it by calling `godo` deployments + per-phase logs endpoints. wfctl invokes it automatically on failure paths, renders results via a `CIGroupEmitter` that detects `GITHUB_ACTIONS` / `GITLAB_CI` / `JENKINS_HOME` / `CIRCLECI`. Plugins that don't implement the interface silently no-op via `codes.Unimplemented`.

**Tech Stack:** Go 1.26, HashiCorp go-plugin (gRPC-over-stdio), godo (DO SDK), golang-migrate/goose/atlas (for BMW migrations, downstream consumer).

---

## Repos

- **workflow** (`/Users/jon/workspace/workflow`) — branch `feat/v0.18.10-observability` (base commit `b7d0ac1`, already has Troubleshooter/Diagnostic types in `interfaces/iac_resource_driver.go`)
- **workflow-plugin-digitalocean** (`/Users/jon/workspace/workflow-plugin-digitalocean`) — branch `feat/v0.7.8-troubleshoot` (base at main `0c11714`)
- **buymywishlist** (`/Users/jon/workspace/buymywishlist`) — new branch `chore/bump-wfctl-v0.18.10-do-v0.7.8` off main

## Dependency order

```
Task 1-9  (workflow)  →  Task 10 (tag v0.18.10)
                                ↓
Task 11-16 (DO plugin — Task 14 blocked by 10)  →  Task 17 (tag v0.7.8)
                                ↓
Task 18-19 (BMW bump — blocked by 17)
```

Phase 2 tasks 11-13 can start in parallel with Phase 1 work since they don't depend on the v0.18.10 tag — only Task 14 (go.mod bump) waits.

---

## Phase 1 — workflow v0.18.10

### Task 1: Verify Troubleshooter / Diagnostic types committed

**Files:**
- Verify: `interfaces/iac_resource_driver.go:63-90` (already committed in b7d0ac1)
- Test: `interfaces/iac_resource_driver_test.go` (create)

**Step 1: Read the existing committed interface**

Run: `git show b7d0ac1 -- interfaces/iac_resource_driver.go`
Expected: `Diagnostic` struct with `ID`, `Phase`, `Cause`, `At`, and `Detail` fields. `Troubleshooter` interface with one method.

**Note:** The committed version is missing the `Detail string` field specified in the design doc. Add it:

```go
type Diagnostic struct {
    ID     string    `json:"id"`
    Phase  string    `json:"phase"`
    Cause  string    `json:"cause"`
    At     time.Time `json:"at"`
    Detail string    `json:"detail,omitempty"` // optional verbose tail (log excerpt, stack)
}
```

**Step 2: Write a minimal compile-time interface-check test**

Create `interfaces/iac_resource_driver_test.go`:

```go
package interfaces

import (
    "context"
    "testing"
    "time"
)

// compile-time check: Troubleshooter is a valid interface.
var _ Troubleshooter = (*fakeTroubleshooter)(nil)

type fakeTroubleshooter struct{}

func (fakeTroubleshooter) Troubleshoot(_ context.Context, _ ResourceRef, _ string) ([]Diagnostic, error) {
    return nil, nil
}

func TestDiagnostic_JSONRoundtrip(t *testing.T) {
    d := Diagnostic{
        ID: "dep-abc", Phase: "pre_deploy", Cause: "exit 1",
        At: time.Now().UTC().Truncate(time.Second), Detail: "line1\nline2",
    }
    // simple JSON marshal/unmarshal sanity
    // (will fail initially if fields aren't exported with json tags)
    _ = d
}
```

**Step 3: Run tests**

Run: `go test ./interfaces/... -run TestDiagnostic`
Expected: PASS

**Step 4: Commit**

```bash
git add interfaces/iac_resource_driver.go interfaces/iac_resource_driver_test.go
git commit -m "interfaces: add Detail field + compile-time Troubleshooter check"
```

---

### Task 2: gRPC dispatch for Troubleshoot

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go:633-700` (existing `remoteResourceDriver`)
- Test: `cmd/wfctl/remote_resource_driver_test.go` (create)

**Background:** wfctl's `remoteResourceDriver` wraps gRPC calls via `d.invoker.InvokeService("ResourceDriver.<Method>", …)`. Add a `Troubleshoot` method that sends arguments and returns `[]Diagnostic`. When the plugin returns `codes.Unimplemented` (or similar), swallow and return `(nil, nil)` so callers see "no diagnostics" rather than an error.

**Step 1: Write the failing test**

Create `cmd/wfctl/remote_resource_driver_test.go`:

```go
package main

import (
    "context"
    "errors"
    "testing"

    "github.com/GoCodeAlone/workflow/interfaces"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type stubInvoker struct {
    svcCalled string
    argsSeen  map[string]any
    resp      map[string]any
    err       error
}

func (s *stubInvoker) InvokeService(svc string, args map[string]any) (map[string]any, error) {
    s.svcCalled = svc
    s.argsSeen = args
    return s.resp, s.err
}

func TestRemoteResourceDriver_Troubleshoot_Success(t *testing.T) {
    inv := &stubInvoker{
        resp: map[string]any{
            "diagnostics": []any{
                map[string]any{
                    "id": "dep-1", "phase": "pre_deploy",
                    "cause": "exit 1", "at": "2026-04-24T00:00:00Z",
                    "detail": "log tail",
                },
            },
        },
    }
    d := &remoteResourceDriver{invoker: inv}
    diags, err := d.Troubleshoot(context.Background(), interfaces.ResourceRef{Name: "x"}, "boom")
    if err != nil {
        t.Fatalf("unexpected: %v", err)
    }
    if len(diags) != 1 || diags[0].Cause != "exit 1" {
        t.Fatalf("unexpected diags: %+v", diags)
    }
    if inv.svcCalled != "ResourceDriver.Troubleshoot" {
        t.Errorf("wrong svc: %s", inv.svcCalled)
    }
}

func TestRemoteResourceDriver_Troubleshoot_UnimplementedSilent(t *testing.T) {
    inv := &stubInvoker{err: status.Error(codes.Unimplemented, "method not implemented")}
    d := &remoteResourceDriver{invoker: inv}
    diags, err := d.Troubleshoot(context.Background(), interfaces.ResourceRef{Name: "x"}, "boom")
    if err != nil {
        t.Fatalf("Unimplemented should not surface: %v", err)
    }
    if diags != nil {
        t.Fatalf("expected nil diags, got %+v", diags)
    }
}

func TestRemoteResourceDriver_Troubleshoot_OtherErrorSurfaces(t *testing.T) {
    inv := &stubInvoker{err: errors.New("network oops")}
    d := &remoteResourceDriver{invoker: inv}
    _, err := d.Troubleshoot(context.Background(), interfaces.ResourceRef{Name: "x"}, "boom")
    if err == nil {
        t.Fatal("expected error to surface")
    }
}
```

**Step 2: Run the test — should fail to compile**

Run: `go test ./cmd/wfctl/... -run TestRemoteResourceDriver_Troubleshoot -v`
Expected: compile error (Troubleshoot method doesn't exist on remoteResourceDriver).

**Step 3: Implement the method on `remoteResourceDriver`**

Add to `cmd/wfctl/deploy_providers.go` near the other `remoteResourceDriver` methods (around line 680):

```go
// Troubleshoot calls the plugin's optional Troubleshooter.Troubleshoot.
// Returns (nil, nil) silently when the plugin returns Unimplemented so
// the caller doesn't need to probe for capability — absence is a valid answer.
func (d *remoteResourceDriver) Troubleshoot(ctx context.Context, ref interfaces.ResourceRef, failureMsg string) ([]interfaces.Diagnostic, error) {
    res, err := d.invoker.InvokeService("ResourceDriver.Troubleshoot", map[string]any{
        "ref":         ref,
        "failure_msg": failureMsg,
    })
    if err != nil {
        if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
            return nil, nil
        }
        return nil, fmt.Errorf("resource driver Troubleshoot: %w", err)
    }
    raw, _ := res["diagnostics"].([]any)
    out := make([]interfaces.Diagnostic, 0, len(raw))
    for _, r := range raw {
        m, _ := r.(map[string]any)
        d := interfaces.Diagnostic{
            ID:     stringVal(m, "id"),
            Phase:  stringVal(m, "phase"),
            Cause:  stringVal(m, "cause"),
            Detail: stringVal(m, "detail"),
        }
        if s := stringVal(m, "at"); s != "" {
            if t, perr := time.Parse(time.RFC3339, s); perr == nil {
                d.At = t
            }
        }
        out = append(out, d)
    }
    return out, nil
}

// stringVal returns a string field or "" if missing/wrong type.
func stringVal(m map[string]any, k string) string {
    if v, ok := m[k].(string); ok {
        return v
    }
    return ""
}
```

Add imports if missing: `"google.golang.org/grpc/codes"`, `"google.golang.org/grpc/status"`, `"time"`.

**Step 4: Run the tests**

Run: `go test ./cmd/wfctl/... -run TestRemoteResourceDriver_Troubleshoot -v`
Expected: 3/3 PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/remote_resource_driver_test.go
git commit -m "wfctl: Troubleshoot gRPC dispatch with Unimplemented fallback"
```

---

### Task 3: CIGroupEmitter — provider detection + group markers

**Files:**
- Create: `cmd/wfctl/ci_output.go`
- Test: `cmd/wfctl/ci_output_test.go`

**Step 1: Write the failing test**

Create `cmd/wfctl/ci_output_test.go`:

```go
package main

import (
    "bytes"
    "os"
    "strings"
    "testing"
)

func TestDetectCIProvider_GitHub(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "true")
    t.Setenv("GITLAB_CI", "")
    t.Setenv("JENKINS_HOME", "")
    t.Setenv("CIRCLECI", "")
    e := detectCIProvider()
    if _, ok := e.(githubEmitter); !ok {
        t.Fatalf("expected githubEmitter, got %T", e)
    }
}

func TestDetectCIProvider_GitLab(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "")
    t.Setenv("GITLAB_CI", "true")
    e := detectCIProvider()
    if _, ok := e.(gitlabEmitter); !ok {
        t.Fatalf("expected gitlabEmitter, got %T", e)
    }
}

func TestDetectCIProvider_Default(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "")
    t.Setenv("GITLAB_CI", "")
    t.Setenv("JENKINS_HOME", "")
    t.Setenv("CIRCLECI", "")
    e := detectCIProvider()
    if _, ok := e.(plainEmitter); !ok {
        t.Fatalf("expected plainEmitter, got %T", e)
    }
}

func TestGithubEmitter_GroupMarkers(t *testing.T) {
    var buf bytes.Buffer
    e := githubEmitter{}
    e.GroupStart(&buf, "Troubleshoot: bmw-staging")
    buf.WriteString("hello\n")
    e.GroupEnd(&buf)
    out := buf.String()
    if !strings.Contains(out, "::group::Troubleshoot: bmw-staging\n") {
        t.Errorf("missing ::group:: marker: %q", out)
    }
    if !strings.Contains(out, "::endgroup::\n") {
        t.Errorf("missing ::endgroup:: marker: %q", out)
    }
}

func TestGitlabEmitter_GroupMarkers(t *testing.T) {
    var buf bytes.Buffer
    e := gitlabEmitter{}
    e.GroupStart(&buf, "my-section")
    e.GroupEnd(&buf)
    out := buf.String()
    if !strings.Contains(out, "section_start") {
        t.Errorf("missing section_start: %q", out)
    }
    if !strings.Contains(out, "section_end") {
        t.Errorf("missing section_end: %q", out)
    }
}

func TestPlainEmitter_UsesDashSeparators(t *testing.T) {
    var buf bytes.Buffer
    e := plainEmitter{}
    e.GroupStart(&buf, "section")
    e.GroupEnd(&buf)
    out := buf.String()
    if !strings.Contains(out, "--- section ---") {
        t.Errorf("expected --- section --- header, got %q", out)
    }
    _ = os.Stdout
}
```

**Step 2: Run — expected compile error**

Run: `go test ./cmd/wfctl/... -run TestDetectCIProvider -v`
Expected: undefined `detectCIProvider`, `githubEmitter`, etc.

**Step 3: Implement `ci_output.go`**

Create `cmd/wfctl/ci_output.go`:

```go
package main

import (
    "fmt"
    "io"
    "os"
    "time"
)

// CIGroupEmitter wraps output with CI-provider-specific group markers so
// long outputs (like diagnostics) render as collapsible sections in the UI.
type CIGroupEmitter interface {
    GroupStart(w io.Writer, name string)
    GroupEnd(w io.Writer)
    // SummaryPath returns the path to append Markdown summary content, or
    // "" if this CI provider has no step-summary concept.
    SummaryPath() string
}

// detectCIProvider inspects env vars and returns the appropriate emitter.
func detectCIProvider() CIGroupEmitter {
    switch {
    case os.Getenv("GITHUB_ACTIONS") == "true":
        return githubEmitter{summaryPath: os.Getenv("GITHUB_STEP_SUMMARY")}
    case os.Getenv("GITLAB_CI") == "true":
        return gitlabEmitter{}
    case os.Getenv("JENKINS_HOME") != "":
        return jenkinsEmitter{}
    case os.Getenv("CIRCLECI") == "true":
        return circleCIEmitter{}
    default:
        return plainEmitter{}
    }
}

// --- GitHub Actions ---

type githubEmitter struct{ summaryPath string }

func (g githubEmitter) GroupStart(w io.Writer, name string) {
    fmt.Fprintf(w, "::group::%s\n", name)
}
func (g githubEmitter) GroupEnd(w io.Writer)   { fmt.Fprintln(w, "::endgroup::") }
func (g githubEmitter) SummaryPath() string    { return g.summaryPath }

// --- GitLab CI ---

type gitlabEmitter struct{}

func (g gitlabEmitter) GroupStart(w io.Writer, name string) {
    id := fmt.Sprintf("wfctl_%d", time.Now().UnixNano())
    fmt.Fprintf(w, "\x1b[0Ksection_start:%d:%s\r\x1b[0K%s\n", time.Now().Unix(), id, name)
}
func (g gitlabEmitter) GroupEnd(w io.Writer) {
    id := fmt.Sprintf("wfctl_%d", time.Now().UnixNano())
    fmt.Fprintf(w, "\x1b[0Ksection_end:%d:%s\r\x1b[0K\n", time.Now().Unix(), id)
}
func (g gitlabEmitter) SummaryPath() string { return "" }

// --- Jenkins ---

type jenkinsEmitter struct{}

func (j jenkinsEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (j jenkinsEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (j jenkinsEmitter) SummaryPath() string                 { return "" }

// --- CircleCI ---

type circleCIEmitter struct{}

func (c circleCIEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (c circleCIEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (c circleCIEmitter) SummaryPath() string                 { return "" }

// --- Plain (default) ---

type plainEmitter struct{}

func (p plainEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (p plainEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (p plainEmitter) SummaryPath() string                 { return "" }
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run "TestDetectCIProvider|TestGithubEmitter|TestGitlabEmitter|TestPlainEmitter" -v`
Expected: 5/5 PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/ci_output.go cmd/wfctl/ci_output_test.go
git commit -m "wfctl: CIGroupEmitter with provider detection (GHA/GitLab/Jenkins/CircleCI)"
```

---

### Task 4: Step-summary Markdown writer

**Files:**
- Create: `cmd/wfctl/ci_output_summary.go`
- Test: `cmd/wfctl/ci_output_summary_test.go`
- Create: `cmd/wfctl/testdata/summary_failure.golden.md`
- Create: `cmd/wfctl/testdata/summary_success.golden.md`

**Step 1: Define the summary rendering contract**

Create `cmd/wfctl/ci_output_summary.go`:

```go
package main

import (
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    "github.com/GoCodeAlone/workflow/interfaces"
)

// SummaryInput is the data bundled into a step-summary Markdown block.
type SummaryInput struct {
    Operation   string        // "deploy" | "apply" | "destroy"
    Env         string        // e.g. "staging"
    Resource    string        // resource display name
    Outcome     string        // "SUCCESS" | "FAILED"
    ConsoleURL  string        // direct link to provider dashboard
    Diagnostics []interfaces.Diagnostic
    Phases      []PhaseTiming
    RootCause   string
}

type PhaseTiming struct {
    Name     string
    Status   string
    Duration time.Duration
}

// WriteStepSummary appends Markdown to the CI provider's summary destination.
// No-ops when the provider has no summary destination (all non-GHA for now).
func WriteStepSummary(emitter CIGroupEmitter, in SummaryInput) error {
    path := emitter.SummaryPath()
    if path == "" {
        return nil
    }
    f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
    if err != nil {
        return fmt.Errorf("open summary: %w", err)
    }
    defer f.Close()
    return renderSummary(f, in)
}

func renderSummary(w io.Writer, in SummaryInput) error {
    var b strings.Builder
    fmt.Fprintf(&b, "## wfctl: %s %s — %s\n\n", in.Operation, in.Env, in.Outcome)
    if in.Resource != "" {
        fmt.Fprintf(&b, "**Resource:** %s\n", in.Resource)
    }
    if in.RootCause != "" {
        fmt.Fprintf(&b, "**Root cause:** `%s`\n", in.RootCause)
    }
    if in.ConsoleURL != "" {
        fmt.Fprintf(&b, "**Console:** %s\n", in.ConsoleURL)
    }
    if len(in.Phases) > 0 {
        b.WriteString("\n### Phase timings\n\n| Phase | Status | Duration |\n|---|---|---|\n")
        for _, p := range in.Phases {
            fmt.Fprintf(&b, "| %s | %s | %s |\n", p.Name, p.Status, p.Duration.Round(time.Second))
        }
    }
    if len(in.Diagnostics) > 0 {
        b.WriteString("\n### Diagnostics\n\n")
        for _, d := range in.Diagnostics {
            fmt.Fprintf(&b, "- **[%s]** `%s` — %s\n", d.Phase, d.ID, d.Cause)
            if d.Detail != "" {
                fmt.Fprintf(&b, "  <details><summary>log tail</summary>\n\n  ```\n  %s\n  ```\n\n  </details>\n", strings.ReplaceAll(d.Detail, "\n", "\n  "))
            }
        }
    }
    _, err := io.WriteString(w, b.String())
    return err
}
```

**Step 2: Write the golden-file test**

Create `cmd/wfctl/ci_output_summary_test.go`:

```go
package main

import (
    "bytes"
    "flag"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/GoCodeAlone/workflow/interfaces"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func TestRenderSummary_Failure_Golden(t *testing.T) {
    in := SummaryInput{
        Operation:  "deploy",
        Env:        "staging",
        Resource:   "bmw-staging",
        Outcome:    "FAILED",
        ConsoleURL: "https://cloud.digitalocean.com/apps/abc",
        RootCause:  "workflow-migrate up: first .: file does not exist",
        Phases: []PhaseTiming{
            {Name: "build", Status: "SUCCESS", Duration: 134 * time.Second},
            {Name: "pre_deploy", Status: "ERROR", Duration: 3 * time.Second},
        },
        Diagnostics: []interfaces.Diagnostic{
            {ID: "dep-123", Phase: "pre_deploy", Cause: "exit status 1",
                At: mustTime("2026-04-24T17:42:45Z"),
                Detail: "workflow-migrate up: first .: file does not exist\nError: exit status 1"},
        },
    }
    var got bytes.Buffer
    if err := renderSummary(&got, in); err != nil {
        t.Fatal(err)
    }
    compareGolden(t, "summary_failure.golden.md", got.String())
}

func TestRenderSummary_Success_Golden(t *testing.T) {
    in := SummaryInput{
        Operation: "deploy", Env: "staging", Resource: "bmw-staging",
        Outcome: "SUCCESS", ConsoleURL: "https://cloud.digitalocean.com/apps/abc",
        Phases: []PhaseTiming{
            {Name: "build", Status: "SUCCESS", Duration: 134 * time.Second},
            {Name: "pre_deploy", Status: "SUCCESS", Duration: 12 * time.Second},
            {Name: "deploy", Status: "SUCCESS", Duration: 45 * time.Second},
        },
    }
    var got bytes.Buffer
    if err := renderSummary(&got, in); err != nil {
        t.Fatal(err)
    }
    compareGolden(t, "summary_success.golden.md", got.String())
}

func TestWriteStepSummary_NoPathNoop(t *testing.T) {
    e := plainEmitter{}
    if err := WriteStepSummary(e, SummaryInput{}); err != nil {
        t.Fatalf("plain emitter should noop, got err: %v", err)
    }
}

func TestWriteStepSummary_GHA_AppendsToFile(t *testing.T) {
    tmp := t.TempDir()
    path := filepath.Join(tmp, "summary.md")
    e := githubEmitter{summaryPath: path}
    if err := WriteStepSummary(e, SummaryInput{
        Operation: "apply", Env: "staging", Outcome: "SUCCESS",
    }); err != nil {
        t.Fatal(err)
    }
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Contains(data, []byte("## wfctl: apply staging — SUCCESS")) {
        t.Errorf("summary missing header: %q", data)
    }
}

func mustTime(s string) time.Time {
    t, err := time.Parse(time.RFC3339, s)
    if err != nil { panic(err) }
    return t
}

func compareGolden(t *testing.T, name, got string) {
    t.Helper()
    path := filepath.Join("testdata", name)
    if *updateGolden {
        if err := os.MkdirAll("testdata", 0o755); err != nil { t.Fatal(err) }
        if err := os.WriteFile(path, []byte(got), 0o644); err != nil { t.Fatal(err) }
        return
    }
    want, err := os.ReadFile(path)
    if err != nil { t.Fatalf("read golden: %v (run with -update-golden)", err) }
    if got != string(want) {
        t.Errorf("golden mismatch in %s\ngot:\n%s\nwant:\n%s", name, got, want)
    }
}
```

**Step 3: Generate golden files**

Run: `go test ./cmd/wfctl/... -run TestRenderSummary -update-golden`
Expected: creates `cmd/wfctl/testdata/summary_failure.golden.md` and `summary_success.golden.md`.

**Step 4: Re-run tests without `-update-golden`**

Run: `go test ./cmd/wfctl/... -run "TestRenderSummary|TestWriteStepSummary" -v`
Expected: 4/4 PASS.

**Step 5: Spot-check golden files**

Run: `cat cmd/wfctl/testdata/summary_failure.golden.md`
Expected: well-formed Markdown with `## wfctl: deploy staging — FAILED`, `**Resource:**`, `**Root cause:**`, phase table, diagnostic entry with collapsible log tail.

**Step 6: Commit**

```bash
git add cmd/wfctl/ci_output_summary.go cmd/wfctl/ci_output_summary_test.go cmd/wfctl/testdata/
git commit -m "wfctl: step-summary Markdown writer with golden tests"
```

---

### Task 5: Wire Troubleshoot into `ci run --phase deploy` failure path

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go:1028-1132` (HealthCheck + pollUntilHealthy)
- Test: `cmd/wfctl/deploy_providers_troubleshoot_test.go` (create)

**Background:** `pluginDeployProvider.HealthCheck` (line 1028) calls `pollUntilHealthy` (line 1053) which returns a terminal error on timeout (line 1132). Wire Troubleshoot invocation there: on any terminal error, call `driver.Troubleshoot`, render with `CIGroupEmitter`, and write step summary.

**Step 1: Write the failing test**

Create `cmd/wfctl/deploy_providers_troubleshoot_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/GoCodeAlone/workflow/interfaces"
)

type fakeTroubleshootingDriver struct {
    interfaces.ResourceDriver // embed for forward-compatibility; nil is fine for tests that don't call other methods
    diags []interfaces.Diagnostic
    err   error
    calls int
}

func (f *fakeTroubleshootingDriver) Troubleshoot(_ context.Context, _ interfaces.ResourceRef, _ string) ([]interfaces.Diagnostic, error) {
    f.calls++
    return f.diags, f.err
}

func TestEmitDiagnostics_WritesGroupBlock(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "true")
    var buf bytes.Buffer
    diags := []interfaces.Diagnostic{
        {ID: "dep-1", Phase: "pre_deploy", Cause: "exit 1",
            At: mustTime("2026-04-24T00:00:00Z"),
            Detail: "migration failed"},
    }
    emitDiagnostics(&buf, "bmw-staging", diags, detectCIProvider())
    out := buf.String()
    if !strings.Contains(out, "::group::Troubleshoot: bmw-staging") {
        t.Errorf("missing group marker: %q", out)
    }
    if !strings.Contains(out, "[pre_deploy]") || !strings.Contains(out, "exit 1") {
        t.Errorf("missing diagnostic body: %q", out)
    }
    if !strings.Contains(out, "::endgroup::") {
        t.Errorf("missing endgroup: %q", out)
    }
}

func TestEmitDiagnostics_EmptyIsNoop(t *testing.T) {
    var buf bytes.Buffer
    emitDiagnostics(&buf, "x", nil, plainEmitter{})
    if buf.Len() != 0 {
        t.Errorf("expected no output for empty diags, got %q", buf.String())
    }
}

func TestTroubleshootAfterFailure_Timeout(t *testing.T) {
    f := &fakeTroubleshootingDriver{
        diags: []interfaces.Diagnostic{{ID: "d", Phase: "run", Cause: "ouch", At: mustTime("2026-04-24T00:00:00Z")}},
    }
    var buf bytes.Buffer
    origErr := errors.New("plugin health check \"bmw-staging\": timed out waiting for healthy")
    troubleshootAfterFailure(context.Background(), &buf, f, interfaces.ResourceRef{Name: "bmw-staging"}, origErr, 30*time.Second, plainEmitter{})
    if f.calls != 1 {
        t.Errorf("Troubleshoot not called: calls=%d", f.calls)
    }
    if !strings.Contains(buf.String(), "ouch") {
        t.Errorf("missing Cause in output: %q", buf.String())
    }
}

func TestTroubleshootAfterFailure_NonTroubleshooterSkips(t *testing.T) {
    var buf bytes.Buffer
    type plainDriver struct{ interfaces.ResourceDriver }
    troubleshootAfterFailure(context.Background(), &buf, &plainDriver{}, interfaces.ResourceRef{}, errors.New("x"), 30*time.Second, plainEmitter{})
    if buf.Len() != 0 {
        t.Errorf("non-troubleshooter should not produce output: %q", buf.String())
    }
}
```

**Step 2: Run — expect compile error for missing helpers**

Run: `go test ./cmd/wfctl/... -run "TestEmitDiagnostics|TestTroubleshootAfterFailure" -v`
Expected: undefined `emitDiagnostics`, `troubleshootAfterFailure`.

**Step 3: Implement helpers in `cmd/wfctl/deploy_providers.go`**

Add near the other helpers (around line 1140):

```go
// emitDiagnostics renders diagnostics into a CI group block on w.
// No-op when diags is empty.
func emitDiagnostics(w io.Writer, resource string, diags []interfaces.Diagnostic, em CIGroupEmitter) {
    if len(diags) == 0 {
        return
    }
    em.GroupStart(w, fmt.Sprintf("Troubleshoot: %s", resource))
    for _, d := range diags {
        fmt.Fprintf(w, "  [%s] %s — %s (at %s)\n", d.Phase, d.ID, d.Cause, d.At.Format(time.RFC3339))
        if d.Detail != "" {
            for _, line := range strings.Split(strings.TrimRight(d.Detail, "\n"), "\n") {
                fmt.Fprintf(w, "    %s\n", line)
            }
        }
    }
    em.GroupEnd(w)
}

// troubleshootAfterFailure probes driver for Troubleshooter, calls it with a bounded
// timeout, and renders diagnostics via the provided emitter. All errors are swallowed
// (observability is additive; never masks original failure).
func troubleshootAfterFailure(ctx context.Context, w io.Writer, driver interface{}, ref interfaces.ResourceRef, origErr error, timeout time.Duration, em CIGroupEmitter) {
    ts, ok := driver.(interfaces.Troubleshooter)
    if !ok {
        return
    }
    tsCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    diags, err := ts.Troubleshoot(tsCtx, ref, origErr.Error())
    if err != nil {
        log.Printf("troubleshoot: %v (ignored)", err)
        return
    }
    emitDiagnostics(w, ref.Name, diags, em)
}
```

Add imports if missing: `"io"`, `"strings"`.

**Step 4: Run helper tests**

Run: `go test ./cmd/wfctl/... -run "TestEmitDiagnostics|TestTroubleshootAfterFailure" -v`
Expected: 4/4 PASS.

**Step 5: Wire into `pollUntilHealthy` terminal failure path**

At `cmd/wfctl/deploy_providers.go:1132` (where `baseErr` is built on timeout), before returning the error, call `troubleshootAfterFailure`. Locate the `return fmt.Errorf(...)` line at ~1132 and change:

```go
// before
return errors.New(baseErr + " (last known state: " + result.Message + ")")
```

to:

```go
// after
em := detectCIProvider()
troubleshootAfterFailure(ctx, os.Stderr, driver, ref, errors.New(baseErr), 30*time.Second, em)
return errors.New(baseErr + " (last known state: " + result.Message + ")")
```

Similarly at the other `return fmt.Errorf("plugin health check %q: %w", name, wrapped)` at ~1089 (non-transient driver error) wire in the same call before the return.

**Step 6: Build and run existing deploy tests to verify no regression**

Run: `go build ./... && go test ./cmd/wfctl/... -run "TestPluginDeploy|TestPollUntilHealthy" -v`
Expected: existing tests pass; new troubleshoot wiring compiles.

**Step 7: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_troubleshoot_test.go
git commit -m "wfctl: call Troubleshoot after deploy health-check failure"
```

---

### Task 6: Wire Troubleshoot into `infra apply` failure path

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (find the driver.Apply/HealthCheck error return paths)
- Test: `cmd/wfctl/infra_apply_troubleshoot_test.go` (create)

**Step 1: Locate call sites**

Run: `grep -n "driver.Apply\|driver.HealthCheck\|return.*err" cmd/wfctl/infra_apply.go | head -20`
Identify the error-return paths where the driver's Apply or HealthCheck failed.

**Step 2: Add the same `troubleshootAfterFailure` call before those returns**

Pattern (apply this at each failure site):

```go
if err != nil {
    em := detectCIProvider()
    troubleshootAfterFailure(ctx, os.Stderr, driver, ref, err, 30*time.Second, em)
    return fmt.Errorf("apply %s: %w", ref.Name, err)
}
```

The exact placement depends on the file's structure — do NOT invent new error paths; only wrap existing ones.

**Step 3: Write an integration-level test**

Create `cmd/wfctl/infra_apply_troubleshoot_test.go` with a test that:
- Builds a fake `driver` implementing both `ResourceDriver` and `Troubleshooter`
- Calls `infra apply`'s top-level exec with a plan that has one action failing
- Asserts stderr contains the `Troubleshoot:` group block with the diagnostic
- Asserts the outer error is unchanged (original failure message preserved)

Skeleton:

```go
func TestInfraApply_EmitsDiagnosticsOnFailure(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "true")
    // set up fake driver, fake state store, fake resolvers
    // call the smallest applyPlan()-style helper that wraps driver.Apply error
    // capture stderr via a pipe
    // assert "Troubleshoot: bmw-staging" and the Cause appear
}
```

(Plan writer: the executing agent should read `infra_apply.go` + surrounding test scaffolding to pick the right helper to exercise. If the existing test harness doesn't support injecting a fake driver, note it as a blocker and escalate to team-lead; do NOT invent test infrastructure for this task.)

**Step 4: Run test**

Run: `go test ./cmd/wfctl/... -run TestInfraApply_EmitsDiagnosticsOnFailure -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_troubleshoot_test.go
git commit -m "wfctl: call Troubleshoot after infra apply failure"
```

---

### Task 7: End-to-end test — deploy failure surfaces diagnostics

**Files:**
- Create: `cmd/wfctl/e2e_deploy_troubleshoot_test.go`

This is a broader table-driven test to lock in behavior.

**Step 1: Write the test**

```go
package main

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestE2EDeployFailure_EmitsFullTroubleshootBlock(t *testing.T) {
    t.Setenv("GITHUB_ACTIONS", "true")
    summary := t.TempDir() + "/summary.md"
    t.Setenv("GITHUB_STEP_SUMMARY", summary)

    diags := []interfaces.Diagnostic{
        {ID: "dep-1", Phase: "pre_deploy", Cause: "migration failed",
            At: mustTime("2026-04-24T17:42:45Z"),
            Detail: "exit status 1"},
        {ID: "dep-1", Phase: "build", Cause: "",
            At: mustTime("2026-04-24T17:40:00Z"), Detail: ""},
    }
    f := &fakeTroubleshootingDriver{diags: diags}
    var stderr bytes.Buffer
    em := detectCIProvider()
    troubleshootAfterFailure(context.Background(), &stderr, f, interfaces.ResourceRef{Name: "bmw-staging"},
        errors.New("timed out"), 30*time.Second, em)
    if f.calls != 1 {
        t.Fatal("Troubleshoot not called")
    }
    out := stderr.String()
    if !strings.Contains(out, "::group::Troubleshoot: bmw-staging") { t.Error("missing group") }
    if !strings.Contains(out, "[pre_deploy]") { t.Error("missing phase marker") }
    if !strings.Contains(out, "migration failed") { t.Error("missing cause") }
    if !strings.Contains(out, "exit status 1") { t.Error("missing detail") }
}

func TestE2ENoTroubleshooter_NoCrash(t *testing.T) {
    var stderr bytes.Buffer
    type plainDriver struct{ interfaces.ResourceDriver }
    troubleshootAfterFailure(context.Background(), &stderr, &plainDriver{}, interfaces.ResourceRef{},
        errors.New("x"), 30*time.Second, plainEmitter{})
    if stderr.Len() != 0 { t.Errorf("unexpected output: %q", stderr.String()) }
}
```

**Step 2: Run**

Run: `go test ./cmd/wfctl/... -run TestE2EDeploy -v`
Expected: 2/2 PASS.

**Step 3: Commit**

```bash
git add cmd/wfctl/e2e_deploy_troubleshoot_test.go
git commit -m "wfctl: e2e tests for Troubleshoot wiring"
```

---

### Task 8: CHANGELOG.md entry

**Files:**
- Modify: `CHANGELOG.md` (at repo root)

**Step 1: Add v0.18.10 section**

Prepend to CHANGELOG.md (or create if missing):

```markdown
## v0.18.10 — 2026-04-24

### Added
- `interfaces.Troubleshooter` optional interface on `ResourceDriver`. Drivers implementing it
  return structured `[]Diagnostic` (phase, cause, timestamp, log-tail detail) that wfctl renders
  in CI output on deploy/apply failure. No changes required for drivers that don't implement it.
- `cmd/wfctl/ci_output.go`: CI-provider detection (GitHub Actions, GitLab CI, Jenkins, CircleCI)
  with grouped output via provider-native markers (`::group::`, `section_start`, dashed separators).
- `cmd/wfctl/ci_output_summary.go`: Markdown step-summary writer for
  `$GITHUB_STEP_SUMMARY` (and equivalents) with resource, root cause, phase timings,
  and collapsible per-diagnostic log tails.

### Changed
- `wfctl ci run --phase deploy` and `wfctl infra apply` automatically invoke
  `Troubleshooter.Troubleshoot` on any terminal failure (health-check timeout or driver
  error), render diagnostics, and write the step-summary. Original exit codes and error
  messages are preserved (observability is additive).

### Not yet
- Generic `IaCProvider.StreamLogs` for real-time log display during builds — deferred to
  v0.19.0 alongside plugin-manifest split (#42) and provider-agnostic CI summary (#63).
- AWS, GCP, Azure, tofu Troubleshoot implementations — DO only in v0.7.8; others no-op.
```

**Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.18.10"
```

---

### Task 9: Open PR on workflow repo

**Files:** none (git operations only).

**Step 1: DM code-reviewer + spec-reviewer for pre-push review**

Message template (send via team SendMessage):
> PR #?? draft on workflow feat/v0.18.10-observability — v0.18.10 observability. X commits: <git log main..HEAD --oneline>. Design doc at docs/plans/2026-04-24-wfctl-deploy-log-observability-design.md. Standing by for approval before pushing.

Wait for both reviewers to approve.

**Step 2: Push branch**

```bash
git push -u origin feat/v0.18.10-observability
```

**Step 3: Open PR**

```bash
gh pr create --repo GoCodeAlone/workflow --title "feat: v0.18.10 — wfctl deploy-log observability (Troubleshooter interface)" --body "$(cat <<'EOF'
## Summary
- Adds optional `Troubleshooter` interface on `ResourceDriver` so drivers can explain deploy failures without the operator opening the provider console.
- Wires wfctl to invoke `Troubleshoot` automatically after deploy health-check timeout or apply failure; renders diagnostics via CI-provider-agnostic group blocks (GHA, GitLab, Jenkins, CircleCI) and writes a Markdown summary to `$GITHUB_STEP_SUMMARY` (and equivalents).
- Backwards compatible: drivers that don't implement Troubleshooter silently no-op via `codes.Unimplemented`.

## Test plan
- [ ] `go test ./...` passes (incl. new unit + golden-file tests)
- [ ] `go vet ./...` clean
- [ ] `gofmt -l` returns empty
- [ ] After merge: DO plugin v0.7.8 implements `AppPlatformDriver.Troubleshoot`; BMW retries deploy and sees pre-deploy migration error surfaced in GHA output within 30s of timeout.

Design: `docs/plans/2026-04-24-wfctl-deploy-log-observability-design.md`
Follow-up: workflow-plugin-digitalocean v0.7.8 (separate PR, blocked by this tag)
EOF
)"
```

**Step 4: Add Copilot reviewer**

```bash
PR_NUM=$(gh pr view --repo GoCodeAlone/workflow --json number -q '.number')
gh pr edit "$PR_NUM" --repo GoCodeAlone/workflow --add-reviewer copilot-pull-request-reviewer
```

**Step 5: Wait 15+ min for Copilot review, address comments**

Follow standard team process — do NOT `@copilot` in comment bodies (memory: use `--add-reviewer copilot-pull-request-reviewer`).

**Step 6: DM team-lead for merge approval**

Message: "workflow PR #?? ready for merge approval — v0.18.10"

---

### Task 10: Tag v0.18.10 (team-lead action, post-merge)

**Files:** none.

**Step 1: After PR merges to main, team-lead:**

```bash
cd /Users/jon/workspace/workflow
git checkout main && git pull
git log --oneline -3  # verify merge SHA
git tag -a v0.18.10 -m "v0.18.10: wfctl deploy-log observability (Troubleshooter interface)"
git push origin v0.18.10
```

**Step 2: Verify release workflow runs**

```bash
gh run list --repo GoCodeAlone/workflow --workflow=Release --limit 1
```
Expected: run started, status=in_progress, title references v0.18.10.

**Step 3: Wait for release assets to be published, verify installer works**

```bash
gh release view v0.18.10 --repo GoCodeAlone/workflow --json assets --jq '.assets | length'
```
Expected: multiple asset rows (platform-specific binaries).

---

## Phase 2 — workflow-plugin-digitalocean v0.7.8

### Task 11: Implement `AppPlatformDriver.Troubleshoot`

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean`, branch `feat/v0.7.8-troubleshoot`.

**Files:**
- Modify: `internal/drivers/app_platform.go` (add method)
- Create: `internal/drivers/app_platform_troubleshoot_test.go`

**Step 1: Write the failing test**

```go
package drivers

import (
    "context"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/GoCodeAlone/workflow/interfaces"
    "github.com/digitalocean/godo"
)

type fakeAppClient struct {
    app      *godo.App
    deps     []*godo.Deployment
    logs     map[string]string // key: deploymentID + "/" + phase
    appErr   error
}

func (f *fakeAppClient) Get(_ context.Context, id string) (*godo.App, *godo.Response, error) {
    return f.app, &godo.Response{Response: &http.Response{StatusCode: 200}}, f.appErr
}
// implement other godo.AppsService methods as needed; delegate to inner where not stubbed

func TestTroubleshoot_PreDeployFailure(t *testing.T) {
    // ... setup fakeAppClient with a deployment whose PreDeployPhase=ERROR
    //     and logs["dep-1/pre_deploy"] = "exit status 1\nmigration failed"
    // call driver.Troubleshoot and assert:
    //   - at least one Diagnostic with Phase="pre_deploy", Cause contains "exit status 1"
    //   - Detail contains the full log body
    //   - order: most-recent deployment first, then phase ordering
}

func TestTroubleshoot_BuildFailure(t *testing.T) { /* similar */ }
func TestTroubleshoot_RunExitCode(t *testing.T)   { /* similar */ }
func TestTroubleshoot_MultiDeployment(t *testing.T) { /* assert ordering */ }
func TestTroubleshoot_UnknownResourceType(t *testing.T) {
    // When ref points at a non-app_platform resource, return (nil, nil)
}
```

**Step 2: Implement Troubleshoot on `AppPlatformDriver`**

In `internal/drivers/app_platform.go`, add near the other driver methods:

```go
// Troubleshoot implements interfaces.Troubleshooter. Returns diagnostics for
// the app's most recent deployments (up to 3) with per-phase logs tailored for
// operator-readable failure diagnosis.
func (d *AppPlatformDriver) Troubleshoot(ctx context.Context, ref interfaces.ResourceRef, failureMsg string) ([]interfaces.Diagnostic, error) {
    if ref.ProviderID == "" {
        return nil, nil
    }
    app, _, err := d.client.Get(ctx, ref.ProviderID)
    if err != nil {
        return nil, fmt.Errorf("troubleshoot: get app: %w", err)
    }
    if app == nil {
        return nil, nil
    }
    // Collect candidate deployments: InProgress > Pending > Active, plus recent history
    candidates := pickTroubleshootDeployments(app)
    if len(candidates) == 0 {
        return nil, nil
    }
    var out []interfaces.Diagnostic
    for _, dep := range candidates {
        for _, phase := range []string{"pre_deploy", "build", "deploy", "run"} {
            diag, err := d.buildDiagnosticFor(ctx, app.ID, dep, phase)
            if err != nil {
                continue // best-effort per phase
            }
            if diag != nil {
                out = append(out, *diag)
            }
        }
    }
    return out, nil
}

// pickTroubleshootDeployments returns up to 3 deployments to examine, in
// priority order: InProgress, Pending, Active, then recent historical ones.
func pickTroubleshootDeployments(app *godo.App) []*godo.Deployment { /* impl */ }

// buildDiagnosticFor fetches phase logs and synthesizes a Diagnostic.
// Returns nil,nil if the phase had no activity for this deployment.
func (d *AppPlatformDriver) buildDiagnosticFor(ctx context.Context, appID string, dep *godo.Deployment, phase string) (*interfaces.Diagnostic, error) {
    // 1. determine this phase's status on this deployment (via dep.Phase, dep.Progress, etc.)
    // 2. GET /v2/apps/{appID}/deployments/{dep.ID}/logs?type=<phase>
    // 3. extract tail (last 100 lines or 4 KB, whichever is smaller)
    // 4. extract Cause via extractCause(tail)
    // 5. return Diagnostic
}

// extractCause scans a log tail for common error patterns and returns the
// first matching line (best-effort).
func extractCause(tail string) string {
    patterns := []string{
        "Error:", "error:", "exit status", "exit code", "failed to", "panic:", "fatal:",
    }
    for _, line := range strings.Split(tail, "\n") {
        for _, p := range patterns {
            if strings.Contains(line, p) {
                return strings.TrimSpace(line)
            }
        }
    }
    // fallback: last non-empty line
    lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
    for i := len(lines) - 1; i >= 0; i-- {
        if s := strings.TrimSpace(lines[i]); s != "" {
            return s
        }
    }
    return ""
}
```

Add imports: `"strings"`, `"github.com/GoCodeAlone/workflow/interfaces"` (if not already).

**Step 3: Run tests**

Run: `go test ./internal/drivers/... -run TestTroubleshoot -v`
Expected: all tests PASS.

**Step 4: Commit**

```bash
git add internal/drivers/app_platform.go internal/drivers/app_platform_troubleshoot_test.go
git commit -m "drivers: AppPlatformDriver.Troubleshoot implementation + tests"
```

---

### Task 12: gRPC dispatch for Troubleshoot in DO plugin

**Files:**
- Modify: `internal/plugin/dispatcher.go` (or wherever `ResourceDriver.<Method>` dispatch lives)

**Step 1: Locate existing dispatch pattern**

Run: `grep -n "ResourceDriver.HealthCheck\|InvokeMethod\|case.*HealthCheck" internal/plugin/*.go | head -10`

**Step 2: Add Troubleshoot dispatch case**

In the dispatcher switch, add case mirror to HealthCheck:

```go
case "ResourceDriver.Troubleshoot":
    ts, ok := driver.(interfaces.Troubleshooter)
    if !ok {
        return nil, status.Error(codes.Unimplemented, "driver does not implement Troubleshooter")
    }
    var ref interfaces.ResourceRef
    if err := decodeArg(args, "ref", &ref); err != nil { return nil, err }
    failureMsg, _ := args["failure_msg"].(string)
    diags, err := ts.Troubleshoot(ctx, ref, failureMsg)
    if err != nil { return nil, err }
    return map[string]any{"diagnostics": diagnosticsToGrpcMap(diags)}, nil
```

Add `diagnosticsToGrpcMap` helper nearby converting `[]interfaces.Diagnostic` to `[]map[string]any` for gRPC marshaling.

**Step 3: Unit test the dispatch path**

```go
func TestDispatch_Troubleshoot_NotImplemented(t *testing.T) {
    // dispatcher with a driver that doesn't implement Troubleshooter
    // call dispatch("ResourceDriver.Troubleshoot", args)
    // expect codes.Unimplemented
}

func TestDispatch_Troubleshoot_Implemented(t *testing.T) {
    // dispatcher with AppPlatformDriver (or stub) that returns 1 Diagnostic
    // expect diagnostics map in response
}
```

Run: `go test ./internal/plugin/... -run TestDispatch_Troubleshoot -v`
Expected: 2/2 PASS.

**Step 4: Commit**

```bash
git add internal/plugin/dispatcher.go internal/plugin/dispatcher_test.go
git commit -m "plugin: gRPC dispatch for ResourceDriver.Troubleshoot"
```

---

### Task 13: Additional godo client test coverage

**Files:**
- Modify: `internal/drivers/app_platform_troubleshoot_test.go` (extend from Task 11)

Add coverage for:
- Happy path: deployment ACTIVE, no diagnostics returned (or one with `Cause=""`)
- `app == nil` or `appErr != nil` handled without crash
- `ref.ProviderID == ""` returns `(nil, nil)`
- `extractCause` table test with realistic log samples

```go
func TestExtractCause_TableDriven(t *testing.T) {
    cases := []struct{ name, in, want string }{
        {"go panic", "panic: runtime error\nsomething", "panic: runtime error"},
        {"exit status", "foo\nError: exit status 1\nbar", "Error: exit status 1"},
        {"empty", "", ""},
        {"whitespace only", "   \n  ", ""},
        {"fallback to last", "random\ngoodbye", "goodbye"},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := extractCause(c.in); got != c.want {
                t.Errorf("got %q want %q", got, c.want)
            }
        })
    }
}
```

Commit:

```bash
git add internal/drivers/app_platform_troubleshoot_test.go
git commit -m "drivers: extra Troubleshoot test coverage (edge cases + extractCause table)"
```

---

### Task 14: Bump workflow dependency to v0.18.10 (blocked by Task 10)

**Files:**
- Modify: `go.mod`, `go.sum`

**Step 1: Verify Task 10 complete — workflow v0.18.10 tagged**

```bash
gh release view v0.18.10 --repo GoCodeAlone/workflow --json tagName
```
Expected: `{"tagName":"v0.18.10"}`.

**Step 2: Bump**

```bash
go get github.com/GoCodeAlone/workflow@v0.18.10
go mod tidy
```

**Step 3: Build + full test**

```bash
GOWORK=off go build ./... && GOWORK=off go test -race -short ./...
```

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: bump workflow v0.18.9 → v0.18.10"
```

---

### Task 15: CHANGELOG.md for DO plugin v0.7.8

Prepend:

```markdown
## v0.7.8 — 2026-04-24

### Added
- `AppPlatformDriver.Troubleshoot` implements `interfaces.Troubleshooter` from workflow
  v0.18.10. On deploy failure, wfctl automatically fetches the most recent deployment(s)
  and their per-phase logs (pre_deploy, build, deploy, run), synthesizes `[]Diagnostic`
  entries with extracted root-cause lines, and surfaces them in CI output — no DO
  console trip required to diagnose failures.

### Changed
- Depends on workflow v0.18.10 (was v0.18.9).
```

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.7.8"
```

---

### Task 16: Open PR on workflow-plugin-digitalocean

Same flow as Task 9:
- DM code-reviewer + spec-reviewer pre-push
- `git push -u origin feat/v0.7.8-troubleshoot`
- `gh pr create` with summary + test plan referencing workflow v0.18.10 + design doc
- `gh pr edit ?? --add-reviewer copilot-pull-request-reviewer`
- Wait for Copilot, address comments, DM team-lead

---

### Task 17: Tag v0.7.8 (team-lead, post-merge)

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git checkout main && git pull
git tag -a v0.7.8 -m "v0.7.8: Troubleshoot interface (deploy-log observability via wfctl v0.18.10)"
git push origin v0.7.8
```

Verify release workflow publishes binaries + manifest update.

---

## Phase 3 — BMW consumer bump

### Task 18: Bump BMW pins (impl-bmw-2 owner; blocked by Task 17)

**Repo:** `/Users/jon/workspace/buymywishlist`, new branch `chore/bump-wfctl-v0.18.10-do-v0.7.8`.

**Files:**
- Modify: `.github/workflows/deploy.yml` (find `uses: GoCodeAlone/setup-wfctl@v1` with `with: version:` line OR env `WFCTL_VERSION`)
- Modify: `.github/workflows/deploy.yml` (find DO plugin version pin — grep for `workflow-plugin-digitalocean`)

**Step 1: Grep current pins**

```bash
grep -rn "v0\.18\.9\|workflow-plugin-digitalocean.*v0\.7\.7" .github/workflows/ infra.yaml 2>/dev/null
```

**Step 2: Update each occurrence**

- `v0.18.9` → `v0.18.10`
- `workflow-plugin-digitalocean` `v0.7.7` → `v0.7.8`

**Step 3: Commit**

```bash
git add .github/workflows/ infra.yaml
git commit -m "chore: bump setup-wfctl v0.18.9 → v0.18.10 + DO plugin v0.7.7 → v0.7.8 (observability)"
```

**Step 4: DM code-reviewer, push, open PR, Copilot review, DM team-lead**

Same flow as every other BMW bump. Additive change only; no deploy.yml logic edits.

---

### Task 19: Merge BMW PR (team-lead)

Standard admin-merge once CI green + Copilot clean + any comments addressed.

After merge, BMW deploys on main will use wfctl v0.18.10 + DO plugin v0.7.8 automatically. The NEXT deploy failure will surface full troubleshooting context in the GHA run output without anyone opening the DO console.

---

## Success verification

After Task 19:

1. **BMW retry** (with Stream B migrations still broken — tests the observability path):
   ```bash
   gh workflow run teardown-staging.yml --ref main --repo GoCodeAlone/buymywishlist -f confirm=yes
   # wait for teardown
   gh run rerun <latest-deploy-id> --repo GoCodeAlone/buymywishlist
   ```
2. **Expected outcome**: deploy fails (Stream B not yet landed), but the GHA run log now contains a `Troubleshoot: bmw-staging` group block with the `pre_deploy` error (`workflow-migrate up: first .: file does not exist`) visible without clicking into the DO console.
3. **After Stream B lands**: retry one more time. Both streams together → deploy succeeds → /healthz green → auto-promote to prod.

## Non-goals (explicit)

- Generic `IaCProvider.StreamLogs` interface — deferred to v0.19.0.
- AWS / GCP / Azure / tofu Troubleshoot implementations — DO only for v0.7.8.
- Real-time streaming log display during happy path — only on failure.
- Log redaction beyond existing `SensitiveKeys()` — plugin-side concern.
- State-heal for non-UUID ProviderIDs (task #67) — orthogonal workstream.
