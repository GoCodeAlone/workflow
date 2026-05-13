package main

// TestInfraCommandsAcceptPluginDirFlag verifies that all infra subcommands that
// load external plugins accept the -plugin-dir flag without a parse error, and
// that the flag value is threaded through to discoverAndLoadIaCProvider via the
// currentInfraPluginDir seam variable.
//
// Each test:
//  1. Writes a minimal infra.yaml with an iac.provider module so the command
//     actually reaches the provider-loading code path.
//  2. Overrides resolveIaCProvider to capture the currentInfraPluginDir value
//     at the moment the provider is loaded, and returns an error so the command
//     fails fast without executing real cloud operations.
//  3. Invokes the command with -plugin-dir set to a sentinel value.
//  4. Asserts the sentinel was visible inside resolveIaCProvider — proving that
//     the flag set currentInfraPluginDir before the first provider load.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

const pluginDirSentinel = "/tmp/test-plugin-dir-sentinel"

// minimalInfraYAML returns a minimal infra.yaml with one iac.provider that
// references provider type "testprovider", plus one infra resource that
// references it. This is enough to trigger provider loading for plan/apply/etc.
func minimalInfraYAML(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	yaml := `name: test-app
modules:
  - name: myprovider
    type: iac.provider
    config:
      provider: testprovider
  - name: myvpc
    type: infra.vpc
    config:
      provider: myprovider
      region: us-east-1
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write infra.yaml: %v", err)
	}
	return cfgPath
}

// capturePluginDirAndFail installs a resolveIaCProvider override that records
// the currentInfraPluginDir at call time into *got and returns a sentinel
// error so the command exits immediately without any real provider work.
// The returned restore function must be deferred by the caller.
func capturePluginDirAndFail(got *string) (restore func()) {
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		*got = currentInfraPluginDir
		return nil, nil, errSentinelProviderLoad
	}
	return func() { resolveIaCProvider = orig }
}

// errSentinelProviderLoad is the fast-exit error injected by the test override.
// Declared as a package-level var so assertions can use strings.Contains without
// hardcoding the literal message.
var errSentinelProviderLoad = infraErrSentinel("sentinel provider load error for plugin-dir test")

type infraErrSentinel string

func (e infraErrSentinel) Error() string { return string(e) }

// TestInfraPlanAcceptsPluginDirFlag verifies -plugin-dir is accepted by
// runInfraPlan and sets currentInfraPluginDir before provider loading.
func TestInfraPlanAcceptsPluginDirFlag(t *testing.T) {
	cfgPath := minimalInfraYAML(t)
	var got string
	defer capturePluginDirAndFail(&got)()

	err := runInfraPlan([]string{"--config", cfgPath, "--plugin-dir", pluginDirSentinel})
	// The command must fail (provider won't load) but must NOT fail because the
	// flag was unknown.
	if err == nil {
		t.Fatal("expected error (sentinel provider load), got nil")
	}
	if strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("-plugin-dir not accepted by runInfraPlan: %v", err)
	}
	if got != pluginDirSentinel {
		t.Errorf("currentInfraPluginDir inside resolveIaCProvider = %q; want %q", got, pluginDirSentinel)
	}
}

// TestInfraApplyAcceptsPluginDirFlag verifies -plugin-dir is accepted by
// runInfraApply and sets currentInfraPluginDir before provider loading.
func TestInfraApplyAcceptsPluginDirFlag(t *testing.T) {
	cfgPath := minimalInfraYAML(t)
	var got string
	defer capturePluginDirAndFail(&got)()

	err := runInfraApply([]string{"--config", cfgPath, "--plugin-dir", pluginDirSentinel, "--auto-approve"})
	if err == nil {
		t.Fatal("expected error (sentinel provider load), got nil")
	}
	if strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("-plugin-dir not accepted by runInfraApply: %v", err)
	}
	if got != pluginDirSentinel {
		t.Errorf("currentInfraPluginDir inside resolveIaCProvider = %q; want %q", got, pluginDirSentinel)
	}
}

// TestInfraApplyDryRunAcceptsPluginDirFlag verifies -plugin-dir works with
// --dry-run (which goes through the same currentInfraPluginDir seam since the
// var is set before the dry-run early-return branch).
func TestInfraApplyDryRunAcceptsPluginDirFlag(t *testing.T) {
	cfgPath := minimalInfraYAML(t)
	var got string
	defer capturePluginDirAndFail(&got)()

	err := runInfraApply([]string{"--config", cfgPath, "--plugin-dir", pluginDirSentinel, "--dry-run"})
	if err == nil {
		t.Fatal("expected error (sentinel provider load), got nil")
	}
	if strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("-plugin-dir not accepted by runInfraApply --dry-run: %v", err)
	}
	if got != pluginDirSentinel {
		t.Errorf("currentInfraPluginDir inside resolveIaCProvider = %q; want %q", got, pluginDirSentinel)
	}
}

// TestInfraPluginDirResetsAfterCommand verifies that currentInfraPluginDir is
// reset to "" after a runInfraApply invocation completes (via defer), so
// subsequent commands in the same process see the clean default.
func TestInfraPluginDirResetsAfterCommand(t *testing.T) {
	cfgPath := minimalInfraYAML(t)
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return nil, nil, errSentinelProviderLoad
	}
	defer func() { resolveIaCProvider = orig }()

	_ = runInfraApply([]string{"--config", cfgPath, "--plugin-dir", pluginDirSentinel, "--auto-approve"})

	if currentInfraPluginDir != "" {
		t.Errorf("currentInfraPluginDir not reset after runInfraApply; got %q", currentInfraPluginDir)
	}
}

// TestDiscoverAndLoadIaCProvider_UsesCurrentInfraPluginDir verifies that
// discoverAndLoadIaCProvider reads currentInfraPluginDir before WFCTL_PLUGIN_DIR.
func TestDiscoverAndLoadIaCProvider_UsesCurrentInfraPluginDir(t *testing.T) {
	// Set a custom sentinel dir (no plugins in it → specific error message).
	customDir := t.TempDir()
	currentInfraPluginDir = customDir
	defer func() { currentInfraPluginDir = "" }()

	// Ensure WFCTL_PLUGIN_DIR points somewhere different so we can distinguish.
	t.Setenv("WFCTL_PLUGIN_DIR", "/nonexistent-env-dir")

	_, _, err := discoverAndLoadIaCProvider(context.Background(), "any-provider", nil)
	if err == nil {
		t.Skip("unexpectedly found a provider in temp dir")
	}
	// The error must mention customDir, not /nonexistent-env-dir.
	if !strings.Contains(err.Error(), customDir) {
		t.Errorf("error %q does not mention customDir %q — currentInfraPluginDir was not consulted first", err.Error(), customDir)
	}
	if strings.Contains(err.Error(), "/nonexistent-env-dir") {
		t.Errorf("error %q mentions WFCTL_PLUGIN_DIR value — env var took precedence over currentInfraPluginDir", err.Error())
	}
}
