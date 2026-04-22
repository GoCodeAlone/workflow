package sdk_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// fakePlugin is a minimal PluginProvider for tests.
type fakePlugin struct{}

func (f *fakePlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{Name: "fake", Version: "0.0.1"}
}

// fakeCLI implements CLIProvider.
type fakeCLI struct {
	called bool
	args   []string
	code   int
}

func (c *fakeCLI) RunCLI(args []string) int {
	c.called = true
	c.args = args
	return c.code
}

// fakeHooks implements HookHandler.
type fakeHooks struct {
	called  bool
	event   string
	payload []byte
	result  []byte
	err     error
}

func (h *fakeHooks) HandleBuildHook(event string, payload []byte) ([]byte, error) {
	h.called = true
	h.event = event
	h.payload = payload
	return h.result, h.err
}

func TestServePluginFull_DispatchCLI(t *testing.T) {
	cli := &fakeCLI{code: 0}
	hooks := &fakeHooks{}

	// Simulate: plugin-binary --wfctl-cli supply-chain scan
	args := []string{"plugin-binary", "--wfctl-cli", "supply-chain", "scan"}
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, cli, hooks)

	if !cli.called {
		t.Error("expected CLI handler to be called")
	}
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if hooks.called {
		t.Error("hook handler should not be called for --wfctl-cli dispatch")
	}
	// args passed to RunCLI should strip the binary name + --wfctl-cli
	if len(cli.args) < 1 || cli.args[0] != "supply-chain" {
		t.Errorf("unexpected CLI args: %v", cli.args)
	}
}

func TestServePluginFull_DispatchHook(t *testing.T) {
	cli := &fakeCLI{}
	hooks := &fakeHooks{result: []byte(`{"ok":true}`)}

	payload := map[string]any{"image": "myapp:latest"}
	payloadBytes, _ := json.Marshal(payload)

	// Write payload to a temp file to simulate stdin.
	tmpFile, err := os.CreateTemp("", "hook-payload-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(payloadBytes); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if _, err := tmpFile.Seek(0, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	// Replace stdin.
	origStdin := os.Stdin
	os.Stdin = tmpFile
	defer func() { os.Stdin = origStdin }()

	args := []string{"plugin-binary", "--wfctl-hook", "post_container_build"}
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, cli, hooks)

	if !hooks.called {
		t.Error("expected hook handler to be called")
	}
	if hooks.event != "post_container_build" {
		t.Errorf("expected event post_container_build, got %q", hooks.event)
	}
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if cli.called {
		t.Error("CLI handler should not be called for --wfctl-hook dispatch")
	}
}

func TestServePluginFull_NilCLI(t *testing.T) {
	// When CLI provider is nil, --wfctl-cli should return exit 1.
	args := []string{"plugin-binary", "--wfctl-cli", "some-command"}
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, nil)
	if exitCode == 0 {
		t.Error("expected non-zero exit when CLIProvider is nil")
	}
}

func TestServePluginFull_NilHooks(t *testing.T) {
	// When HookHandler is nil, --wfctl-hook should return exit 1.
	args := []string{"plugin-binary", "--wfctl-hook", "post_build"}
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, nil)
	if exitCode == 0 {
		t.Error("expected non-zero exit when HookHandler is nil")
	}
}
