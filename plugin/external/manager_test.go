package external

import (
	"log"
	"testing"
)

func TestExternalPluginManagerStoresCallbackServer(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	callback := NewCallbackServer(nil, nil, log.Default())

	manager.SetCallbackServer(callback)

	if manager.callbackServer != callback {
		t.Fatal("expected manager to retain callback server for plugin load")
	}
}
