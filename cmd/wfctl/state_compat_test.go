package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestStateFileCompat_v0_14_2_to_v1_0_0 asserts that wfctl v1.0.0 reads
// state files written by v0.14.2 cleanly — the strict-contracts force-
// cutover changes the wfctl <-> plugin gRPC envelope but MUST NOT
// change the on-disk JSON state-file format. This is the cycle 1 I-4
// pre-flight that catches the cascade-block risk for PR 5 (operator
// pin bumps) BEFORE that PR merges.
//
// Per Task 31 of the strict-contracts force-cutover plan:
//
//   - The fixture (test/fixtures/state-v0.14.2.json) is intended to be
//     replaced by a real production state snapshot during PR 4 prep
//     (operator copies coredump-staging/iac-state/state.json from the
//     Spaces backend; see plan §Step 1). The synthetic fixture
//     committed to the repo represents the v0.14.2 schema shape so the
//     test can run in CI; the operator-captured replacement provides
//     the real-world fidelity check.
//
//   - The test reads the fixture via the v1.0.0 wfctl iacStateRecord
//     decoder (the same path loadFSState / fsWfctlStateStore.ListResources
//     use), then converts it via iacRecordToResourceState and asserts every
//     load-bearing field survived.
//
// If this test FAILS in CI: PR 5 cascade-block surfaces. Plan response
// (per Task 31 §If FAIL):
//   - File a separate workflow PR (feat: state-file v0.14.2 compat
//     shim) that adds the compatibility layer.
//   - Hold PR 5 (and consequently PRs 6–9) until the shim PR merges.
//   - Document the gap in PR 4's CHANGELOG.
func TestStateFileCompat_v0_14_2_to_v1_0_0(t *testing.T) {
	path := fixturePath(t, "state-v0.14.2.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("fixture is not valid JSON: %s", path)
	}

	var record iacStateRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("unmarshal v0.14.2 fixture into v1.0.0 iacStateRecord: %v", err)
	}

	if record.ResourceID == "" {
		t.Fatalf("v0.14.2 fixture has empty resource_id (schema regression?)")
	}
	if record.ResourceType == "" {
		t.Fatalf("v0.14.2 fixture has empty resource_type (schema regression?)")
	}

	// Convert via the same path loadCurrentState uses on the v1.0.0
	// read pipeline. Any panics, type assertions, or schema mismatches
	// surface as test failures here.
	state := iacRecordToResourceState(record)
	if state.ID != record.ResourceID {
		t.Errorf("ID = %q; want %q", state.ID, record.ResourceID)
	}
	if state.Type != record.ResourceType {
		t.Errorf("Type = %q; want %q", state.Type, record.ResourceType)
	}
	if state.Provider != record.Provider {
		t.Errorf("Provider = %q; want %q", state.Provider, record.Provider)
	}
	// MINOR-1: strict-equality on ProviderID rather than non-empty.
	// Non-empty would pass silently if `provider_id` stops decoding,
	// because iacRecordToResourceState falls back to ResourceID.
	// The fixture provider_id is DO00FIXTURE1234567890; pin it.
	if state.ProviderID != record.ProviderID {
		t.Errorf("ProviderID = %q; want %q (silent fallback to ResourceID indicates "+
			"provider_id decode regression)", state.ProviderID, record.ProviderID)
	}
	// MINOR-2: nil-checks on AppliedConfig / Outputs would pass when
	// the maps decoded as empty {} (silent map-content drop). Pin to
	// at least one known fixture key per map.
	if state.AppliedConfig == nil {
		t.Errorf("AppliedConfig is nil after decode (config map dropped during conversion)")
	} else if got, ok := state.AppliedConfig["name"].(string); !ok || got != "iac-state-spaces-key" {
		t.Errorf("AppliedConfig[\"name\"] = %v; want %q (config-map content drop?)",
			state.AppliedConfig["name"], "iac-state-spaces-key")
	}
	if state.Outputs == nil {
		t.Errorf("Outputs is nil after decode (outputs map dropped during conversion)")
	} else if got, ok := state.Outputs["access_key"].(string); !ok || got == "" {
		t.Errorf("Outputs[\"access_key\"] = %v; want non-empty (outputs-map content drop?)",
			state.Outputs["access_key"])
	}
}

// TestStateFileCompat_v0_14_2_NoUnknownFieldsLost guards against the
// regression where v0.14.2 produced fields that v1.0.0's decoder
// silently drops. Uses json.Decoder with DisallowUnknownFields so an
// unexpected v0.14.2 field surfaces as a test failure naming it.
//
// Note: this test is INTENTIONALLY allowed to skip the fixture's
// _fixture_metadata header (synthetic-fixture annotation, not a
// production v0.14.2 field) by reading the bytes through a thin
// pre-processor that strips that field before strict decoding. When
// the operator replaces the fixture with a real v0.14.2 capture, the
// metadata header pre-process becomes a no-op and the strict decode
// runs against the real wire bytes.
func TestStateFileCompat_v0_14_2_NoUnknownFieldsLost(t *testing.T) {
	path := fixturePath(t, "state-v0.14.2.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	delete(raw, "_fixture_metadata")
	stripped, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal stripped: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(stripped))
	dec.DisallowUnknownFields()
	var record iacStateRecord
	if err := dec.Decode(&record); err != nil {
		t.Fatalf("v1.0.0 strict decode of v0.14.2 fixture failed — schema regression: %v", err)
	}
}

// fixturePath resolves the absolute path to a fixture under
// test/fixtures/<name>, walking up from the package's runtime
// directory until it finds a "test/fixtures" sibling. This avoids
// hardcoding a relative path that breaks under `go test ./...` from
// repo root vs from the package dir.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed; cannot locate fixture %q", name)
	}
	dir := filepath.Dir(here)
	for {
		candidate := filepath.Join(dir, "test", "fixtures", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find test/fixtures/%s walking up from %s", name, filepath.Dir(here))
		}
		dir = parent
	}
}
