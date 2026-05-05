package platform

import "testing"

// TestConfigHash_Stable_AcrossMapIterationOrder verifies that configHash
// returns the same value on every call for a given config map. Go map
// iteration is deliberately randomised; this test runs 100 iterations to
// expose any hash instability that would cause spurious "update" plan actions
// on successive applies with an unchanged config.
func TestConfigHash_Stable_AcrossMapIterationOrder(t *testing.T) {
	config := map[string]any{
		"engine":   "postgres",
		"size":     "medium",
		"region":   "nyc3",
		"replicas": 3,
		"tags":     map[string]any{"env": "staging", "team": "platform"},
	}
	first := configHash(config)
	if first == "" {
		t.Fatal("configHash returned empty string for non-empty config")
	}
	for i := 0; i < 100; i++ {
		if h := configHash(config); h != first {
			t.Fatalf("iteration %d: got %q, want %q — hash is non-deterministic", i, h, first)
		}
	}
}

// TestConfigHash_EmptyMapReturnsEmpty verifies the zero-value sentinel.
func TestConfigHash_EmptyMapReturnsEmpty(t *testing.T) {
	if got := configHash(nil); got != "" {
		t.Errorf("nil map: want %q, got %q", "", got)
	}
	if got := configHash(map[string]any{}); got != "" {
		t.Errorf("empty map: want %q, got %q", "", got)
	}
}

// TestConfigHash_DifferentConfigsDifferentHashes is a basic sanity check that
// two semantically different configs produce different hashes.
func TestConfigHash_DifferentConfigsDifferentHashes(t *testing.T) {
	a := map[string]any{"engine": "postgres"}
	b := map[string]any{"engine": "mysql"}
	if configHash(a) == configHash(b) {
		t.Error("different configs produced identical hashes")
	}
}
