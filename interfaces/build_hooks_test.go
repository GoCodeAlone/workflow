package interfaces_test

import (
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestHookEvent_AllValidConstants(t *testing.T) {
	validEvents := []interfaces.HookEvent{
		interfaces.HookEventPreBuild,
		interfaces.HookEventPreTargetBuild,
		interfaces.HookEventPostTargetBuild,
		interfaces.HookEventPreContainerBuild,
		interfaces.HookEventPostContainerBuild,
		interfaces.HookEventPreContainerPush,
		interfaces.HookEventPostContainerPush,
		interfaces.HookEventPreArtifactsPublish,
		interfaces.HookEventPostArtifactsPublish,
		interfaces.HookEventPreBuildFail,
		interfaces.HookEventPostBuild,
		interfaces.HookEventInstallVerify,
	}
	for _, ev := range validEvents {
		if !interfaces.IsValidHookEvent(string(ev)) {
			t.Errorf("valid event %q returned false from IsValidHookEvent", ev)
		}
	}
}

func TestIsValidHookEvent_Bogus(t *testing.T) {
	bogus := []string{"", "bogus", "PRE_BUILD", "pre-build", "install_verify_extra"}
	for _, s := range bogus {
		if interfaces.IsValidHookEvent(s) {
			t.Errorf("bogus event %q should return false", s)
		}
	}
}

func TestHookEvent_Count(t *testing.T) {
	all := interfaces.AllHookEvents()
	if len(all) != 12 {
		t.Errorf("expected 12 hook events, got %d", len(all))
	}
}

func TestHookPayload_Marshaling(t *testing.T) {
	p := interfaces.HookPayload{
		Event:     interfaces.HookEventPostBuild,
		Plugin:    "supply-chain",
		BuildID:   "build-123",
		Timestamp: 1700000000,
		Data: map[string]any{
			"image_ref":   "registry.example.com/myapp:latest",
			"digest":      "sha256:abc123",
			"duration_ms": int64(5000),
		},
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got interfaces.HookPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != p.Event {
		t.Errorf("Event: got %q, want %q", got.Event, p.Event)
	}
	if got.BuildID != p.BuildID {
		t.Errorf("BuildID: got %q, want %q", got.BuildID, p.BuildID)
	}
}

func TestInstallVerifyPayload_Marshaling(t *testing.T) {
	p := interfaces.InstallVerifyPayload{
		TarballPath:               "/tmp/plugin.tar.gz",
		ExpectedSignatureIdentity: "https://github.com/example/.github/workflows/release.yml",
		VulnPolicy:                "block-critical",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got interfaces.InstallVerifyPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TarballPath != p.TarballPath {
		t.Errorf("TarballPath: got %q, want %q", got.TarballPath, p.TarballPath)
	}
	if got.VulnPolicy != p.VulnPolicy {
		t.Errorf("VulnPolicy: got %q, want %q", got.VulnPolicy, p.VulnPolicy)
	}
}
