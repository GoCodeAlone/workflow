//go:build scenario_stub

package all_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugins/all"
)

// TestDefaultPlugins_ContainsStub asserts that when compiled with the
// "scenario_stub" build tag, DefaultPlugins() includes the stub provider
// plugin named "stubprovider".
func TestDefaultPlugins_ContainsStub(t *testing.T) {
	plugins := all.DefaultPlugins()
	for _, p := range plugins {
		if p.Name() == "stubprovider" {
			return // found
		}
	}
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name())
	}
	t.Errorf("DefaultPlugins() does not contain 'stubprovider'; plugins: %v", names)
}

// TestDefaultPlugins_ContainsLocalAuthz asserts that when compiled with the
// "scenario_stub" build tag, DefaultPlugins() includes the in-process
// authz enforcer plugin named "localauthz".
func TestDefaultPlugins_ContainsLocalAuthz(t *testing.T) {
	plugins := all.DefaultPlugins()
	for _, p := range plugins {
		if p.Name() == "localauthz" {
			return // found
		}
	}
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name())
	}
	t.Errorf("DefaultPlugins() does not contain 'localauthz'; plugins: %v", names)
}
