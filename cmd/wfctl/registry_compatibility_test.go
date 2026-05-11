package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testArchiveSHA256      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testOtherArchiveSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestRegistryCompatibilityUpdateHelp(t *testing.T) {
	output, err := captureStderr(t, func() error {
		return runPluginRegistry([]string{"compatibility", "update", "--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runPluginRegistry compatibility update --help error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"--registry-dir", "--plugin", "--version", "--evidence", "--derive-ranges", "--latest-engine"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestRegistryCompatibilityUpdateWritesStableIndex(t *testing.T) {
	registryDir := prepareCompatibilityRegistry(t, "workflow-plugin-test", "v0.1.0", testArchiveSHA256)
	evPath := writeCompatibilityEvidence(t, registryDir, PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "0.1.0",
		EngineVersion: "0.51.2",
		WfctlVersion:  "0.51.2",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testArchiveSHA256,
		GeneratedBy:   "test",
	})

	if err := runPluginRegistry([]string{
		"compatibility", "update",
		"--registry-dir", registryDir,
		"--plugin", "workflow-plugin-test",
		"--version", "v0.1.0",
		"--evidence", evPath,
	}); err != nil {
		t.Fatalf("compatibility update: %v", err)
	}

	idx := readCompatibilityIndex(t, registryDir, "workflow-plugin-test")
	if idx.Plugin != "workflow-plugin-test" || len(idx.Versions) != 1 {
		t.Fatalf("unexpected index: %#v", idx)
	}
	rec := idx.Versions[0]
	if rec.Version != "v0.1.0" || rec.MinEngineVersion != "v0.50.0" {
		t.Fatalf("unexpected version record: %#v", rec)
	}
	if len(rec.Compatibility) != 1 {
		t.Fatalf("compatibility count = %d, want 1", len(rec.Compatibility))
	}
	ev := rec.Compatibility[0]
	if ev.Plugin != "workflow-plugin-test" || ev.Version != "v0.1.0" || ev.EngineVersion != "v0.51.2" {
		t.Fatalf("evidence not normalized: %#v", ev)
	}
	if ev.EvidenceDigest == "" {
		t.Fatalf("missing evidence digest: %#v", ev)
	}
}

func TestRegistryCompatibilityUpdateRejectsArchiveMismatchAndLeavesIndex(t *testing.T) {
	registryDir := prepareCompatibilityRegistry(t, "workflow-plugin-test", "v0.1.0", testArchiveSHA256)
	indexPath := filepath.Join(registryDir, "compatibility", "workflow-plugin-test", "index.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o750); err != nil {
		t.Fatalf("mkdir index dir: %v", err)
	}
	original := []byte(`{"plugin":"workflow-plugin-test","versions":[]}` + "\n")
	if err := os.WriteFile(indexPath, original, 0o600); err != nil {
		t.Fatalf("write original index: %v", err)
	}
	evPath := writeCompatibilityEvidence(t, registryDir, PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "v0.1.0",
		EngineVersion: "v0.51.2",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testOtherArchiveSHA256,
	})

	err := runPluginRegistry([]string{
		"compatibility", "update",
		"--registry-dir", registryDir,
		"--plugin", "workflow-plugin-test",
		"--version", "v0.1.0",
		"--evidence", evPath,
	})
	if err == nil {
		t.Fatal("expected archive mismatch error")
	}
	if !strings.Contains(err.Error(), "archiveSHA256") {
		t.Fatalf("error = %v, want archiveSHA256 context", err)
	}
	got, readErr := os.ReadFile(indexPath)
	if readErr != nil {
		t.Fatalf("read index: %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("index changed after failure:\n%s", got)
	}
}

func TestRegistryCompatibilityUpdateSortsVersionsEvidenceAndMarksStale(t *testing.T) {
	registryDir := prepareCompatibilityRegistry(t, "workflow-plugin-test", "v0.2.0", testArchiveSHA256)
	writeInitialCompatibilityIndex(t, registryDir, PluginVersionIndex{
		Plugin: "workflow-plugin-test",
		Versions: []PluginVersionRecord{{
			Version: "v0.1.0",
			Compatibility: []PluginCompatibilityEvidence{{
				Plugin:        "workflow-plugin-test",
				Version:       "v0.1.0",
				EngineVersion: "v0.51.0",
				Mode:          PluginCompatibilityModeTypedIaC,
				Status:        PluginCompatibilityStatusPass,
				OS:            "linux",
				Arch:          "amd64",
			}},
		}},
	})
	ev1 := writeCompatibilityEvidenceNamed(t, registryDir, "ev1.json", PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "v0.2.0",
		EngineVersion: "v0.51.2",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "linux",
		Arch:          "amd64",
		ArchiveSHA256: testArchiveSHA256,
	})
	ev2 := writeCompatibilityEvidenceNamed(t, registryDir, "ev2.json", PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "v0.2.0",
		EngineVersion: "v0.51.1",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testArchiveSHA256,
	})

	if err := runPluginRegistry([]string{
		"compatibility", "update",
		"--registry-dir", registryDir,
		"--plugin", "workflow-plugin-test",
		"--version", "v0.2.0",
		"--evidence", ev1,
		"--evidence", ev2,
		"--latest-engine", "v0.51.3",
	}); err != nil {
		t.Fatalf("compatibility update: %v", err)
	}

	idx := readCompatibilityIndex(t, registryDir, "workflow-plugin-test")
	if len(idx.Versions) != 2 || idx.Versions[0].Version != "v0.2.0" || idx.Versions[1].Version != "v0.1.0" {
		t.Fatalf("versions not sorted descending: %#v", idx.Versions)
	}
	if !idx.EvidencePolicy.Stale || idx.EvidencePolicy.LatestEngine != "v0.51.3" {
		t.Fatalf("stale policy not set: %#v", idx.EvidencePolicy)
	}
	if got := idx.Versions[0].Compatibility[0]; got.EngineVersion != "v0.51.1" || got.OS != "darwin" {
		t.Fatalf("evidence not sorted/attached: %#v", got)
	}
}

func TestRegistryCompatibilityUpdateDerivesPassRange(t *testing.T) {
	registryDir := prepareCompatibilityRegistry(t, "workflow-plugin-test", "v0.1.0", testArchiveSHA256)
	ev1 := writeCompatibilityEvidenceNamed(t, registryDir, "ev1.json", PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "v0.1.0",
		EngineVersion: "v0.51.1",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testArchiveSHA256,
	})
	ev2 := writeCompatibilityEvidenceNamed(t, registryDir, "ev2.json", PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "v0.1.0",
		EngineVersion: "v0.51.2",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testArchiveSHA256,
	})
	if err := runPluginRegistry([]string{
		"compatibility", "update",
		"--registry-dir", registryDir,
		"--plugin", "workflow-plugin-test",
		"--version", "v0.1.0",
		"--evidence", ev1,
		"--evidence", ev2,
		"--derive-ranges",
	}); err != nil {
		t.Fatalf("compatibility update: %v", err)
	}
	idx := readCompatibilityIndex(t, registryDir, "workflow-plugin-test")
	foundRange := false
	for _, ev := range idx.Versions[0].Compatibility {
		if ev.CompatibleEngineRange != nil {
			foundRange = true
			if ev.CompatibleEngineRange.Min != "v0.51.1" || ev.CompatibleEngineRange.Max != "v0.51.2" {
				t.Fatalf("unexpected range: %#v", ev.CompatibleEngineRange)
			}
		}
	}
	if !foundRange {
		t.Fatalf("missing derived range: %#v", idx.Versions[0].Compatibility)
	}
}

func prepareCompatibilityRegistry(t *testing.T, plugin, version, archiveSHA string) string {
	t.Helper()
	dir := t.TempDir()
	writeManifest(t, dir, plugin, version, archiveSHA)
	return dir
}

func writeManifest(t *testing.T, registryDir, plugin, version, archiveSHA string) {
	t.Helper()
	manifest := RegistryManifest{
		Name:             plugin,
		Version:          version,
		Author:           "workflow",
		Description:      "test plugin",
		Type:             "external",
		Tier:             "community",
		MinEngineVersion: "v0.50.0",
		Downloads: []PluginDownload{{
			OS:     "darwin",
			Arch:   "arm64",
			URL:    "https://example.invalid/plugin.tar.gz",
			SHA256: archiveSHA,
		}, {
			OS:     "linux",
			Arch:   "amd64",
			URL:    "https://example.invalid/plugin-linux.tar.gz",
			SHA256: archiveSHA,
		}},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(registryDir, "plugins", plugin, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeCompatibilityEvidence(t *testing.T, registryDir string, ev PluginCompatibilityEvidence) string {
	t.Helper()
	return writeCompatibilityEvidenceNamed(t, registryDir, "evidence.json", ev)
}

func writeCompatibilityEvidenceNamed(t *testing.T, registryDir, name string, ev PluginCompatibilityEvidence) string {
	t.Helper()
	normalized, err := ValidateCompatibilityEvidence(ev)
	if err != nil {
		t.Fatalf("validate evidence: %v", err)
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	path := filepath.Join(registryDir, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	return path
}

func writeInitialCompatibilityIndex(t *testing.T, registryDir string, idx PluginVersionIndex) {
	t.Helper()
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	path := filepath.Join(registryDir, "compatibility", idx.Plugin, "index.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir index dir: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

func readCompatibilityIndex(t *testing.T, registryDir, plugin string) PluginVersionIndex {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(registryDir, "compatibility", plugin, "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var idx PluginVersionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	return idx
}
