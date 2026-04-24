package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// infraStateStore is the minimal state persistence interface used by wfctl
// direct-path commands (apply, destroy, status, drift). It is a subset of
// interfaces.IaCStateStore focused on the operations the CLI actually performs.
type infraStateStore interface {
	ListResources(ctx context.Context) ([]interfaces.ResourceState, error)
	SaveResource(ctx context.Context, state interfaces.ResourceState) error
	DeleteResource(ctx context.Context, name string) error
}

// noopStateStore is an infraStateStore that silently discards all writes.
// It is used when no iac.state backend is configured or when an optional
// store is passed as nil.
type noopStateStore struct{}

func (n *noopStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	return nil, nil
}
func (n *noopStateStore) SaveResource(_ context.Context, _ interfaces.ResourceState) error {
	return nil
}
func (n *noopStateStore) DeleteResource(_ context.Context, _ string) error { return nil }

// resolveStateStore returns an infraStateStore for the iac.state backend
// declared in cfgFile. Returns a noopStateStore (not an error) when no
// iac.state module is present — first-run callers just get no-op persistence.
//
// When envName is non-empty, per-environment overrides (e.g. region, bucket
// prefix) are applied before the backend is initialised. This is required for
// remote backends (Spaces, S3, etc.) where credentials or endpoints differ per
// environment — without it the base config is used, which may be missing
// required fields such as region, causing init to fail.
func resolveStateStore(cfgFile, envName string) (infraStateStore, error) {
	cfgToUse := cfgFile
	if envName != "" {
		// Attempt env resolution so per-env backend config (e.g. region, prefix)
		// is applied before initialising the store. Failure is non-fatal — fall
		// back to the base config rather than dropping state persistence entirely.
		if tmp, err := writeEnvResolvedConfig(cfgFile, envName); err == nil {
			defer os.Remove(tmp)
			cfgToUse = tmp
		}
	}
	iacStates, _, _, err := discoverInfraModules(cfgToUse)
	if err != nil {
		return &noopStateStore{}, nil //nolint:nilerr // config not found / parse error means no state module; noop is correct
	}
	if len(iacStates) == 0 {
		return &noopStateStore{}, nil
	}
	m := iacStates[0]
	cfg := config.ExpandEnvInMap(m.Config)
	backend, _ := cfg["backend"].(string)

	switch backend {
	case "filesystem", "":
		dir, _ := cfg["directory"].(string)
		if dir == "" {
			dir = "/var/lib/workflow/iac-state"
		}
		return &fsWfctlStateStore{dir: dir}, nil

	case "spaces":
		return resolveSpacesStateStore(cfg)

	case "postgres":
		return resolvePostgresStateStore(cfg)

	case "s3":
		return nil, fmt.Errorf("s3 state store backend not yet supported by wfctl direct-path commands; " +
			"create the bucket manually and reference it in iac.state.bucket. " +
			"Contribute a resolveS3StateStore helper to unblock this")

	case "gcs":
		return nil, fmt.Errorf("gcs state store backend not yet supported by wfctl direct-path commands; " +
			"create the bucket manually and reference it in iac.state.bucket. " +
			"Contribute a resolveGCSStateStore helper to unblock this")

	case "azure":
		return nil, fmt.Errorf("azure state store backend not yet supported by wfctl direct-path commands; " +
			"create the container manually and reference it in iac.state.bucket. " +
			"Contribute a resolveAzureStateStore helper to unblock this")

	default:
		return nil, fmt.Errorf("unknown iac.state backend %q", backend)
	}
}

// ── Filesystem backend ─────────────────────────────────────────────────────────

// fsWfctlStateStore persists ResourceState records as JSON files under a
// directory, using the same on-disk format as loadFSState so that state
// written by applyWithProviderAndStore can be read back by loadCurrentState.
type fsWfctlStateStore struct {
	dir string
}

// iacStateRecord mirrors the JSON schema used by the filesystem and Spaces
// backends. The field names must stay stable to remain compatible with the
// existing loadFSState reader and the importFromTFState / importFromPulumi
// writers.
type iacStateRecord struct {
	ResourceID   string         `json:"resource_id"`
	ResourceType string         `json:"resource_type"`
	Provider     string         `json:"provider"`
	Status       string         `json:"status"`
	Config       map[string]any `json:"config"`
	Outputs      map[string]any `json:"outputs"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

func (s *fsWfctlStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list state: %w", err)
	}
	var states []interfaces.ResourceState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".lock.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var r iacStateRecord
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		states = append(states, iacRecordToResourceState(r))
	}
	return states, nil
}

func (s *fsWfctlStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("save state: mkdir: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	r := iacStateRecord{
		ResourceID:   state.ID,
		ResourceType: state.Type,
		Provider:     state.Provider,
		Status:       "active",
		Config:       state.AppliedConfig,
		Outputs:      state.Outputs,
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

func (s *fsWfctlStateStore) DeleteResource(_ context.Context, name string) error {
	fname := filepath.Join(s.dir, sanitizeStateID(name)+".json")
	if err := os.Remove(fname); err != nil {
		if os.IsNotExist(err) {
			return nil // idempotent
		}
		return fmt.Errorf("delete state %q: %w", name, err)
	}
	return nil
}

// ── Spaces backend ─────────────────────────────────────────────────────────────

// resolveSpacesStateStore builds a Spaces-backed state store from the expanded
// iac.state module config. Credentials fall back to DO_SPACES_ACCESS_KEY /
// DO_SPACES_SECRET_KEY environment variables via module.NewSpacesIaCStateStore.
func resolveSpacesStateStore(cfg map[string]any) (infraStateStore, error) {
	bucket, _ := cfg["bucket"].(string)
	region, _ := cfg["region"].(string)
	prefix, _ := cfg["prefix"].(string)

	accessKey, _ := cfg["accessKey"].(string)
	if accessKey == "" {
		accessKey, _ = cfg["access_key"].(string)
	}
	secretKey, _ := cfg["secretKey"].(string)
	if secretKey == "" {
		secretKey, _ = cfg["secret_key"].(string)
	}
	if bucket == "" {
		return nil, fmt.Errorf("iac.state backend=spaces requires 'bucket' in config")
	}
	inner, err := module.NewSpacesIaCStateStore(region, bucket, prefix, accessKey, secretKey, "")
	if err != nil {
		return nil, fmt.Errorf("init spaces state store: %w", err)
	}
	return &spacesWfctlStateStore{inner: inner}, nil
}

// spacesWfctlStateStore wraps module.SpacesIaCStateStore to implement
// infraStateStore, bridging module.IaCState ↔ interfaces.ResourceState.
type spacesWfctlStateStore struct {
	inner *module.SpacesIaCStateStore
}

func (s *spacesWfctlStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	records, err := s.inner.ListStates(nil)
	if err != nil {
		return nil, fmt.Errorf("list spaces state: %w", err)
	}
	states := make([]interfaces.ResourceState, 0, len(records))
	for _, r := range records {
		states = append(states, iacStateToResourceState(r))
	}
	return states, nil
}

func (s *spacesWfctlStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	return s.inner.SaveState(resourceStateToIaCState(state))
}

func (s *spacesWfctlStateStore) DeleteResource(_ context.Context, name string) error {
	return s.inner.DeleteState(name)
}

// ── Postgres backend ───────────────────────────────────────────────────────────

// resolvePostgresStateStore builds a Postgres-backed state store from the
// expanded iac.state module config. The config must include a `dsn` field
// (or `connection_string`) with a valid PostgreSQL DSN.
func resolvePostgresStateStore(cfg map[string]any) (infraStateStore, error) {
	dsn, _ := cfg["dsn"].(string)
	if dsn == "" {
		dsn, _ = cfg["connection_string"].(string)
	}
	if dsn == "" {
		return nil, fmt.Errorf("iac.state backend=postgres requires 'dsn' or 'connection_string' in config")
	}
	inner, err := module.NewPostgresIaCStateStore(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("init postgres state store: %w", err)
	}
	return &postgresWfctlStateStore{inner: inner}, nil
}

// postgresWfctlStateStore wraps module.PostgresIaCStateStore to implement
// infraStateStore, bridging module.IaCState ↔ interfaces.ResourceState.
type postgresWfctlStateStore struct {
	inner *module.PostgresIaCStateStore
}

func (s *postgresWfctlStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	records, err := s.inner.ListStates(nil)
	if err != nil {
		return nil, fmt.Errorf("list postgres state: %w", err)
	}
	states := make([]interfaces.ResourceState, 0, len(records))
	for _, r := range records {
		states = append(states, iacStateToResourceState(r))
	}
	return states, nil
}

func (s *postgresWfctlStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	return s.inner.SaveState(resourceStateToIaCState(state))
}

func (s *postgresWfctlStateStore) DeleteResource(_ context.Context, name string) error {
	return s.inner.DeleteState(name)
}

// ── Conversion helpers ─────────────────────────────────────────────────────────

func iacRecordToResourceState(r iacStateRecord) interfaces.ResourceState {
	return interfaces.ResourceState{
		ID:            r.ResourceID,
		Name:          r.ResourceID,
		Type:          r.ResourceType,
		Provider:      r.Provider,
		ProviderID:    r.ResourceID,
		ConfigHash:    configHashMap(r.Config),
		AppliedConfig: r.Config,
		Outputs:       r.Outputs,
	}
}

func iacStateToResourceState(r *module.IaCState) interfaces.ResourceState {
	return interfaces.ResourceState{
		ID:            r.ResourceID,
		Name:          r.ResourceID,
		Type:          r.ResourceType,
		Provider:      r.Provider,
		ProviderID:    r.ResourceID,
		ConfigHash:    configHashMap(r.Config),
		AppliedConfig: r.Config,
		Outputs:       r.Outputs,
	}
}

func resourceStateToIaCState(state interfaces.ResourceState) *module.IaCState {
	now := time.Now().UTC().Format(time.RFC3339)
	return &module.IaCState{
		ResourceID:   state.ID,
		ResourceType: state.Type,
		Provider:     state.Provider,
		Status:       "active",
		Config:       state.AppliedConfig,
		Outputs:      state.Outputs,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}
