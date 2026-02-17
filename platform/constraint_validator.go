package platform

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ConstraintValidator validates capability declarations against constraints
// imposed by parent tiers. It supports resource unit parsing (memory, CPU)
// and standard comparison operators.
type ConstraintValidator struct{}

// NewConstraintValidator creates a new ConstraintValidator.
func NewConstraintValidator() *ConstraintValidator {
	return &ConstraintValidator{}
}

// Validate checks the given properties against a set of constraints and returns
// any violations found. Properties is a map of field names to values (matching
// CapabilityDeclaration.Properties), and constraints are the accumulated limits
// from parent tiers.
func (cv *ConstraintValidator) Validate(properties map[string]any, constraints []Constraint) []ConstraintViolation {
	var violations []ConstraintViolation
	for _, c := range constraints {
		actual, ok := properties[c.Field]
		if !ok {
			// Field not present in properties; nothing to validate.
			continue
		}
		if !cv.satisfies(actual, c.Operator, c.Value) {
			violations = append(violations, ConstraintViolation{
				Constraint: c,
				Actual:     actual,
				Message: fmt.Sprintf("field %q value %v violates constraint %s %v (source: %s)",
					c.Field, actual, c.Operator, c.Value, c.Source),
			})
		}
	}
	return violations
}

// satisfies checks whether actual <op> limit is true.
func (cv *ConstraintValidator) satisfies(actual any, operator string, limit any) bool {
	switch operator {
	case "in":
		return cv.evalIn(actual, limit)
	case "not_in":
		return !cv.evalIn(actual, limit)
	case "==":
		return cv.compare(actual, limit) == 0
	case "!=":
		return cv.compare(actual, limit) != 0
	case "<=":
		return cv.compare(actual, limit) <= 0
	case ">=":
		return cv.compare(actual, limit) >= 0
	case "<":
		return cv.compare(actual, limit) < 0
	case ">":
		return cv.compare(actual, limit) > 0
	default:
		// Unknown operator is treated as a violation.
		return false
	}
}

// evalIn checks whether actual is contained in the limit collection.
// limit should be a slice of values.
func (cv *ConstraintValidator) evalIn(actual any, limit any) bool {
	switch items := limit.(type) {
	case []any:
		for _, item := range items {
			if cv.compare(actual, item) == 0 {
				return true
			}
		}
	case []string:
		s := fmt.Sprintf("%v", actual)
		for _, item := range items {
			if s == item {
				return true
			}
		}
	}
	return false
}

// compare returns -1, 0, or 1 indicating the ordering of a vs b.
// It attempts resource-unit parsing (memory, CPU), then numeric, then string.
func (cv *ConstraintValidator) compare(a, b any) int {
	// Try memory units first.
	aBytes, aMemOK := parseMemory(a)
	bBytes, bMemOK := parseMemory(b)
	if aMemOK && bMemOK {
		return compareInt64(aBytes, bBytes)
	}

	// Try CPU units.
	aMillis, aCPUOK := parseCPU(a)
	bMillis, bCPUOK := parseCPU(b)
	if aCPUOK && bCPUOK {
		return compareInt64(aMillis, bMillis)
	}

	// Try numeric comparison.
	aFloat, aNumOK := toFloat64(a)
	bFloat, bNumOK := toFloat64(b)
	if aNumOK && bNumOK {
		return compareFloat64(aFloat, bFloat)
	}

	// Fall back to string comparison.
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	if aStr < bStr {
		return -1
	}
	if aStr > bStr {
		return 1
	}
	return 0
}

// parseMemory tries to parse a value as a memory quantity with binary units.
// Supported suffixes: Ki (1024), Mi (1024^2), Gi (1024^3), Ti (1024^4).
// Returns the value in bytes and true if parsing succeeded.
func parseMemory(v any) (int64, bool) {
	s, ok := asString(v)
	if !ok {
		return 0, false
	}

	suffixes := map[string]int64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
	}
	for suffix, multiplier := range suffixes {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return int64(num * float64(multiplier)), true
		}
	}
	return 0, false
}

// parseCPU tries to parse a value as CPU millicores.
// "500m" = 500 millicores, "1" = 1000 millicores, "2.5" = 2500 millicores.
// Returns the value in millicores and true if parsing succeeded.
func parseCPU(v any) (int64, bool) {
	s, ok := asString(v)
	if !ok {
		return 0, false
	}

	if strings.HasSuffix(s, "m") {
		numStr := strings.TrimSuffix(s, "m")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return int64(num), true
	}

	// Plain number (whole CPU cores).
	num, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int64(num * 1000), true
}

// asString extracts a string representation from a value, returning true
// only if the value is already a string type.
func asString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	default:
		return "", false
	}
}

// toFloat64 attempts to convert a value to float64 for numeric comparison.
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// compareInt64 returns -1, 0, or 1 for int64 values.
func compareInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareFloat64 returns -1, 0, or 1 for float64 values with epsilon tolerance.
func compareFloat64(a, b float64) int {
	const epsilon = 1e-9
	if math.Abs(a-b) < epsilon {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}
