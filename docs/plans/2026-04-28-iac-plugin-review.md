# IaC Plugin Cross-Provider Review Discipline & Audit — Phase A Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Phase A of the IaC review discipline rollout — author the markdown checklist, build the executable test-helper package, extend the strict-contracts tracker for IaC plugins, and ship the generic plugin-pattern-reviewer skill. Phase B (P-1 BMW) auto-chains after Phase A merges.

**Architecture:** Four parallel deliverables in the workflow framework repo. D-1 + D-3 + D-4 are doc-shape (markdown). D-2 is a small Go package (`workflow/plugin/sdk/iaclint/`) with three matcher functions backed by unit tests. All four ship in a single PR off `design/iac-plugin-review` since they're cohesive and the design doc + plan are already on that branch.

**Tech Stack:** Go 1.26+ stdlib + `google.golang.org/protobuf/types/known/structpb` (already a workflow dep) for D-2; markdown for D-1/D-3/D-4.

**Working dir:** `/Users/jon/workspace/workflow/_worktrees/iac-plugin-review/` (branch `design/iac-plugin-review`, already exists at `6e159ee` with the design doc committed).

**Design reference:** `docs/plans/2026-04-28-iac-plugin-review-design.md` (committed at `6e159ee`).

---

## Task 1: D-2 package skeleton + ValidationKind enum

**Files:**
- Create: `plugin/sdk/iaclint/iaclint.go`
- Create: `plugin/sdk/iaclint/iaclint_test.go`

**Step 1: Write the failing test**

```go
package iaclint_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

func TestValidationKind_String(t *testing.T) {
	cases := map[iaclint.ValidationKind]string{
		iaclint.KindTCPPort:          "TCPPort",
		iaclint.KindNonNegativeInt:   "NonNegativeInt",
		iaclint.KindNonEmptyString:   "NonEmptyString",
		iaclint.KindStringEnum:       "StringEnum",
		iaclint.KindIntegerOnlyFloat: "IntegerOnlyFloat",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("kind %d: got %q, want %q", kind, got, want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/...`
Expected: FAIL with `package github.com/GoCodeAlone/workflow/plugin/sdk/iaclint is not in std`.

**Step 3: Write minimal implementation**

```go
// Package iaclint provides cross-provider review discipline as executable
// test helpers for IaC plugin authors. The bug-class taxonomy and rationale
// for each helper live in the project review checklist:
//
//	docs/IAC_PLUGIN_REVIEW_CHECKLIST.md
//
// IaC plugins import this package in their test suites so the bug classes
// surfaced during the workflow-plugin-digitalocean v0.8.0 review cycle are
// caught at CI time rather than during production gRPC dispatch or in code
// review.
package iaclint

// ValidationKind enumerates the standard {field, value-class} probes used by
// AssertValidationMatrix. Each kind exercises a battery of edge values that
// match the bug-class definitions in the project review checklist.
type ValidationKind int

const (
	// KindTCPPort probes 0, -1, 1, 65535, 65536. Closes BC-4 port-range gap.
	KindTCPPort ValidationKind = iota
	// KindNonNegativeInt probes 0, -1, 1.
	KindNonNegativeInt
	// KindNonEmptyString probes "", "  ", "valid".
	KindNonEmptyString
	// KindStringEnum probes each known value, "" (absent), random string, non-string Go types.
	KindStringEnum
	// KindIntegerOnlyFloat probes 1.0, 1.9, NaN, Inf. Closes BC-4 fractional-float gap.
	KindIntegerOnlyFloat
)

// String returns the human-readable name of the kind, suitable for test output.
func (k ValidationKind) String() string {
	switch k {
	case KindTCPPort:
		return "TCPPort"
	case KindNonNegativeInt:
		return "NonNegativeInt"
	case KindNonEmptyString:
		return "NonEmptyString"
	case KindStringEnum:
		return "StringEnum"
	case KindIntegerOnlyFloat:
		return "IntegerOnlyFloat"
	}
	return "Unknown"
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/...`
Expected: PASS, output `ok  github.com/GoCodeAlone/workflow/plugin/sdk/iaclint`.

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/iaclint_test.go
git commit -m "feat(iaclint): add ValidationKind enum + package skeleton (D-2)"
```

---

## Task 2: D-2 AssertOutputsRoundTripStructpb (BC-2 helper)

**Files:**
- Modify: `plugin/sdk/iaclint/iaclint.go`
- Test: `plugin/sdk/iaclint/iaclint_test.go`

**Step 1: Write the failing test**

```go
func TestAssertOutputsRoundTripStructpb_RejectsTypedSlice(t *testing.T) {
	// Typed slices ([]int, []string, []GoStruct) are rejected by structpb.
	// AssertOutputsRoundTripStructpb must surface the rejection as a test failure.
	tt := &mockT{}
	iaclint.AssertOutputsRoundTripStructpb(tt, map[string]any{
		"droplet_ids": []int{123, 456}, // BC-2: typed slice rejected by structpb
	})
	if !tt.failed {
		t.Fatal("AssertOutputsRoundTripStructpb accepted typed []int slice; expected failure")
	}
	if !strings.Contains(tt.fatalMsg, "droplet_ids") {
		t.Errorf("fatal msg %q missing field name 'droplet_ids'", tt.fatalMsg)
	}
}

func TestAssertOutputsRoundTripStructpb_AcceptsCanonicalShape(t *testing.T) {
	tt := &mockT{}
	iaclint.AssertOutputsRoundTripStructpb(tt, map[string]any{
		"droplet_ids":   []any{float64(123), float64(456)},
		"tags":          []any{"a", "b"},
		"inbound_rules": []any{map[string]any{"protocol": "tcp"}},
		"name":          "fw-1",
	})
	if tt.failed {
		t.Fatalf("AssertOutputsRoundTripStructpb rejected canonical shape: %s", tt.fatalMsg)
	}
}

// mockT captures testing.TB calls without actually failing the outer test.
type mockT struct {
	failed   bool
	fatalMsg string
}

func (m *mockT) Helper()                                {}
func (m *mockT) Fatalf(format string, args ...any)      { m.failed = true; m.fatalMsg = fmt.Sprintf(format, args...) }
func (m *mockT) Errorf(format string, args ...any)      { m.failed = true; m.fatalMsg = fmt.Sprintf(format, args...) }
```

(Add `"fmt"`, `"strings"` to test imports.)

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/... -run TestAssertOutputsRoundTripStructpb`
Expected: FAIL with `undefined: iaclint.AssertOutputsRoundTripStructpb`.

**Step 3: Write minimal implementation**

```go
import (
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
)

// TB is the subset of testing.TB the iaclint matchers use. Accepting an
// interface (rather than *testing.T) lets the matchers be unit-tested with a
// mock that captures failures.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

// AssertOutputsRoundTripStructpb verifies that every value in outputs survives
// a structpb.NewStruct → AsMap() round-trip without breaking downstream type
// assertions. Closes BC-2 (structpb gRPC boundary): typed slices ([]int,
// []string, []godo.X) are rejected by structpb.NewStruct outright; godo
// structs round-trip as map[string]any so reader-side type assertions to the
// original struct type fail silently.
//
// Plugins on legacy compat dispatch (no internal/contracts/ proto package,
// plugin.json mode != "strict") MUST call this matcher in their test suite for
// every Outputs map written by Create/Update/Read, so the canonical-shape
// invariant is enforced at CI time.
//
// Strict-mode plugins (plugin.json mode == "strict") are immune to BC-2 and do
// not need this matcher.
func AssertOutputsRoundTripStructpb(t TB, outputs map[string]any) {
	t.Helper()
	if outputs == nil {
		return
	}
	if _, err := structpb.NewStruct(outputs); err != nil {
		// Identify the offending key to give the test author a direct pointer.
		for k, v := range outputs {
			if _, single := structpb.NewStruct(map[string]any{k: v}); single != nil {
				t.Fatalf("Outputs[%q] (%T) is not structpb-compatible: %v — see BC-2 in IAC_PLUGIN_REVIEW_CHECKLIST.md", k, v, single)
				return
			}
		}
		t.Fatalf("Outputs not structpb-compatible: %v", err)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/... -run TestAssertOutputsRoundTripStructpb`
Expected: PASS, both sub-tests green.

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/iaclint_test.go
git commit -m "feat(iaclint): AssertOutputsRoundTripStructpb (D-2 BC-2 matcher)"
```

---

## Task 3: D-2 AssertValidationMatrix — KindTCPPort + KindIntegerOnlyFloat

**Files:**
- Modify: `plugin/sdk/iaclint/iaclint.go`
- Test: `plugin/sdk/iaclint/iaclint_test.go`

**Step 1: Write the failing test**

```go
func TestAssertValidationMatrix_TCPPort_StrictParserPasses(t *testing.T) {
	// A parser that rejects 0, negative, and >65535 should pass the TCPPort matrix.
	parser := func(cfg map[string]any) (any, error) {
		v, ok := cfg["port"].(int)
		if !ok {
			if f, fok := cfg["port"].(float64); fok && f == float64(int(f)) {
				v = int(f)
			} else {
				return nil, fmt.Errorf("port: must be an integer")
			}
		}
		if v < 1 || v > 65535 {
			return nil, fmt.Errorf("port: %d invalid (must be 1..65535)", v)
		}
		return v, nil
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "port", iaclint.KindTCPPort)
	if tt.failed {
		t.Fatalf("strict TCPPort parser failed matrix: %s", tt.fatalMsg)
	}
}

func TestAssertValidationMatrix_TCPPort_LooseParserFails(t *testing.T) {
	// A parser that accepts 0 (loose) should fail the TCPPort matrix.
	parser := func(cfg map[string]any) (any, error) {
		v, _ := cfg["port"].(int)
		if v < 0 || v > 65535 {
			return nil, fmt.Errorf("port: %d invalid", v)
		}
		return v, nil // accepts 0 — BC-4 violation
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "port", iaclint.KindTCPPort)
	if !tt.failed {
		t.Fatal("loose TCPPort parser passed matrix; expected failure for value 0")
	}
}

func TestAssertValidationMatrix_IntegerOnlyFloat_StrictParserPasses(t *testing.T) {
	parser := func(cfg map[string]any) (any, error) {
		v, ok := cfg["id"].(float64)
		if !ok {
			return nil, fmt.Errorf("id: must be a number")
		}
		if v != float64(int64(v)) {
			return nil, fmt.Errorf("id: %v is not an integer", v)
		}
		return int64(v), nil
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "id", iaclint.KindIntegerOnlyFloat)
	if tt.failed {
		t.Fatalf("strict IntegerOnlyFloat parser failed matrix: %s", tt.fatalMsg)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/... -run TestAssertValidationMatrix`
Expected: FAIL with `undefined: iaclint.AssertValidationMatrix`.

**Step 3: Write minimal implementation**

```go
import "math"

// ConfigParser is a closure that extracts and validates one config field. The
// parser receives a config map (mirroring the cfg map[string]any shape used
// across IaC drivers) and returns the parsed value or an error.
//
// AssertValidationMatrix calls the parser repeatedly with edge-case inputs and
// asserts the parser correctly accepts/rejects each.
type ConfigParser func(cfg map[string]any) (any, error)

// AssertValidationMatrix runs the standard {field, value-class} battery
// against parser. Closes BC-4 (validation matrix) by exercising the edge
// values that have historically been silently accepted by IaC plugin parsers.
//
// fieldName is the cfg key the parser reads (e.g., "port", "droplet_ids").
// kind selects which battery to run.
func AssertValidationMatrix(t TB, parser ConfigParser, fieldName string, kind ValidationKind) {
	t.Helper()
	switch kind {
	case KindTCPPort:
		// Each entry: (value, expectAccept)
		probes := []struct {
			value        any
			expectAccept bool
			label        string
		}{
			{0, false, "zero"},
			{-1, false, "negative"},
			{1, true, "min valid"},
			{65535, true, "max valid"},
			{65536, false, "above max"},
		}
		runProbes(t, parser, fieldName, kind, probes)
	case KindIntegerOnlyFloat:
		probes := []struct {
			value        any
			expectAccept bool
			label        string
		}{
			{1.0, true, "integer-valued float"},
			{1.9, false, "fractional"},
			{math.NaN(), false, "NaN"},
			{math.Inf(1), false, "Inf"},
		}
		runProbes(t, parser, fieldName, kind, probes)
	default:
		t.Fatalf("AssertValidationMatrix: unhandled kind %s", kind)
	}
}

// runProbes is the shared driver for each kind's probe table.
func runProbes(t TB, parser ConfigParser, fieldName string, kind ValidationKind, probes []struct {
	value        any
	expectAccept bool
	label        string
},
) {
	t.Helper()
	for _, p := range probes {
		_, err := parser(map[string]any{fieldName: p.value})
		got := err == nil
		if got != p.expectAccept {
			verb := "rejected"
			if got {
				verb = "accepted"
			}
			t.Errorf("%s probe %q (value=%v): parser %s; expected %s — see BC-4 in IAC_PLUGIN_REVIEW_CHECKLIST.md",
				kind, p.label, p.value, verb, acceptStr(p.expectAccept))
		}
	}
}

func acceptStr(b bool) string {
	if b {
		return "accept"
	}
	return "reject"
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/...`
Expected: PASS, all 5 tests green (3 from this task + 2 from prior tasks).

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/iaclint_test.go
git commit -m "feat(iaclint): AssertValidationMatrix — TCPPort + IntegerOnlyFloat (D-2 BC-4)"
```

---

## Task 4: D-2 AssertValidationMatrix — KindNonNegativeInt + KindNonEmptyString + KindStringEnum

**Files:**
- Modify: `plugin/sdk/iaclint/iaclint.go`
- Test: `plugin/sdk/iaclint/iaclint_test.go`

**Step 1: Write the failing test**

Add to `iaclint_test.go`:

```go
func TestAssertValidationMatrix_NonNegativeInt_Strict(t *testing.T) {
	parser := func(cfg map[string]any) (any, error) {
		v, _ := cfg["count"].(int)
		if v < 0 {
			return nil, fmt.Errorf("count: %d invalid", v)
		}
		return v, nil
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "count", iaclint.KindNonNegativeInt)
	if tt.failed {
		t.Fatalf("strict NonNegativeInt parser failed matrix: %s", tt.fatalMsg)
	}
}

func TestAssertValidationMatrix_NonEmptyString_Strict(t *testing.T) {
	parser := func(cfg map[string]any) (any, error) {
		v, _ := cfg["name"].(string)
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("name: must be non-empty")
		}
		return v, nil
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "name", iaclint.KindNonEmptyString)
	if tt.failed {
		t.Fatalf("strict NonEmptyString parser failed matrix: %s", tt.fatalMsg)
	}
}

func TestAssertValidationMatrix_StringEnum_Strict(t *testing.T) {
	allowed := []string{"public", "internal"}
	parser := func(cfg map[string]any) (any, error) {
		v, exists := cfg["expose"]
		if !exists {
			return "", nil // absent is fine
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expose: must be a string, got %T", v)
		}
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return s, nil
		}
		for _, a := range allowed {
			if s == a {
				return s, nil
			}
		}
		return nil, fmt.Errorf("expose: %q invalid; must be one of %v", s, allowed)
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "expose", iaclint.WithStringEnumOptions(allowed))
	if tt.failed {
		t.Fatalf("strict StringEnum parser failed matrix: %s", tt.fatalMsg)
	}
}

func TestAssertValidationMatrix_StringEnum_LooseFailsOnNonString(t *testing.T) {
	allowed := []string{"public", "internal"}
	parser := func(cfg map[string]any) (any, error) {
		// BC-4 violation: silently treats non-string as omitted.
		s, _ := cfg["expose"].(string)
		return s, nil
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "expose", iaclint.WithStringEnumOptions(allowed))
	if !tt.failed {
		t.Fatal("loose StringEnum parser passed matrix; expected failure on non-string probe")
	}
}
```

(Add `"strings"` if not already in test imports.)

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/... -run TestAssertValidationMatrix_(NonNegativeInt|NonEmptyString|StringEnum)`
Expected: FAIL with `undefined: iaclint.WithStringEnumOptions`.

**Step 3: Write minimal implementation**

```go
// stringEnumKind is StringEnum with attached allowed values. Returned by
// WithStringEnumOptions so the matrix knows which strings are valid for the
// non-string and unknown-string probes.
type stringEnumKind struct {
	ValidationKind
	allowed []string
}

// WithStringEnumOptions returns a StringEnum kind bound to the given allowed
// values. Use this instead of the bare KindStringEnum constant when calling
// AssertValidationMatrix:
//
//	iaclint.AssertValidationMatrix(t, parser, "expose",
//	    iaclint.WithStringEnumOptions([]string{"public", "internal"}))
func WithStringEnumOptions(allowed []string) ValidationKind {
	// Encode allowed in a package-level registry keyed by the returned kind.
	// We can't extend the ValidationKind enum at call sites, so use a
	// thread-safe lookup keyed by a unique kind id.
	id := nextEnumID()
	enumOptions[id] = append([]string(nil), allowed...)
	return id
}

var (
	enumOptionsMu sync.Mutex
	enumOptions   = map[ValidationKind][]string{}
	nextEnumIDVal = ValidationKind(1000) // reserve [0..999] for static kinds
)

func nextEnumID() ValidationKind {
	enumOptionsMu.Lock()
	defer enumOptionsMu.Unlock()
	id := nextEnumIDVal
	nextEnumIDVal++
	return id
}
```

Then extend `AssertValidationMatrix` switch with:

```go
	case KindNonNegativeInt:
		probes := []struct {
			value        any
			expectAccept bool
			label        string
		}{
			{-1, false, "negative"},
			{0, true, "zero"},
			{1, true, "positive"},
		}
		runProbes(t, parser, fieldName, kind, probes)
	case KindNonEmptyString:
		probes := []struct {
			value        any
			expectAccept bool
			label        string
		}{
			{"", false, "empty"},
			{"   ", false, "whitespace"},
			{"valid", true, "non-empty"},
		}
		runProbes(t, parser, fieldName, kind, probes)
	default:
		// StringEnum kinds (returned by WithStringEnumOptions) carry IDs >= 1000.
		enumOptionsMu.Lock()
		allowed, isEnum := enumOptions[kind]
		enumOptionsMu.Unlock()
		if isEnum {
			runStringEnumProbes(t, parser, fieldName, allowed)
			return
		}
		t.Fatalf("AssertValidationMatrix: unhandled kind %s", kind)
	}
```

And add `runStringEnumProbes`:

```go
func runStringEnumProbes(t TB, parser ConfigParser, fieldName string, allowed []string) {
	t.Helper()
	type probe struct {
		value        any
		expectAccept bool
		label        string
	}
	var probes []probe
	for _, a := range allowed {
		probes = append(probes, probe{a, true, "allowed " + a})
	}
	probes = append(probes,
		probe{"", true, "absent (empty string)"},
		probe{"definitely-not-a-real-value", false, "unknown string"},
		probe{true, false, "non-string bool"},
		probe{123, false, "non-string int"},
		probe{[]string{}, false, "non-string slice"},
	)
	for _, p := range probes {
		var cfg map[string]any
		if p.value == "" && p.label == "absent (empty string)" {
			cfg = map[string]any{} // genuinely absent — no key
		} else {
			cfg = map[string]any{fieldName: p.value}
		}
		_, err := parser(cfg)
		got := err == nil
		if got != p.expectAccept {
			verb := "rejected"
			if got {
				verb = "accepted"
			}
			t.Errorf("StringEnum probe %q (value=%v): parser %s; expected %s — see BC-4 in IAC_PLUGIN_REVIEW_CHECKLIST.md",
				p.label, p.value, verb, acceptStr(p.expectAccept))
		}
	}
}
```

(Add `"sync"` to imports.)

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/...`
Expected: PASS, all 9 tests green.

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/iaclint_test.go
git commit -m "feat(iaclint): AssertValidationMatrix — NonNegativeInt + NonEmptyString + StringEnum (D-2 BC-4)"
```

---

## Task 5: D-2 AssertDiffPopulatesAllOutputFields (BC-3 helper)

**Files:**
- Modify: `plugin/sdk/iaclint/iaclint.go`
- Test: `plugin/sdk/iaclint/iaclint_test.go`

**Step 1: Write the failing test**

```go
// fakeDriver is a minimal interfaces.ResourceDriver impl used to exercise
// AssertDiffPopulatesAllOutputFields. The contract: every key Diff reads from
// current.Outputs MUST be populated by Create/Read/Update on the writer side.
type fakeDriver struct {
	createOutputs map[string]any
	diffReadsKeys []string // keys this fake's Diff would read
}

func (f *fakeDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:    spec.Name,
		Type:    spec.Type,
		Outputs: f.createOutputs,
		Status:  "running",
	}, nil
}

// (Implement other ResourceDriver methods as no-ops.)

// Diff records that it read the expected keys but doesn't actually compare.
func (f *fakeDriver) Diff(ctx context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	for _, k := range f.diffReadsKeys {
		_ = current.Outputs[k] // simulate reading the field
	}
	return &interfaces.DiffResult{NeedsUpdate: false}, nil
}

func TestAssertDiffPopulatesAllOutputFields_OK(t *testing.T) {
	d := &fakeDriver{
		createOutputs: map[string]any{"image": "x:v1", "expose": "internal"},
		diffReadsKeys: []string{"image", "expose"},
	}
	tt := &mockT{}
	iaclint.AssertDiffPopulatesAllOutputFields(tt, d,
		interfaces.ResourceSpec{Name: "fake", Type: "fake.thing", Config: map[string]any{}})
	if tt.failed {
		t.Fatalf("driver with matching writer/reader keys failed assertion: %s", tt.fatalMsg)
	}
}

func TestAssertDiffPopulatesAllOutputFields_MissingKey(t *testing.T) {
	// Writer doesn't populate "expose" but Diff reads it.
	d := &fakeDriver{
		createOutputs: map[string]any{"image": "x:v1"}, // no "expose"
		diffReadsKeys: []string{"image", "expose"},
	}
	tt := &mockT{}
	iaclint.AssertDiffPopulatesAllOutputFields(tt, d,
		interfaces.ResourceSpec{Name: "fake", Type: "fake.thing", Config: map[string]any{}})
	if !tt.failed {
		t.Fatal("driver with missing writer key passed assertion; expected failure")
	}
	if !strings.Contains(tt.fatalMsg, "expose") {
		t.Errorf("fatal msg missing key name 'expose': %q", tt.fatalMsg)
	}
}
```

(Add the rest of the no-op methods on fakeDriver. Add `context`, `interfaces` to test imports.)

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test ./plugin/sdk/iaclint/... -run TestAssertDiffPopulatesAllOutputFields`
Expected: FAIL with `undefined: iaclint.AssertDiffPopulatesAllOutputFields`.

**Step 3: Write minimal implementation**

This matcher requires the driver to declare which keys its Diff reads — Go's reflection can't introspect runtime field reads safely. So the API takes the keys explicitly via a small interface plugins implement on their drivers (or the test calls a sibling function that exposes the read set):

```go
// DiffOutputKeyDeclarer is an optional interface a ResourceDriver may
// implement to declare which Outputs[*] keys its Diff implementation reads.
// AssertDiffPopulatesAllOutputFields uses this to verify the writer side
// (Create/Read/Update) populates all declared keys.
//
// Plugins typically implement this as a sibling method that returns a static
// slice of canonical key names — small surface, easy to keep in sync.
type DiffOutputKeyDeclarer interface {
	DiffReadsOutputKeys() []string
}

// AssertDiffPopulatesAllOutputFields verifies that for every key the driver's
// Diff implementation reads from current.Outputs, the matching Create call
// populates that key. Closes BC-3 (Outputs-vs-Diff invariant).
//
// The driver must implement DiffOutputKeyDeclarer to declare its read set.
// The matcher invokes Create with the provided sample spec and inspects the
// returned ResourceOutput.Outputs map for the declared keys.
//
// Sample spec must be a representative input that exercises the writer's
// happy path. Use the spec the plugin's own tests use.
func AssertDiffPopulatesAllOutputFields(t TB, driver interfaces.ResourceDriver, sampleSpec interfaces.ResourceSpec) {
	t.Helper()
	declarer, ok := driver.(DiffOutputKeyDeclarer)
	if !ok {
		t.Fatalf("driver %T does not implement DiffOutputKeyDeclarer; cannot verify BC-3 invariant", driver)
		return
	}
	out, err := driver.Create(context.Background(), sampleSpec)
	if err != nil {
		t.Fatalf("driver.Create failed: %v", err)
		return
	}
	if out == nil || out.Outputs == nil {
		t.Fatalf("driver.Create returned nil Outputs")
		return
	}
	for _, key := range declarer.DiffReadsOutputKeys() {
		if _, present := out.Outputs[key]; !present {
			t.Errorf("Outputs[%q] not populated by Create, but Diff reads it — see BC-3 in IAC_PLUGIN_REVIEW_CHECKLIST.md", key)
		}
	}
}
```

Update fakeDriver in test to implement DiffOutputKeyDeclarer:

```go
func (f *fakeDriver) DiffReadsOutputKeys() []string { return f.diffReadsKeys }
```

(Add `"context"` to package imports if not already.)

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/...`
Expected: PASS, all 11 tests green.

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/iaclint_test.go
git commit -m "feat(iaclint): AssertDiffPopulatesAllOutputFields (D-2 BC-3 matcher)"
```

---

## Task 6: D-2 package godoc + usage example (Example test)

**Files:**
- Modify: `plugin/sdk/iaclint/iaclint.go` (expand top-level package doc)
- Create: `plugin/sdk/iaclint/example_test.go`

**Step 1: Write the failing test**

```go
package iaclint_test

import (
	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

// Example shows a typical IaC plugin test importing iaclint to assert all
// three bug-class invariants on a single driver.
func Example() {
	// In a real plugin test:
	//
	//   driver := &MyFirewallDriver{client: mockClient}
	//   iaclint.AssertOutputsRoundTripStructpb(t, mustCreate(t, driver).Outputs)
	//   iaclint.AssertDiffPopulatesAllOutputFields(t, driver, sampleSpec)
	//   iaclint.AssertValidationMatrix(t, parsePort, "port", iaclint.KindTCPPort)
	//
	// See docs/IAC_PLUGIN_REVIEW_CHECKLIST.md for the bug-class taxonomy.
	_ = iaclint.KindTCPPort
	// Output:
}
```

**Step 2: Run test to verify it fails (or passes — Example tests don't have a "fail-first" mode)**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/...`
Expected: PASS (Example test always runs; just verifies it compiles).

**Step 3: Update package doc**

Expand the existing package doc-comment in `iaclint.go` (the one starting with `// Package iaclint provides...`) to mention all three matchers and their bug-class mapping:

```go
// Package iaclint provides cross-provider review discipline as executable
// test helpers for IaC plugin authors.
//
// The bug-class taxonomy and rationale for each helper live in the project
// review checklist:
//
//	docs/IAC_PLUGIN_REVIEW_CHECKLIST.md
//
// Three matchers are exposed:
//
//   - AssertOutputsRoundTripStructpb (BC-2): verifies Outputs map values
//     survive structpb.NewStruct round-trip without breaking type assertions.
//     Use for plugins on legacy compat dispatch.
//
//   - AssertDiffPopulatesAllOutputFields (BC-3): verifies every Outputs[*]
//     key the driver's Diff reads is populated by the matching Create writer.
//     Use for every ResourceDriver in the plugin.
//
//   - AssertValidationMatrix (BC-4): exercises edge values for a config-field
//     parser (TCP port, integer-only float, non-empty string, string enum,
//     non-negative int). Use for every field-level config validator.
//
// IaC plugins import this package in their test suites so the bug classes
// surfaced during the workflow-plugin-digitalocean v0.8.0 review cycle are
// caught at CI time rather than during production gRPC dispatch or in code
// review.
```

**Step 4: Run tests to verify all green**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/... && GOWORK=off go vet ./plugin/sdk/iaclint/...`
Expected: PASS, vet clean, godoc renders (verify with `go doc github.com/GoCodeAlone/workflow/plugin/sdk/iaclint`).

**Step 5: Commit**

```bash
git add plugin/sdk/iaclint/iaclint.go plugin/sdk/iaclint/example_test.go
git commit -m "docs(iaclint): expand package godoc + add Example (D-2 polish)"
```

---

## Task 7: D-1 markdown checklist — skeleton + BC-1 + BC-2

**Files:**
- Create: `docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`

**Step 1: Write the file**

```markdown
# IaC Plugin Review Checklist

This checklist captures the cross-provider bug-class taxonomy surfaced during
plugin review cycles (initially `workflow-plugin-digitalocean v0.8.0`, P-2
phase). Each bug class is reproducible across every IaC provider plugin (DO,
AWS, GCP, Azure, Tofu, CI-generator). Apply this checklist when reviewing any
plugin PR that touches ResourceDriver implementations, Outputs writers, or
config-field validators.

For executable enforcement, see the test-helper package
`workflow/plugin/sdk/iaclint/`. Each bug class names the matcher that closes
it.

## How to use this checklist

- **As a reviewer:** scan the diff for each bug class. The "Reviewer scan"
  sub-section names the concrete grep / read steps.
- **As a plugin author:** import `github.com/GoCodeAlone/workflow/plugin/sdk/iaclint`
  in your test suite and call the named matcher for every driver/field.
- **As a maintainer auditing existing plugins:** apply each bug-class scan to
  the plugin's `main` HEAD and file one issue per finding.

## BC-1: Plan/Diff cascade gap

**Failure mode:** A driver's `Diff` implementation either always returns nil
(stub) or only compares a subset of fields, so in-place updates silently
no-op or emit spurious changes on every reconcile.

**Repro pattern:** `workflow-plugin-digitalocean` PR #35 round 1
(`AppPlatformDriver.Diff` only compared `image`); PR #36 round 1
(`FirewallDriver.Diff` always returned `NeedsUpdate=false`).

**Fix shape:** `Diff` compares every canonical config field; the matching
`appOutput` / `fwOutput` writer populates `Outputs[*]` for every field `Diff`
reads (see also BC-3).

**Test pattern:** combine `iaclint.AssertDiffPopulatesAllOutputFields` (BC-3
matcher) with explicit `_DetectsXChange` test cases per field. See
`workflow-plugin-digitalocean/internal/drivers/firewall_test.go` for the
canonical structure (8 sub-cases per Diff: each field's positive case +
no-change baseline + reorder/normalization cases).

**Reviewer scan:**

1. `grep -nE 'func.*\bDiff\b' internal/drivers/*.go provider/drivers/*.go`
2. For each Diff, read its body. Does it always return nil? Does it only
   compare one or two fields when the Create/Update accepts more?
3. If the answer is "yes" to either, surface as **BC-1 BLOCKING**.

## BC-2: structpb gRPC boundary (legacy compat plugins only)

**Failure mode:** `Outputs["..."]` stores a typed slice (`[]int`, `[]string`,
`[]godo.X` etc.) that is **rejected** by `structpb.NewStruct` at the
wfctl→plugin gRPC boundary. After the boundary round-trip, reader-side type
assertions fail (`current.Outputs["X"].([]godo.Y)` returns `ok=false`),
treating current state as nil and emitting perpetual spurious changes from
Diff. The whole Diff cascade fix becomes a no-op in production gRPC mode.

**Repro pattern:** `workflow-plugin-digitalocean` PR #36 round 2 (Diff cascade
fix landed but typed-slice Outputs broke under realistic gRPC dispatch; round
3 introduced canonical-shape Outputs to close the gap).

**Constraint reference:** `internal/grpc_dispatch_test.go:30-32` in any
external-dispatch plugin documents the structpb constraint:

> "Slices must be `[]any`; native typed slices (`[]string`, `[]int`, etc.)
> are rejected by `structpb.NewStruct` with 'proto: invalid type'."

**Fix shape:**

- `Outputs[<int-slice key>]` → `[]any` of `float64` (structpb collapses all
  numerics to `float64`; storing as `float64` from the start makes the shape
  symmetric with both pre- and post-roundtrip reads).
- `Outputs[<string-slice key>]` → `[]any` of `string`.
- `Outputs[<struct-slice key>]` → `[]any` of `map[string]any`, with a flatten
  helper per godo struct type.
- Reader-side helpers (`outputsAsIntSlice`, `outputsAsStringSlice`, etc.)
  accept BOTH typed-slice (in-process pre-roundtrip path) AND `[]any` of
  primitive/map (post-roundtrip path).

**Test pattern:** import `iaclint` and call `iaclint.AssertOutputsRoundTripStructpb(t, out.Outputs)`
in the driver's Create/Read/Update tests. For Diff, write a
`_StructpbBoundary_DiffSurvivesRoundTrip` test that builds an Outputs map,
round-trips through `structpb.NewStruct`/`AsMap()`, then calls Diff against a
matching desired and asserts `NeedsUpdate=false`.

**Reviewer scan:**

1. Check `plugin.json` for `mode: strict`. If `strict`, BC-2 doesn't apply.
2. Otherwise: `grep -nE 'Outputs\["[^"]+"\] *= *\[\]' internal/drivers/*.go`
   surfaces typed-slice writes. Each is a **BC-2 BLOCKING** instance.
3. `grep -nE 'current\.Outputs\["[^"]+"\]\.\(\[\]' internal/drivers/*.go`
   surfaces typed-slice reads in Diff. Each is a **BC-2 BLOCKING** instance.

## (Bug classes BC-3 through BC-8 to follow in Task 8)
```

**Step 2: Run any verification**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && grep -c "## BC-" docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`
Expected: `2` (BC-1 + BC-2 sections present).

**Step 3: Commit**

```bash
git add docs/IAC_PLUGIN_REVIEW_CHECKLIST.md
git commit -m "docs(iac): D-1 review checklist skeleton + BC-1 + BC-2"
```

---

## Task 8: D-1 markdown checklist — BC-3 through BC-8

**Files:**
- Modify: `docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`

**Step 1: Append the remaining bug classes**

Append BC-3, BC-4, BC-5, BC-6, BC-7, BC-8 sections following the same
four-sub-section structure (Failure mode / Repro pattern / Fix shape / Test
pattern + Reviewer scan). Pull the content directly from the design doc
(`docs/plans/2026-04-28-iac-plugin-review-design.md` "Bug-class taxonomy"
section), expanding each into the same shape as BC-1/BC-2.

For BC-3 (Outputs-vs-Diff invariant), the test pattern is
`iaclint.AssertDiffPopulatesAllOutputFields` and the driver must implement
`iaclint.DiffOutputKeyDeclarer`.

For BC-4 (validation matrix), the test pattern is
`iaclint.AssertValidationMatrix(t, parser, fieldName, kind)` for each
{field, kind} pair the driver accepts.

BC-5/BC-6/BC-7/BC-8 don't have iaclint matchers (they're discipline-shape
patterns reviewers apply manually); call this out in the test pattern
section: "No iaclint matcher; verify by reviewer scan and manual test cases."

**Step 2: Run verification**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && grep -c "## BC-" docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`
Expected: `8` (all 8 bug classes documented).

Also run: `grep -nE 'BC-[1-8]' docs/IAC_PLUGIN_REVIEW_CHECKLIST.md | wc -l`
Expected: at least 16 (each bug class referenced multiple times in cross-links).

**Step 3: Commit**

```bash
git add docs/IAC_PLUGIN_REVIEW_CHECKLIST.md
git commit -m "docs(iac): D-1 review checklist BC-3..BC-8 (full taxonomy)"
```

---

## Task 9: D-1 link from CONTRIBUTING.md

**Files:**
- Modify: `CONTRIBUTING.md`

**Step 1: Add a section pointing to the checklist**

Open `CONTRIBUTING.md`. Find the "Plugin Development" section (or a similar
plugin-related heading). Add a sub-section:

```markdown
### Reviewing IaC plugin PRs

When reviewing a PR in any `GoCodeAlone/workflow-plugin-*` repository that
implements an IaC provider, apply the cross-provider review checklist:

- [`docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`](docs/IAC_PLUGIN_REVIEW_CHECKLIST.md)

For executable enforcement, plugin authors should import the
`workflow/plugin/sdk/iaclint/` test helpers in their CI test suite. The
checklist's "Test pattern" sub-sections name the matcher for each bug class.
```

If `CONTRIBUTING.md` has no plugin-related section, add the sub-section under
"Code Review" or after "Project Structure".

**Step 2: Run verification**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && grep -c 'IAC_PLUGIN_REVIEW_CHECKLIST' CONTRIBUTING.md`
Expected: `1` (single link reference).

**Step 3: Commit**

```bash
git add CONTRIBUTING.md
git commit -m "docs(contributing): link IAC_PLUGIN_REVIEW_CHECKLIST from CONTRIBUTING (D-1)"
```

---

## Task 10: D-3 strict-contracts tracker extension

**Files:**
- Modify: `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md`
- Modify: `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md`

**Step 1: Add IaC plugin migration table**

In both documents, locate the per-plugin migration tracking section. Add (or
extend) a sub-section:

```markdown
### IaC Provider Plugins

These plugins implement `interfaces.ResourceDriver` and consume the IaC
canonical schema. They are tracked separately because their migration
benefits from the cross-provider review discipline at
[`docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`](../IAC_PLUGIN_REVIEW_CHECKLIST.md).

| Plugin | Strict-mode status (target v0.9.0) | Pre-migration audit findings |
|---|---|---|
| `workflow-plugin-aws` | active migration | (Phase C audit pending) |
| `workflow-plugin-azure` | verified locally, awaiting PR | (Phase C audit pending) |
| `workflow-plugin-ci-generator` | merged | n/a (already shipped strict) |
| `workflow-plugin-digitalocean` | pending — v0.8.0 ships legacy compat | F4/F5/F7 cycle: BC-1, BC-2, BC-3, BC-4, BC-5, BC-6, BC-8 closed in v0.8.0; BC-7 not applicable. Issue #37 (Update naming consistency) deferred to v0.8.x |
| `workflow-plugin-gcp` | pending | (Phase C audit pending) |
| `workflow-plugin-tofu` | pending | (Phase C audit pending) |

Each plugin's v0.9.0 strict-migration PR adds a "Pre-migration findings
closed" sub-section to its CHANGELOG referencing the bug classes addressed.
```

**Step 2: Run verification**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && grep -c 'workflow-plugin-digitalocean' docs/plans/2026-04-26-strict-grpc-plugin-contracts.md`
Expected: at least `1` (DO row added to migration table).

Also: `grep -c 'IAC_PLUGIN_REVIEW_CHECKLIST' docs/plans/2026-04-26-strict-grpc-plugin-contracts.md`
Expected: at least `1`.

**Step 3: Commit**

```bash
git add docs/plans/2026-04-26-strict-grpc-plugin-contracts.md docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md
git commit -m "docs(plans): D-3 extend strict-contracts tracker with IaC plugins"
```

---

## Task 11: D-4 plugin-pattern-reviewer skill

**Files:**
- Create: `~/.claude/skills/workflow-plugin-reviewer/SKILL.md`

(Note: this file lives in the user's home dir, not the workflow repo. The
skill is workspace-local.)

**Step 1: Write the skill file**

```markdown
---
name: workflow-plugin-reviewer
description: Use when reviewing a PR in any GoCodeAlone/workflow-plugin-* repository — auto-detects the provider pattern from plugin.json and applies the matching cross-provider bug-class checklist. v1 ships with IaC pattern populated; other patterns added as they emerge.
---

# Workflow Plugin Review Discipline

When reviewing a PR in a workflow plugin repo, follow this dispatch:

## 1. Identify the provider pattern

Read `plugin.json` from the PR's working directory. Look for one of these
top-level keys:

| Key in plugin.json | Pattern | Checklist source |
|---|---|---|
| `iacProvider` | IaC provider | `workflow/docs/IAC_PLUGIN_REVIEW_CHECKLIST.md` |
| `authProvider` | Auth provider | (TBD — checklist pending) |
| `paymentProvider` | Payment provider | (TBD — checklist pending) |
| `agentProvider` | Agent provider | (TBD — checklist pending) |
| `auditProvider` | Audit provider | (TBD — checklist pending) |
| (none of the above) | Module/step plugin | (TBD — generic step-driver checklist pending) |

If multiple keys are present, apply all matching checklists.

## 2. Load the matched checklist

Read the named checklist file from a local clone of `GoCodeAlone/workflow`.
The checklist defines bug classes (e.g., BC-1 through BC-8 for IaC) with
four sub-sections each: failure mode, repro pattern, fix shape, reviewer
scan. The reviewer scan is the concrete steps to apply to the diff.

## 3. Apply the checklist as part of the bug-class scan

In addition to the standard adversarial-review framing from
`agents/team-conventions.md` and `skills/requesting-code-review/SKILL.md`:

- For each bug class in the loaded checklist, run its "Reviewer scan"
  sub-section against the diff.
- Report findings inline at file:line, tagging the bug class identifier
  (e.g., "BC-2 BLOCKING: typed `[]int` slice in firewall.go:407 won't
  survive structpb roundtrip").
- Promote BC-1, BC-2, BC-3, BC-7 findings to **Important** by default
  (these are correctness or security gaps).
- BC-4, BC-5, BC-6, BC-8 default to **Minor** unless they exhibit a clear
  blast radius (e.g., security-relevant validation gap).

## 4. Legacy compat dispatch detection

If the plugin is on legacy compat dispatch (no `internal/contracts/` package
in the repo, plugin.json `mode` ≠ `"strict"`), include the BC-2 structpb
boundary scan even if not in the checklist's default order — legacy plugins
have this gap as a recurring risk class.

## Pattern dispatch table is pre-allocated; only IaC has populated content as of 2026-04-28

Future provider-pattern checklists slot into the dispatch table without
rewriting this skill. When a new pattern's checklist lands in
`workflow/docs/`, add a row to the table above.
```

**Step 2: Run verification**

Run: `ls -la ~/.claude/skills/workflow-plugin-reviewer/SKILL.md`
Expected: file exists, non-empty.

Also: `grep -c 'iacProvider\|authProvider\|paymentProvider' ~/.claude/skills/workflow-plugin-reviewer/SKILL.md`
Expected: at least `3`.

**Step 3: Commit (this file is outside the workflow repo, so no commit needed inside the worktree)**

The skill file is a workspace-local artifact, not part of the workflow repo
itself. Note its existence in the PR description when opening the PR for
this branch — reviewers should be aware the skill exists for the auto-load
behavior to take effect.

---

## Task 12: Smoke check — import iaclint in workflow-plugin-digitalocean test suite

**Files:**
- (Read-only — no commit in this PR. Just verify the helpers can be imported and called.)

**Step 1: Add a temporary smoke test**

In a separate scratch worktree of `workflow-plugin-digitalocean`:

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin
git checkout main && git pull
```

Then create a temporary file `internal/drivers/iaclint_smoke_scratch.go` (do
NOT commit) that imports the helpers:

```go
//go:build ignore

package drivers_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

func TestIaclintSmokeImport(t *testing.T) {
	// Verify the package imports and matchers exist with the expected signatures.
	_ = iaclint.KindTCPPort
	_ = iaclint.AssertOutputsRoundTripStructpb
	_ = iaclint.AssertDiffPopulatesAllOutputFields
	_ = iaclint.AssertValidationMatrix
}
```

**Step 2: Run smoke check**

Run: `cd /Users/jon/workspace/workflow-plugin-digitalocean && GOWORK=off go vet -tags=ignore ./...`
Expected: no errors related to the iaclint package import. (If errors emerge
because workflow-plugin-digitalocean go.mod doesn't yet pin a workflow
version that includes the new package, that's expected and is closed in
Phase C — for this smoke check, the goal is verifying the helpers compile in
the workflow worktree itself.)

A safer smoke check: in the workflow worktree, add a temporary
`plugin/sdk/iaclint/_examples/firewall_smoke_test.go.example` (named with
`.example` to keep it out of the build) that mimics how a plugin would call
the matchers, and verify it parses with `go vet`:

```go
// Save as plugin/sdk/iaclint/_examples/firewall_smoke_test.go.example
// (renamed in Phase C when consumed by an actual plugin).
//
// Demonstrates the calling pattern; not a runnable test in this repo.
package _examples

// (calling-pattern code as comments)
```

**Step 3: Clean up**

Remove the scratch file. The smoke check is informational only.

**Step 4: No commit**

This task produces no committed artifacts. It exists to verify the helpers
have a sensible API before the plan-end review.

---

## Task 13: Phase A wrap-up — push branch + open PR

**Files:**
- (No file changes — this task pushes commits and opens the PR.)

**Step 1: Verify all tasks done**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && git log --oneline origin/main..HEAD | wc -l`
Expected: at least 8 commits (Tasks 1-6 + 7-10; Task 11 is outside the repo, Task 12 is no-commit).

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && GOWORK=off go test -race ./plugin/sdk/iaclint/... && GOWORK=off go vet ./plugin/sdk/iaclint/...`
Expected: PASS, vet clean.

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && grep -c '## BC-' docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`
Expected: `8`.

**Step 2: Push branch**

Run: `cd /Users/jon/workspace/workflow/_worktrees/iac-plugin-review && git push origin design/iac-plugin-review`
Expected: branch updated.

**Step 3: Open PR**

```bash
gh pr create --repo GoCodeAlone/workflow \
  --base main \
  --head design/iac-plugin-review \
  --title "feat(plugins): IaC cross-provider review discipline (Phase A)" \
  --body "$(cat <<'EOF'
## Summary

Phase A of the IaC plugin review discipline rollout. Captures the bug-class
taxonomy from workflow-plugin-digitalocean v0.8.0 (P-2) review cycle as
cross-provider discipline that benefits all IaC provider plugins.

## Deliverables

- **D-1** \`docs/IAC_PLUGIN_REVIEW_CHECKLIST.md\` — markdown taxonomy of 8 bug
  classes (Plan/Diff cascade, structpb boundary, Outputs-vs-Diff invariant,
  validation matrix, plan-time vs apply-time docs, Diff-side vs Apply-side
  parity, CIDR widening, canonical-key registration). Linked from
  CONTRIBUTING.md.
- **D-2** \`plugin/sdk/iaclint/\` — Go test-helper package with three
  matchers: \`AssertOutputsRoundTripStructpb\`, \`AssertDiffPopulatesAllOutputFields\`,
  \`AssertValidationMatrix\`. Each IaC plugin imports for CI enforcement.
- **D-3** \`docs/plans/2026-04-26-strict-grpc-plugin-contracts.md\` —
  extended with IaC plugin migration table (DO/GCP/Tofu rows added; v0.8.0
  closures recorded for DO).
- **D-4** \`~/.claude/skills/workflow-plugin-reviewer/SKILL.md\` (workspace-
  local; not in this repo) — generic plugin-pattern-reviewer skill that
  auto-detects provider pattern from plugin.json and loads the matching
  checklist.

## Design reference

\`docs/plans/2026-04-28-iac-plugin-review-design.md\` (committed in this
branch).

## Phase B / Phase C

After this lands, Phase B (P-1 BMW cleanup) auto-dispatches per the existing
\`docs/plans/2026-04-27-iac-do-staging-implementation.md\` plan. Phase C
(retroactive plugin audit fix-forward) auto-chains after P-1.

## Test plan

- [x] \`GOWORK=off go test -race ./plugin/sdk/iaclint/...\` — all matcher tests pass
- [x] \`GOWORK=off go vet ./plugin/sdk/iaclint/...\` — clean
- [x] Checklist contains all 8 bug classes
- [x] Strict-contracts tracker references checklist + adds IaC plugin rows

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

**Step 4: DM team-lead with PR URL**

DM the orchestrator with the PR number + branch + head commit. The PR moves
to gate-clearing (CI + Copilot + spec/code reviewers per the standard
pipeline). After SHIP-IT + Copilot OK + admin-merge, Phase B auto-dispatches.

---

## Acceptance (full Phase A)

- [ ] All 13 tasks complete; 11+ commits on `design/iac-plugin-review`.
- [ ] \`GOWORK=off go test -race ./plugin/sdk/iaclint/...\` passes (all matcher tests green).
- [ ] \`docs/IAC_PLUGIN_REVIEW_CHECKLIST.md\` contains all 8 bug classes.
- [ ] CONTRIBUTING.md links to checklist.
- [ ] Strict-contracts tracker has IaC plugin migration table.
- [ ] `~/.claude/skills/workflow-plugin-reviewer/SKILL.md` exists.
- [ ] PR open against `main`, CI green, Copilot OK.

After PR merges to main: Phase B (P-1 BMW) auto-chains via the orchestrator.
