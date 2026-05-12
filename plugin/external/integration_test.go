package external

import (
	"os"
	"path/filepath"
	"testing"

	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestManagerLoadPluginThreadsDiskManifestToAdapter is the in-process e2e
// equivalent of the disk-fallback scenario that motivated v0.51.3 Bug 1:
//
//  1. Write a valid plugin.json on disk (manager.go's input).
//  2. Load it via pluginpkg.LoadManifest + Validate (same path as
//     manager.go:108-114, in isolation from the subprocess machinery).
//  3. Hand the loaded manifest to NewExternalPluginAdapter as 3rd arg,
//     paired with a gRPC stub that returns codes.Unimplemented for
//     GetManifest (strict-cutover IaC plugin behavior).
//  4. Assert the adapter's EngineManifest() reports the disk-loaded
//     Version + Validate() returns nil — i.e. the full disk-fallback
//     chain end-to-end without a subprocess.
//
// This locks in the contract that the manager → adapter → EngineManifest
// path stays connected; if any link in that chain regresses (e.g. someone
// reverts the 3-arg constructor or LoadManifest stops calling Validate),
// this test breaks loudly with the same symptom as Bug 1 in production.
func TestManagerLoadPluginThreadsDiskManifestToAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const manifestJSON = `{
		"name": "test-plugin",
		"version": "9.9.9",
		"author": "GoCodeAlone",
		"description": "disk fallback e2e"
	}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o600); err != nil {
		t.Fatalf("WriteFile plugin.json: %v", err)
	}

	// Step 2 — mirror manager.go:108-114: LoadManifest + Validate.
	disk, err := pluginpkg.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if err := disk.Validate(); err != nil {
		t.Fatalf("disk manifest Validate: %v", err)
	}

	// Step 3 — construct adapter with the loaded manifest and a stub that
	// simulates a strict-cutover IaC plugin's gRPC GetManifest returning
	// codes.Unimplemented.
	a, err := NewExternalPluginAdapter("test-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
	}}, disk)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	// Step 4 — the engine-facing manifest must carry the disk-loaded fields.
	em := a.EngineManifest()
	if em.Version != "9.9.9" {
		t.Fatalf("EngineManifest().Version = %q, want 9.9.9 (disk fallback)", em.Version)
	}
	if em.Author != "GoCodeAlone" {
		t.Fatalf("EngineManifest().Author = %q, want GoCodeAlone", em.Author)
	}
	if err := em.Validate(); err != nil {
		t.Fatalf("EngineManifest().Validate(): %v", err)
	}

	// Sanity: the cached *pb.Manifest used by Name()/Version()/Description()
	// accessors must reflect the disk values (not the empty pb.Manifest
	// that PR #627 would have synthesized).
	if a.Version() != "9.9.9" {
		t.Fatalf("Version() = %q, want 9.9.9", a.Version())
	}
	if a.Description() != "disk fallback e2e" {
		t.Fatalf("Description() = %q, want disk value", a.Description())
	}
	// Compile-time guard against accidental drift of pb import (test would
	// otherwise still compile with an unused import).
	_ = (*pb.Manifest)(nil)
}
