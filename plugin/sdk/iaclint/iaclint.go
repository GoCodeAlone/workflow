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
//     are structpb-compatible (NewStruct accepts them). Catches typed-slice
//     writes that would degrade at the wfctl→plugin gRPC boundary. Does NOT
//     exercise the full NewStruct → AsMap round-trip; see BC-3 for
//     post-roundtrip type-assertion coverage. Use for plugins on legacy
//     compat dispatch.
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
// review. Plugins on legacy compat dispatch (no internal/contracts/ proto
// package, plugin.json mode != "strict") MUST call AssertOutputsRoundTripStructpb
// for every Outputs map written by Create/Update/Read. Strict-mode plugins
// (plugin.json mode == "strict") are immune to BC-2 and may skip that matcher.
package iaclint

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/GoCodeAlone/workflow/interfaces"
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

// ValidationKind enumerates the standard {field, value-class} probes used by
// AssertValidationMatrix. Each kind exercises a battery of edge values that
// match the bug-class definitions in the project review checklist.
type ValidationKind int

const (
	// KindTCPPort probes 0, -1, 1, 65535, 65536 in BOTH int and float64
	// shapes. The float64 probes cover the gRPC-dispatch coercion path
	// (structpb collapses every numeric to float64), so a single call to
	// AssertValidationMatrix is sufficient for plugins on either dispatch
	// path. Closes BC-4 port-range gap.
	KindTCPPort ValidationKind = iota
	// KindNonNegativeInt probes 0, -1, 1.
	KindNonNegativeInt
	// KindNonEmptyString probes "", "  ", "valid".
	KindNonEmptyString
	// KindStringEnum probes each known value, "" (absent), random string,
	// non-string Go types. The bare constant carries no allowed-values set,
	// so it is NOT directly usable with AssertValidationMatrix — pass
	// WithStringEnumOptions([]string{...}) instead. AssertValidationMatrix
	// fails loudly with that guidance if called with the bare constant.
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

// AssertOutputsRoundTripStructpb verifies that every value in outputs is
// structpb-compatible — that is, that structpb.NewStruct accepts the outputs
// map without error. structpb's encoding rejects typed slices ([]int,
// []string, []godo.X) outright, so this matcher catches the most common BC-2
// failure mode: typed-slice writes that would silently degrade at the
// wfctl→plugin gRPC boundary.
//
// Scope note: this matcher does NOT exercise the full NewStruct → AsMap
// round-trip. Values that NewStruct accepts but degrade structurally on
// AsMap (e.g., godo structs becoming map[string]any so reader-side type
// assertions to the original struct type fail silently) are not caught. If
// your plugin's Diff reads typed values from current.Outputs, also test the
// post-roundtrip path explicitly — see BC-3 in IAC_PLUGIN_REVIEW_CHECKLIST.md.
//
// Plugins on legacy compat dispatch (no internal/contracts/ proto package,
// plugin.json mode != "strict") MUST call this matcher in their test suite for
// every Outputs map written by Create/Update/Read, so the canonical-shape
// invariant is enforced at CI time.
//
// Strict-mode plugins (plugin.json mode == "strict") are immune to BC-2 and do
// not need this matcher.
//
// Returns silently for nil or empty outputs; both are trivially structpb-
// compatible. Detecting whether Outputs is populated at all (the writer-side
// invariant) is BC-3's job — pair this matcher with
// AssertDiffPopulatesAllOutputFields for full coverage.
func AssertOutputsRoundTripStructpb(t TB, outputs map[string]any) {
	t.Helper()
	if outputs == nil {
		return
	}
	if _, err := structpb.NewStruct(outputs); err != nil {
		// Identify the offending key to give the test author a direct pointer.
		// Sort the keys first so the reported key is deterministic when
		// multiple keys are bad — Go map iteration is randomized, which
		// would otherwise produce flaky failure messages across test runs.
		keys := make([]string, 0, len(outputs))
		for k := range outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, single := structpb.NewStruct(map[string]any{k: outputs[k]}); single != nil {
				t.Fatalf("Outputs[%q] (%T) is not structpb-compatible: %v — see BC-2 in IAC_PLUGIN_REVIEW_CHECKLIST.md", k, outputs[k], single)
				return
			}
		}
		t.Fatalf("Outputs not structpb-compatible: %v", err)
	}
}

// ConfigParser is a closure that extracts and validates one config field. The
// parser receives a config map (mirroring the cfg map[string]any shape used
// across IaC drivers) and returns the parsed value or an error.
//
// AssertValidationMatrix calls the parser repeatedly with edge-case inputs and
// asserts the parser correctly accepts/rejects each.
type ConfigParser func(cfg map[string]any) (any, error)

// validationProbe is one row of the {value, expectAccept, label} battery.
type validationProbe struct {
	value        any
	expectAccept bool
	label        string
}

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
		// Probe both int and float64 shapes — gRPC dispatch coerces every
		// numeric to float64 via structpb, so parsers strict on int but lax
		// on float64 ship green-CI yet fail in production. Covering both
		// shapes in a single call closes that BC-4 gap.
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{0, false, "zero (int)"},
			{-1, false, "negative (int)"},
			{1, true, "min valid (int)"},
			{65535, true, "max valid (int)"},
			{65536, false, "above max (int)"},
			{float64(0), false, "zero (float64 — gRPC coercion)"},
			{float64(-1), false, "negative (float64 — gRPC coercion)"},
			{float64(1), true, "min valid (float64 — gRPC coercion)"},
			{float64(65535), true, "max valid (float64 — gRPC coercion)"},
			{float64(65536), false, "above max (float64 — gRPC coercion)"},
		})
	case KindIntegerOnlyFloat:
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{1.0, true, "integer-valued float"},
			{1.9, false, "fractional"},
			{math.NaN(), false, "NaN"},
			{math.Inf(1), false, "Inf"},
		})
	case KindNonNegativeInt:
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{-1, false, "negative"},
			{0, true, "zero"},
			{1, true, "positive"},
		})
	case KindNonEmptyString:
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{"", false, "empty"},
			{"   ", false, "whitespace"},
			{"valid", true, "non-empty"},
		})
	case KindStringEnum:
		// Bare KindStringEnum has no allowed-values set, so it cannot run a
		// matrix on its own. Fail loudly with actionable guidance instead of
		// the generic "unhandled kind" path. Callers must construct a kind
		// via WithStringEnumOptions to bind allowed values.
		t.Fatalf("AssertValidationMatrix: KindStringEnum requires allowed values — call iaclint.WithStringEnumOptions([]string{...}) instead of using the bare KindStringEnum constant")
		return
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
}

// stringEnum allowed-values registry. Each call to WithStringEnumOptions
// reserves a fresh ValidationKind id (>= 1000) so callers can pass distinct
// allowed sets without mutating the package-wide enum constants.
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

// WithStringEnumOptions returns a StringEnum kind bound to the given allowed
// values. Use this instead of the bare KindStringEnum constant when calling
// AssertValidationMatrix:
//
//	iaclint.AssertValidationMatrix(t, parser, "expose",
//	    iaclint.WithStringEnumOptions([]string{"public", "internal"}))
//
// Note on internal state: each call registers the allowed-values slice in a
// package-level map keyed by a unique ValidationKind ID. Map entries are not
// reclaimed after a test exits — practically harmless for `go test` (process
// exits, OS reclaims memory). If iaclint is ever embedded in a long-running
// review server, consider threading a *RegistryHandle through the matcher
// signature instead (deferred to v2).
func WithStringEnumOptions(allowed []string) ValidationKind {
	id := nextEnumID()
	enumOptionsMu.Lock()
	enumOptions[id] = append([]string(nil), allowed...)
	enumOptionsMu.Unlock()
	return id
}

// DiffOutputKeyDeclarer is an optional interface a ResourceDriver may
// implement to declare which Outputs[*] keys its Diff implementation reads.
// AssertDiffPopulatesAllOutputFields uses this to verify the writer side
// (Create/Read/Update) populates all declared keys.
//
// Plugins typically implement this as a sibling method that returns a static
// slice of canonical key names — small surface, easy to keep in sync.
//
// WARNING: the returned slice is the source of truth for
// AssertDiffPopulatesAllOutputFields — drift between this slice and the
// actual Diff implementation makes the matcher silently vacuous (it'll
// happily verify a stale, shrinking key set while real Diff reads grow
// uncovered). Treat additions or removals to the Diff body and this slice
// as paired commits, ideally enforced by code review per BC-3 in
// docs/IAC_PLUGIN_REVIEW_CHECKLIST.md.
type DiffOutputKeyDeclarer interface {
	DiffReadsOutputKeys() []string
}

// AssertDiffPopulatesAllOutputFields verifies that for every key the driver's
// Diff implementation reads from current.Outputs, the driver's Create call
// populates that key. Closes BC-3 (Outputs-vs-Diff invariant) for the Create
// path.
//
// The driver must implement DiffOutputKeyDeclarer to declare its read set.
// The matcher invokes Create with the provided sample spec and inspects the
// returned ResourceOutput.Outputs map for the declared keys.
//
// sampleSpec must be a representative input that exercises the writer's
// happy path. Use the spec the plugin's own tests use.
//
// Scope note: this matcher only verifies the Create writer. Read and Update
// population are NOT exercised here — if your driver only populates a key on
// the Read or Update path (e.g., post-deployment status, in-place rotation),
// add a separate test that calls Read/Update against a representative ref
// and asserts the same key set. Drift between writer paths is a recurring
// BC-3 sub-class.
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

func runStringEnumProbes(t TB, parser ConfigParser, fieldName string, allowed []string) {
	t.Helper()
	// stringEnumProbe carries an explicit genuineAbsent flag so the loop
	// dispatches on a bool rather than on label-string equality, which would
	// be brittle to label edits.
	type stringEnumProbe struct {
		value         any
		expectAccept  bool
		label         string
		genuineAbsent bool // when true, omit the field key from cfg entirely
	}
	var probes []stringEnumProbe
	for _, a := range allowed {
		probes = append(probes, stringEnumProbe{value: a, expectAccept: true, label: "allowed " + a})
	}
	probes = append(probes,
		stringEnumProbe{value: nil, expectAccept: true, label: "absent (no key)", genuineAbsent: true},
		stringEnumProbe{value: "definitely-not-a-real-value", expectAccept: false, label: "unknown string"},
		stringEnumProbe{value: true, expectAccept: false, label: "non-string bool"},
		stringEnumProbe{value: 123, expectAccept: false, label: "non-string int"},
		stringEnumProbe{value: []string{}, expectAccept: false, label: "non-string slice"},
	)
	for _, p := range probes {
		var cfg map[string]any
		if p.genuineAbsent {
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

// runProbes is the shared driver for each kind's probe table.
func runProbes(t TB, parser ConfigParser, fieldName string, kind ValidationKind, probes []validationProbe) {
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
