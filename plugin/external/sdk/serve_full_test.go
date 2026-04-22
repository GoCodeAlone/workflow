package sdk_test

import (
	"bytes"
	"encoding/json"
	"strings"
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
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, cli, hooks, stdin, &stdout)

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

	args := []string{"plugin-binary", "--wfctl-hook", "post_container_build"}
	stdin := bytes.NewReader(payloadBytes)
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, cli, hooks, stdin, &stdout)

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
	// Verify the hook received the correct payload.
	if !bytes.Equal(hooks.payload, payloadBytes) {
		t.Errorf("hook payload mismatch: got %s, want %s", hooks.payload, payloadBytes)
	}
	// Verify the result was written to stdout.
	if !bytes.Equal(stdout.Bytes(), hooks.result) {
		t.Errorf("stdout mismatch: got %s, want %s", stdout.Bytes(), hooks.result)
	}
}

func TestServePluginFull_DispatchHook_NoResult(t *testing.T) {
	// When the hook returns nil/empty result, nothing should be written to stdout.
	hooks := &fakeHooks{result: nil}
	args := []string{"plugin-binary", "--wfctl-hook", "pre_build"}
	stdin := strings.NewReader(`{}`)
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, hooks, stdin, &stdout)
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout, got %q", stdout.String())
	}
}

func TestServePluginFull_NoFlags(t *testing.T) {
	// No wfctl flags → DispatchArgs returns -1 (caller falls back to gRPC Serve).
	args := []string{"plugin-binary"}
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, nil, stdin, &stdout)
	if exitCode != -1 {
		t.Errorf("expected -1 (fallback), got %d", exitCode)
	}
}

func TestServePluginFull_NilCLI(t *testing.T) {
	// When CLI provider is nil, --wfctl-cli should return exit 1.
	args := []string{"plugin-binary", "--wfctl-cli", "some-command"}
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, nil, stdin, &stdout)
	if exitCode == 0 {
		t.Error("expected non-zero exit when CLIProvider is nil")
	}
}

func TestServePluginFull_NilHooks(t *testing.T) {
	// When HookHandler is nil, --wfctl-hook should return exit 1.
	args := []string{"plugin-binary", "--wfctl-hook", "post_build"}
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	exitCode := sdk.DispatchArgs(args, &fakePlugin{}, nil, nil, stdin, &stdout)
	if exitCode == 0 {
		t.Error("expected non-zero exit when HookHandler is nil")
	}
}
