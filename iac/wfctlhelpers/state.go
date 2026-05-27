package wfctlhelpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ResolveStateStore loads cfgFile, finds the iac.state module, and returns
// its backend as a full interfaces.IaCStateStore. envName is forwarded for
// per-environment backend config resolution; empty = no env overrides.
//
// pluginDir locates plugin binaries for plugin-served backends
// (spaces/s3/gcs). The lookup order is:
//  1. pluginDir argument when non-empty
//  2. WFCTL_PLUGIN_DIR environment variable
//  3. "./data/plugins" (legacy default)
//
// The host-side infra.admin module (workflow/module/infra_admin.go) is
// expected to pass an empty string and rely on the WFCTL_PLUGIN_DIR
// fallback so a single env var configures both CLI and module. The CLI
// passes its `currentInfraPluginDir` seam variable to honor the
// --plugin-dir flag.
//
// Returns a no-op store (not an error) when no iac.state module is
// declared so first-run callers get silent no-op persistence — same
// behavior as the wfctl-internal resolveStateStore this helper was lifted
// from per docs/plans/2026-05-27-infra-admin-dynamic.md Task 1.
//
// Per design doc cycle-5 row 4: out-of-subset methods on the returned
// store (Lock, SavePlan, GetPlan) panic with a clear message. The handler
// library and host module call only the subset
// {SaveResource, GetResource, ListResources, DeleteResource, Close}.
func ResolveStateStore(cfgFile, envName, pluginDir string) (interfaces.IaCStateStore, error) {
	cfgToUse := cfgFile
	if envName != "" {
		tmp, err := WriteEnvResolvedConfig(cfgFile, envName)
		if err != nil {
			return nil, fmt.Errorf("resolve %q environment for state store: %w", envName, err)
		}
		defer os.Remove(tmp)
		cfgToUse = tmp
	}
	cfg, err := config.LoadFromFile(cfgToUse)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", cfgToUse, err)
	}
	var stateModule *config.ModuleConfig
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "iac.state" {
			stateModule = &cfg.Modules[i]
			break
		}
	}
	if stateModule == nil {
		return &NoopStateStore{}, nil
	}
	mcfg := config.ExpandEnvInMap(stateModule.Config)
	backend, _ := mcfg["backend"].(string)

	switch backend {
	case "memory":
		return wrapModuleStore(module.NewMemoryIaCStateStore()), nil

	case "filesystem", "":
		dir, _ := mcfg["directory"].(string)
		if dir == "" {
			dir = "/var/lib/workflow/iac-state"
		}
		return &FSStateStore{dir: dir}, nil

	case "postgres":
		dsn, _ := mcfg["dsn"].(string)
		if dsn == "" {
			dsn, _ = mcfg["connection_string"].(string)
		}
		if dsn == "" {
			return nil, fmt.Errorf("iac.state backend=postgres requires 'dsn' or 'connection_string' in config")
		}
		inner, err := module.NewPostgresIaCStateStore(context.Background(), dsn)
		if err != nil {
			return nil, fmt.Errorf("init postgres state store: %w", err)
		}
		return wrapModuleStore(inner), nil

	case "spaces", "s3", "gcs":
		return resolvePluginStore(context.Background(), backend, mcfg, pluginDir)

	case "azure":
		return nil, fmt.Errorf("azure state store backend not yet supported by wfctl direct-path commands; " +
			"create the container manually and reference it in iac.state.bucket. " +
			"Contribute a resolveAzureStateStore helper to unblock this")

	default:
		return nil, fmt.Errorf("unknown iac.state backend %q", backend)
	}
}

// IsNoopStateStore reports whether the resolved store is the no-op
// fallback returned when no iac.state module is configured. Accepts any
// concrete or interface value so wfctl-side subset-interface holders can
// check without having to widen their static type to interfaces.IaCStateStore.
func IsNoopStateStore(s any) bool {
	_, ok := s.(*NoopStateStore)
	return ok
}

// ── No-op store ────────────────────────────────────────────────────────────────

// NoopStateStore satisfies interfaces.IaCStateStore but silently discards
// all writes and returns no resources / no plans. Used when no iac.state
// module is declared so callers get a usable handle without needing to
// special-case the missing-state case.
type NoopStateStore struct{}

func (n *NoopStateStore) SaveResource(_ context.Context, _ interfaces.ResourceState) error {
	return nil
}
func (n *NoopStateStore) GetResource(_ context.Context, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (n *NoopStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	return nil, nil
}
func (n *NoopStateStore) DeleteResource(_ context.Context, _ string) error { return nil }
func (n *NoopStateStore) SavePlan(_ context.Context, _ interfaces.IaCPlan) error {
	panic("wfctlhelpers: NoopStateStore.SavePlan called — out-of-subset method on the handler/module store")
}
func (n *NoopStateStore) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	panic("wfctlhelpers: NoopStateStore.GetPlan called — out-of-subset method on the handler/module store")
}
func (n *NoopStateStore) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	panic("wfctlhelpers: NoopStateStore.Lock called — out-of-subset method on the handler/module store")
}
func (n *NoopStateStore) Close() error { return nil }

// ── Filesystem store ───────────────────────────────────────────────────────────

// StateRecord mirrors the JSON schema used by the wfctl filesystem
// backend. Field names must stay byte-stable with cmd/wfctl's
// iacStateRecord so state written by either path is mutually readable.
// See cmd/wfctl/state_compat_test.go for the on-disk-format compatibility
// matrix.
type StateRecord struct {
	ResourceID   string         `json:"resource_id"`
	ResourceType string         `json:"resource_type"`
	Provider     string         `json:"provider"`
	ProviderRef  string         `json:"provider_ref,omitempty"`
	ProviderID   string         `json:"provider_id,omitempty"`
	ConfigHash   string         `json:"config_hash,omitempty"`
	Status       string         `json:"status"`
	Config       map[string]any `json:"config"`
	Outputs      map[string]any `json:"outputs"`
	Dependencies []string       `json:"dependencies,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

// FSStateStore persists ResourceState records as JSON files under a
// directory, using the same on-disk format as cmd/wfctl's fsWfctlStateStore
// so state written by either is mutually readable.
type FSStateStore struct {
	dir string
}

func (s *FSStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list state: %w", err)
	}
	var states []interfaces.ResourceState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") ||
			strings.HasSuffix(e.Name(), ".lock.json") || e.Name() == "metadata.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read state %q: %w", e.Name(), err)
		}
		var r StateRecord
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parse state %q: %w", e.Name(), err)
		}
		states = append(states, stateRecordToResource(r))
	}
	return states, nil
}

func (s *FSStateStore) GetResource(ctx context.Context, name string) (*interfaces.ResourceState, error) {
	fname := filepath.Join(s.dir, sanitizeStateID(name)+".json")
	data, err := os.ReadFile(fname)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state %q: %w", name, err)
	}
	var r StateRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse state %q: %w", name, err)
	}
	rs := stateRecordToResource(r)
	return &rs, nil
}

func (s *FSStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("save state: mkdir: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	r := StateRecord{
		ResourceID:   state.ID,
		ResourceType: state.Type,
		Provider:     state.Provider,
		ProviderRef:  state.ProviderRef,
		ProviderID:   state.ProviderID,
		ConfigHash:   state.ConfigHash,
		Status:       "active",
		Config:       state.AppliedConfig,
		Outputs:      state.Outputs,
		Dependencies: append([]string(nil), state.Dependencies...),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("save state %q: marshal: %w", state.ID, err)
	}
	fname := filepath.Join(s.dir, sanitizeStateID(state.ID)+".json")
	if err := os.WriteFile(fname, data, 0o600); err != nil {
		return fmt.Errorf("save state %q: write: %w", state.ID, err)
	}
	return nil
}

func (s *FSStateStore) DeleteResource(_ context.Context, name string) error {
	fname := filepath.Join(s.dir, sanitizeStateID(name)+".json")
	if err := os.Remove(fname); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete state %q: %w", name, err)
	}
	return nil
}

// SaveMetadata writes the generator metadata.json file alongside the
// per-resource state files. cmd/wfctl's apply path performs a runtime
// type assertion against an internal metadataPersister interface; mirroring
// the method here keeps that assertion working when the store is built
// through this helper.
func (s *FSStateStore) SaveMetadata(_ context.Context, meta interfaces.GeneratorMetadata) error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("save metadata: mkdir: %w", err)
	}
	wrapper := struct {
		GeneratorMetadata interfaces.GeneratorMetadata `json:"generator_metadata"`
	}{GeneratorMetadata: meta}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("save metadata: marshal: %w", err)
	}
	fname := filepath.Join(s.dir, "metadata.json")
	if err := os.WriteFile(fname, data, 0o600); err != nil {
		return fmt.Errorf("save metadata: write: %w", err)
	}
	return nil
}

func (s *FSStateStore) SavePlan(_ context.Context, _ interfaces.IaCPlan) error {
	panic("wfctlhelpers: FSStateStore.SavePlan called — out-of-subset method on the handler/module store")
}
func (s *FSStateStore) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	panic("wfctlhelpers: FSStateStore.GetPlan called — out-of-subset method on the handler/module store")
}
func (s *FSStateStore) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	panic("wfctlhelpers: FSStateStore.Lock called — out-of-subset method on the handler/module store")
}
func (s *FSStateStore) Close() error { return nil }

// ── module.IaCStateStore adapter (memory + postgres + gRPC plugin) ─────────────

// moduleStoreAdapter wraps a module.IaCStateStore (which uses
// {GetState/SaveState/ListStates/DeleteState/Lock/Unlock} with
// *module.IaCState records) and exposes the full interfaces.IaCStateStore
// (which uses {SaveResource/...} with interfaces.ResourceState records).
// Out-of-subset methods (SavePlan, GetPlan, Lock with TTL) panic per design
// doc cycle-5 row 4.
type moduleStoreAdapter struct {
	inner module.IaCStateStore
	mu    sync.Mutex
	mgr   *external.ExternalPluginManager // non-nil for plugin-served backends; Shutdown on Close
}

func wrapModuleStore(inner module.IaCStateStore) *moduleStoreAdapter {
	return &moduleStoreAdapter{inner: inner}
}

func (a *moduleStoreAdapter) SaveResource(ctx context.Context, state interfaces.ResourceState) error {
	return a.inner.SaveState(ctx, resourceStateToIaCState(state))
}

func (a *moduleStoreAdapter) GetResource(ctx context.Context, name string) (*interfaces.ResourceState, error) {
	rec, err := a.inner.GetState(ctx, name)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	rs := iacStateToResourceState(rec)
	return &rs, nil
}

func (a *moduleStoreAdapter) ListResources(ctx context.Context) ([]interfaces.ResourceState, error) {
	states, err := a.inner.ListStates(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]interfaces.ResourceState, 0, len(states))
	for _, s := range states {
		out = append(out, iacStateToResourceState(s))
	}
	return out, nil
}

func (a *moduleStoreAdapter) DeleteResource(ctx context.Context, name string) error {
	return a.inner.DeleteState(ctx, name)
}

func (a *moduleStoreAdapter) SavePlan(_ context.Context, _ interfaces.IaCPlan) error {
	panic("wfctlhelpers: moduleStoreAdapter.SavePlan called — out-of-subset method on the handler/module store")
}
func (a *moduleStoreAdapter) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	panic("wfctlhelpers: moduleStoreAdapter.GetPlan called — out-of-subset method on the handler/module store")
}
func (a *moduleStoreAdapter) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	panic("wfctlhelpers: moduleStoreAdapter.Lock called — out-of-subset method on the handler/module store")
}

func (a *moduleStoreAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mgr != nil {
		a.mgr.Shutdown()
		a.mgr = nil
	}
	return nil
}

// ── Plugin-served backends (spaces/s3/gcs via external plugin) ────────────────

func resolvePluginStore(ctx context.Context, backend string, cfg map[string]any, pluginDir string) (interfaces.IaCStateStore, error) {
	if pluginDir == "" {
		pluginDir = os.Getenv("WFCTL_PLUGIN_DIR")
	}
	if pluginDir == "" {
		pluginDir = "./data/plugins"
	}

	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("iac.state backend %q is plugin-served but plugin directory %q is unavailable: %w", backend, pluginDir, err)
	}

	mgr := external.NewExternalPluginManager(pluginDir, nil)
	for _, pluginName := range stateBackendPluginCandidates(backend, entries) {
		clients, clientsErr := loadPluginStateBackendClients(mgr, pluginName, backend)
		if clientsErr != nil {
			mgr.Shutdown()
			return nil, clientsErr
		}
		client, ok := clients[backend]
		if !ok {
			continue
		}
		store := module.NewGRPCIaCStateStore(client)
		if err := store.Configure(ctx, backend, cfg); err != nil {
			mgr.Shutdown()
			return nil, fmt.Errorf("configure plugin-served iac.state backend %q via plugin %q: %w", backend, pluginName, err)
		}
		adapter := wrapModuleStore(store)
		adapter.mgr = mgr // Close → Shutdown the plugin process
		return adapter, nil
	}

	mgr.Shutdown()
	return nil, fmt.Errorf("iac.state backend %q is plugin-served but no installed plugin in %s advertises it", backend, pluginDir)
}

// loadPluginStateBackendClients is exposed as a package-level variable so
// tests can substitute a fake plugin loader without touching real plugin
// binaries. The default loads via the external plugin manager.
var loadPluginStateBackendClients = func(mgr *external.ExternalPluginManager, pluginName, backend string) (map[string]pb.IaCStateBackendClient, error) {
	adapter, loadErr := mgr.LoadPlugin(pluginName)
	if loadErr != nil {
		return nil, fmt.Errorf("load plugin %q for iac.state backend %q: %w", pluginName, backend, loadErr)
	}
	clients, clientsErr := adapter.IaCStateBackendClients()
	if clientsErr != nil {
		return nil, fmt.Errorf("plugin %q iac.state backends: %w", pluginName, clientsErr)
	}
	return clients, nil
}

// stateBackendPluginCandidates returns the ordered list of plugin
// directories under pluginDir that may serve the requested backend.
// First-match candidates (digitalocean→spaces, aws→s3, gcp→gcs) are
// prioritized; remaining entries follow in directory order.
func stateBackendPluginCandidates(backend string, entries []os.DirEntry) []string {
	seen := map[string]struct{}{}
	var candidates []string
	hasDir := func(name string) bool {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() == name {
				return true
			}
		}
		return false
	}
	add := func(name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}
	switch backend {
	case "spaces":
		if hasDir("digitalocean") {
			add("digitalocean")
		}
	case "s3":
		if hasDir("aws") {
			add("aws")
		}
	case "gcs":
		if hasDir("gcp") {
			add("gcp")
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			add(entry.Name())
		}
	}
	return candidates
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// sanitizeStateID returns a filesystem-safe filename for a resource ID.
// The same algorithm is used by cmd/wfctl/infra_state.go:sanitizeStateID
// so files written by either path collide on the same key.
func sanitizeStateID(id string) string {
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._"
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if strings.ContainsRune(allowed, r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func stateRecordToResource(r StateRecord) interfaces.ResourceState {
	providerID := r.ProviderID
	if providerID == "" {
		providerID = r.ResourceID
	}
	return interfaces.ResourceState{
		ID:            r.ResourceID,
		Name:          r.ResourceID,
		Type:          r.ResourceType,
		Provider:      r.Provider,
		ProviderRef:   r.ProviderRef,
		ProviderID:    providerID,
		ConfigHash:    r.ConfigHash,
		AppliedConfig: r.Config,
		Outputs:       r.Outputs,
		Dependencies:  append([]string(nil), r.Dependencies...),
	}
}

func iacStateToResourceState(r *module.IaCState) interfaces.ResourceState {
	providerID := r.ProviderID
	if providerID == "" {
		providerID = r.ResourceID
	}
	return interfaces.ResourceState{
		ID:            r.ResourceID,
		Name:          r.ResourceID,
		Type:          r.ResourceType,
		Provider:      r.Provider,
		ProviderRef:   r.ProviderRef,
		ProviderID:    providerID,
		ConfigHash:    r.ConfigHash,
		AppliedConfig: r.Config,
		Outputs:       r.Outputs,
		Dependencies:  append([]string(nil), r.Dependencies...),
	}
}

func resourceStateToIaCState(state interfaces.ResourceState) *module.IaCState {
	now := time.Now().UTC().Format(time.RFC3339)
	return &module.IaCState{
		ResourceID:   state.ID,
		ResourceType: state.Type,
		Provider:     state.Provider,
		ProviderRef:  state.ProviderRef,
		ProviderID:   state.ProviderID,
		ConfigHash:   state.ConfigHash,
		Status:       "active",
		Config:       state.AppliedConfig,
		Outputs:      state.Outputs,
		Dependencies: append([]string(nil), state.Dependencies...),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}
