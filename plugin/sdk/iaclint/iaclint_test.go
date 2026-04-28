package iaclint_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

// mockT captures testing.TB calls without actually failing the outer test.
type mockT struct {
	failed   bool
	fatalMsg string
}

func (m *mockT) Helper() {}
func (m *mockT) Fatalf(format string, args ...any) {
	m.failed = true
	m.fatalMsg = fmt.Sprintf(format, args...)
}
func (m *mockT) Errorf(format string, args ...any) {
	m.failed = true
	m.fatalMsg = fmt.Sprintf(format, args...)
}

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

// TestAssertValidationMatrix_TCPPort_FloatLooseParserFails exercises the
// gRPC-coercion gap: a parser that's strict on int but silently accepts
// float64 ships green-CI but fails in production gRPC dispatch (where the
// structpb boundary collapses every numeric to float64). The TCPPort matrix
// MUST probe both int and float64 shapes for a single call to be sufficient.
func TestAssertValidationMatrix_TCPPort_FloatLooseParserFails(t *testing.T) {
	parser := func(cfg map[string]any) (any, error) {
		// Strict on int — passes all int probes.
		if v, ok := cfg["port"].(int); ok {
			if v < 1 || v > 65535 {
				return nil, fmt.Errorf("port: %d invalid", v)
			}
			return v, nil
		}
		// Loose on float64 — accepts everything (BC-4 violation under gRPC coercion).
		if f, ok := cfg["port"].(float64); ok {
			return int(f), nil
		}
		return nil, fmt.Errorf("port: must be a number")
	}
	tt := &mockT{}
	iaclint.AssertValidationMatrix(tt, parser, "port", iaclint.KindTCPPort)
	if !tt.failed {
		t.Fatal("float-loose TCPPort parser passed matrix; expected failure on float64 probes (gRPC coercion path uncovered)")
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

func (f *fakeDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

func (f *fakeDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

func (f *fakeDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error { return nil }

// Diff records that it read the expected keys but doesn't actually compare.
func (f *fakeDriver) Diff(ctx context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	for _, k := range f.diffReadsKeys {
		_ = current.Outputs[k] // simulate reading the field
	}
	return &interfaces.DiffResult{NeedsUpdate: false}, nil
}

func (f *fakeDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}

func (f *fakeDriver) Scale(ctx context.Context, ref interfaces.ResourceRef, replicas int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

func (f *fakeDriver) SensitiveKeys() []string { return nil }

func (f *fakeDriver) DiffReadsOutputKeys() []string { return f.diffReadsKeys }

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
