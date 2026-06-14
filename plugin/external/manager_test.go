package external

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goplugin "github.com/GoCodeAlone/go-plugin"
)

func TestPluginStderrForwarderPrefixesPluginLines(t *testing.T) {
	var out bytes.Buffer
	logger := log.New(&out, "[external-plugins] ", 0)
	forwarder := newPluginStderrForwarder("hover", logger)

	n, err := forwarder.Write([]byte("first line\nsecond line\r\n\nthird"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("first line\nsecond line\r\n\nthird") {
		t.Fatalf("Write returned n=%d, want %d", n, len("first line\nsecond line\r\n\nthird"))
	}
	if strings.Contains(out.String(), "third") {
		t.Fatalf("forwarded stderr should not emit partial line:\n%s", out.String())
	}
	n, err = forwarder.Write([]byte(" line\n"))
	if err != nil {
		t.Fatalf("Write continuation: %v", err)
	}
	if n != len(" line\n") {
		t.Fatalf("Write continuation returned n=%d, want %d", n, len(" line\n"))
	}
	got := out.String()
	for _, want := range []string{
		`[external-plugins] plugin "hover" stderr: first line`,
		`[external-plugins] plugin "hover" stderr: second line`,
		`[external-plugins] plugin "hover" stderr: third line`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("forwarded stderr missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `stderr: `+"\n") {
		t.Fatalf("forwarded stderr should skip blank lines:\n%s", got)
	}
}

func TestExternalPluginManagerStoresCallbackServer(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	callback := NewCallbackServer(nil, nil, log.Default())

	manager.SetCallbackServer(callback)

	if manager.callbackServer != callback {
		t.Fatal("expected manager to retain callback server for plugin load")
	}
}

func TestExternalPluginManagerReloadFailureKeepsExistingPluginLoaded(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate handshake failed")
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected reload failure")
	}

	got := manager.clients["safe-plugin"]
	if got != oldClient {
		t.Fatal("reload failure replaced or removed the active plugin client")
	}
	if !manager.IsLoaded("safe-plugin") {
		t.Fatal("reload failure should leave active plugin loaded")
	}
}

func TestExternalPluginManagerLoadPluginStoresCandidateAfterValidation(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	candidate := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.startPlugin = func(name string) (*pluginLaunch, error) {
		if name != "safe-plugin" {
			t.Fatalf("unexpected plugin name %q", name)
		}
		return &pluginLaunch{client: candidate, adapter: adapter}, nil
	}

	got, err := manager.LoadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got != adapter {
		t.Fatal("load did not return candidate adapter")
	}
	if manager.clients["safe-plugin"] != candidate {
		t.Fatal("load did not register candidate client")
	}
	if _, err := manager.LoadPlugin("safe-plugin"); err == nil {
		t.Fatal("duplicate load should fail")
	}
}

func TestExternalPluginManagerLoadPluginRejectsInvalidCandidate(t *testing.T) {
	for name, launch := range map[string]*pluginLaunch{
		"nil-launch":  nil,
		"nil-client":  {adapter: &ExternalPluginAdapter{}},
		"nil-adapter": {client: &goplugin.Client{}},
	} {
		t.Run(name, func(t *testing.T) {
			manager := NewExternalPluginManager(t.TempDir(), log.Default())
			manager.startPlugin = func(string) (*pluginLaunch, error) {
				return launch, nil
			}

			if _, err := manager.LoadPlugin("safe-plugin"); err == nil {
				t.Fatal("expected invalid candidate error")
			}
			if manager.IsLoaded("safe-plugin") {
				t.Fatal("invalid candidate should not be registered")
			}
		})
	}
}

func TestExternalPluginManagerLoadPluginReturnsStartError(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate start failed")
	}

	if _, err := manager.LoadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "candidate start failed") {
		t.Fatalf("expected start error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("start failure should not register plugin")
	}
}

func TestExternalPluginManagerLoadPluginValidatesDiskCandidateBeforeStart(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginDir := filepath.Join(pluginsDir, "safe-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "safe-plugin"), 0o755); err != nil {
		t.Fatalf("create fake binary directory: %v", err)
	}
	manifest := []byte(`{
		"name": "safe-plugin",
		"version": "1.0.0",
		"author": "test",
		"description": "test plugin"
	}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manager := NewExternalPluginManager(pluginsDir, log.Default())

	if _, err := manager.LoadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "binary path is a directory") {
		t.Fatalf("expected directory binary validation error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("disk validation failure should not register plugin")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginLoadsCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	candidate := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: candidate, adapter: adapter}, nil
	}

	got, err := manager.ReloadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got != adapter {
		t.Fatal("reload did not return candidate adapter")
	}
	if manager.clients["safe-plugin"] != candidate {
		t.Fatal("reload without active plugin did not register candidate client")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginRejectsInvalidCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}}, nil
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected invalid candidate error")
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("invalid reload candidate should not be registered")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginReturnsStartError(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate start failed")
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "candidate start failed") {
		t.Fatalf("expected start error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("start failure should not register plugin")
	}
}

func TestExternalPluginManagerReloadSuccessSwapsAfterCandidateStarts(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	newClient := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		if manager.clients["safe-plugin"] != oldClient {
			t.Fatal("candidate started after old plugin was removed")
		}
		return &pluginLaunch{client: newClient, adapter: adapter}, nil
	}

	got, err := manager.ReloadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got != adapter {
		t.Fatal("reload did not return candidate adapter")
	}
	if got := manager.clients["safe-plugin"]; got != newClient {
		t.Fatal("reload success did not register candidate plugin client")
	}
}

func TestExternalPluginManagerReloadLoadedRejectsInvalidCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}}, nil
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected invalid candidate error")
	}
	if manager.clients["safe-plugin"] != oldClient {
		t.Fatal("invalid reload candidate replaced active plugin")
	}
}
