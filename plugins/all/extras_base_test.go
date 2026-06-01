//go:build !scenario_stub

package all

import "testing"

// TestDefaultPlugins_BaseExcludesStub asserts that without the
// "scenario_stub" build tag, DefaultPlugins() does NOT include the
// stub provider plugin. This guards against accidentally shipping the
// stub in production server builds.
func TestDefaultPlugins_BaseExcludesStub(t *testing.T) {
	for _, p := range DefaultPlugins() {
		if p.Name() == "stubprovider" {
			t.Error("DefaultPlugins() contains 'stubprovider' in a non-scenario_stub build — stub must not appear in production")
			return
		}
	}
}
