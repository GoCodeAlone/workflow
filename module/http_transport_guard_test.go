package module

import (
	"net/http"
	"testing"
)

func TestSnapshotDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	snapshot := SnapshotDefaultTransport()
	if snapshot != original {
		t.Errorf("snapshot should equal http.DefaultTransport at call time")
	}
}

func TestDetectTransportMutation_NoChange(t *testing.T) {
	snapshot := SnapshotDefaultTransport()
	if DetectTransportMutation(snapshot) {
		t.Error("expected no mutation when transport is unchanged")
	}
}

func TestDetectTransportMutation_Changed(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	snapshot := SnapshotDefaultTransport()
	http.DefaultTransport = &http.Transport{} // simulate rogue plugin mutation
	if !DetectTransportMutation(snapshot) {
		t.Error("expected mutation to be detected")
	}
}

func TestRestoreDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	snapshot := SnapshotDefaultTransport()
	http.DefaultTransport = &http.Transport{} // mutate
	RestoreDefaultTransport(snapshot)

	if http.DefaultTransport != original {
		t.Error("RestoreDefaultTransport did not restore the original transport")
	}
	if DetectTransportMutation(snapshot) {
		t.Error("DetectTransportMutation should return false after restore")
	}
}

func TestTransportGuardCycle(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	// Simulate the guard pattern used in engine.go:
	// snapshot → plugin runs → detect → restore
	snapshot := SnapshotDefaultTransport()
	rogue := &http.Transport{}
	http.DefaultTransport = rogue // rogue plugin mutation

	if !DetectTransportMutation(snapshot) {
		t.Fatal("mutation should be detected")
	}
	RestoreDefaultTransport(snapshot)
	if http.DefaultTransport == rogue {
		t.Error("transport should have been restored, not left as rogue")
	}
	if DetectTransportMutation(snapshot) {
		t.Error("after restore, no mutation should be detected")
	}
}
