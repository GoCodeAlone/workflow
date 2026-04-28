package iaclint_test

import (
	"fmt"
	"strings"
	"testing"

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
