package interfaces_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestDriftClass_Constants(t *testing.T) {
	cases := []struct {
		name     string
		c        interfaces.DriftClass
		expected string
	}{
		{"unknown", interfaces.DriftClassUnknown, ""},
		{"in-sync", interfaces.DriftClassInSync, "in-sync"},
		{"ghost", interfaces.DriftClassGhost, "ghost"},
		{"config", interfaces.DriftClassConfig, "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.c) != tc.expected {
				t.Errorf("got %q, want %q", string(tc.c), tc.expected)
			}
		})
	}
}

func TestDriftResult_ClassOmitEmpty(t *testing.T) {
	// Class="" (DriftClassUnknown) should be omitted from JSON for backwards compat
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: false}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"class"`) {
		t.Errorf("expected no class field with empty Class, got %s", b)
	}
}

func TestDriftResult_ClassPresent(t *testing.T) {
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"class":"ghost"`) {
		t.Errorf("expected class:ghost in JSON, got %s", b)
	}
}

func TestDriftResult_ClassRoundTrip(t *testing.T) {
	cases := []interfaces.DriftClass{
		interfaces.DriftClassInSync,
		interfaces.DriftClassGhost,
		interfaces.DriftClassConfig,
	}
	for _, c := range cases {
		orig := interfaces.DriftResult{Name: "res", Type: "infra.vpc", Drifted: true, Class: c}
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("marshal %q: %v", c, err)
		}
		var got interfaces.DriftResult
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", c, err)
		}
		if got.Class != c {
			t.Errorf("round-trip Class: got %q, want %q", got.Class, c)
		}
	}
}
