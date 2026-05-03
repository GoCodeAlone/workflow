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
