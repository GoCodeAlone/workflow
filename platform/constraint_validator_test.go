package platform

import (
	"testing"
)

func TestConstraintValidator_LessThanOrEqual(t *testing.T) {
	cv := NewConstraintValidator()

	tests := []struct {
		name       string
		props      map[string]any
		constraint Constraint
		wantPass   bool
	}{
		{
			name:       "int <= int pass",
			props:      map[string]any{"replicas": 3},
			constraint: Constraint{Field: "replicas", Operator: "<=", Value: 10},
			wantPass:   true,
		},
		{
			name:       "int <= int fail",
			props:      map[string]any{"replicas": 15},
			constraint: Constraint{Field: "replicas", Operator: "<=", Value: 10},
			wantPass:   false,
		},
		{
			name:       "int <= int equal",
			props:      map[string]any{"replicas": 10},
			constraint: Constraint{Field: "replicas", Operator: "<=", Value: 10},
			wantPass:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := cv.Validate(tt.props, []Constraint{tt.constraint})
			if tt.wantPass && len(violations) > 0 {
				t.Errorf("expected pass, got violation: %s", violations[0].Message)
			}
			if !tt.wantPass && len(violations) == 0 {
				t.Error("expected violation, got pass")
			}
		})
	}
}

func TestConstraintValidator_GreaterThanOrEqual(t *testing.T) {
	cv := NewConstraintValidator()

	tests := []struct {
		name     string
		props    map[string]any
		limit    Constraint
		wantPass bool
	}{
		{
			name:     "int >= int pass",
			props:    map[string]any{"min_replicas": 3},
			limit:    Constraint{Field: "min_replicas", Operator: ">=", Value: 2},
			wantPass: true,
		},
		{
			name:     "int >= int fail",
			props:    map[string]any{"min_replicas": 1},
			limit:    Constraint{Field: "min_replicas", Operator: ">=", Value: 2},
			wantPass: false,
		},
		{
			name:     "int >= int equal",
			props:    map[string]any{"min_replicas": 2},
			limit:    Constraint{Field: "min_replicas", Operator: ">=", Value: 2},
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := cv.Validate(tt.props, []Constraint{tt.limit})
			if tt.wantPass && len(violations) > 0 {
				t.Errorf("expected pass, got violation: %s", violations[0].Message)
			}
			if !tt.wantPass && len(violations) == 0 {
				t.Error("expected violation, got pass")
			}
		})
	}
}

func TestConstraintValidator_Equal(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"engine": "postgresql"},
		[]Constraint{{Field: "engine", Operator: "==", Value: "postgresql"}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for equal strings, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"engine": "mysql"},
		[]Constraint{{Field: "engine", Operator: "==", Value: "postgresql"}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for unequal strings, got pass")
	}

	violations = cv.Validate(
		map[string]any{"replicas": 5},
		[]Constraint{{Field: "replicas", Operator: "==", Value: 5}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for equal ints, got violation: %s", violations[0].Message)
	}
}

func TestConstraintValidator_NotEqual(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"engine": "mysql"},
		[]Constraint{{Field: "engine", Operator: "!=", Value: "postgresql"}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for != different strings, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"engine": "postgresql"},
		[]Constraint{{Field: "engine", Operator: "!=", Value: "postgresql"}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for != equal strings, got pass")
	}
}

func TestConstraintValidator_LessThan(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"connections": 49},
		[]Constraint{{Field: "connections", Operator: "<", Value: 50}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for 49 < 50, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"connections": 50},
		[]Constraint{{Field: "connections", Operator: "<", Value: 50}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for 50 < 50, got pass")
	}
}

func TestConstraintValidator_GreaterThan(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"min_nodes": 5},
		[]Constraint{{Field: "min_nodes", Operator: ">", Value: 3}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for 5 > 3, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"min_nodes": 3},
		[]Constraint{{Field: "min_nodes", Operator: ">", Value: 3}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for 3 > 3, got pass")
	}
}

func TestConstraintValidator_In(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"region": "us-east-1"},
		[]Constraint{{Field: "region", Operator: "in", Value: []any{"us-east-1", "us-west-2", "eu-west-1"}}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for value in list, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"region": "ap-south-1"},
		[]Constraint{{Field: "region", Operator: "in", Value: []any{"us-east-1", "us-west-2", "eu-west-1"}}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for value not in list, got pass")
	}
}

func TestConstraintValidator_NotIn(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"instance_type": "m5.xlarge"},
		[]Constraint{{Field: "instance_type", Operator: "not_in", Value: []any{"t2.micro", "t2.nano"}}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for value not in excluded list, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"instance_type": "t2.micro"},
		[]Constraint{{Field: "instance_type", Operator: "not_in", Value: []any{"t2.micro", "t2.nano"}}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for value in excluded list, got pass")
	}
}

func TestConstraintValidator_MemoryStrings(t *testing.T) {
	cv := NewConstraintValidator()

	tests := []struct {
		name     string
		actual   string
		op       string
		limit    string
		wantPass bool
	}{
		{"512Mi <= 4Gi pass", "512Mi", "<=", "4Gi", true},
		{"4Gi <= 4Gi pass", "4Gi", "<=", "4Gi", true},
		{"8Gi <= 4Gi fail", "8Gi", "<=", "4Gi", false},
		{"1Gi >= 512Mi pass", "1Gi", ">=", "512Mi", true},
		{"256Mi >= 512Mi fail", "256Mi", ">=", "512Mi", false},
		{"1024Ki == 1Mi pass", "1024Ki", "==", "1Mi", true},
		{"2Gi != 4Gi pass", "2Gi", "!=", "4Gi", true},
		{"4Gi != 4Gi fail", "4Gi", "!=", "4Gi", false},
		{"512Mi < 1Gi pass", "512Mi", "<", "1Gi", true},
		{"1Gi < 1Gi fail", "1Gi", "<", "1Gi", false},
		{"2Gi > 1Gi pass", "2Gi", ">", "1Gi", true},
		{"1Gi > 1Gi fail", "1Gi", ">", "1Gi", false},
		{"1Ti > 1Gi pass", "1Ti", ">", "1Gi", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := cv.Validate(
				map[string]any{"memory": tt.actual},
				[]Constraint{{Field: "memory", Operator: tt.op, Value: tt.limit}},
			)
			if tt.wantPass && len(violations) > 0 {
				t.Errorf("expected pass, got violation: %s", violations[0].Message)
			}
			if !tt.wantPass && len(violations) == 0 {
				t.Error("expected violation, got pass")
			}
		})
	}
}

func TestConstraintValidator_CPUStrings(t *testing.T) {
	cv := NewConstraintValidator()

	tests := []struct {
		name     string
		actual   string
		op       string
		limit    string
		wantPass bool
	}{
		{"500m <= 2000m pass", "500m", "<=", "2000m", true},
		{"2000m <= 2000m pass", "2000m", "<=", "2000m", true},
		{"3000m <= 2000m fail", "3000m", "<=", "2000m", false},
		{"1 == 1000m pass (whole core)", "1", "==", "1000m", true},
		{"2 == 2000m pass", "2", "==", "2000m", true},
		{"500m >= 250m pass", "500m", ">=", "250m", true},
		{"100m >= 250m fail", "100m", ">=", "250m", false},
		{"500m < 1000m pass", "500m", "<", "1000m", true},
		{"1000m < 1000m fail", "1000m", "<", "1000m", false},
		{"2000m > 1000m pass", "2000m", ">", "1000m", true},
		{"500m != 1000m pass", "500m", "!=", "1000m", true},
		{"1000m != 1000m fail", "1000m", "!=", "1000m", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := cv.Validate(
				map[string]any{"cpu": tt.actual},
				[]Constraint{{Field: "cpu", Operator: tt.op, Value: tt.limit}},
			)
			if tt.wantPass && len(violations) > 0 {
				t.Errorf("expected pass, got violation: %s", violations[0].Message)
			}
			if !tt.wantPass && len(violations) == 0 {
				t.Error("expected violation, got pass")
			}
		})
	}
}

func TestConstraintValidator_NumericComparisons(t *testing.T) {
	cv := NewConstraintValidator()

	// float64 comparisons
	violations := cv.Validate(
		map[string]any{"threshold": 0.75},
		[]Constraint{{Field: "threshold", Operator: "<=", Value: 0.9}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for 0.75 <= 0.9, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"threshold": 0.95},
		[]Constraint{{Field: "threshold", Operator: "<=", Value: 0.9}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for 0.95 <= 0.9, got pass")
	}

	// int and float cross-type
	violations = cv.Validate(
		map[string]any{"count": 5},
		[]Constraint{{Field: "count", Operator: "<=", Value: 5.0}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for int 5 <= float 5.0, got violation: %s", violations[0].Message)
	}
}

func TestConstraintValidator_MissingField(t *testing.T) {
	cv := NewConstraintValidator()

	// Field not in properties should not produce a violation.
	violations := cv.Validate(
		map[string]any{"memory": "512Mi"},
		[]Constraint{{Field: "cpu", Operator: "<=", Value: "2000m"}},
	)
	if len(violations) > 0 {
		t.Errorf("expected no violation for missing field, got: %s", violations[0].Message)
	}
}

func TestConstraintValidator_MultipleConstraints(t *testing.T) {
	cv := NewConstraintValidator()

	props := map[string]any{
		"memory":   "8Gi",
		"cpu":      "500m",
		"replicas": 3,
	}
	constraints := []Constraint{
		{Field: "memory", Operator: "<=", Value: "4Gi", Source: "tier1"},
		{Field: "cpu", Operator: "<=", Value: "2000m", Source: "tier1"},
		{Field: "replicas", Operator: "<=", Value: 10, Source: "tier2"},
	}

	violations := cv.Validate(props, constraints)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Constraint.Field != "memory" {
		t.Errorf("expected memory violation, got %s", violations[0].Constraint.Field)
	}
}

func TestConstraintValidator_ViolationMessage(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"replicas": 20},
		[]Constraint{{Field: "replicas", Operator: "<=", Value: 10, Source: "tier2"}},
	)
	if len(violations) == 0 {
		t.Fatal("expected violation")
	}
	msg := violations[0].Message
	if msg == "" {
		t.Error("expected non-empty violation message")
	}
	// Verify the message contains meaningful information.
	if violations[0].Constraint.Field != "replicas" {
		t.Errorf("expected field 'replicas', got %s", violations[0].Constraint.Field)
	}
	if violations[0].Actual != 20 {
		t.Errorf("expected actual value 20, got %v", violations[0].Actual)
	}
}

func TestConstraintValidator_UnknownOperator(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"field": "value"},
		[]Constraint{{Field: "field", Operator: "~=", Value: "value"}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for unknown operator, got pass")
	}
}

func TestConstraintValidator_InWithStringSlice(t *testing.T) {
	cv := NewConstraintValidator()

	violations := cv.Validate(
		map[string]any{"tier": "premium"},
		[]Constraint{{Field: "tier", Operator: "in", Value: []string{"basic", "premium", "enterprise"}}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for string slice in, got violation: %s", violations[0].Message)
	}

	violations = cv.Validate(
		map[string]any{"tier": "free"},
		[]Constraint{{Field: "tier", Operator: "in", Value: []string{"basic", "premium", "enterprise"}}},
	)
	if len(violations) == 0 {
		t.Error("expected violation for string not in string slice")
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"512Mi", 512 * 1024 * 1024, true},
		{"1Gi", 1024 * 1024 * 1024, true},
		{"4Gi", 4 * 1024 * 1024 * 1024, true},
		{"1024Ki", 1024 * 1024, true},
		{"1Ti", 1024 * 1024 * 1024 * 1024, true},
		{"notmemory", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseMemory(tt.input)
			if ok != tt.ok {
				t.Fatalf("parseMemory(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("parseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestToFloat64_AllTypes(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want float64
		ok   bool
	}{
		{"int", int(42), 42.0, true},
		{"int8", int8(8), 8.0, true},
		{"int16", int16(16), 16.0, true},
		{"int32", int32(32), 32.0, true},
		{"int64", int64(64), 64.0, true},
		{"float32", float32(3.14), float64(float32(3.14)), true},
		{"float64", float64(2.718), 2.718, true},
		{"string number", "123.45", 123.45, true},
		{"string not number", "abc", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloat64(tt.val)
			if ok != tt.ok {
				t.Fatalf("toFloat64(%v) ok = %v, want %v", tt.val, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("toFloat64(%v) = %f, want %f", tt.val, got, tt.want)
			}
		})
	}
}

func TestConstraintValidator_NumericTypeMix(t *testing.T) {
	cv := NewConstraintValidator()

	// int32 <= int64
	violations := cv.Validate(
		map[string]any{"count": int32(5)},
		[]Constraint{{Field: "count", Operator: "<=", Value: int64(10)}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for int32(5) <= int64(10), got violation")
	}

	// float32 <= float64
	violations = cv.Validate(
		map[string]any{"ratio": float32(0.5)},
		[]Constraint{{Field: "ratio", Operator: "<=", Value: float64(1.0)}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for float32(0.5) <= float64(1.0), got violation")
	}

	// string number comparison
	violations = cv.Validate(
		map[string]any{"version": "15"},
		[]Constraint{{Field: "version", Operator: ">=", Value: "10"}},
	)
	if len(violations) > 0 {
		t.Errorf("expected pass for string '15' >= string '10' (numeric), got violation")
	}
}

func TestParseMemory_InvalidNumber(t *testing.T) {
	_, ok := parseMemory("abcMi")
	if ok {
		t.Error("expected false for invalid memory number")
	}
}

func TestParseCPU_InvalidMillicore(t *testing.T) {
	_, ok := parseCPU("abcm")
	if ok {
		t.Error("expected false for invalid CPU millicore number")
	}
}

func TestParseMemory_NonString(t *testing.T) {
	_, ok := parseMemory(12345)
	if ok {
		t.Error("expected false for non-string memory value")
	}
}

func TestParseCPU_NonString(t *testing.T) {
	_, ok := parseCPU(12345)
	if ok {
		t.Error("expected false for non-string CPU value")
	}
}

func TestContextPathForTier_DefaultCase(t *testing.T) {
	got := contextPathForTier("acme", "prod", "", Tier(99))
	if got != "acme/prod" {
		t.Errorf("expected 'acme/prod' for unknown tier, got %q", got)
	}
}

func TestExtractConstraints_NonMapValue(t *testing.T) {
	props := map[string]any{
		"bad_entry": "not a map",
		"constraint_0": map[string]any{
			"field":    "memory",
			"operator": "<=",
			"value":    "4Gi",
			"source":   "tier1",
		},
	}
	constraints := extractConstraints(props, "tier1")
	if len(constraints) != 1 {
		t.Errorf("expected 1 constraint (skipping non-map), got %d", len(constraints))
	}
}

func TestCompare_MixedUnitTypes(t *testing.T) {
	cv := NewConstraintValidator()

	// One value is memory, other is not — falls through to string comparison.
	violations := cv.Validate(
		map[string]any{"field": "512Mi"},
		[]Constraint{{Field: "field", Operator: "==", Value: 512}},
	)
	// These should not match (string "512Mi" vs numeric 512).
	if len(violations) == 0 {
		t.Error("expected violation when comparing memory string to plain number")
	}
}

func TestParseCPU(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"500m", 500, true},
		{"1000m", 1000, true},
		{"2000m", 2000, true},
		{"1", 1000, true},
		{"2", 2000, true},
		{"notcpu", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseCPU(tt.input)
			if ok != tt.ok {
				t.Fatalf("parseCPU(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("parseCPU(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
