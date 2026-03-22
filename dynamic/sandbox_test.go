package dynamic

import (
	"net/http"
	"testing"
)

// TestSandbox_DangerousPackagesBlocked verifies that packages that could allow
// sandbox escapes are not in AllowedPackages (and are in BlockedPackages).
func TestSandbox_DangerousPackagesBlocked(t *testing.T) {
	dangerous := []string{"os/exec", "syscall", "unsafe", "reflect", "os"}
	for _, pkg := range dangerous {
		if AllowedPackages[pkg] {
			t.Errorf("dangerous package %q should not be in AllowedPackages", pkg)
		}
		if !BlockedPackages[pkg] {
			t.Errorf("dangerous package %q should be in BlockedPackages", pkg)
		}
	}
}

// TestSandbox_NetHTTPAllowed confirms net/http is allowed (needed for outbound
// HTTP calls from dynamic components) but guarded at runtime.
func TestSandbox_NetHTTPAllowed(t *testing.T) {
	if !AllowedPackages["net/http"] {
		t.Error("net/http should be in AllowedPackages")
	}
}

// TestGuardTransport_RestoresOnMutation verifies that guardTransport restores
// http.DefaultTransport when a dynamic component mutates it.
func TestGuardTransport_RestoresOnMutation(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	snapshot := snapshotDefaultTransport()
	http.DefaultTransport = &http.Transport{} // simulate mutation by dynamic code
	guardTransport(snapshot, "test-component")

	if http.DefaultTransport != original {
		t.Error("guardTransport should have restored the original DefaultTransport")
	}
}

// TestGuardTransport_NoOpWhenUnchanged verifies that guardTransport does nothing
// when http.DefaultTransport has not been mutated.
func TestGuardTransport_NoOpWhenUnchanged(t *testing.T) {
	original := http.DefaultTransport
	snapshot := snapshotDefaultTransport()
	guardTransport(snapshot, "test-component")

	if http.DefaultTransport != original {
		t.Error("guardTransport should not change DefaultTransport when there is no mutation")
	}
}

// TestLoadFromSource_TransportGuard verifies that a dynamic component whose
// top-level init code mutates http.DefaultTransport has it restored after load.
func TestLoadFromSource_TransportGuard(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	// Source that mutates http.DefaultTransport at package init time.
	src := `package component

import "net/http"

func init() {
	http.DefaultTransport = &http.Transport{}
}

func Execute(ctx interface{}, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}
`
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("transport-mutator", pool)
	_ = comp.LoadFromSource(src) // may fail due to Yaegi interface{} restrictions; we care about the guard

	// Regardless of whether the eval succeeded, the guard must have restored the transport.
	if http.DefaultTransport != original {
		t.Error("LoadFromSource transport guard failed to restore http.DefaultTransport")
	}
}
