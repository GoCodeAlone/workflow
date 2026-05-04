package inputsnapshot

import (
	"testing"
)

func TestCompute_FingerprintIs16HexChars(t *testing.T) {
	snap := Compute([]string{"FOO"}, func(k string) (string, bool) {
		return "the-value", true
	})
	if got := snap["FOO"]; len(got) != 16 {
		t.Errorf("fingerprint len = %d, want 16; got %q", len(got), got)
	}
}

func TestCompute_DeterministicAcrossRuns(t *testing.T) {
	env := func(k string) (string, bool) { return "v", true }
	a := Compute([]string{"FOO"}, env)
	b := Compute([]string{"FOO"}, env)
	if a["FOO"] != b["FOO"] {
		t.Errorf("non-deterministic: %q vs %q", a["FOO"], b["FOO"])
	}
}

func TestCompute_DifferentValuesDifferentFingerprints(t *testing.T) {
	env1 := func(k string) (string, bool) { return "value-one", true }
	env2 := func(k string) (string, bool) { return "value-two", true }
	a := Compute([]string{"FOO"}, env1)
	b := Compute([]string{"FOO"}, env2)
	if a["FOO"] == b["FOO"] {
		t.Errorf("fingerprints should differ: %q == %q", a["FOO"], b["FOO"])
	}
}

func TestCompute_MissingEnvVarOmitted(t *testing.T) {
	snap := Compute([]string{"NOT_SET"}, func(k string) (string, bool) {
		return "", false
	})
	if _, ok := snap["NOT_SET"]; ok {
		t.Errorf("missing env should be omitted, got %q", snap["NOT_SET"])
	}
}

func TestNewTolerantEnvProvider_UnsetButPlanned_ReturnsSentinel(t *testing.T) {
	// Use a test-unique env-var name to avoid colliding with anything the
	// process or other tests might rely on; we never set or unset it, so
	// no cleanup is required and there is no cross-test state leak.
	const key = "WFCTL_TEST_INPUTSNAPSHOT_UNSET_KEY"
	plan := map[string]string{key: "deadbeef00000000"}
	provider := NewTolerantEnvProvider(plan)
	val, ok := provider(key)
	if !ok || val != preservedFingerprint {
		t.Errorf("expected (preservedFingerprint, true) for plan-time-set unset-now var; got (%q, %v)", val, ok)
	}
}

func TestCompute_PreservesSentinel(t *testing.T) {
	snap := Compute([]string{"FOO"}, func(name string) (string, bool) {
		return preservedFingerprint, true
	})
	if snap["FOO"] != preservedFingerprint {
		t.Errorf("Compute should pass sentinel through unhashed; got %q", snap["FOO"])
	}
}
