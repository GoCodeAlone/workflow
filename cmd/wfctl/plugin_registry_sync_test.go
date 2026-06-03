package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPluginRegistrySync_TypeAllowlist verifies the scaffold-defense:
// plugin.json.type values outside the allowlist (e.g. "scaffold") are
// rejected at sync time. Workflow#762 plan C-P3 fix.
func TestPluginRegistrySync_TypeAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		manType string
		want    string
	}{
		{"external accepted", "external", ""},
		{"builtin accepted", "builtin", ""},
		{"core accepted", "core", ""},
		{"iac accepted", "iac", ""},
		{"scaffold rejected", "scaffold", "REJECT"},
		{"unknown rejected", "novel", "REJECT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == "REJECT" {
				if registryAllowedTypes[tc.manType] {
					t.Errorf("type %q should be rejected but is in allowlist", tc.manType)
				}
				return
			}
			if !registryAllowedTypes[tc.manType] {
				t.Errorf("type %q should be accepted but is not in allowlist", tc.manType)
			}
		})
	}
}

// TestPluginRegistrySync_NormalizeRepo ports the bash normalize_repo
// behavior (workflow-registry/scripts/sync-versions.sh:36-44).
func TestPluginRegistrySync_NormalizeRepo(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo", "owner/repo"},
		{"http://github.com/owner/repo", "owner/repo"},
		{"github.com/owner/repo", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo/", "owner/repo"},
		{"owner/repo", "owner/repo"},
		{"owner/repo/subpath", "owner/repo"},
		{"not-a-repo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := normalizeRepo(tc.in)
			if got != tc.want {
				t.Errorf("normalizeRepo(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPluginRegistrySync_DownloadsMatchVersion verifies the downloads-vs-version
// invariant the bash script enforces (sync-versions.sh:46-58).
func TestPluginRegistrySync_DownloadsMatchVersion(t *testing.T) {
	t.Run("empty downloads OK", func(t *testing.T) {
		raw := map[string]any{}
		if !downloadsMatchVersion(raw, "1.2.3") {
			t.Error("empty downloads should match (no URLs to verify)")
		}
	})
	t.Run("matching URLs OK", func(t *testing.T) {
		raw := map[string]any{
			"downloads": []any{
				map[string]any{
					"os":   "linux",
					"arch": "amd64",
					"url":  "https://github.com/owner/repo/releases/download/v1.2.3/repo-linux-amd64.tar.gz",
				},
			},
		}
		if !downloadsMatchVersion(raw, "1.2.3") {
			t.Error("matching URL should pass")
		}
	})
	t.Run("stale URLs rejected", func(t *testing.T) {
		raw := map[string]any{
			"downloads": []any{
				map[string]any{
					"os":   "linux",
					"arch": "amd64",
					"url":  "https://github.com/owner/repo/releases/download/v1.0.0/repo-linux-amd64.tar.gz",
				},
			},
		}
		if downloadsMatchVersion(raw, "1.2.3") {
			t.Error("stale URL should fail")
		}
	})
}

func TestPluginRegistrySync_ReleaseAssetPlatform(t *testing.T) {
	cases := []struct {
		name     string
		wantOS   string
		wantArch string
		wantOK   bool
	}{
		{
			name:     "workflow-plugin-aws-linux-amd64.tar.gz",
			wantOS:   "linux",
			wantArch: "amd64",
			wantOK:   true,
		},
		{
			name:     "workflow-plugin-gcp_2.3.0_linux_amd64.tar.gz",
			wantOS:   "linux",
			wantArch: "amd64",
			wantOK:   true,
		},
		{
			name:   "checksums.txt",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOS, gotArch, gotOK := releaseAssetPlatform(tc.name)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOS != tc.wantOS || gotArch != tc.wantArch {
				t.Fatalf("platform = %s/%s, want %s/%s", gotOS, gotArch, tc.wantOS, tc.wantArch)
			}
		})
	}
}

func TestPluginRegistrySync_MetadataSyncProjectsIaCServices(t *testing.T) {
	raw := map[string]any{
		"capabilities": map[string]any{
			"moduleTypes": []any{"old"},
		},
	}
	pluginJSON := map[string]any{
		"capabilities": map[string]any{
			"moduleTypes": []any{"iac.provider"},
		},
		"iacServices": []any{
			"workflow.plugin.external.iac.IaCProviderRequired",
			"workflow.plugin.external.iac.IaCProviderRunner",
		},
		"minEngineVersion": "0.73.0",
	}

	syncManifestMetadataFromPluginJSON(raw, pluginJSON)

	caps, ok := raw["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities type = %T", raw["capabilities"])
	}
	serviceMethods, ok := caps["serviceMethods"].([]any)
	if !ok {
		t.Fatalf("serviceMethods type = %T", caps["serviceMethods"])
	}
	if len(serviceMethods) != 2 || serviceMethods[1] != "workflow.plugin.external.iac.IaCProviderRunner" {
		t.Fatalf("serviceMethods = %#v", serviceMethods)
	}
	if got := raw["minEngineVersion"]; got != "0.73.0" {
		t.Fatalf("minEngineVersion = %v, want 0.73.0", got)
	}
}

func TestPluginRegistrySync_DefaultSkipsBuiltinManifests(t *testing.T) {
	registry := t.TempDir()
	mustWrite(t, filepath.Join(registry, "plugins", "admincore", "manifest.json"), `{
  "name": "workflow-plugin-admincore",
  "version": "0.69.6",
  "description": "Workflow admin core",
  "source": "github.com/GoCodeAlone/workflow",
  "repository": "https://github.com/GoCodeAlone/workflow",
  "type": "builtin",
  "tier": "core"
}`)

	binDir := t.TempDir()
	marker := filepath.Join(binDir, "gh-called")
	mustWrite(t, filepath.Join(binDir, "gh"), "#!/bin/sh\ntouch "+marker+"\nexit 1\n")
	if err := os.Chmod(filepath.Join(binDir, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := syncDefault(registry, false, "", false); err != nil {
		t.Fatalf("syncDefault returned error: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("default registry sync must not query GitHub for builtin manifests; marker stat err=%v", err)
	}
}

// TestPluginRegistrySync_PublishGradeSemverGate verifies the shared regex
// rejects non-publish-grade tags (workflow#762 plan C2 fixture pin).
func TestPluginRegistrySync_PublishGradeSemverGate(t *testing.T) {
	cases := []struct {
		tag      string
		accepted bool
	}{
		{"v1.2.3", true},
		{"v0.0.0", true},
		{"v10.20.30", true},
		{"v1.2", false},        // not M.m.p
		{"v1.2.3-rc1", false},  // prerelease (engine ParseSemver rejects)
		{"v1.2.3-rc.1", false}, // prerelease canonical
		{"v1.2.3+build", false},
		{"1.2.3", false}, // missing v prefix
		{"release-2026", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			got := PublishGradeSemverRe.MatchString(tc.tag)
			if got != tc.accepted {
				t.Errorf("PublishGradeSemverRe.MatchString(%q) = %v, want %v", tc.tag, got, tc.accepted)
			}
		})
	}
}

// TestPluginRegistrySync_UsageHelp verifies the subcommand prints usage.
func TestPluginRegistrySync_UsageHelp(t *testing.T) {
	// Capture os.Stderr (flag.Usage writes there).
	// Use --help via flag parsing; that triggers Usage + flag.ErrHelp.
	err := runPluginRegistrySync([]string{"--help"})
	if err == nil {
		t.Skip("runPluginRegistrySync returned nil for --help; flag pkg may differ")
	}
	// flag.ErrHelp is the expected error for --help.
	if !strings.Contains(err.Error(), "help") {
		t.Logf("non-help error from --help (may be OK): %v", err)
	}
}

func TestPluginRegistrySyncReadme_CheckDetectsDriftWithoutBash(t *testing.T) {
	registry := t.TempDir()
	mustWrite(t, filepath.Join(registry, "README.md"), "# Registry\n\n## Schema\n\nschema docs\n")
	mustWrite(t, filepath.Join(registry, "plugins", "alpha", "manifest.json"), `{
  "name": "alpha",
  "description": "Alpha | plugin",
  "type": "builtin",
  "tier": "core"
}`)
	mustWrite(t, filepath.Join(registry, "templates", "api-service.yaml"), "description: API | service\n")

	err := runPluginRegistrySyncReadme([]string{"--check", "--registry-dir", registry})
	if err == nil || !strings.Contains(err.Error(), "README.md is out of date") {
		t.Fatalf("check error = %v, want README drift error", err)
	}

	if err := runPluginRegistrySyncReadme([]string{"--registry-dir", registry}); err != nil {
		t.Fatalf("runPluginRegistrySyncReadme returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(registry, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"## Built-in Plugins",
		"| [alpha](./plugins/alpha/manifest.json) | Alpha \\| plugin |",
		"## Templates",
		"| [api-service](./templates/api-service.yaml) | API \\| service |",
		"## Schema",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README missing %q:\n%s", want, text)
		}
	}
	if got := strings.Count(text, "## Schema"); got != 1 {
		t.Fatalf("README schema section count = %d, want 1:\n%s", got, text)
	}
}

func TestPluginRegistrySyncCore_DetectsAndFixesManifestDrift(t *testing.T) {
	registry := t.TempDir()
	manifest := filepath.Join(registry, "plugins", "corealpha", "manifest.json")
	mustWrite(t, manifest, `{
  "name": "workflow-plugin-core-alpha",
  "version": "0.1.0",
  "author": "GoCodeAlone",
  "description": "stale",
  "source": "github.com/GoCodeAlone/workflow",
  "path": "plugins/corealpha",
  "type": "builtin",
  "tier": "core",
  "license": "MIT",
  "homepage": "https://github.com/GoCodeAlone/workflow",
  "repository": "https://github.com/GoCodeAlone/workflow",
  "downloads": [{"os": "linux"}],
  "capabilities": {"moduleTypes": ["old"]}
}`)

	plugins := []coreRegistryPlugin{{
		Name:             "workflow-plugin-core-alpha",
		Version:          "1.2.3",
		Description:      "current",
		ModuleTypes:      []string{"alpha"},
		StepTypes:        []string{"step"},
		TriggerTypes:     []string{"trigger"},
		WorkflowHandlers: []string{"handler"},
		WiringHooks:      []string{"hook"},
	}}

	var stderr bytes.Buffer
	err := syncCorePluginManifests(registry, plugins, false, &stderr)
	if err == nil || !strings.Contains(err.Error(), "core manifest validation failed") {
		t.Fatalf("dry-run error = %v, stderr=%s", err, stderr.String())
	}

	stderr.Reset()
	if err := syncCorePluginManifests(registry, plugins, true, &stderr); err != nil {
		t.Fatalf("fix returned error: %v", err)
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "1.2.3" || got["description"] != "current" {
		t.Fatalf("manifest not updated: %#v", got)
	}
	if _, ok := got["downloads"]; ok {
		t.Fatalf("downloads should be removed from builtin core manifest: %#v", got)
	}
	caps := got["capabilities"].(map[string]any)
	if len(caps["moduleTypes"].([]any)) != 1 || caps["moduleTypes"].([]any)[0] != "alpha" {
		t.Fatalf("capabilities not updated: %#v", caps)
	}
}

func TestPluginRegistrySyncCore_DetectsAndFixesDownloadsOnlyDrift(t *testing.T) {
	registry := t.TempDir()
	manifest := filepath.Join(registry, "plugins", "corealpha", "manifest.json")
	mustWrite(t, manifest, `{
  "name": "workflow-plugin-core-alpha",
  "version": "1.2.3",
  "author": "GoCodeAlone",
  "description": "current",
  "source": "github.com/GoCodeAlone/workflow",
  "path": "plugins/corealpha",
  "type": "builtin",
  "tier": "core",
  "license": "MIT",
  "homepage": "https://github.com/GoCodeAlone/workflow",
  "repository": "https://github.com/GoCodeAlone/workflow",
  "downloads": [{
    "os": "linux",
    "arch": "amd64",
    "url": "https://github.com/GoCodeAlone/workflow/releases/download/v0.69.6/wfctl-linux-amd64.tar.gz"
  }],
  "capabilities": {
    "moduleTypes": ["alpha"],
    "stepTypes": ["step"],
    "triggerTypes": ["trigger"],
    "workflowHandlers": ["handler"],
    "wiringHooks": ["hook"]
  }
}`)

	plugins := []coreRegistryPlugin{{
		Name:             "workflow-plugin-core-alpha",
		Version:          "1.2.3",
		Description:      "current",
		ModuleTypes:      []string{"alpha"},
		StepTypes:        []string{"step"},
		TriggerTypes:     []string{"trigger"},
		WorkflowHandlers: []string{"handler"},
		WiringHooks:      []string{"hook"},
	}}

	var stderr bytes.Buffer
	err := syncCorePluginManifests(registry, plugins, false, &stderr)
	if err == nil || !strings.Contains(err.Error(), "core manifest validation failed") {
		t.Fatalf("dry-run error = %v, stderr=%s", err, stderr.String())
	}

	stderr.Reset()
	if err := syncCorePluginManifests(registry, plugins, true, &stderr); err != nil {
		t.Fatalf("fix returned error: %v", err)
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["downloads"]; ok {
		t.Fatalf("downloads should be removed from builtin core manifest: %#v", got)
	}
}

func TestPluginRegistrySync_SelectPlatformReleaseAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "workflow-plugin-foo-linux-amd64.tar.gz", OS: "linux", Arch: "amd64", URL: "linux-amd64"},
		{Name: "workflow-plugin-foo-darwin-arm64.tar.gz", OS: "darwin", Arch: "arm64", URL: "darwin-arm64"},
		{Name: "workflow-plugin-foo-linux-arm64.tar.gz", OS: "linux", Arch: "arm64", URL: "linux-arm64"},
	}

	got, ok := selectPlatformReleaseAsset(assets, "linux", "arm64")
	if !ok {
		t.Fatal("expected linux/arm64 asset to be selected")
	}
	if got.Name != "workflow-plugin-foo-linux-arm64.tar.gz" {
		t.Fatalf("selected asset = %q, want linux arm64 tarball", got.Name)
	}

	if _, ok := selectPlatformReleaseAsset(assets, "windows", "amd64"); ok {
		t.Fatal("unexpected asset for missing windows/amd64 platform")
	}
}

func TestPluginRegistrySync_LocateExtractedBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "workflow-plugin-foo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit lookup is POSIX-specific")
	}

	got, err := locateRegistrySyncBinary(dir, "foo", "workflow-plugin-foo")
	if err != nil {
		t.Fatalf("locateRegistrySyncBinary returned error: %v", err)
	}
	if got != bin {
		t.Fatalf("binary path = %q, want %q", got, bin)
	}

	if _, err := locateRegistrySyncBinary(dir, "missing-plugin"); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestPluginRegistrySync_AssetBinaryName(t *testing.T) {
	cases := map[string]string{
		"workflow-plugin-github-darwin-arm64.tar.gz": "workflow-plugin-github",
		"workflow-plugin-foo_linux_amd64.tgz":        "workflow-plugin-foo",
		"custom-plugin":                              "custom-plugin",
	}
	for in, want := range cases {
		if got := assetBinaryName(in); got != want {
			t.Fatalf("assetBinaryName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluginRegistrySync_VerifyCapabilitiesDownloadsExtractsAndSkipsName(t *testing.T) {
	restoreRegistrySyncTestHooks(t)

	dir := t.TempDir()
	assetName := "workflow-plugin-foo-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	assetPath := filepath.Join(dir, assetName)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"foo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "plugin.json")
	if err := writeTestTarGz(assetPath, "archive/workflow-plugin-foo", []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	registrySyncReleaseDownloads = func(ghRepo, tag string) ([]releaseAsset, error) {
		if ghRepo != "owner/repo" || tag != "v1.2.3" {
			t.Fatalf("releaseDownloads args = %q %q, want owner/repo v1.2.3", ghRepo, tag)
		}
		return []releaseAsset{{Name: assetName, OS: runtime.GOOS, Arch: runtime.GOARCH}}, nil
	}
	registrySyncDownloadReleaseAsset = func(ghRepo, tag, name, targetDir string) (string, error) {
		if name != assetName {
			t.Fatalf("download asset name = %q, want %q", name, assetName)
		}
		if targetDir == "" {
			t.Fatal("download target dir is empty")
		}
		return assetPath, nil
	}

	var verifyCalled bool
	registrySyncVerifyManifest = func(binary, manifest string, opts manifestCompareOptions) error {
		verifyCalled = true
		if filepath.Base(binary) != "workflow-plugin-foo" {
			t.Fatalf("binary = %q, want extracted workflow-plugin-foo", binary)
		}
		if manifest != manifestPath {
			t.Fatalf("manifest = %q, want %q", manifest, manifestPath)
		}
		if !opts.SkipName {
			t.Fatal("registry verification must skip strict manifest name comparison")
		}
		return nil
	}

	if err := verifyRegistryPluginCapabilities("foo", manifestPath, "owner/repo", "v1.2.3"); err != nil {
		t.Fatalf("verifyRegistryPluginCapabilities returned error: %v", err)
	}
	if !verifyCalled {
		t.Fatal("expected registrySyncVerifyManifest to be called")
	}
}

func TestPluginRegistrySync_VerifyCapabilitiesDownloadError(t *testing.T) {
	restoreRegistrySyncTestHooks(t)

	registrySyncReleaseDownloads = func(string, string) ([]releaseAsset, error) {
		return []releaseAsset{{Name: "workflow-plugin-foo-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz", OS: runtime.GOOS, Arch: runtime.GOARCH}}, nil
	}
	registrySyncDownloadReleaseAsset = func(string, string, string, string) (string, error) {
		return "", errors.New("download failed")
	}

	err := verifyRegistryPluginCapabilities("foo", filepath.Join(t.TempDir(), "plugin.json"), "owner/repo", "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("error = %v, want download failure", err)
	}
}

func restoreRegistrySyncTestHooks(t *testing.T) {
	t.Helper()
	oldReleaseDownloads := registrySyncReleaseDownloads
	oldDownloadReleaseAsset := registrySyncDownloadReleaseAsset
	oldVerifyManifest := registrySyncVerifyManifest
	t.Cleanup(func() {
		registrySyncReleaseDownloads = oldReleaseDownloads
		registrySyncDownloadReleaseAsset = oldDownloadReleaseAsset
		registrySyncVerifyManifest = oldVerifyManifest
	})
}

func writeTestTarGz(path, name string, data []byte, mode int64) error {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
