package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRepairDryRunPlansLockThenInstall(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{})
	lock, err := config.LoadWfctlLockfile(fixture.lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	lock.SourceManifestSHA256 = "stale"
	if err := config.SaveWfctlLockfile(fixture.lockPath, lock); err != nil {
		t.Fatalf("save stale lockfile: %v", err)
	}

	var out bytes.Buffer
	if err := runRepairWithOutput(fixture.args(), &out); err != nil {
		t.Fatalf("repair dry-run: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"wfctl repair", "Mode: DRY-RUN", "wfctl plugin lock", "wfctl plugin install"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repair output missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "wfctl plugin lock") > strings.Index(text, "wfctl plugin install") {
		t.Fatalf("expected lock before install:\n%s", text)
	}
}

func TestRepairMissingManifestIsSuggestionOnly(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte("modules: []\n"), 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var out bytes.Buffer
	err := runRepairWithOutput([]string{
		"--workflow", workflowPath,
		"--manifest", filepath.Join(dir, "wfctl.yaml"),
		"--lock-file", filepath.Join(dir, ".wfctl-lock.yaml"),
		"--plugin-dir", filepath.Join(dir, "data", "plugins"),
	}, &out)
	if err != nil {
		t.Fatalf("repair missing manifest: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"No automatic repairs available", "plugin manifest", "wfctl plugin add"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repair output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "wfctl plugin lock") || strings.Contains(text, "wfctl plugin install") {
		t.Fatalf("missing manifest should not plan plugin mutation:\n%s", text)
	}
}

func TestRepairApplyRunsLockThenInstall(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{})
	lock, err := config.LoadWfctlLockfile(fixture.lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	lock.SourceManifestSHA256 = "stale"
	if err := config.SaveWfctlLockfile(fixture.lockPath, lock); err != nil {
		t.Fatalf("save stale lockfile: %v", err)
	}

	runner := &recordingRepairRunner{}
	var out bytes.Buffer
	if err := runRepairWithRunner(append(fixture.args(), "--apply"), &out, runner); err != nil {
		t.Fatalf("repair apply: %v\n%s", err, out.String())
	}
	if got, want := strings.Join(runner.calls, ","), "lock,install"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	lockArgs := strings.Join(runner.lockArgs, " ")
	for _, want := range []string{"--config " + fixture.workflowPath, "--manifest " + fixture.manifestPath, "--lock-file " + fixture.lockPath} {
		if !strings.Contains(lockArgs, want) {
			t.Fatalf("lock args missing %q: %v", want, runner.lockArgs)
		}
	}
	installArgs := strings.Join(runner.installArgs, " ")
	for _, want := range []string{"--manifest " + fixture.manifestPath, "--lock-file " + fixture.lockPath, "--plugin-dir " + fixture.pluginDir} {
		if !strings.Contains(installArgs, want) {
			t.Fatalf("install args missing %q: %v", want, runner.installArgs)
		}
	}
	if !strings.Contains(out.String(), "Mode: APPLY") {
		t.Fatalf("apply output missing mode:\n%s", out.String())
	}
}

func TestRepairJSONOutput(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{})
	var out bytes.Buffer
	if err := runRepairWithOutput(append(fixture.args(), "--format", "json"), &out); err != nil {
		t.Fatalf("repair json: %v\n%s", err, out.String())
	}
	var report struct {
		Mode    string `json:"mode"`
		Status  string `json:"status"`
		Actions []struct {
			Kind    string `json:"kind"`
			Command string `json:"command,omitempty"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("repair output is not valid JSON: %v\n%s", err, out.String())
	}
	if report.Mode != "dry-run" || report.Status != "planned" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.Actions) == 0 || report.Actions[0].Command == "" {
		t.Fatalf("expected executable repair action: %+v", report)
	}
}

func TestRepairCommandWiring(t *testing.T) {
	if commands["repair"] == nil {
		t.Fatal("commands map is missing repair")
	}
	configText := string(wfctlConfigBytes)
	for _, want := range []string{"name: repair", "cmd-repair", "command: repair"} {
		if !strings.Contains(configText, want) {
			t.Fatalf("embedded wfctl config missing %q", want)
		}
	}
}

type recordingRepairRunner struct {
	calls       []string
	lockArgs    []string
	installArgs []string
}

func (r *recordingRepairRunner) PluginLock(args []string) error {
	r.calls = append(r.calls, "lock")
	r.lockArgs = append([]string(nil), args...)
	return nil
}

func (r *recordingRepairRunner) PluginInstall(args []string) error {
	r.calls = append(r.calls, "install")
	r.installArgs = append([]string(nil), args...)
	return nil
}
