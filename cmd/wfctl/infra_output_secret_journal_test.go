package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

const aliasJournalMarker = "known-private-alias-journal-value"

type aliasJournalDriver struct{ stubSensitiveDriver }

func (*aliasJournalDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "cloud-database",
		Outputs: map[string]any{"uri": aliasJournalMarker, "host": "consumer-payload"}, Sensitive: map[string]bool{"uri": true}}, nil
}

func TestInfraOutputAliasJournal_CLIAndConfigRemoval(t *testing.T) {
	dir := t.TempDir()
	stateDir, routedDir, aliasDir := filepath.Join(dir, "state"), filepath.Join(dir, "routed"), filepath.Join(dir, "aliases")
	cfgFile := filepath.Join(dir, "infra.yaml")
	writeConfig := func(withAlias bool) {
		t.Helper()
		cfg := fmt.Sprintf("infra: {auto_bootstrap: false}\nmodules:\n  - name: provider\n    type: iac.provider\n    config: {provider: test-cloud}\n  - name: state\n    type: iac.state\n    config: {backend: filesystem, directory: %s}\n  - name: database\n    type: infra.database\n    config: {provider: provider}\nsecretStores:\n  named:\n    provider: file\n    config: {path: %s}\nsecrets:\n  provider: file\n  config: {path: %s}\n", stateDir, aliasDir, routedDir)
		if withAlias {
			cfg += "  generate:\n    - key: DATABASE_URL\n      type: infra_output\n      source: database.uri\n      store: named\n"
		}
		if err := os.WriteFile(cfgFile, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(true)
	provider := &declarativeCLIProvider{driver: &aliasJournalDriver{}}
	original := resolveIaCProvider
	resolveIaCProvider = func(context.Context, string, map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return provider, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = original })
	if err := runInfraApply([]string{"--config", cfgFile, "--auto-approve"}); err != nil {
		t.Fatal(err)
	}
	aliasProvider := secrets.NewFileProvider(aliasDir)
	if value, err := aliasProvider.Get(t.Context(), "DATABASE_URL"); err != nil || value != aliasJournalMarker {
		t.Fatalf("actual file provider did not receive unchanged output: err=%v", err)
	}
	store, err := resolveStateStore(cfgFile, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupResource(t.Context(), store, "database")
	if err != nil || state == nil || state.Lifecycle == nil {
		t.Fatalf("source journal missing: state=%v err=%v", state, err)
	}
	target := secrets.DescribeTarget(aliasProvider)
	want := interfaces.RoutedSecretReference{Key: "DATABASE_URL", Store: "named", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
	if !containsAliasReference(state.Lifecycle, want) || len(state.Lifecycle.Secrets) != 2 || state.Lifecycle.Generation == "" || state.Lifecycle.Phase != interfaces.ResourcePhaseActive {
		t.Fatalf("durable journal lost alias/derived identifiers: %+v", state.Lifecycle)
	}
	assertAliasStateSafe(t, store)
	if state.Outputs["host"] != "consumer-payload" || state.Outputs["uri"] != sensitive.Placeholder("database", "uri") {
		t.Fatal("alias sync altered source outputs")
	}
	writeConfig(false)
	removedCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	removedSecrets, err := parseSecretsConfig(cfgFile)
	if err != nil || len(removedSecrets.Generate) != 0 {
		t.Fatalf("generator removal fixture invalid: err=%v", err)
	}
	routedProvider := secrets.NewFileProvider(routedDir)
	if err := aliasProvider.Set(t.Context(), "UNRELATED", "consumer-payload"); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareResourceSecretDeletion(t.Context(), store, routedProvider, interfaces.PlanAction{
		Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcileResourceSecretDeletion(t.Context(), store, func(ref interfaces.RoutedSecretReference) (secrets.Provider, error) {
		return providerForSecretGen(removedCfg, routedProvider, SecretGen{Key: ref.Key, Store: ref.Store}, "")
	}, *prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := aliasProvider.Get(t.Context(), "DATABASE_URL"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("named alias survived cleanup after generator removal")
	}
	if _, err := routedProvider.Get(t.Context(), sensitive.SecretKey("database", "uri")); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("derived output key survived cleanup")
	}
	if value, err := aliasProvider.Get(t.Context(), "UNRELATED"); err != nil || value != "consumer-payload" {
		t.Fatal("cleanup changed unrelated secret")
	}
	if states, err := store.ListResources(t.Context()); err != nil || len(states) != 0 {
		t.Fatalf("cleanup did not finalize state: err=%v count=%d", err, len(states))
	}
}

func containsAliasReference(lifecycle *interfaces.ResourceLifecycle, want interfaces.RoutedSecretReference) bool {
	if lifecycle == nil {
		return false
	}
	for _, ref := range lifecycle.Secrets {
		if reflect.DeepEqual(ref, want) {
			return true
		}
	}
	return false
}

func assertAliasStateSafe(t *testing.T, store infraStateStore) {
	t.Helper()
	states, err := store.ListResources(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(states)
	if err != nil || strings.Contains(string(data), aliasJournalMarker) {
		t.Fatalf("journal/state exposed known output bytes: err=%v", err)
	}
}

type aliasJournalFixture struct {
	store    infraStateStore
	state    interfaces.ResourceState
	workflow *config.WorkflowConfig
	secrets  *SecretsConfig
	provider *secrets.FileProvider
}

func newAliasJournalFixture(t *testing.T) aliasJournalFixture {
	t.Helper()
	store := cleanupStateStore(t)
	provider := secrets.NewFileProvider(t.TempDir())
	state := interfaces.ResourceState{ID: "database", Name: "database", Type: "infra.database", ProviderID: "cloud-id",
		Outputs: map[string]any{"uri": sensitive.Placeholder("database", "uri"), "host": "consumer-payload"},
		Lifecycle: &interfaces.ResourceLifecycle{Generation: "existing-generation", Phase: interfaces.ResourcePhaseActive,
			Secrets: []interfaces.RoutedSecretReference{{Key: "prior-derived-key", Provider: "file", Scope: "directory", Subject: "prior-store"}}}}
	if err := store.SaveResource(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	return aliasJournalFixture{store: store, state: state, provider: provider,
		workflow: &config.WorkflowConfig{SecretStores: map[string]*config.SecretStoreConfig{
			"named": {Provider: "file", Config: map[string]any{"path": secrets.DescribeTarget(provider).Subject}},
		}},
		secrets: &SecretsConfig{Generate: []SecretGen{{Key: "DATABASE_URL", Type: "infra_output", Source: "database.uri", Store: "named"}}}}
}

func (f aliasJournalFixture) sync(ctx context.Context, store infraStateStore, refresh bool) error {
	return syncInfraOutputSecretsScoped(ctx, f.secrets, nil, []interfaces.ResourceState{f.state}, f.workflow, "",
		map[string]string{sensitive.SecretKey("database", "uri"): aliasJournalMarker}, refresh, nil, store)
}

type aliasAcknowledgmentStore struct {
	infraStateStore
	saves       int
	lists       int
	fault       string
	beforeSave  func(interfaces.ResourceState)
	ackObserved func()
}

func (s *aliasAcknowledgmentStore) SaveResource(ctx context.Context, state interfaces.ResourceState) error {
	s.saves++
	if s.beforeSave != nil {
		s.beforeSave(state)
	}
	if s.fault == "save" {
		return fmt.Errorf("%w: %s", interfaces.ErrTransient, aliasJournalMarker)
	}
	if s.fault == "drop-lifecycle" {
		state.Lifecycle = nil
	}
	if s.fault == "drop-alias" {
		state.Lifecycle = cloneResourceLifecycle(state.Lifecycle)
		state.Lifecycle.Secrets = state.Lifecycle.Secrets[:1]
	}
	return s.infraStateStore.SaveResource(ctx, state)
}

func (s *aliasAcknowledgmentStore) ListResources(ctx context.Context) ([]interfaces.ResourceState, error) {
	s.lists++
	if s.fault == "read" || (s.fault == "ack-read" && s.saves > 0) || (s.fault == "ownership-read" && s.lists == 2) {
		return nil, fmt.Errorf("%w: %s", interfaces.ErrTransient, aliasJournalMarker)
	}
	states, err := s.infraStateStore.ListResources(ctx)
	if s.saves > 0 && s.ackObserved != nil {
		s.ackObserved()
	}
	return states, err
}

func TestInfraOutputAliasJournal_WriteAheadAndDedup(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh=%t", refresh), func(t *testing.T) {
			f := newAliasJournalFixture(t)
			target := secrets.DescribeTarget(f.provider)
			want := interfaces.RoutedSecretReference{Key: "DATABASE_URL", Store: "named", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
			if refresh {
				if err := f.provider.Set(t.Context(), want.Key, "old-value"); err != nil {
					t.Fatal(err)
				}
				prior := want
				prior.VerifiedAbsent = true
				f.state.Lifecycle.Secrets = append(f.state.Lifecycle.Secrets, prior)
				if err := f.store.SaveResource(t.Context(), f.state); err != nil {
					t.Fatal(err)
				}
			}
			assertNotWritten := func() {
				value, err := f.provider.Get(t.Context(), want.Key)
				if (!refresh && !errors.Is(err, secrets.ErrNotFound)) || (refresh && (err != nil || value != "old-value")) {
					t.Fatal("alias Set preceded durable journal acknowledgment")
				}
			}
			store := &aliasAcknowledgmentStore{infraStateStore: f.store, ackObserved: assertNotWritten}
			store.beforeSave = func(state interfaces.ResourceState) {
				assertNotWritten()
				if !containsAliasReference(state.Lifecycle, want) {
					t.Fatal("pre-write journal does not describe actual alias target")
				}
			}
			if err := f.sync(t.Context(), store, refresh); err != nil {
				t.Fatal(err)
			}
			if store.saves != 1 {
				t.Fatalf("alias intent must be saved once before Set, got %d", store.saves)
			}
			state, err := loadCleanupResource(t.Context(), f.store, f.state.Name)
			if err != nil || !containsAliasReference(state.Lifecycle, want) || len(state.Lifecycle.Secrets) != 2 || state.Lifecycle.Generation != "existing-generation" || state.Lifecycle.Phase != interfaces.ResourcePhaseActive {
				t.Fatalf("alias replaced generation, lost prior ref, or retained absence evidence: state=%v err=%v", state, err)
			}
			if value, err := f.provider.Get(t.Context(), want.Key); err != nil || value != aliasJournalMarker {
				t.Fatal("Set did not preserve output bytes")
			}
			assertAliasStateSafe(t, f.store)
		})
	}
}

func TestInfraOutputAliasJournal_SkippedAliasesNotClaimed(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh=%t", refresh), func(t *testing.T) {
			f := newAliasJournalFixture(t)
			value := "pre-existing-value"
			if refresh {
				value = aliasJournalMarker
			}
			if err := f.provider.Set(t.Context(), "DATABASE_URL", value); err != nil {
				t.Fatal(err)
			}
			store := &aliasAcknowledgmentStore{infraStateStore: f.store}
			if err := f.sync(t.Context(), store, refresh); err != nil {
				t.Fatal(err)
			}
			state, err := loadCleanupResource(t.Context(), f.store, f.state.Name)
			if err != nil || store.saves != 0 || !reflect.DeepEqual(state.Lifecycle, f.state.Lifecycle) {
				t.Fatalf("skipped user-managed alias was claimed: saves=%d err=%v", store.saves, err)
			}
			if got, err := f.provider.Get(t.Context(), "DATABASE_URL"); err != nil || got != value {
				t.Fatal("skipped alias changed")
			}
		})
	}
}

func TestInfraOutputAliasJournal_FailsClosedBeforeSet(t *testing.T) {
	for _, fault := range []string{"save", "read", "ack-read", "drop-lifecycle", "drop-alias", "nil-store", "noop", "missing", "generation", "identity", "cleanup-pending"} {
		t.Run(fault, func(t *testing.T) {
			f := newAliasJournalFixture(t)
			store := &aliasAcknowledgmentStore{infraStateStore: f.store, fault: fault}
			var selected infraStateStore = store
			switch fault {
			case "nil-store":
				selected = nil
			case "noop":
				selected = &noopStateStore{}
			case "missing":
				if err := f.store.DeleteResource(t.Context(), f.state.Name); err != nil {
					t.Fatal(err)
				}
			case "generation", "identity", "cleanup-pending":
				changed := f.state
				changed.Lifecycle = cloneResourceLifecycle(changed.Lifecycle)
				switch fault {
				case "generation":
					changed.Lifecycle.Generation = "other-generation"
				case "identity":
					changed.ProviderID = "other-cloud-resource"
				default:
					changed.Lifecycle.Phase = interfaces.ResourcePhaseCloudDeletedSecretCleanupPending
				}
				if err := f.store.SaveResource(t.Context(), changed); err != nil {
					t.Fatal(err)
				}
			}
			err := f.sync(t.Context(), selected, false)
			if err == nil {
				t.Fatal("unacknowledged/mismatched journal allowed alias Set")
			}
			if strings.Contains(err.Error(), aliasJournalMarker) {
				t.Fatal("journal error disclosed known value")
			}
			if fault == "save" || fault == "read" || fault == "ack-read" {
				if !errors.Is(err, interfaces.ErrTransient) {
					t.Fatal("safe diagnostic lost backend cause")
				}
			}
			if _, err := f.provider.Get(t.Context(), "DATABASE_URL"); !errors.Is(err, secrets.ErrNotFound) {
				t.Fatal("alias written without metadata ACK")
			}
			assertAliasStateSafe(t, f.store)
		})
	}
}

type aliasFailingProvider struct {
	*secrets.FileProvider
	fault string
}

func (p aliasFailingProvider) Get(ctx context.Context, key string) (string, error) {
	if p.fault == "get" {
		return "", fmt.Errorf("%w: %s", interfaces.ErrForbidden, aliasJournalMarker)
	}
	if p.fault == "list" {
		return "", secrets.ErrUnsupported
	}
	return p.FileProvider.Get(ctx, key)
}

func (p aliasFailingProvider) List(context.Context) ([]string, error) {
	return nil, fmt.Errorf("%w: %s", interfaces.ErrForbidden, aliasJournalMarker)
}

func (p aliasFailingProvider) Set(ctx context.Context, key, value string) error {
	if err := p.FileProvider.Set(ctx, key, value); err != nil {
		return err
	}
	return fmt.Errorf("%w: provider echoed %s", interfaces.ErrForbidden, value)
}

func TestInfraOutputAliasJournal_ProviderErrorsSafeAndDebtRetained(t *testing.T) {
	for _, fault := range []string{"get", "list", "set-after-write"} {
		t.Run(fault, func(t *testing.T) {
			f := newAliasJournalFixture(t)
			f.secrets.Generate[0].Store = ""
			provider := aliasFailingProvider{FileProvider: f.provider, fault: fault}
			err := syncInfraOutputSecretsScoped(t.Context(), f.secrets, provider, []interfaces.ResourceState{f.state}, f.workflow, "",
				map[string]string{sensitive.SecretKey("database", "uri"): aliasJournalMarker}, false, nil, f.store)
			if err == nil || !errors.Is(err, interfaces.ErrForbidden) || strings.Contains(err.Error(), aliasJournalMarker) {
				t.Fatalf("provider failure must preserve cause but hide bytes: err=%v", err)
			}
			state, stateErr := loadCleanupResource(t.Context(), f.store, f.state.Name)
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			if fault == "set-after-write" {
				target := secrets.DescribeTarget(provider)
				want := interfaces.RoutedSecretReference{Key: "DATABASE_URL", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
				if !containsAliasReference(state.Lifecycle, want) {
					t.Fatal("post-write failure lost durable alias debt")
				}
				if value, err := f.provider.Get(t.Context(), want.Key); err != nil || value != aliasJournalMarker {
					t.Fatal("provider payload bytes were changed")
				}
			} else if !reflect.DeepEqual(state.Lifecycle, f.state.Lifecycle) {
				t.Fatal("pre-write read failure claimed an alias")
			}
			assertAliasStateSafe(t, f.store)
		})
	}
}

func TestInfraOutputAliasJournal_LegacyEnvSourceRetainsDerivedKeys(t *testing.T) {
	f := newAliasJournalFixture(t)
	if err := f.store.DeleteResource(t.Context(), f.state.Name); err != nil {
		t.Fatal(err)
	}
	f.state.ID, f.state.Name = "database-staging", "database-staging"
	f.state.Lifecycle = nil
	f.state.Outputs["uri"] = sensitive.Placeholder(f.state.Name, "uri")
	if err := f.store.SaveResource(t.Context(), f.state); err != nil {
		t.Fatal(err)
	}
	f.workflow.Modules = []config.ModuleConfig{{Name: "database", Type: f.state.Type,
		Environments: map[string]*config.InfraEnvironmentResolution{"staging": {Config: map[string]any{"name": f.state.Name}}}}}
	f.secrets.Generate = append(f.secrets.Generate,
		SecretGen{Key: "SECOND_ALIAS", Type: "infra_output", Source: "database.uri", Store: "named"},
		SecretGen{Key: "OUT_OF_SCOPE", Type: "infra_output", Source: "other.uri", Store: "named"})
	defaultProvider := secrets.NewFileProvider(t.TempDir())
	store := &aliasAcknowledgmentStore{infraStateStore: f.store}
	var generation string
	store.beforeSave = func(state interfaces.ResourceState) {
		if generation == "" {
			generation = state.Lifecycle.Generation
		}
		if generation == "" || state.Lifecycle.Generation != generation {
			t.Fatal("aliases for one source changed generation")
		}
	}
	err := syncInfraOutputSecretsScoped(t.Context(), f.secrets, defaultProvider, []interfaces.ResourceState{f.state}, f.workflow, "staging",
		map[string]string{sensitive.SecretKey(f.state.Name, "uri"): aliasJournalMarker}, false, map[string]struct{}{f.state.Name: {}}, store)
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupResource(t.Context(), f.store, f.state.Name)
	if err != nil || state == nil || state.Lifecycle == nil {
		t.Fatalf("env-resolved source journal missing: err=%v", err)
	}
	target := secrets.DescribeTarget(defaultProvider)
	derived := interfaces.RoutedSecretReference{Key: sensitive.SecretKey(f.state.Name, "uri"), Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
	if store.saves != 2 || len(state.Lifecycle.Secrets) != 3 || !containsAliasReference(state.Lifecycle, derived) {
		t.Fatalf("legacy source lost derived cleanup refs: saves=%d lifecycle=%+v", store.saves, state.Lifecycle)
	}
	for _, key := range []string{"DATABASE_URL", "SECOND_ALIAS"} {
		target := secrets.DescribeTarget(f.provider)
		if !containsAliasReference(state.Lifecycle, interfaces.RoutedSecretReference{Key: key, Store: "named", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}) {
			t.Fatal("named alias absent from env-resolved journal")
		}
		if value, err := f.provider.Get(t.Context(), key); err != nil || value != aliasJournalMarker {
			t.Fatal("alias payload changed")
		}
	}
	if _, err := f.provider.Get(t.Context(), "OUT_OF_SCOPE"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("out-of-scope alias was claimed/written")
	}
	assertAliasStateSafe(t, f.store)
}

func TestInfraOutputAliasJournal_ConflictingOwnerRejectsRefresh(t *testing.T) {
	for _, ownerStore := range []string{"named", "renamed-store", ""} {
		for _, phase := range []interfaces.ResourcePhase{interfaces.ResourcePhaseActive, interfaces.ResourcePhaseCloudDeletedSecretCleanupPending} {
			t.Run(fmt.Sprintf("store=%q/phase=%s", ownerStore, phase), func(t *testing.T) {
				f := newAliasJournalFixture(t)
				target := secrets.DescribeTarget(f.provider)
				owner := interfaces.ResourceState{ID: "sourceA", Name: "sourceA", Type: f.state.Type, ProviderID: "owner-cloud-id",
					Lifecycle: &interfaces.ResourceLifecycle{Generation: "owner-generation", Phase: phase,
						Secrets: []interfaces.RoutedSecretReference{{Key: "DATABASE_URL", Store: ownerStore, Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}}}}
				if err := f.store.SaveResource(t.Context(), owner); err != nil {
					t.Fatal(err)
				}
				if err := f.provider.Set(t.Context(), "DATABASE_URL", "owner-A-value"); err != nil {
					t.Fatal(err)
				}
				before, err := f.store.ListResources(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				store := &aliasAcknowledgmentStore{infraStateStore: f.store}
				// The observed slice contains only sourceB; ownership must come
				// from the authoritative store, not the scoped apply snapshot.
				err = f.sync(t.Context(), store, true)
				value, getErr := f.provider.Get(t.Context(), "DATABASE_URL")
				if !errors.Is(err, interfaces.ErrValidation) || store.saves != 0 || getErr != nil || value != "owner-A-value" {
					t.Fatalf("conflicting owner allowed alias refresh: err=%v saves=%d overwritten=%t readErr=%v", err, store.saves, value != "owner-A-value", getErr)
				}
				if strings.Contains(err.Error(), aliasJournalMarker) || strings.Contains(err.Error(), "owner-A-value") {
					t.Fatal("conflict diagnostic disclosed alias bytes")
				}
				after, err := f.store.ListResources(t.Context())
				if err != nil || !reflect.DeepEqual(after, before) {
					t.Fatalf("conflict changed source journals: err=%v", err)
				}
				assertAliasStateSafe(t, f.store)
			})
		}
	}
}

func TestInfraOutputAliasJournal_NonconflictingOwnerAllowed(t *testing.T) {
	for _, difference := range []string{"provider", "scope", "subject", "key"} {
		t.Run(difference, func(t *testing.T) {
			f := newAliasJournalFixture(t)
			target := secrets.DescribeTarget(f.provider)
			ref := interfaces.RoutedSecretReference{Key: "DATABASE_URL", Store: "named", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
			switch difference {
			case "provider":
				ref.Provider = "other-provider"
			case "scope":
				ref.Scope = "other-scope"
			case "subject":
				ref.Subject = secrets.DescribeTarget(secrets.NewFileProvider(t.TempDir())).Subject
			case "key":
				ref.Key = "OTHER_KEY"
			}
			owner := interfaces.ResourceState{ID: "sourceA", Name: "sourceA", Type: f.state.Type, ProviderID: "owner-cloud-id",
				Lifecycle: &interfaces.ResourceLifecycle{Generation: "owner-generation", Phase: interfaces.ResourcePhaseActive,
					Secrets: []interfaces.RoutedSecretReference{ref}}}
			if err := f.store.SaveResource(t.Context(), owner); err != nil {
				t.Fatal(err)
			}
			before, err := loadCleanupResource(t.Context(), f.store, owner.Name)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.provider.Set(t.Context(), "DATABASE_URL", "old-value"); err != nil {
				t.Fatal(err)
			}
			store := &aliasAcknowledgmentStore{infraStateStore: f.store}
			if err := f.sync(t.Context(), store, true); err != nil {
				t.Fatal(err)
			}
			if value, err := f.provider.Get(t.Context(), "DATABASE_URL"); err != nil || value != aliasJournalMarker || store.saves != 1 {
				t.Fatalf("nonconflicting alias write rejected or altered: err=%v saves=%d", err, store.saves)
			}
			after, err := loadCleanupResource(t.Context(), f.store, owner.Name)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("alias write changed another owner's journal")
			}
			assertAliasStateSafe(t, f.store)
		})
	}
}

func TestInfraOutputAliasJournal_OwnershipReadFailsBeforeMutation(t *testing.T) {
	f := newAliasJournalFixture(t)
	store := &aliasAcknowledgmentStore{infraStateStore: f.store, fault: "ownership-read"}
	err := f.sync(t.Context(), store, false)
	if !errors.Is(err, interfaces.ErrTransient) || strings.Contains(err.Error(), aliasJournalMarker) {
		t.Fatalf("ownership read must fail closed with a safe cause: err=%v", err)
	}
	if store.saves != 0 {
		t.Fatalf("ownership read failed after journal mutation: saves=%d", store.saves)
	}
	if _, err := f.provider.Get(t.Context(), "DATABASE_URL"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("alias written despite unreadable ownership")
	}
	assertAliasStateSafe(t, f.store)
}
