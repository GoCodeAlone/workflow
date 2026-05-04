package inputsnapshot

import "testing"

func TestComputeDrift_PreservedSentinelSkipsDrift(t *testing.T) {
	planSnap := map[string]string{"FOO": "abcdef0000000000"}
	applySnap := map[string]string{"FOO": preservedFingerprint}
	drift := ComputeDrift(planSnap, applySnap)
	if len(drift) != 0 {
		t.Errorf("preserved-sentinel should suppress drift; got %+v", drift)
	}
}

func TestComputeDrift_DifferentFingerprint_ReportsDrift(t *testing.T) {
	planSnap := map[string]string{"FOO": "abcdef0000000000"}
	applySnap := map[string]string{"FOO": "deadbeef00000000"}
	drift := ComputeDrift(planSnap, applySnap)
	if len(drift) != 1 || drift[0].Name != "FOO" {
		t.Errorf("differing fingerprints should produce one drift entry; got %+v", drift)
	}
}

func TestComputeDrift_KeyMissingInApplySnap_ReportsDrift(t *testing.T) {
	planSnap := map[string]string{"FOO": "abcdef0000000000"}
	applySnap := map[string]string{} // FOO missing entirely
	drift := ComputeDrift(planSnap, applySnap)
	// Assert behavior, not literal placeholder string. Drift exists,
	// ApplyFingerprint differs from PlanFingerprint, and uses the in-package
	// unsetFingerprintPlaceholder constant.
	if len(drift) != 1 || drift[0].Name != "FOO" {
		t.Fatalf("missing key should produce one drift entry for FOO; got %+v", drift)
	}
	if drift[0].ApplyFingerprint == drift[0].PlanFingerprint {
		t.Errorf("ApplyFingerprint should differ from PlanFingerprint; got identical %q", drift[0].ApplyFingerprint)
	}
	if drift[0].ApplyFingerprint != unsetFingerprintPlaceholder {
		t.Errorf("ApplyFingerprint should equal unsetFingerprintPlaceholder; got %q", drift[0].ApplyFingerprint)
	}
}

func TestComputeDrift_MatchingFingerprints_NoDrift(t *testing.T) {
	planSnap := map[string]string{"FOO": "abcdef0000000000"}
	applySnap := map[string]string{"FOO": "abcdef0000000000"}
	if drift := ComputeDrift(planSnap, applySnap); len(drift) != 0 {
		t.Errorf("matching fingerprints should produce no drift; got %+v", drift)
	}
}

// TestComputeDrift_ResultIsSortedByName verifies the returned slice is
// stable across map-iteration randomness so callers (logs, JSON marshal,
// test asserts) get deterministic output. Multiple keys + multiple runs
// would each produce a different non-deterministic order without the sort;
// asserting a single canonical order across one call is enough to catch
// regression of the sort.
func TestComputeDrift_ResultIsSortedByName(t *testing.T) {
	planSnap := map[string]string{
		"ZULU":   "ffff000000000000",
		"ALPHA":  "1111000000000000",
		"MIKE":   "8888000000000000",
		"BRAVO":  "2222000000000000",
		"YANKEE": "eeee000000000000",
	}
	applySnap := map[string]string{
		"ZULU":   "f0f0000000000000",
		"ALPHA":  "1010000000000000",
		"MIKE":   "8080000000000000",
		"BRAVO":  "2020000000000000",
		"YANKEE": "e0e0000000000000",
	}
	drift := ComputeDrift(planSnap, applySnap)
	if len(drift) != 5 {
		t.Fatalf("expected 5 drift entries; got %d", len(drift))
	}
	want := []string{"ALPHA", "BRAVO", "MIKE", "YANKEE", "ZULU"}
	for i, w := range want {
		if drift[i].Name != w {
			t.Errorf("drift[%d].Name = %q; want %q (slice should be sorted by Name)", i, drift[i].Name, w)
		}
	}
}
