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
