package module

import (
	"fmt"
	"strings"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ─────────────────────────────────────────────────────────────────────────────
// iacStateBackendRegistry — engine-side registry mapping an iac.state backend
// name to a plugin-served pb.IaCStateBackendClient.
//
// The engine populates the package-level singleton at plugin-load time
// (Task 14); IaCModule.Init consults it for any backend name not handled by an
// in-process core case. Reserved core backend names (memory/filesystem/postgres)
// — the backends that have no cloud SDK and stay in core — cannot be claimed by
// a plugin.
// ─────────────────────────────────────────────────────────────────────────────

// reservedIaCStateBackends are the core backend names a plugin may never claim.
var reservedIaCStateBackends = map[string]struct{}{
	"memory":     {},
	"filesystem": {},
	"postgres":   {},
}

// iacStateBackendRegistry maps a backend name to a plugin gRPC client.
type iacStateBackendRegistry struct {
	mu      sync.RWMutex
	clients map[string]pb.IaCStateBackendClient
}

// newIaCStateBackendRegistry constructs an empty registry.
func newIaCStateBackendRegistry() *iacStateBackendRegistry {
	return &iacStateBackendRegistry{clients: make(map[string]pb.IaCStateBackendClient)}
}

// register associates a backend name with a plugin client. The name must be
// non-empty (after trimming) and the client must be non-nil. Reserved core
// backend names are rejected. Re-registering a non-reserved name overwrites the
// previous client (last plugin loaded wins).
func (r *iacStateBackendRegistry) register(name string, client pb.IaCStateBackendClient) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("iac.state backend registration: name must not be empty")
	}
	if client == nil {
		return fmt.Errorf("iac.state backend registration %q: client must not be nil", name)
	}
	if _, reserved := reservedIaCStateBackends[name]; reserved {
		return fmt.Errorf("plugin registered reserved iac.state backend name %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = client
	return nil
}

// resolve returns the plugin client for a backend name, and whether one is
// registered.
func (r *iacStateBackendRegistry) resolve(name string) (pb.IaCStateBackendClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

// iacStateBackendRegistryInstance is the package-level singleton the engine
// populates and IaCModule.Init consults.
var iacStateBackendRegistryInstance = newIaCStateBackendRegistry()

// RegisterIaCStateBackend associates an iac.state backend name with a
// plugin-served gRPC client in the package-level registry. The engine calls
// this at plugin-load for each backend name a loaded plugin advertises;
// IaCModule.Init then resolves `backend: <name>` configs against it. Reserved
// core backend names (memory/filesystem/postgres) and empty names / nil clients
// are rejected — see iacStateBackendRegistry.register. Amendment A2
// (decisions/0035).
func RegisterIaCStateBackend(name string, client pb.IaCStateBackendClient) error {
	return iacStateBackendRegistryInstance.register(name, client)
}
