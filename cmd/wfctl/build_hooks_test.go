package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// writeTestPlugin writes a fake plugin binary and manifest to pluginsDir.
// The binary is a tiny shell script that records its invocation and exits with
// the given exit code.
func writeTestPlugin(t *testing.T, pluginsDir, name string, hooks []config.BuildHookDeclaration, onHookFailure string, exitCode int) {
	t.Helper()
	dir := filepath.Join(pluginsDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	// Write a shell script that appends its name to a temp file and exits.
	recordFile := filepath.Join(pluginsDir, "invocations.txt")
	script := "#!/bin/sh\n"
	script += "echo '" + name + "' >> " + recordFile + "\n"
	script += "exit " + strconv.Itoa(exitCode) + "\n"

	binPath := filepath.Join(dir, name)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write binary %s: %v", binPath, err)
	}

	// Write manifest.
	caps := config.PluginCapabilities{BuildHooks: hooks}
	if onHookFailure != "" {
		caps.OnHookFailure = onHookFailure
	}
	manifest := config.PluginManifestFile{
		Name:         name,
		Version:      "0.1.0",
		Capabilities: caps,
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), b, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func readInvocations(pluginsDir string) []string {
	b, err := os.ReadFile(filepath.Join(pluginsDir, "invocations.txt"))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestHookDispatcher_PriorityOrder(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	// Plugin "b" has lower priority (100) and should fire first.
	// Plugin "a" has higher priority (500) and should fire second.
	writeTestPlugin(t, pluginsDir, "plugin-a", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 500},
	}, "fail", 0)
	writeTestPlugin(t, pluginsDir, "plugin-b", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "fail", 0)

	disp := NewHookDispatcher(pluginsDir)
	payload := interfaces.HookPayload{Event: interfaces.HookEventPostBuild}
	if err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, payload); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readInvocations(pluginsDir)
	if len(got) != 2 {
		t.Fatalf("expected 2 invocations, got %d: %v", len(got), got)
	}
	if got[0] != "plugin-b" {
		t.Errorf("expected plugin-b first (priority 100), got %q", got[0])
	}
	if got[1] != "plugin-a" {
		t.Errorf("expected plugin-a second (priority 500), got %q", got[1])
	}
}

func TestHookDispatcher_PriorityTieBreakByName(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	// Same priority — lexical order on plugin name: "plugin-a" < "plugin-z"
	writeTestPlugin(t, pluginsDir, "plugin-z", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "fail", 0)
	writeTestPlugin(t, pluginsDir, "plugin-a", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "fail", 0)

	disp := NewHookDispatcher(pluginsDir)
	payload := interfaces.HookPayload{Event: interfaces.HookEventPostBuild}
	if err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, payload); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readInvocations(pluginsDir)
	if len(got) != 2 {
		t.Fatalf("expected 2 invocations, got %v", got)
	}
	if got[0] != "plugin-a" {
		t.Errorf("tie-break: expected plugin-a first, got %q", got[0])
	}
}

func TestHookDispatcher_FailPolicy_Aborts(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	// plugin-bad exits 1 with fail policy → dispatch returns error.
	// plugin-after has higher priority (should NOT run after abort).
	writeTestPlugin(t, pluginsDir, "plugin-bad", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "fail", 1)
	writeTestPlugin(t, pluginsDir, "plugin-after", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 200},
	}, "fail", 0)

	disp := NewHookDispatcher(pluginsDir)
	payload := interfaces.HookPayload{Event: interfaces.HookEventPostBuild}
	err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, payload)
	if err == nil {
		t.Error("expected error from fail policy, got nil")
	}
	// plugin-after should NOT have run
	got := readInvocations(pluginsDir)
	for _, name := range got {
		if name == "plugin-after" {
			t.Error("plugin-after should not have been invoked after fail abort")
		}
	}
}

func TestHookDispatcher_WarnPolicy_Continues(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	writeTestPlugin(t, pluginsDir, "plugin-warn", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "warn", 1)
	writeTestPlugin(t, pluginsDir, "plugin-next", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 200},
	}, "fail", 0)

	disp := NewHookDispatcher(pluginsDir)
	payload := interfaces.HookPayload{Event: interfaces.HookEventPostBuild}
	if err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, payload); err != nil {
		t.Errorf("warn policy should not return error, got: %v", err)
	}
	got := readInvocations(pluginsDir)
	if len(got) != 2 {
		t.Errorf("warn policy: expected both plugins to run, got %v", got)
	}
}

func TestHookDispatcher_SkipPolicy_Silent(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	writeTestPlugin(t, pluginsDir, "plugin-skip", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 100},
	}, "skip", 1)
	writeTestPlugin(t, pluginsDir, "plugin-next", []config.BuildHookDeclaration{
		{Event: string(interfaces.HookEventPostBuild), Priority: 200},
	}, "fail", 0)

	disp := NewHookDispatcher(pluginsDir)
	payload := interfaces.HookPayload{Event: interfaces.HookEventPostBuild}
	if err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, payload); err != nil {
		t.Errorf("skip policy should not return error, got: %v", err)
	}
	got := readInvocations(pluginsDir)
	if len(got) != 2 {
		t.Errorf("skip policy: expected both plugins to run, got %v", got)
	}
}

func TestHookDispatcher_Timeout(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	pluginsDir := t.TempDir()

	// Write a plugin that sleeps longer than the timeout.
	// Use "exec sleep 10" so the shell process is replaced by sleep directly;
	// killing the process kills sleep immediately (no orphan child keeps pipes open).
	dir := filepath.Join(pluginsDir, "plugin-slow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec sleep 10\n"
	binPath := filepath.Join(dir, "plugin-slow")
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	caps := config.PluginCapabilities{
		BuildHooks: []config.BuildHookDeclaration{
			{Event: string(interfaces.HookEventPostBuild), Priority: 100, TimeoutSeconds: 1},
		},
		OnHookFailure: "fail",
	}
	manifest := config.PluginManifestFile{Name: "plugin-slow", Version: "0.1.0", Capabilities: caps}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), b, 0644); err != nil {
		t.Fatal(err)
	}

	disp := NewHookDispatcher(pluginsDir)
	ctx := context.Background()
	start := time.Now()
	err := disp.Dispatch(ctx, interfaces.HookEventPostBuild, interfaces.HookPayload{
		Event: interfaces.HookEventPostBuild,
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout not enforced: elapsed %v, expected ~1s", elapsed)
	}
}

func TestHookDispatcher_NoHandlers_OK(t *testing.T) {
	pluginsDir := t.TempDir()
	// Empty plugins dir — should succeed silently.
	disp := NewHookDispatcher(pluginsDir)
	err := disp.Dispatch(context.Background(), interfaces.HookEventPostBuild, interfaces.HookPayload{
		Event: interfaces.HookEventPostBuild,
	})
	if err != nil {
		t.Errorf("no handlers should be a no-op, got: %v", err)
	}
}
