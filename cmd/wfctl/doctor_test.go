package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestDoctorReportsStaleLockfile(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{pluginInstalled: true})
	lock, err := config.LoadWfctlLockfile(fixture.lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	lock.SourceManifestSHA256 = "not-the-current-manifest"
	if err := config.SaveWfctlLockfile(fixture.lockPath, lock); err != nil {
		t.Fatalf("save stale lockfile: %v", err)
	}

	var out bytes.Buffer
	err = runDoctorWithOutput(fixture.args(), &out)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	for _, want := range []string{"WARN", "lockfile is stale", "wfctl plugin lock"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorReportsMissingLockedPlugin(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{})

	var out bytes.Buffer
	err := runDoctorWithOutput(fixture.args(), &out)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	for _, want := range []string{"WARN", "workflow-plugin-auth", "not installed", "wfctl plugin install"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorJSONOutput(t *testing.T) {
	fixture := writeDoctorFixture(t, doctorFixtureOptions{})

	var out bytes.Buffer
	err := runDoctorWithOutput(append(fixture.args(), "--format", "json"), &out)
	if err != nil {
		t.Fatalf("doctor json: %v\n%s", err, out.String())
	}
	var report struct {
		Status   string `json:"status"`
		Sections []struct {
			Name   string `json:"name"`
			Checks []struct {
				Status string `json:"status"`
				Fix    string `json:"fix,omitempty"`
			} `json:"checks"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid JSON: %v\n%s", err, out.String())
	}
	if report.Status != "WARN" {
		t.Fatalf("status = %q, want WARN; report=%+v", report.Status, report)
	}
	if len(report.Sections) == 0 || len(report.Sections[0].Checks) == 0 {
		t.Fatalf("expected sections and checks in report: %+v", report)
	}
}

func TestDoctorOnlineReportsUpdateAvailable(t *testing.T) {
	origVersion := version
	version = "v1.0.0"
	defer func() { version = origVersion }()

	fixture := writeDoctorFixture(t, doctorFixtureOptions{pluginInstalled: true})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName: "v9.9.9",
			HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v9.9.9",
		})
	}))
	defer srv.Close()
	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	var out bytes.Buffer
	err := runDoctorWithOutput(append(fixture.args(), "--online"), &out)
	if err != nil {
		t.Fatalf("doctor online: %v\n%s", err, out.String())
	}
	for _, want := range []string{"WARN", "Update available", "wfctl update --check"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorHealthyProject(t *testing.T) {
	origVersion := version
	version = "v1.2.3"
	defer func() { version = origVersion }()

	fixture := writeDoctorFixture(t, doctorFixtureOptions{pluginInstalled: true})

	var out bytes.Buffer
	err := runDoctorWithOutput(fixture.args(), &out)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Overall: OK") {
		t.Fatalf("expected healthy doctor output, got:\n%s", out.String())
	}
}

func TestDoctorCommandWiring(t *testing.T) {
	if commands["doctor"] == nil {
		t.Fatal("commands map is missing doctor")
	}
	configText := string(wfctlConfigBytes)
	for _, want := range []string{"name: doctor", "cmd-doctor", "command: doctor"} {
		if !strings.Contains(configText, want) {
			t.Fatalf("embedded wfctl config missing %q", want)
		}
	}
}

type doctorFixtureOptions struct {
	pluginInstalled bool
}

type doctorFixture struct {
	workflowPath string
	manifestPath string
	lockPath     string
	pluginDir    string
}

func (f doctorFixture) args() []string {
	return []string{
		"--workflow", f.workflowPath,
		"--manifest", f.manifestPath,
		"--lock-file", f.lockPath,
		"--plugin-dir", f.pluginDir,
	}
}

func writeDoctorFixture(t *testing.T, opts doctorFixtureOptions) doctorFixture {
	t.Helper()
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte("modules: []\n"), 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	manifest := &config.WfctlManifest{
		Version: 1,
		Plugins: []config.WfctlPluginEntry{{
			Name:    "workflow-plugin-auth",
			Version: "v1.2.3",
			Source:  "github.com/example/workflow-plugin-auth",
		}},
	}
	if err := config.SaveWfctlManifest(manifestPath, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	lock := &config.WfctlLockfile{
		Version: 1,
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/example/workflow-plugin-auth",
			},
		},
	}
	if err := config.PopulateWfctlLockfileProvenance(manifest, lock); err != nil {
		t.Fatalf("populate lock provenance: %v", err)
	}
	if err := config.SaveWfctlLockfile(lockPath, lock); err != nil {
		t.Fatalf("save lockfile: %v", err)
	}
	pluginDir := filepath.Join(dir, "data", "plugins")
	if opts.pluginInstalled {
		installDir := filepath.Join(pluginDir, "auth")
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			t.Fatalf("mkdir installed plugin: %v", err)
		}
		pluginJSON := `{"name":"workflow-plugin-auth","version":"v1.2.3","description":"Auth plugin"}`
		if err := os.WriteFile(filepath.Join(installDir, "plugin.json"), []byte(pluginJSON), 0o600); err != nil {
			t.Fatalf("write plugin.json: %v", err)
		}
	}
	return doctorFixture{
		workflowPath: workflowPath,
		manifestPath: manifestPath,
		lockPath:     lockPath,
		pluginDir:    pluginDir,
	}
}
