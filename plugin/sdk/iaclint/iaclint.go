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

import (
	"math"

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
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{0, false, "zero"},
			{-1, false, "negative"},
			{1, true, "min valid"},
			{65535, true, "max valid"},
			{65536, false, "above max"},
		})
	case KindIntegerOnlyFloat:
		runProbes(t, parser, fieldName, kind, []validationProbe{
			{1.0, true, "integer-valued float"},
			{1.9, false, "fractional"},
			{math.NaN(), false, "NaN"},
			{math.Inf(1), false, "Inf"},
		})
	default:
		t.Fatalf("AssertValidationMatrix: unhandled kind %s", kind)
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
