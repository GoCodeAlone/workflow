package external

import (
	"errors"
	"log"
	"testing"

	goplugin "github.com/GoCodeAlone/go-plugin"
)

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

func TestExternalPluginManagerReloadSuccessSwapsAfterCandidateStarts(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	newClient := &goplugin.Client{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		if manager.clients["safe-plugin"] != oldClient {
			t.Fatal("candidate started after old plugin was removed")
		}
		return &pluginLaunch{client: newClient}, nil
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got := manager.clients["safe-plugin"]; got != newClient {
		t.Fatal("reload success did not register candidate plugin client")
	}
}
