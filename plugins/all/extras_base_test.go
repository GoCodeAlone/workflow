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

// TestDefaultPlugins_BaseExcludesLocalAuthz asserts that without the
// "scenario_stub" build tag, DefaultPlugins() does NOT include the
// in-process authz enforcer. This guards against shipping the test-only
// exact-match RBAC module in production server builds.
func TestDefaultPlugins_BaseExcludesLocalAuthz(t *testing.T) {
	for _, p := range DefaultPlugins() {
		if p.Name() == "localauthz" {
			t.Error("DefaultPlugins() contains 'localauthz' in a non-scenario_stub build — must not appear in production")
			return
		}
	}
}
