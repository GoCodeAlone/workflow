package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWfctlLockfile_RoundTrip(t *testing.T) {
	lf := WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Plugins: map[string]WfctlLockPluginEntry{
			"workflow-plugin-digitalocean": {
				Version: "v0.7.6",
				Source:  "github.com/GoCodeAlone/workflow-plugin-digitalocean",
				SHA256:  "abc123",
				Platforms: map[string]WfctlLockPlatform{
					"linux-amd64": {
						URL:    "https://github.com/GoCodeAlone/workflow-plugin-digitalocean/releases/download/v0.7.6/plugin-linux-amd64.tar.gz",
						SHA256: "def456",
					},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, ".wfctl-lock.yaml")
	if err := SaveWfctlLockfile(path, &lf); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadWfctlLockfile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("version = %d, want 1", loaded.Version)
	}
	entry, ok := loaded.Plugins["workflow-plugin-digitalocean"]
	if !ok {
		t.Fatal("plugin entry missing after round-trip")
	}
	if entry.Version != "v0.7.6" {
		t.Errorf("version = %q, want v0.7.6", entry.Version)
	}
	if entry.SHA256 != "abc123" {
		t.Errorf("sha256 = %q, want abc123", entry.SHA256)
	}
	plat, ok := entry.Platforms["linux-amd64"]
	if !ok {
		t.Fatal("platform linux-amd64 missing")
	}
	if plat.SHA256 != "def456" {
		t.Errorf("platform sha256 = %q, want def456", plat.SHA256)
	}
}

func TestWfctlLockfile_DeterministicOutput(t *testing.T) {
	// Two lockfiles with identical content should produce byte-identical YAML.
	mkLockfile := func() WfctlLockfile {
		return WfctlLockfile{
			Version:     1,
			GeneratedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
			Plugins: map[string]WfctlLockPluginEntry{
				"b-plugin": {Version: "v1.0.0", Source: "github.com/b/b", SHA256: "bbb"},
				"a-plugin": {Version: "v2.0.0", Source: "github.com/a/a", SHA256: "aaa"},
			},
		}
	}
	dir := t.TempDir()
	p1 := filepath.Join(dir, "lock1.yaml")
	p2 := filepath.Join(dir, "lock2.yaml")
	if err := SaveWfctlLockfile(p1, func() *WfctlLockfile { l := mkLockfile(); return &l }()); err != nil {
		t.Fatal(err)
	}
	if err := SaveWfctlLockfile(p2, func() *WfctlLockfile { l := mkLockfile(); return &l }()); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output:\n--- lock1 ---\n%s\n--- lock2 ---\n%s", b1, b2)
	}
	// Also assert plugin keys appear in alphabetical order.
	idx_a := strings.Index(string(b1), "a-plugin")
	idx_b := strings.Index(string(b1), "b-plugin")
	if idx_a > idx_b {
		t.Errorf("expected a-plugin before b-plugin in output, got a@%d b@%d", idx_a, idx_b)
	}
}

func TestWfctlLockfile_NotFound(t *testing.T) {
	_, err := LoadWfctlLockfile("/nonexistent/.wfctl-lock.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}
