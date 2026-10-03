package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/secrets"
)

type cleanupApplyDriver struct {
	stubSensitiveDriver
	store   infraStateStore
	secrets *envTestProvider
	creates int
}

type cleanupRestartDriver struct {
	stubSensitiveDriver
	provider secrets.Provider
	creates  int
}

func (d *cleanupRestartDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if err := sensitive.VerifyAbsent(ctx, d.provider, "WF_CLEANUP_RESTART_KEY"); err != nil {
		return nil, err
	}
	d.creates++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "new-cloud-id", Outputs: map[string]any{"public": "consumer-payload"}}, nil
}

func TestCleanupPending_DirectAndSavedApplyResumeDurableState(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "saved"}[saved], func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "infra.yaml")
			if err := os.WriteFile(cfgFile, []byte("modules:\n  - name: state\n    type: iac.state\n    config: {backend: filesystem, directory: "+filepath.Join(dir, "state")+"}\nsecrets: {provider: env}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WF_CLEANUP_RESTART_KEY", "known-restart-credential")
			provider := secrets.NewEnvProvider("")
			target := secrets.DescribeTarget(provider)
			store, err := resolveStateStore(cfgFile, "")
			if err != nil {
				t.Fatal(err)
			}
			state := interfaces.ResourceState{ID: "database", Name: "database", Type: "infra.database", Provider: "test-cloud", ProviderID: "old-cloud-id", Lifecycle: &interfaces.ResourceLifecycle{Generation: "restart-generation", Phase: interfaces.ResourcePhaseCloudDeletedSecretCleanupPending, Secrets: []interfaces.RoutedSecretReference{{Key: "WF_CLEANUP_RESTART_KEY", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}}}}
			if err := store.SaveResource(t.Context(), state); err != nil {
				t.Fatal(err)
			}
			driver := &cleanupRestartDriver{provider: provider}
			cloud := &declarativeCLIProvider{driver: driver}
			specs := []interfaces.ResourceSpec{{Name: state.Name, Type: state.Type}}
			if saved {
				plan, planErr := platform.ComputePlan(t.Context(), cloud, specs, []interfaces.ResourceState{state})
				if planErr != nil {
					t.Fatal(planErr)
				}
				data, marshalErr := json.Marshal(plan)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				var reloaded interfaces.IaCPlan
				if err := json.Unmarshal(data, &reloaded); err != nil {
					t.Fatal(err)
				}
				err = applyPrecomputedPlanWithStore(t.Context(), reloaded, cloud, "test-cloud", store, io.Discard, "", cfgFile, nil)
			} else {
				err = applyWithProviderAndStore(t.Context(), cloud, "test-cloud", specs, []interfaces.ResourceState{state}, store, io.Discard, "", cfgFile, nil)
			}
			states, loadErr := store.ListResources(t.Context())
			if err != nil || loadErr != nil || driver.creates != 1 || len(driver.deleteCalls) != 0 || len(states) != 1 || states[0].ProviderID != "new-cloud-id" {
				t.Fatalf("durable restart was not applied: err=%v load=%v creates=%d deletes=%v states=%v", err, loadErr, driver.creates, driver.deleteCalls, states)
			}
			if err := sensitive.VerifyAbsent(t.Context(), provider, "WF_CLEANUP_RESTART_KEY"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCleanupPending_UpdateImportsAllLegacyKeys(t *testing.T) {
	for _, routes := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-new-sensitive-output", true: "partial-sensitive-output"}[routes], func(t *testing.T) {
			store := cleanupStateStore(t)
			provider := newEnvTestProvider()
			state := interfaces.ResourceState{ID: "legacy", Name: "legacy", Type: "infra.key", ProviderID: "exact-id", Outputs: map[string]any{}}
			for _, key := range []string{"access_key", "secret_key"} {
				state.Outputs[key] = sensitive.Placeholder(state.Name, key)
				provider.values[sensitive.SecretKey(state.Name, key)] = "known-legacy-credential"
			}
			if err := store.SaveResource(t.Context(), state); err != nil {
				t.Fatal(err)
			}
			out := interfaces.ResourceOutput{ProviderID: state.ProviderID, Outputs: map[string]any{"public": "consumer-payload"}}
			if routes {
				out.Outputs["access_key"] = "known-updated-credential"
				out.Sensitive = map[string]bool{"access_key": true}
			}
			if _, err := persistApplyMode(t.Context(), store, provider, nil, state, out, false); err != nil {
				t.Fatal(err)
			}
			updated, err := loadCleanupResource(t.Context(), store, state.Name)
			if err != nil || updated == nil || updated.Lifecycle == nil || len(updated.Lifecycle.Secrets) != 2 {
				t.Fatalf("update forgot previously routed keys: state=%+v err=%v", updated, err)
			}
			prepared, err := prepareResourceSecretDeletion(t.Context(), store, provider, interfaces.PlanAction{Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: updated})
			if err != nil {
				t.Fatal(err)
			}
			if err := reconcileResourceSecretDeletion(t.Context(), store, func(interfaces.RoutedSecretReference) (secrets.Provider, error) { return provider, nil }, *prepared); err != nil {
				t.Fatal(err)
			}
			if len(provider.values) != 0 {
				t.Fatal("cleanup forgot a legacy credential")
			}
		})
	}
}

type interruptedRoutingProvider struct{ *envTestProvider }

func (*interruptedRoutingProvider) Set(context.Context, string, string) error {
	panic("simulated controller interruption before secret write")
}

type interruptedRoutingDriver struct {
	stubSensitiveDriver
	provider secrets.Provider
	creates  int
}

type upsertRoutingDriver struct {
	stubSensitiveDriver
	creates int
	updates int
}

func (*upsertRoutingDriver) SupportsUpsert() bool { return true }

func (d *upsertRoutingDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.creates++
	return nil, interfaces.ErrResourceAlreadyExists
}

func (*upsertRoutingDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: "existing-upsert-id"}, nil
}

func (d *upsertRoutingDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updates++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID, Outputs: map[string]any{"password": "known-upsert-credential"}, Sensitive: map[string]bool{"password": true}}, nil
}

func TestCleanupPending_UpsertRoutingCannotAuthorizeExistingDelete(t *testing.T) {
	store := cleanupStateStore(t)
	driver := &upsertRoutingDriver{}
	cloud := &declarativeCLIProvider{driver: driver}
	spec := interfaces.ResourceSpec{Name: "upsert-existing", Type: "infra.database"}
	plan, err := platform.ComputePlan(t.Context(), cloud, []interfaces.ResourceSpec{spec}, nil)
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Action != "create" {
		t.Fatalf("initial plan: %+v err=%v", plan, err)
	}
	var interruption any
	func() {
		defer func() { interruption = recover() }()
		_, err = wfctlhelpers.ApplyPlanWithHooks(t.Context(), cloud, &plan, statePersistenceHooks(store, &interruptedRoutingProvider{newEnvTestProvider()}, cloud, "test-cloud", "initial", nil))
	}()
	if interruption != "simulated controller interruption before secret write" || driver.updates != 1 {
		t.Fatalf("real upsert routing was not interrupted: interruption=%v updates=%d err=%v", interruption, driver.updates, err)
	}
	states, err := store.ListResources(t.Context())
	if err != nil || len(states) != 1 || states[0].ProviderID != "existing-upsert-id" || states[0].Lifecycle == nil || states[0].Lifecycle.RoutingCreated {
		t.Fatalf("upsert falsely authorized creation rollback: states=%+v err=%v", states, err)
	}
	_, err = platform.ComputePlan(t.Context(), cloud, []interfaces.ResourceSpec{spec}, states)
	if !errors.Is(err, interfaces.ErrValidation) || len(driver.deleteCalls) != 0 {
		t.Fatalf("upsert routing did not block destructive recovery: err=%v deletes=%v", err, driver.deleteCalls)
	}
}

func TestCleanupPending_UpsertWriteFailureMustRetainExistingResource(t *testing.T) {
	store := cleanupStateStore(t)
	driver := &upsertRoutingDriver{}
	cloud := &declarativeCLIProvider{driver: driver}
	provider := &journalCheckedRoutingProvider{cleanupBoundaryProvider: &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider()}, store: store, failAfterWrite: true}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "create", Resource: interfaces.ResourceSpec{Name: "upsert-existing", Type: "infra.database"}}}}
	_, err := wfctlhelpers.ApplyPlanWithHooks(t.Context(), cloud, plan, statePersistenceHooks(store, provider, cloud, "test-cloud", "initial", nil))
	if err == nil || !provider.checked || driver.updates != 1 || len(driver.deleteCalls) != 0 {
		t.Fatalf("upsert routing error compensated an existing resource: err=%v checked=%v updates=%d deletes=%v", err, provider.checked, driver.updates, driver.deleteCalls)
	}
	state, err := loadCleanupResource(t.Context(), store, "upsert-existing")
	if err != nil || state == nil || state.Lifecycle == nil || state.Lifecycle.RoutingCreated || state.Lifecycle.Phase != interfaces.ResourcePhaseSecretRoutingPending {
		t.Fatalf("upsert error lost non-destructive routing debt: state=%+v err=%v", state, err)
	}
}

func TestCleanupPending_SavedPlanCannotAssertCreationOwnership(t *testing.T) {
	owned := true
	action := interfaces.PlanAction{Action: "create", CreationOwned: &owned, Resource: interfaces.ResourceSpec{Name: "database", Type: "infra.database"}}
	data, err := json.Marshal(action)
	if err != nil || strings.Contains(string(data), "CreationOwned") || strings.Contains(string(data), "creation_owned") {
		t.Fatalf("runtime creation ownership entered a saved plan: %s err=%v", data, err)
	}
	var forged interfaces.PlanAction
	if err := json.Unmarshal([]byte(`{"action":"create","creation_owned":true,"CreationOwned":true}`), &forged); err != nil || forged.CreationOwned != nil {
		t.Fatalf("saved plan asserted runtime ownership: action=%+v err=%v", forged, err)
	}
}

func (d *interruptedRoutingDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if err := sensitive.VerifyAbsent(ctx, d.provider, sensitive.SecretKey(spec.Name, "password")); err != nil {
		return nil, err
	}
	d.creates++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "replacement-id", Outputs: map[string]any{"public": "consumer-payload"}}, nil
}

func TestCleanupPending_InterruptedRoutingCannotConverge(t *testing.T) {
	for _, created := range []bool{false, true} {
		for _, saved := range []bool{false, true} {
			t.Run(map[bool]string{false: "pre-existing", true: "created"}[created]+"/"+map[bool]string{false: "direct", true: "saved"}[saved], func(t *testing.T) {
				store := cleanupStateStore(t)
				provider := &interruptedRoutingProvider{newEnvTestProvider()}
				state := interfaces.ResourceState{ID: "interrupted", Name: "interrupted", Type: "infra.key", ProviderID: "old-id"}
				out := interfaces.ResourceOutput{ProviderID: state.ProviderID, Outputs: map[string]any{"password": "known-interrupted-credential"}, Sensitive: map[string]bool{"password": true}}
				func() {
					defer func() {
						if recover() == nil {
							t.Fatal("fixture did not interrupt after write-ahead persistence")
						}
					}()
					_, _ = persistApplyMode(t.Context(), store, provider, &stubSensitiveDriver{}, state, out, created)
				}()
				states, err := store.ListResources(t.Context())
				if err != nil || len(states) != 1 || states[0].Lifecycle == nil || states[0].Lifecycle.Phase != interfaces.ResourcePhaseSecretRoutingPending {
					t.Fatalf("interrupted intent was not durable: states=%v err=%v", states, err)
				}
				driver := &interruptedRoutingDriver{provider: provider}
				cloud := &declarativeCLIProvider{driver: driver}
				plan, err := platform.ComputePlan(t.Context(), cloud, []interfaces.ResourceSpec{{Name: state.Name, Type: state.Type}}, states)
				if !created {
					if !errors.Is(err, interfaces.ErrValidation) || len(driver.deleteCalls) != 0 {
						t.Fatalf("pre-existing routing debt was declared converged: plan=%+v err=%v", plan, err)
					}
					return
				}
				if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Action != "replace" {
					t.Fatalf("owned interrupted creation was declared converged: plan=%+v err=%v", plan, err)
				}
				if saved {
					data, err := json.Marshal(plan)
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(data, &plan); err != nil {
						t.Fatal(err)
					}
				}
				hooks := statePersistenceHooks(store, provider, cloud, "test-cloud", "restart", nil)
				result, err := wfctlhelpers.ApplyPlanWithHooks(t.Context(), cloud, &plan, hooks)
				if err != nil || len(result.Errors) != 0 || driver.creates != 1 || len(driver.deleteCalls) != 1 || driver.deleteCalls[0].ProviderID != state.ProviderID {
					t.Fatalf("owned routing debt was not recovered: result=%+v err=%v creates=%d deletes=%v", result, err, driver.creates, driver.deleteCalls)
				}
			})
		}
	}
}

func (d *cleanupApplyDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	states, err := d.store.ListResources(ctx)
	if err != nil {
		return err
	}
	if len(states) != 1 || states[0].Lifecycle == nil || states[0].Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletePending {
		return interfaces.ErrValidation
	}
	return d.stubSensitiveDriver.Delete(ctx, ref)
}

func (d *cleanupApplyDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if _, ok := d.secrets.values["exact-derived-key"]; ok {
		return nil, interfaces.ErrValidation
	}
	if _, ok := d.secrets.values["NAMED_ALIAS"]; ok {
		return nil, interfaces.ErrValidation
	}
	d.creates++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "replacement-id"}, nil
}

func TestReplaceCleanup_RealApplyHooksRetainAndFinalizeDebt(t *testing.T) {
	for _, kind := range []string{"delete", "replace"} {
		for _, phase := range []interfaces.ResourcePhase{interfaces.ResourcePhaseActive, interfaces.ResourcePhaseCloudDeletedSecretCleanupPending} {
			t.Run(kind+"/"+string(phase), func(t *testing.T) {
				store := cleanupStateStore(t)
				secretProvider := newEnvTestProvider()
				secretProvider.values["exact-derived-key"], secretProvider.values["NAMED_ALIAS"] = "known-live-hook-marker", "known-live-hook-marker"
				state := cleanupResourceFixture(t, store, secretProvider)
				state.Lifecycle.Phase = phase
				state.Lifecycle.Secrets[1].Store = ""
				if err := store.SaveResource(t.Context(), state); err != nil {
					t.Fatal(err)
				}
				driver := &cleanupApplyDriver{store: store, secrets: secretProvider}
				provider := &declarativeCLIProvider{driver: driver}
				plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: kind, Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: &state}}}
				hooks := statePersistenceHooks(store, secretProvider, provider, "test-cloud", "cleanup-plan", nil)
				result, err := wfctlhelpers.ApplyPlanWithHooks(t.Context(), provider, plan, hooks)
				wantDelete := 1
				if phase == interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
					wantDelete = 0
				}
				wantCreate := 0
				if kind == "replace" {
					wantCreate = 1
				}
				if err != nil || len(result.Errors) != 0 || len(driver.deleteCalls) != wantDelete || driver.creates != wantCreate {
					t.Fatalf("real apply cleanup contract failed: err=%v result=%+v deletes=%v creates=%d", err, result, driver.deleteCalls, driver.creates)
				}
				states, err := store.ListResources(t.Context())
				if err != nil || len(states) != wantCreate {
					t.Fatalf("state not finalized: states=%v err=%v", states, err)
				}
				if len(secretProvider.values) != 0 {
					t.Fatal("apply forgot state while secrets survived")
				}
			})
		}
	}
}

func cleanupStateStore(t *testing.T) infraStateStore {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(cfg, []byte("modules:\n  - name: state\n    type: iac.state\n    config:\n      backend: filesystem\n      directory: "+t.TempDir()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := resolveStateStore(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type cleanupBoundaryProvider struct {
	*envTestProvider
	deleteCount int
	crashAt     int
	retain      bool
}

type journalCheckedRoutingProvider struct {
	*cleanupBoundaryProvider
	store          infraStateStore
	failAfterWrite bool
	checked        bool
}

func (p *journalCheckedRoutingProvider) Set(ctx context.Context, key, value string) error {
	states, err := p.store.ListResources(ctx)
	if err != nil {
		return err
	}
	if len(states) != 1 || states[0].Lifecycle == nil || states[0].Lifecycle.Phase != interfaces.ResourcePhaseSecretRoutingPending {
		return interfaces.ErrValidation
	}
	found := false
	for _, ref := range states[0].Lifecycle.Secrets {
		if ref.Key == key {
			found = true
		}
	}
	data, err := json.Marshal(states)
	if err != nil || !found || strings.Contains(string(data), value) {
		return interfaces.ErrValidation
	}
	p.checked = true
	if err := p.envTestProvider.Set(ctx, key, value); err != nil {
		return err
	}
	if p.failAfterWrite {
		return context.Canceled
	}
	return nil
}

func TestCleanupPending_RoutingIntentPrecedesWriteAndFailedCreate(t *testing.T) {
	for _, failedCreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed-create"}[failedCreate], func(t *testing.T) {
			store := &cleanupBoundaryStore{infraStateStore: cleanupStateStore(t)}
			provider := &journalCheckedRoutingProvider{cleanupBoundaryProvider: &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider(), retain: failedCreate}, store: store, failAfterWrite: failedCreate}
			driver := &stubSensitiveDriver{}
			state := interfaces.ResourceState{ID: "database", Name: "database", Type: "infra.database", ProviderID: "cloud-id"}
			out := interfaces.ResourceOutput{Name: state.Name, Type: state.Type, ProviderID: state.ProviderID, Outputs: map[string]any{"password": "known-writeahead-marker", "public": "consumer-payload"}, Sensitive: map[string]bool{"password": true}}
			_, err := persistApplyMode(t.Context(), store, provider, driver, state, out, true)
			if !provider.checked {
				t.Fatal("secret was written before durable intent/readback")
			}
			states, stateErr := store.ListResources(t.Context())
			if stateErr != nil || len(states) != 1 || states[0].Lifecycle == nil {
				t.Fatalf("routing or cleanup debt lost: states=%v err=%v", states, stateErr)
			}
			data, jsonErr := json.Marshal(states)
			if jsonErr != nil || strings.Contains(string(data), "known-writeahead-marker") || states[0].Outputs["public"] != "consumer-payload" {
				t.Fatalf("journal/state leaked or altered payload: %s err=%v", data, jsonErr)
			}
			if failedCreate {
				if err == nil || !errors.Is(err, context.Canceled) || len(driver.deleteCalls) != 1 || states[0].Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
					t.Fatalf("failed-create cleanup did not retain debt: err=%v deletes=%v state=%v", err, driver.deleteCalls, states)
				}
				provider.retain = false
				if err := reconcileResourceSecretDeletion(t.Context(), store, func(interfaces.RoutedSecretReference) (secrets.Provider, error) { return provider, nil }, states[0]); err != nil {
					t.Fatal(err)
				}
				states, err = store.ListResources(t.Context())
				if err != nil || len(states) != 0 || len(provider.values) != 0 {
					t.Fatalf("failed-create restart did not finish: states=%v values=%v err=%v", states, provider.values, err)
				}
			} else if err != nil || len(driver.deleteCalls) != 0 || states[0].Lifecycle.Phase != interfaces.ResourcePhaseActive {
				t.Fatalf("successful routing did not commit: err=%v deletes=%v states=%v", err, driver.deleteCalls, states)
			} else if store.saves != 2 {
				t.Fatalf("routing must save intent and active state once each, got %d saves", store.saves)
			}
		})
	}
}

func TestCleanupPending_AdoptedRoutingPersistsIntentWithoutDeletingCloud(t *testing.T) {
	for _, failAfterWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "interrupted"}[failAfterWrite], func(t *testing.T) {
			store := cleanupStateStore(t)
			provider := &journalCheckedRoutingProvider{cleanupBoundaryProvider: &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider()}, store: store, failAfterWrite: failAfterWrite}
			state := interfaces.ResourceState{ID: "adopted", Name: "adopted", Type: "infra.database", ProviderID: "existing-id"}
			out := interfaces.ResourceOutput{Outputs: map[string]any{"password": "known-adopted-marker", "public": "consumer-payload"}, Sensitive: map[string]bool{"password": true}}
			_, err := persistAdoptRouteMode(t.Context(), store, provider, state, out)
			if !provider.checked {
				t.Fatal("adoption wrote a credential without durable intent")
			}
			states, loadErr := store.ListResources(t.Context())
			if loadErr != nil || len(states) != 1 || states[0].Lifecycle == nil {
				t.Fatalf("adopted routing debt disappeared: states=%v err=%v", states, loadErr)
			}
			wantPhase := interfaces.ResourcePhaseActive
			if failAfterWrite {
				wantPhase = interfaces.ResourcePhaseSecretRoutingPending
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("interruption lost cause: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, jsonErr := json.Marshal(states)
			if jsonErr != nil || strings.Contains(string(data), "known-adopted-marker") || states[0].Lifecycle.Phase != wantPhase || provider.deleteCount != 0 {
				t.Fatalf("adoption altered or leaked retained debt: state=%s phase=%s revokes=%d err=%v", data, states[0].Lifecycle.Phase, provider.deleteCount, jsonErr)
			}
		})
	}
}

func TestCleanupPending_AdoptionConfigCannotDuplicateSensitiveOutput(t *testing.T) {
	for _, route := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "route"}[route], func(t *testing.T) {
			const marker = "known-adopt-config-credential"
			store := cleanupStateStore(t)
			provider := newEnvTestProvider()
			out := interfaces.ResourceOutput{ProviderID: "existing-id", Outputs: map[string]any{"password": marker, "public": "consumer-payload"}, Sensitive: map[string]bool{"password": true}}
			state, err := resourceStateFromLiveOutput(interfaces.ResourceSpec{Name: "adopted", Type: "infra.database"}, "test-cloud", &out)
			if err != nil {
				t.Fatal(err)
			}
			mode := persistModeRead
			if route {
				mode = persistModeAdoptRoute
			}
			if _, err := persistResourceWithSecretRouting(t.Context(), store, provider, nil, state, out, mode); err != nil {
				t.Fatal(err)
			}
			states, err := store.ListResources(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(states)
			if err != nil || strings.Contains(string(data), marker) || len(states) != 1 || states[0].AppliedConfig["public"] != "consumer-payload" || out.Outputs["password"] != marker {
				t.Fatalf("adoption duplicated credential in config or altered payload: state=%s err=%v", data, err)
			}
		})
	}
}

func TestCleanupPending_FailedIntentDoesNotDeleteByGuessedName(t *testing.T) {
	store := &stubInfraStore{saveErr: context.Canceled}
	provider := newEnvTestProvider()
	driver := &stubSensitiveDriver{deleteErr: interfaces.ErrForbidden}
	state := interfaces.ResourceState{Name: "database", Type: "infra.database", ProviderID: "exact-created-id"}
	out := interfaces.ResourceOutput{Outputs: map[string]any{"password": "known-created-password"}, Sensitive: map[string]bool{"password": true}}
	_, err := persistApplyMode(t.Context(), store, provider, driver, state, out, true)
	if err == nil || len(driver.deleteCalls) != 1 || driver.deleteCalls[0].ProviderID != "exact-created-id" || len(provider.values) != 0 {
		t.Fatalf("failed intent retried a guessed identity: err=%v deletes=%v secrets=%v", err, driver.deleteCalls, provider.values)
	}
}

func (p *cleanupBoundaryProvider) Delete(ctx context.Context, key string) error {
	p.deleteCount++
	if !p.retain {
		_ = p.envTestProvider.Delete(ctx, key)
	}
	if p.crashAt == p.deleteCount {
		return context.Canceled
	}
	return nil
}

type cleanupBoundaryStore struct {
	infraStateStore
	crashOnPhase  bool
	dropLifecycle bool
	saves         int
}

func (s *cleanupBoundaryStore) SaveResource(ctx context.Context, state interfaces.ResourceState) error {
	s.saves++
	if s.dropLifecycle {
		state.Lifecycle = nil
	}
	if err := s.infraStateStore.SaveResource(ctx, state); err != nil {
		return err
	}
	if s.crashOnPhase && state.Lifecycle != nil && state.Lifecycle.Phase == interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
		return context.Canceled
	}
	return nil
}

func cleanupResourceFixture(t *testing.T, store infraStateStore, provider secrets.Provider) interfaces.ResourceState {
	t.Helper()
	target := secrets.DescribeTarget(provider)
	state := interfaces.ResourceState{ID: "database", Name: "database", Type: "infra.database", ProviderID: "cloud-id",
		Outputs: map[string]any{"password": sensitive.PlaceholderPrefix + "exact-derived-key"},
		Lifecycle: &interfaces.ResourceLifecycle{Generation: "generation-1", Phase: interfaces.ResourcePhaseActive,
			Secrets: []interfaces.RoutedSecretReference{
				{Key: "exact-derived-key", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject},
				{Key: "NAMED_ALIAS", Store: "named", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject},
			}}}
	if err := store.SaveResource(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDeleteCrash_SecretCleanupResumesAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"cloud-delete", "tombstone-write", "first-revoke", "second-revoke"} {
		t.Run(boundary, func(t *testing.T) {
			store := cleanupStateStore(t)
			provider := &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider()}
			named := newEnvTestProvider()
			provider.values["exact-derived-key"], provider.values["untouched"] = "known-cleanup-marker", "consumer-payload"
			named.values["NAMED_ALIAS"] = "known-cleanup-marker"
			state := cleanupResourceFixture(t, store, provider)
			prepared, err := prepareResourceSecretDeletion(t.Context(), store, provider, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: &state})
			if err != nil {
				t.Fatal(err)
			}
			resolver := func(ref interfaces.RoutedSecretReference) (secrets.Provider, error) {
				if ref.Store == "named" {
					return named, nil
				}
				return provider, nil
			}
			crashStore := &cleanupBoundaryStore{infraStateStore: store, crashOnPhase: boundary == "tombstone-write"}
			if boundary == "first-revoke" {
				provider.crashAt = 1
			}
			if boundary == "second-revoke" {
				// Both references deliberately share a backend here to interrupt
				// after the second actual revoke rather than a synthetic timer.
				provider.values["NAMED_ALIAS"] = named.values["NAMED_ALIAS"]
				resolver = func(interfaces.RoutedSecretReference) (secrets.Provider, error) { return provider, nil }
				provider.crashAt = 2
			}
			if boundary != "cloud-delete" {
				if err := reconcileResourceSecretDeletion(t.Context(), crashStore, resolver, *prepared); !errors.Is(err, context.Canceled) {
					t.Fatalf("expected controller interruption: %v", err)
				}
			}
			states, err := store.ListResources(t.Context())
			if err != nil || len(states) != 1 || states[0].Lifecycle == nil {
				t.Fatalf("cleanup forgot the resource: states=%v err=%v", states, err)
			}
			data, err := json.Marshal(states)
			if err != nil || strings.Contains(string(data), "known-cleanup-marker") {
				t.Fatalf("journal stored credential bytes: %s err=%v", data, err)
			}
			provider.crashAt = 0
			if err := reconcileResourceSecretDeletion(t.Context(), store, resolver, states[0]); err != nil {
				t.Fatal(err)
			}
			states, err = store.ListResources(t.Context())
			if err != nil || len(states) != 0 {
				t.Fatalf("restart did not finalize cleanup: states=%v err=%v", states, err)
			}
			if _, ok := provider.values["exact-derived-key"]; ok {
				t.Fatal("derived key survived finalization")
			}
			if boundary == "second-revoke" {
				if _, ok := provider.values["NAMED_ALIAS"]; ok {
					t.Fatal("alias survived finalization")
				}
			} else if _, ok := named.values["NAMED_ALIAS"]; ok {
				t.Fatal("alias survived finalization")
			}
			if provider.values["untouched"] != "consumer-payload" {
				t.Fatal("cleanup modified unrelated payload")
			}
		})
	}
}

func TestCleanupPending_FencesGenerationAndUnsupportedStores(t *testing.T) {
	provider := &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider()}
	store := cleanupStateStore(t)
	state := cleanupResourceFixture(t, store, provider)
	prepared, err := prepareResourceSecretDeletion(t.Context(), store, provider, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: &state})
	if err != nil {
		t.Fatal(err)
	}
	replacement := state
	replacement.Lifecycle = &interfaces.ResourceLifecycle{Generation: "generation-2", Phase: interfaces.ResourcePhaseActive}
	if err := store.SaveResource(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	err = reconcileResourceSecretDeletion(t.Context(), store, func(interfaces.RoutedSecretReference) (secrets.Provider, error) { return provider, nil }, *prepared)
	if err == nil || provider.deleteCount != 0 {
		t.Fatalf("stale cleanup touched replacement: err=%v deletes=%d", err, provider.deleteCount)
	}
	state = cleanupResourceFixture(t, store, provider)
	unsupported := &cleanupBoundaryStore{infraStateStore: store, dropLifecycle: true}
	if _, err := prepareResourceSecretDeletion(t.Context(), unsupported, provider, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: &state}); err == nil {
		t.Fatal("backend discarded tombstone but deletion was accepted")
	}
}

func TestCleanupPending_RetainedSecretPreventsStateRemoval(t *testing.T) {
	provider := &cleanupBoundaryProvider{envTestProvider: newEnvTestProvider(), retain: true}
	provider.values["exact-derived-key"] = "known-cleanup-marker"
	store := cleanupStateStore(t)
	state := cleanupResourceFixture(t, store, provider)
	prepared, err := prepareResourceSecretDeletion(t.Context(), store, provider, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: &state})
	if err != nil {
		t.Fatal(err)
	}
	err = reconcileResourceSecretDeletion(t.Context(), store, func(interfaces.RoutedSecretReference) (secrets.Provider, error) { return provider, nil }, *prepared)
	if err == nil || strings.Contains(err.Error(), "known-cleanup-marker") {
		t.Fatalf("absence must fail closed with safe diagnostics: %v", err)
	}
	states, err := store.ListResources(t.Context())
	if err != nil || len(states) != 1 || states[0].Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
		t.Fatalf("absence failure forgot cleanup debt: states=%v err=%v", states, err)
	}
}
