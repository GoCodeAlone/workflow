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
