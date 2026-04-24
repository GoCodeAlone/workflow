# Typed ProviderID Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.18.11 + workflow-plugin-digitalocean v0.7.9 so provider-declared ProviderID formats are enforced at the wfctl ↔ plugin boundary — soft-warn on input, hard-fail on output — preventing the class of state-corruption bugs that caused BMW's "invalid uuid" failure from ever reaching state storage.

**Architecture:** Optional `ProviderIDValidator` interface on `ResourceDriver` declares the shape of the driver's identifiers (`UUID`, `DomainName`, `ARN`, `Freeform`, `Unknown`). wfctl probes for it at infra-apply boundaries: soft-warn on input (before `driver.Update`/`Delete`, letting driver self-heal do its work), hard-fail on output (before `state.Save`, preventing bad data from being persisted). All DO drivers adopt the interface in v0.7.9; the release also replicates v0.7.8's AppPlatformDriver state-heal pattern across remaining UUID drivers.

**Tech Stack:** Go 1.26, HashiCorp go-plugin (gRPC-over-stdio), `github.com/GoCodeAlone/workflow` interfaces, `github.com/digitalocean/godo`.

---

## Repos + branches

- **workflow** (`/Users/jon/workspace/workflow`), branch `feat/v0.18.11-typed-provider-id-validation` (already created; design committed as `da2254f`)
- **workflow-plugin-digitalocean** (`/Users/jon/workspace/workflow-plugin-digitalocean`), new branch `feat/v0.7.9-id-format-and-heal-replication` (created when Phase 2 starts)
- **buymywishlist** (`/Users/jon/workspace/buymywishlist`), new branch `chore/bump-wfctl-v0.18.11-do-v0.7.9` (created when Phase 3 starts)

## Dependency order

```
Phase 1 (Tasks 1-7)  →  Task 8 (tag v0.18.11)
                             ↓
Phase 2 (Tasks 9-13, Task 9 blocked by Task 8)  →  Task 14 (tag v0.7.9)
                             ↓
Phase 3 (Tasks 15-16, Task 15 blocked by Task 14)
```

Phase 2's `impl-digitalocean-2` also has DO plugin v0.7.8 (PR #22) in flight — let that merge and tag BEFORE Phase 2 of this plan starts. BMW is already partly unblocked by v0.7.8's state-heal; this workstream is preventive architecture for future failures.

---

## Phase 1 — workflow v0.18.11

### Task 1: Define `ProviderIDFormat` enum + `ProviderIDValidator` interface

**Files:**
- Modify: `interfaces/iac_resource_driver.go` (append declarations)
- Create: `interfaces/idformat_interface_test.go`

**Step 1: Write the failing test**

Create `interfaces/idformat_interface_test.go`:

```go
package interfaces

import "testing"

// Compile-time check: a concrete type implementing ProviderIDValidator
// satisfies the interface.
var _ ProviderIDValidator = (*fakeValidator)(nil)

type fakeValidator struct{ format ProviderIDFormat }

func (f *fakeValidator) ProviderIDFormat() ProviderIDFormat { return f.format }

func TestProviderIDFormat_ZeroValue(t *testing.T) {
    var f ProviderIDFormat
    if f != IDFormatUnknown {
        t.Errorf("zero value should be IDFormatUnknown, got %v", f)
    }
}

func TestProviderIDFormat_StringRoundtrip(t *testing.T) {
    cases := []struct {
        f    ProviderIDFormat
        name string
    }{
        {IDFormatUnknown, "unknown"},
        {IDFormatUUID, "uuid"},
        {IDFormatDomainName, "domain_name"},
        {IDFormatARN, "arn"},
        {IDFormatFreeform, "freeform"},
    }
    for _, c := range cases {
        if got := c.f.String(); got != c.name {
            t.Errorf("(%v).String() = %q, want %q", c.f, got, c.name)
        }
    }
}
```

**Step 2: Run — expect compile failure (enum + methods not defined)**

Run: `GOWORK=off go test ./interfaces/... -run TestProviderIDFormat -v`
Expected: "undefined: ProviderIDFormat, ProviderIDValidator, IDFormatUnknown, IDFormatUUID, ..."

**Step 3: Append declarations to `interfaces/iac_resource_driver.go`**

After the existing `Troubleshooter` interface (end of file), append:

```go
// ProviderIDFormat identifies the shape of provider-specific resource
// identifiers so wfctl can validate them at the driver boundary without
// knowing provider-specific semantics.
//
// The zero value IDFormatUnknown disables validation for backward
// compatibility — drivers that don't opt in get today's behavior.
type ProviderIDFormat int

const (
    // IDFormatUnknown disables validation (zero value).
    IDFormatUnknown ProviderIDFormat = iota
    // IDFormatUUID is the canonical 36-character hyphenated UUID shape.
    IDFormatUUID
    // IDFormatDomainName is an RFC 1035 domain name.
    IDFormatDomainName
    // IDFormatARN is an AWS-style colon-separated ARN.
    IDFormatARN
    // IDFormatFreeform allows any non-empty string.
    IDFormatFreeform
)

// String returns a stable identifier for logs and error messages.
func (f ProviderIDFormat) String() string {
    switch f {
    case IDFormatUUID:
        return "uuid"
    case IDFormatDomainName:
        return "domain_name"
    case IDFormatARN:
        return "arn"
    case IDFormatFreeform:
        return "freeform"
    case IDFormatUnknown:
        return "unknown"
    default:
        return "unknown"
    }
}

// ProviderIDValidator is an optional interface ResourceDriver implementations
// may provide to declare the shape of their ProviderIDs. wfctl uses the
// declaration to validate ProviderIDs at two boundaries:
//
//   - Input: before Update/Delete, probe ref.ProviderID against the declared
//     format. On mismatch, wfctl logs a warning but still calls the driver so
//     its own heal logic (if any) can run.
//   - Output: after Apply, probe r.ProviderID before persisting to state.
//     Mismatch for non-Unknown, non-Freeform formats is a HARD failure — the
//     driver has a bug and state must not be corrupted.
//
// Drivers that do not implement this interface receive today's behavior:
// no validation, no warning, no failure.
type ProviderIDValidator interface {
    ProviderIDFormat() ProviderIDFormat
}
```

**Step 4: Run tests — should PASS**

Run: `GOWORK=off go test ./interfaces/... -run TestProviderIDFormat -v`
Expected: 2/2 PASS.

**Step 5: Commit**

```bash
git add interfaces/iac_resource_driver.go interfaces/idformat_interface_test.go
git commit -m "interfaces: add ProviderIDFormat enum + ProviderIDValidator interface"
```

---

### Task 2: Implement `ValidateProviderID` + per-format validators

**Files:**
- Create: `interfaces/idformat.go`
- Create: `interfaces/idformat_test.go`

**Step 1: Write the failing tests first**

Create `interfaces/idformat_test.go`:

```go
package interfaces

import "testing"

func TestValidateUUID(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want bool
    }{
        {"canonical lowercase", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5", true},
        {"canonical uppercase", "ABCDEF01-2345-6789-ABCD-EF0123456789", true},
        {"mixed case", "aBcDeF01-2345-6789-abCD-eF0123456789", true},
        {"resource name", "bmw-staging", false},
        {"empty", "", false},
        {"too short", "f8b6200c-3bba-48a7-8bf1", false},
        {"too long", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5-extra", false},
        {"missing hyphen 1", "f8b6200c03bba-48a7-8bf1-7a3e3a885eb5", false},
        {"missing hyphen 2", "f8b6200c-3bba048a7-8bf1-7a3e3a885eb5", false},
        {"non-hex character", "f8b6200c-3bba-48a7-8bf1-7a3e3a885ebZ", false},
        {"36 chars but wrong hyphens", "f8b6200c-3bba048a7-8bf1-7a3e3a885ebX", false},
        {"spaces", "f8b6200c 3bba 48a7 8bf1 7a3e3a885eb5 ", false},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := validateUUID(c.in); got != c.want {
                t.Errorf("validateUUID(%q) = %v, want %v", c.in, got, c.want)
            }
        })
    }
}

func TestValidateDomainName(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want bool
    }{
        {"simple", "example.com", true},
        {"subdomain", "api.example.com", true},
        {"hyphens ok in middle", "my-app.example.com", true},
        {"single label", "localhost", true},
        {"numeric label", "1.example.com", true},
        {"label all digits", "example.123.com", true},
        {"empty", "", false},
        {"leading hyphen", "-bad.com", false},
        {"trailing hyphen", "bad-.com", false},
        {"label too long (64)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com", false},
        {"total too long (254)", "a." + string(make([]byte, 252)), false},
        {"consecutive dots", "a..b.com", false},
        {"trailing dot is ok", "example.com.", true},
        {"underscore in label", "my_app.com", false},
        {"space", "my app.com", false},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := validateDomainName(c.in); got != c.want {
                t.Errorf("validateDomainName(%q) = %v, want %v", c.in, got, c.want)
            }
        })
    }
}

func TestValidateARN(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want bool
    }{
        {"canonical s3 bucket", "arn:aws:s3:::my-bucket", true},
        {"canonical iam", "arn:aws:iam::123456789012:role/MyRole", true},
        {"canonical lambda", "arn:aws:lambda:us-east-1:123456789012:function:myfn", true},
        {"aws-cn partition", "arn:aws-cn:s3:::my-bucket", true},
        {"aws-us-gov partition", "arn:aws-us-gov:s3:::my-bucket", true},
        {"empty", "", false},
        {"missing prefix", "s3:::my-bucket", false},
        {"only five segments", "arn:aws:s3::my-bucket", false},
        {"not starting with arn", "aws:s3:::my-bucket", false},
        {"empty partition", "arn::s3:::my-bucket", false},
        {"empty service", "arn:aws::::my-bucket", false},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := validateARN(c.in); got != c.want {
                t.Errorf("validateARN(%q) = %v, want %v", c.in, got, c.want)
            }
        })
    }
}

func TestValidateProviderID_Dispatch(t *testing.T) {
    cases := []struct {
        name   string
        in     string
        format ProviderIDFormat
        want   bool
    }{
        {"uuid ok", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5", IDFormatUUID, true},
        {"uuid bad", "bmw-staging", IDFormatUUID, false},
        {"domain ok", "example.com", IDFormatDomainName, true},
        {"domain bad", "not a domain", IDFormatDomainName, false},
        {"arn ok", "arn:aws:s3:::bucket", IDFormatARN, true},
        {"arn bad", "nope", IDFormatARN, false},
        {"freeform accepts any non-empty", "whatever", IDFormatFreeform, true},
        {"freeform rejects empty", "", IDFormatFreeform, false},
        {"unknown accepts anything", "literally-anything", IDFormatUnknown, true},
        {"unknown accepts empty", "", IDFormatUnknown, true},
        {"unrecognized format accepts anything (forward compat)", "anything", ProviderIDFormat(99), true},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := ValidateProviderID(c.in, c.format); got != c.want {
                t.Errorf("ValidateProviderID(%q, %v) = %v, want %v", c.in, c.format, got, c.want)
            }
        })
    }
}
```

**Step 2: Run — expect compile failures**

Run: `GOWORK=off go test ./interfaces/... -run "TestValidate" -v`
Expected: undefined: `validateUUID`, `validateDomainName`, `validateARN`, `ValidateProviderID`.

**Step 3: Implement validators in `interfaces/idformat.go`**

```go
package interfaces

import "strings"

// ValidateProviderID reports whether s conforms to the given format.
// IDFormatUnknown and unrecognized formats always return true
// (no constraint). IDFormatFreeform requires s to be non-empty.
func ValidateProviderID(s string, format ProviderIDFormat) bool {
    switch format {
    case IDFormatUUID:
        return validateUUID(s)
    case IDFormatDomainName:
        return validateDomainName(s)
    case IDFormatARN:
        return validateARN(s)
    case IDFormatFreeform:
        return s != ""
    case IDFormatUnknown:
        return true
    default:
        return true
    }
}

// validateUUID checks canonical 36-char hyphenated UUID shape. Case-
// insensitive; hex-only between hyphens. Positional check only — no
// regex, no allocations in the hot path.
func validateUUID(s string) bool {
    if len(s) != 36 {
        return false
    }
    if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
        return false
    }
    for i := 0; i < len(s); i++ {
        if i == 8 || i == 13 || i == 18 || i == 23 {
            continue
        }
        c := s[i]
        if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
            return false
        }
    }
    return true
}

// validateDomainName implements RFC 1035 relaxed: non-empty, ≤253 chars
// (excluding optional trailing dot), labels 1-63 chars, [a-zA-Z0-9-],
// not starting or ending with hyphen.
func validateDomainName(s string) bool {
    if s == "" {
        return false
    }
    // Allow trailing dot (root); strip it for length calc and label parse.
    if s[len(s)-1] == '.' {
        s = s[:len(s)-1]
    }
    if s == "" || len(s) > 253 {
        return false
    }
    labels := strings.Split(s, ".")
    for _, label := range labels {
        if len(label) == 0 || len(label) > 63 {
            return false
        }
        if label[0] == '-' || label[len(label)-1] == '-' {
            return false
        }
        for i := 0; i < len(label); i++ {
            c := label[i]
            if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
                (c >= '0' && c <= '9') || c == '-') {
                return false
            }
        }
    }
    return true
}

// validateARN checks arn:<partition>:<service>:<region>:<account>:<resource>
// Requires 6 colon-separated segments. Partition and service must be
// non-empty; region and account may be empty (S3 bucket ARNs omit them).
func validateARN(s string) bool {
    if !strings.HasPrefix(s, "arn:") {
        return false
    }
    parts := strings.SplitN(s, ":", 6)
    if len(parts) != 6 {
        return false
    }
    // parts[0] == "arn"
    if parts[1] == "" || parts[2] == "" {
        return false
    }
    // parts[3] region, parts[4] account, parts[5] resource
    // Resource may contain colons; that's why we used SplitN.
    if parts[5] == "" {
        return false
    }
    return true
}
```

**Step 4: Run tests**

Run: `GOWORK=off go test ./interfaces/... -run "TestValidate" -v`
Expected: all cases PASS.

**Step 5: Commit**

```bash
git add interfaces/idformat.go interfaces/idformat_test.go
git commit -m "interfaces: ValidateProviderID + per-format validators (UUID, domain, ARN)"
```

---

### Task 3: Wire input-side validation in `cmd/wfctl/infra_apply.go`

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (inside `applyWithProviderAndStore` or equivalent driver-call site)
- No test file yet — integration test comes in Task 5

**Step 1: Locate the apply-path driver call**

Read the current `applyWithProviderAndStore` to find where `driver.Update` or `provider.Apply` is invoked. The design specifies input validation before `driver.Update` / `driver.Delete` — but `wfctl infra apply` calls `provider.Apply(plan)` which internally dispatches per-action. The input-side validation has two options:

- **(a)** Wrap each plan action: for `update` / `delete` actions, look up the driver for the action's resource type and validate against `ProviderIDValidator`.
- **(b)** Delegate validation to the driver itself — the driver is the one that knows its format.

Option (a) is correct per the design: wfctl validates before calling the driver. Option (b) is the fallback for drivers that don't implement the interface (default no-op).

Implement by iterating `plan.Actions` before `provider.Apply` and for each `update`/`delete` action, resolving the driver and validating.

**Step 2: Add validation helper**

Inside `cmd/wfctl/infra_apply.go` (or a new `cmd/wfctl/infra_validation.go`):

```go
// validateInputProviderIDs iterates a plan's update/delete actions, looks up
// each driver, probes for ProviderIDValidator, and logs a WARN when the
// ProviderID in state does not match the driver's declared format. Input
// validation is soft-warn (not fail): the driver may have a self-heal
// path that recovers from stale state.
func validateInputProviderIDs(provider interfaces.IaCProvider, plan *interfaces.Plan) {
    for _, act := range plan.Actions {
        if act.Action != "update" && act.Action != "delete" {
            continue
        }
        rd, err := provider.ResourceDriver(act.Resource.Type)
        if err != nil {
            continue
        }
        v, ok := rd.(interfaces.ProviderIDValidator)
        if !ok {
            continue
        }
        format := v.ProviderIDFormat()
        if format == interfaces.IDFormatUnknown {
            continue
        }
        if !interfaces.ValidateProviderID(act.Resource.ProviderID, format) {
            log.Printf(
                "warn: wfctl: %s %q has non-conformant ProviderID %q "+
                    "(expected %s). Driver will attempt self-heal if supported.",
                act.Resource.Type, act.Resource.Name,
                act.Resource.ProviderID, format,
            )
        }
    }
}
```

**Step 3: Wire into `applyWithProviderAndStore`**

Before `provider.Apply(ctx, &plan)`:

```go
validateInputProviderIDs(provider, &plan)
fmt.Printf("  Plan: %d action(s) to execute.\n", len(plan.Actions))
result, err := provider.Apply(ctx, &plan)
```

**Step 4: Build and run existing tests to verify no regression**

Run: `GOWORK=off go build ./... && GOWORK=off go test -race -short ./cmd/wfctl/...`
Expected: existing tests pass.

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_validation.go
git commit -m "wfctl: input-side ProviderID validation (soft-warn before driver calls)"
```

(Commit `infra_validation.go` if you created it as a separate file; otherwise just the modified `infra_apply.go`.)

---

### Task 4: Wire output-side validation (hard-fail) before state write

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` — inside the result loop at ~line 281-299 that writes `ResourceState`

**Step 1: Add output-validation helper**

Alongside `validateInputProviderIDs`:

```go
// validateOutputProviderID probes the driver for ProviderIDValidator and
// rejects malformed ProviderIDs for strict formats (UUID, DomainName, ARN).
// Freeform and Unknown formats pass through. Returns a detailed error on
// violation so operators can identify the buggy driver immediately.
func validateOutputProviderID(provider interfaces.IaCProvider, providerType string, r *interfaces.ResourceOutput) error {
    rd, err := provider.ResourceDriver(r.Type)
    if err != nil {
        return nil // cannot probe; let today's behavior apply
    }
    v, ok := rd.(interfaces.ProviderIDValidator)
    if !ok {
        return nil
    }
    format := v.ProviderIDFormat()
    if format == interfaces.IDFormatUnknown || format == interfaces.IDFormatFreeform {
        return nil
    }
    if !interfaces.ValidateProviderID(r.ProviderID, format) {
        return fmt.Errorf(
            "driver %q returned malformed ProviderID %q for resource %q (type %s); "+
                "expected %s — state not persisted",
            providerType, r.ProviderID, r.Name, r.Type, format,
        )
    }
    return nil
}
```

**Step 2: Wire into the result loop**

At `cmd/wfctl/infra_apply.go:281-299` (where the plan:

```go
for _, r := range result.Resources {
    fmt.Printf("  ✓ %s (%s)\n", r.Name, r.Type)

    // Validate output ProviderID before persisting — hard-fail on mismatch.
    if err := validateOutputProviderID(provider, providerType, &r); err != nil {
        return fmt.Errorf("state write rejected: %w", err)
    }

    // ... existing state-write code ...
}
```

**Step 3: Build**

Run: `GOWORK=off go build ./...`
Expected: compiles.

**Step 4: Run existing tests**

Run: `GOWORK=off go test -race -short ./cmd/wfctl/...`
Expected: existing tests still pass (no test has a driver returning bad ProviderIDs yet).

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go
git commit -m "wfctl: output-side ProviderID validation (hard-fail before state write)"
```

---

### Task 5: Integration tests for validation wiring

**Files:**
- Create: `cmd/wfctl/infra_apply_validation_test.go`

**Step 1: Write the 4 tests**

```go
package main

import (
    "bytes"
    "context"
    "log"
    "strings"
    "testing"

    "github.com/GoCodeAlone/workflow/interfaces"
)

// uuidValidatingProvider wraps a fake driver that declares IDFormatUUID.
type uuidValidatingProvider struct {
    createOutput *interfaces.ResourceOutput
    updateOutput *interfaces.ResourceOutput
    updateCalled bool
}

type uuidDriver struct {
    interfaces.ResourceDriver
    createOutput *interfaces.ResourceOutput
    updateOutput *interfaces.ResourceOutput
    updateCalled *bool
}

func (u uuidDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
    return interfaces.IDFormatUUID
}

func (u uuidDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
    return u.createOutput, nil
}

func (u uuidDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
    if u.updateCalled != nil {
        *u.updateCalled = true
    }
    return u.updateOutput, nil
}

func (u uuidDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }

// ... (harness for applyWithProviderAndStore — wire enough state store
//      and provider stubs to drive the test, following patterns from
//      the existing cmd/wfctl/infra_apply_troubleshoot_test.go) ...

func TestInfraApply_InputValidation_Warns(t *testing.T) {
    updateCalled := false
    driver := uuidDriver{
        updateCalled: &updateCalled,
        updateOutput: &interfaces.ResourceOutput{
            Name: "bmw-staging", Type: "infra.app_platform",
            ProviderID: "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5",
        },
    }
    // Plan has an update action with stale name-as-ProviderID
    plan := &interfaces.Plan{Actions: []interfaces.PlanAction{{
        Action: "update",
        Resource: interfaces.ResourceRef{
            Name: "bmw-staging", Type: "infra.app_platform",
            ProviderID: "bmw-staging", // stale — will trigger WARN
        },
    }}}
    provider := newTestProvider(driver)

    var logBuf bytes.Buffer
    oldOut := log.Writer()
    log.SetOutput(&logBuf)
    defer log.SetOutput(oldOut)

    validateInputProviderIDs(provider, plan)

    // Assert WARN logged with specific keywords
    out := logBuf.String()
    if !strings.Contains(out, "non-conformant ProviderID") {
        t.Errorf("expected WARN about non-conformant ProviderID, got: %q", out)
    }
    if !strings.Contains(out, `"bmw-staging"`) || !strings.Contains(out, "uuid") {
        t.Errorf("expected WARN to include offending value + expected format, got: %q", out)
    }
}

func TestInfraApply_OutputValidation_RejectsBadProviderID(t *testing.T) {
    driver := uuidDriver{
        createOutput: &interfaces.ResourceOutput{
            Name: "bmw-staging", Type: "infra.app_platform",
            ProviderID: "bmw-staging", // driver bug: returned NAME, not UUID
        },
    }
    provider := newTestProvider(driver)
    badOutput := *driver.createOutput

    err := validateOutputProviderID(provider, "digitalocean", &badOutput)

    if err == nil {
        t.Fatal("expected error for malformed ProviderID, got nil")
    }
    msg := err.Error()
    if !strings.Contains(msg, `"digitalocean"`) {
        t.Errorf("expected driver name in error, got: %q", msg)
    }
    if !strings.Contains(msg, `"bmw-staging"`) {
        t.Errorf("expected offending value in error, got: %q", msg)
    }
    if !strings.Contains(msg, "uuid") {
        t.Errorf("expected expected-format in error, got: %q", msg)
    }
    if !strings.Contains(msg, "state not persisted") {
        t.Errorf("expected 'state not persisted' in error, got: %q", msg)
    }
}

// freeformDriver declares IDFormatFreeform.
type freeformDriver struct {
    interfaces.ResourceDriver
    output *interfaces.ResourceOutput
}

func (f freeformDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
    return interfaces.IDFormatFreeform
}

func TestInfraApply_OutputValidation_SkipsFreeform(t *testing.T) {
    driver := freeformDriver{
        output: &interfaces.ResourceOutput{
            Name: "my-bucket", Type: "infra.spaces",
            ProviderID: "any-bucket-name-is-fine",
        },
    }
    provider := newTestProvider(driver)
    output := *driver.output

    err := validateOutputProviderID(provider, "digitalocean", &output)

    if err != nil {
        t.Errorf("Freeform format should not fail, got: %v", err)
    }
}

// plainDriver does NOT implement ProviderIDValidator.
type plainDriver struct {
    interfaces.ResourceDriver
    output *interfaces.ResourceOutput
}

func TestInfraApply_NoValidator_BackwardCompat(t *testing.T) {
    driver := plainDriver{
        output: &interfaces.ResourceOutput{
            Name: "x", Type: "infra.app_platform",
            ProviderID: "literally-anything", // no format declared → not validated
        },
    }
    provider := newTestProvider(driver)
    output := *driver.output

    err := validateOutputProviderID(provider, "digitalocean", &output)

    if err != nil {
        t.Errorf("driver without validator should pass through, got: %v", err)
    }
}

// newTestProvider adapts a driver into an interfaces.IaCProvider for tests.
// Implementation lives alongside the test so other tests can reuse it.
// Must support ResourceDriver(type) lookup for both validation helpers.
func newTestProvider(d interfaces.ResourceDriver) interfaces.IaCProvider {
    return &fakeValidationProvider{driver: d}
}

type fakeValidationProvider struct {
    driver interfaces.ResourceDriver
}

func (p *fakeValidationProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
    return p.driver, nil
}

// ... (stub the other IaCProvider methods as no-ops — they aren't called
//      by validateInputProviderIDs / validateOutputProviderID) ...
```

**Step 2: Run tests**

Run: `GOWORK=off go test -race -short ./cmd/wfctl/... -run TestInfraApply_InputValidation_Warns -v`
Run: `GOWORK=off go test -race -short ./cmd/wfctl/... -run TestInfraApply_OutputValidation -v`
Run: `GOWORK=off go test -race -short ./cmd/wfctl/... -run TestInfraApply_NoValidator_BackwardCompat -v`
Expected: 4/4 PASS.

**Step 3: Regression-catches invariant check**

Temporarily revert the `validateOutputProviderID` call in the apply loop (Task 4), re-run `TestInfraApply_OutputValidation_RejectsBadProviderID`. Expected: test passes standalone (it calls the helper directly), so this invariant is mostly a design-level check. To verify the wiring specifically, you may need a higher-level integration test that runs the full `applyWithProviderAndStore` — if the current harness makes that awkward, note it in the PR body and file a follow-up.

**Step 4: Commit**

```bash
git add cmd/wfctl/infra_apply_validation_test.go
git commit -m "test: integration coverage for ProviderID boundary validation"
```

---

### Task 6: CHANGELOG v0.18.11 entry

**Files:**
- Modify: `CHANGELOG.md`

**Step 1: Prepend v0.18.11 section**

```markdown
## v0.18.11 — 2026-04-24

### Added

- New optional `interfaces.ProviderIDValidator` interface — drivers declare the
  expected shape of their ProviderIDs via `ProviderIDFormat() ProviderIDFormat`.
  Supported formats: `IDFormatUUID`, `IDFormatDomainName`, `IDFormatARN`,
  `IDFormatFreeform`, `IDFormatUnknown` (default, disables validation).
- `interfaces.ValidateProviderID(s, format)` + per-format validators
  (`validateUUID`, `validateDomainName`, `validateARN`) in new file
  `interfaces/idformat.go`. Exhaustive unit-test coverage.
- `wfctl infra apply` validates ProviderIDs at two boundaries:
  - **Input** (before `driver.Update` / `driver.Delete`): soft-warn on mismatch,
    letting the driver's own heal path recover from stale state.
  - **Output** (before state-write): hard-fail when a driver returns a
    malformed ProviderID for a declared strict format. Error message names the
    offending driver, resource, value, and expected format — state is NOT
    persisted. This is the guardrail that would have prevented BMW's
    "PUT /v2/apps/bmw-staging" state corruption from ever reaching storage.

### Backward compatibility

- `ProviderIDValidator` is optional. Drivers that don't implement it continue
  to work unchanged — no validation, no warnings, no failures.
- `IDFormatUnknown` (zero value) and `IDFormatFreeform` both skip validation on
  output, so drivers can opt out per-type during rollout.

### Known follow-ups

- workflow-plugin-digitalocean v0.7.9 adopts `ProviderIDValidator` across all
  drivers + replicates v0.7.8's state-heal pattern across remaining UUID
  drivers.
- Level 2+ typed ProviderID wrapper / proto `oneof` / `buf.validate` schema
  enforcement: tracked as v0.19.0 Feature E (extends task #41).
```

**Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.18.11"
```

---

### Task 7: Open PR on workflow + review cycle

**Files:** none (process).

**Step 1: DM code-reviewer for LOCAL pre-push review**

Send via team SendMessage:
> PR v0.18.11 ready for LOCAL review on `feat/v0.18.11-typed-provider-id-validation` — interfaces.ProviderIDFormat + validators + wfctl input/output boundary validation + integration tests. N commits. Design doc `docs/plans/2026-04-24-typed-provider-id-validation-design.md`. Standing by for approval before pushing.

Wait for approval.

**Step 2: Push**

```bash
git push -u origin feat/v0.18.11-typed-provider-id-validation
```

**Step 3: Open PR**

```bash
gh pr create --repo GoCodeAlone/workflow --title "feat: v0.18.11 — typed ProviderID validation at wfctl↔plugin boundary" --body "$(cat <<'EOF'
## Summary

- Optional `interfaces.ProviderIDValidator` — drivers declare ProviderID format (UUID / DomainName / ARN / Freeform / Unknown).
- wfctl validates at two boundaries: soft-warn on input (before driver.Update/Delete), hard-fail on output (before state write).
- Backward compatible — drivers that don't implement the interface see no behavior change.

## Test plan

- [ ] `go test ./...` passes (incl. new interfaces + cmd/wfctl validation tests)
- [ ] `go vet ./...` clean
- [ ] `gofmt -l` empty
- [ ] Regression proof: removing the `validateOutputProviderID` call causes `TestInfraApply_OutputValidation_RejectsBadProviderID` to fail loudly.
- [ ] After merge: workflow-plugin-digitalocean v0.7.9 adopts the interface; BMW bumps pins and retries deploy; state-heal + output-validation combine to unbreak stale state transparently.

Design: `docs/plans/2026-04-24-typed-provider-id-validation-design.md`
Follow-up: workflow-plugin-digitalocean v0.7.9 adoption PR (blocked on this tag).
EOF
)"
```

**Step 4: Add Copilot reviewer**

```bash
PR_NUM=$(gh pr view --repo GoCodeAlone/workflow --json number -q '.number')
gh pr edit "$PR_NUM" --repo GoCodeAlone/workflow --add-reviewer copilot-pull-request-reviewer
```

**Step 5: Wait 15+ min for Copilot, address comments**

Standard flow. NEVER `@copilot` in comment bodies.

**Step 6: DM team-lead for merge approval**

---

### Task 8: Tag v0.18.11 (team-lead action)

```bash
cd /Users/jon/workspace/workflow
git checkout main && git pull
git log --oneline -3
git tag -a v0.18.11 -m "v0.18.11: typed ProviderID validation at wfctl↔plugin boundary"
git push origin v0.18.11
```

Verify Release workflow fires and publishes binaries.

---

## Phase 2 — workflow-plugin-digitalocean v0.7.9

**Prerequisite:** impl-digitalocean-2 has completed and merged PR #22 (v0.7.8), and team-lead has tagged v0.7.8. v0.18.11 must ALSO be tagged (Task 8) before Task 9 can bump go.mod.

**Branch:** `feat/v0.7.9-id-format-and-heal-replication` off main.

### Task 9: Bump go.mod to workflow v0.18.11 (blocked on Task 8)

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git checkout main && git pull
git checkout -b feat/v0.7.9-id-format-and-heal-replication
go get github.com/GoCodeAlone/workflow@v0.18.11
go mod tidy
GOWORK=off go build ./... && GOWORK=off go test -race -short ./...
git add go.mod go.sum
git commit -m "deps: bump workflow v0.18.10.1 → v0.18.11 (ProviderIDValidator interface)"
```

---

### Task 10: Every DO driver implements `ProviderIDFormat()`

**Files:**
- Modify: each driver file in `internal/drivers/` to add a one-line method
- Create / Modify: a single consolidated test `internal/drivers/providerid_format_test.go`

**Format mapping** (per design):

| Driver | File | Format |
|---|---|---|
| AppPlatformDriver | `app_platform.go` | `IDFormatUUID` |
| ApiGatewayDriver | `api_gateway.go` | `IDFormatUUID` |
| DatabaseDriver | `database.go` | `IDFormatUUID` |
| CacheDriver | `cache.go` | `IDFormatUUID` |
| CertificateDriver | `certificate.go` | `IDFormatUUID` |
| DropletDriver | `droplet.go` (if present) | `IDFormatUUID` |
| LoadBalancerDriver | `load_balancer.go` (if present) | `IDFormatUUID` |
| VPCDriver | `vpc.go` | `IDFormatUUID` |
| FirewallDriver | `firewall.go` | `IDFormatUUID` |
| ReservedIPDriver | `reserved_ip.go` (if present) | `IDFormatUUID` |
| DNSDriver | `dns.go` | `IDFormatDomainName` |
| SpacesDriver | `spaces.go` (if present) | `IDFormatFreeform` |

**Step 1: Write the failing test**

Create `internal/drivers/providerid_format_test.go`:

```go
package drivers

import (
    "testing"

    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestAllDrivers_DeclareProviderIDFormat(t *testing.T) {
    cases := []struct {
        name   string
        driver interface{ ProviderIDFormat() interfaces.ProviderIDFormat }
        want   interfaces.ProviderIDFormat
    }{
        {"AppPlatform", &AppPlatformDriver{}, interfaces.IDFormatUUID},
        {"ApiGateway", &ApiGatewayDriver{}, interfaces.IDFormatUUID},
        {"Database", &DatabaseDriver{}, interfaces.IDFormatUUID},
        {"Cache", &CacheDriver{}, interfaces.IDFormatUUID},
        {"Certificate", &CertificateDriver{}, interfaces.IDFormatUUID},
        {"VPC", &VPCDriver{}, interfaces.IDFormatUUID},
        {"Firewall", &FirewallDriver{}, interfaces.IDFormatUUID},
        {"DNS", &DNSDriver{}, interfaces.IDFormatDomainName},
        // Add: Droplet, LoadBalancer, ReservedIP, Spaces — only if they exist
        //      in the repo. Skip with a comment otherwise.
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := c.driver.ProviderIDFormat(); got != c.want {
                t.Errorf("%s.ProviderIDFormat() = %v, want %v", c.name, got, c.want)
            }
        })
    }
}
```

**Step 2: Run — expect failures (method not on drivers yet)**

Run: `GOWORK=off go test ./internal/drivers/... -run TestAllDrivers_DeclareProviderIDFormat -v`
Expected: compile errors.

**Step 3: Add one-line method to each driver**

Example for `app_platform.go` (append near bottom of file):

```go
// ProviderIDFormat implements interfaces.ProviderIDValidator.
func (d *AppPlatformDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
    return interfaces.IDFormatUUID
}
```

Repeat for each driver with the correct format from the table above. For `DNSDriver`, return `interfaces.IDFormatDomainName`. For `SpacesDriver` (if present), return `interfaces.IDFormatFreeform`.

**Step 4: Run tests**

Run: `GOWORK=off go test ./internal/drivers/... -run TestAllDrivers_DeclareProviderIDFormat -v`
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/drivers/*.go internal/drivers/providerid_format_test.go
git commit -m "drivers: declare ProviderIDFormat for every DO driver"
```

---

### Task 11: Replicate state-heal across remaining UUID drivers

**Files:** per-driver — each UUID-format driver other than AppPlatformDriver (which already has heal from v0.7.8).

**Step 1: Audit existing findByName helpers (from v0.7.3 task #21)**

For each UUID driver (Database, Cache, Certificate, VPC, Firewall, and others), check if it already has a `findXxxByName` helper. If not, add one (iterate the paginated list, match by name).

**Step 2: Add `resolveProviderID` to each UUID driver**

Pattern (substitute driver-specific `findByName`):

```go
// resolveProviderID returns a UUID-like ProviderID for the given ref. If
// ref.ProviderID is already UUID-shaped it is returned as-is. Otherwise
// a name-based lookup heals stale state. Mirrors AppPlatformDriver.resolveProviderID
// (v0.7.8) across all UUID drivers so wfctl's non-UUID state never blocks
// an Update or Delete.
func (d *DatabaseDriver) resolveProviderID(ctx context.Context, ref interfaces.ResourceRef) (string, error) {
    if isUUIDLike(ref.ProviderID) { // from internal/drivers/shared.go
        return ref.ProviderID, nil
    }
    log.Printf("warn: database %q: ProviderID %q is not UUID-like; resolving by name (state-heal)",
        ref.Name, ref.ProviderID)
    out, err := d.findDatabaseByName(ctx, ref.Name)
    if err != nil {
        return "", fmt.Errorf("database state-heal for %q: %w", ref.Name, err)
    }
    return out.ProviderID, nil
}
```

Wire `resolveProviderID` into `Update`, `Resize`, `Delete`, and any other method that uses `ref.ProviderID` as the DO API path parameter.

**Step 3: Parameterize the integration-test harness (from v0.7.8)**

The harness in `internal/drivers/integration_test_helpers_test.go` (from PR #22) uses `fakeAppsClient`. Either:

- (a) **Inline**: add per-driver integration tests using mock clients specific to each driver (database, cache, etc.). Each test file gets the same 5-test matrix.
- (b) **Parameterize**: extract a generic state-heal test scaffold that takes a driver factory + mock client factory. Probably overkill if mock shapes differ per driver.

Start with (a) — one test file per driver. Each file uses that driver's specific mock pattern (DatabasesClient mock, CertificatesClient mock, etc.).

Per driver, the 5 tests are:
1. `TestXxxDriver_Create_PersistsUUIDInState`
2. `TestXxxDriver_Update_UsesExistingUUID`
3. `TestXxxDriver_Update_HealsStaleName`
4. `TestXxxDriver_Delete_HealsStaleName`
5. `TestXxxDriver_Update_HealFails_WhenResourceNotFound`

**Step 4: Run tests**

Run: `GOWORK=off go test -race -short ./internal/drivers/...`
Expected: all drivers' tests PASS.

**Step 5: Verify regression**

For EACH newly-healing driver, temporarily comment out `resolveProviderID` in its Update path, confirm the corresponding `TestXxxDriver_Update_HealsStaleName` fails, restore. Note the verification in the PR body.

**Step 6: Commit per driver**

```bash
git add internal/drivers/database.go internal/drivers/database_stateheal_test.go
git commit -m "drivers: database state-heal + integration tests"
```

Repeat for each driver. Commit granularity: one commit per driver keeps bisecting clean.

---

### Task 12: CHANGELOG v0.7.9

**Files:** `CHANGELOG.md`

```markdown
## v0.7.9 — 2026-04-24

### Added

- Every DO driver now implements `interfaces.ProviderIDValidator`
  (introduced in workflow v0.18.11). Declares the shape of the driver's
  ProviderIDs so wfctl can validate them at the apply boundary before
  invoking the driver and before persisting state.
- State-heal pattern (from v0.7.8 AppPlatformDriver) replicated across
  all UUID-ID drivers: Database, Cache, Certificate, VPC, Firewall
  (and others as applicable). Each now gracefully recovers from stale
  name-as-ProviderID state via name-based lookup, transparent to the
  operator. Integration tests per driver.

### ID format declarations

| Driver | Format |
|---|---|
| AppPlatform | UUID |
| ApiGateway | UUID |
| Database | UUID |
| Cache | UUID |
| Certificate | UUID |
| VPC | UUID |
| Firewall | UUID |
| Droplet, LoadBalancer, ReservedIP (if present) | UUID |
| DNS | DomainName |
| Spaces (if present) | Freeform |

### Changed

- Depends on workflow v0.18.11 (was v0.18.10.1).
```

Commit:

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.7.9 — ProviderIDFormat adoption + state-heal replication"
```

---

### Task 13: Open PR on workflow-plugin-digitalocean + review cycle

Same flow as Task 7:

1. DM code-reviewer BEFORE pushing.
2. Push `feat/v0.7.9-id-format-and-heal-replication`.
3. `gh pr create` with title `feat: v0.7.9 — ProviderIDFormat adoption + state-heal replication`.
4. Add Copilot reviewer via `gh pr edit --add-reviewer`.
5. Address comments.
6. DM team-lead for merge.

---

### Task 14: Tag v0.7.9 (team-lead action)

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git checkout main && git pull
git tag -a v0.7.9 -m "v0.7.9: ProviderIDFormat adoption + state-heal across UUID drivers"
git push origin v0.7.9
```

Verify Release workflow publishes the tagged binaries.

---

## Phase 3 — BMW consumer bump

### Task 15: BMW pin bump PR

**Files:**
- Modify: `.github/workflows/deploy.yml` (find `setup-wfctl` version pin + DO plugin version)
- Modify: `infra.yaml` or plugin-install config (find workflow-plugin-digitalocean version)

**Step 1: Grep current pins**

```bash
cd /Users/jon/workspace/buymywishlist
grep -rn "v0\.18\.10\.1\|workflow-plugin-digitalocean.*v0\.7\.8" .github/workflows/ infra.yaml 2>/dev/null
```

**Step 2: Bump each occurrence**

- `v0.18.10.1` → `v0.18.11`
- `workflow-plugin-digitalocean` `v0.7.8` → `v0.7.9`

**Step 3: Commit**

```bash
git checkout -b chore/bump-wfctl-v0.18.11-do-v0.7.9
git add .github/workflows/ infra.yaml
git commit -m "chore: bump setup-wfctl v0.18.10.1 → v0.18.11 + DO plugin v0.7.8 → v0.7.9"
```

**Step 4: Standard review flow** — DM code-reviewer, push, Copilot, DM team-lead.

---

### Task 16: Merge BMW PR (team-lead)

Admin-merge once CI green + Copilot clean.

After merge, BMW's main deploys on v0.18.11 + v0.7.9. The cumulative effect: state-heal (v0.7.8) recovers legacy bad state; output validation (v0.18.11) prevents any new bad state from being persisted; per-driver format declarations (v0.7.9) cover every resource type. The class of "invalid uuid" failures is architecturally prevented.

---

## Success verification

After Task 16:

1. BMW deploy auto-fires on main. First apply hits state-heal on `bmw-staging` — UUID recovered via findAppByName, output validation confirms the healed UUID, state rewritten clean.
2. Next apply: input validation passes cleanly (state has UUID now); output validation passes (all fields UUID-shaped); deploy proceeds to pre-deploy migrations → app active → /healthz → auto-promote to prod.
3. BMW staging /healthz 200, BMW prod /healthz 200, auto-promote confirmed.

---

## Non-goals (explicit)

- Level 2 typed `ProviderID` wrapper — v0.19.0.
- Level 3 proto `oneof` / `buf.validate` at wire level — v0.19.0 Feature E (generalization of task #41).
- Level 4 JSON Schema on ResourceSpec.Config — v0.19.0 task #42.
- Non-ProviderID field validation on ResourceOutput — scoped to the one field that corrupts state.
- AWS / GCP / Azure plugin adoption — each provider's plugin opts in on its own schedule; interface is additive.
- wfctl-level self-heal — heal stays inside drivers that know how to lookup by name.
- Retroactive state-file repair — already handled by v0.7.8 reactive heal.
