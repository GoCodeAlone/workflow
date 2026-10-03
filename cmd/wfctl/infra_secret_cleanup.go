package main

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/google/uuid"
)

type cleanupSecretResolver func(interfaces.RoutedSecretReference) (secrets.Provider, error)

type cleanupProviderOptions struct {
	ConfigFile  string
	Environment string
}

func wireResourceSecretCleanup(hooks *wfctlhelpers.ApplyPlanHooks, store infraStateStore, provider secrets.Provider, options cleanupProviderOptions) {
	prepared := make(map[string]*interfaces.ResourceState)
	priorBefore := hooks.OnBeforeAction
	hooks.OnBeforeAction = func(ctx context.Context, action interfaces.PlanAction) error {
		if priorBefore != nil {
			if err := priorBefore(ctx, action); err != nil {
				return err
			}
		}
		if action.Action != "delete" && action.Action != "replace" {
			return nil
		}
		state, err := prepareResourceSecretDeletion(ctx, store, provider, action)
		if err != nil {
			return err
		}
		prepared[action.Resource.Name] = state
		return nil
	}
	hooks.ResourceDeletionComplete = func(ctx context.Context, action interfaces.PlanAction) (bool, error) {
		state := prepared[action.Resource.Name]
		if state == nil {
			return false, nil
		}
		current, err := requireCleanupGeneration(ctx, store, *state)
		if err != nil {
			return false, err
		}
		return current.Lifecycle.Phase == interfaces.ResourcePhaseCloudDeletedSecretCleanupPending, nil
	}
	resolver := func(ref interfaces.RoutedSecretReference) (secrets.Provider, error) {
		if ref.Store == "" {
			return provider, nil
		}
		cfg, err := config.LoadFromFile(options.ConfigFile)
		if err != nil {
			return nil, err
		}
		return providerForSecretGen(cfg, provider, SecretGen{Key: ref.Key, Store: ref.Store}, options.Environment)
	}
	priorDeleted := hooks.OnResourceDeleted
	hooks.OnResourceDeleted = func(ctx context.Context, action interfaces.PlanAction) error {
		state := prepared[action.Resource.Name]
		if state == nil {
			if priorDeleted != nil {
				return priorDeleted(ctx, action)
			}
			return nil
		}
		// Cloud deletion has completed even if the caller was interrupted.
		// Persist that fact with a bounded fresh context before any revoke.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return reconcileResourceSecretDeletion(cleanupCtx, store, resolver, *state)
	}
}

func cloneResourceLifecycle(lifecycle *interfaces.ResourceLifecycle) *interfaces.ResourceLifecycle {
	if lifecycle == nil {
		return nil
	}
	copy := *lifecycle
	copy.Secrets = slices.Clone(lifecycle.Secrets)
	return &copy
}

func loadCleanupResource(ctx context.Context, store infraStateStore, name string) (*interfaces.ResourceState, error) {
	states, err := store.ListResources(ctx)
	if err != nil {
		return nil, cleanupStateError{operation: "read", name: name, cause: err}
	}
	var found *interfaces.ResourceState
	for i := range states {
		if states[i].Name != name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: duplicate cleanup resource %q", interfaces.ErrValidation, name)
		}
		copy := states[i]
		copy.Lifecycle = cloneResourceLifecycle(copy.Lifecycle)
		found = &copy
	}
	return found, nil
}

// A save is not accepted until the backend reads the same journal back. Older
// plugin backends must fail closed instead of silently discarding new metadata.
func saveCleanupResource(ctx context.Context, store infraStateStore, state interfaces.ResourceState) error {
	if state.ID == "" {
		state.ID = state.Name
	}
	if err := store.SaveResource(ctx, state); err != nil {
		return cleanupStateError{operation: "save", name: state.Name, cause: err}
	}
	stored, err := loadCleanupResource(ctx, store, state.Name)
	if err != nil {
		return err
	}
	if stored == nil || stored.ProviderID != state.ProviderID || !reflect.DeepEqual(stored.Lifecycle, state.Lifecycle) {
		return fmt.Errorf("%w: state backend did not preserve cleanup journal for %q", interfaces.ErrValidation, state.Name)
	}
	return nil
}

func prepareResourceSecretDeletion(ctx context.Context, store infraStateStore, provider secrets.Provider, action interfaces.PlanAction) (*interfaces.ResourceState, error) {
	state, err := loadCleanupResource(ctx, store, action.Resource.Name)
	if err != nil {
		return nil, err
	}
	if state == nil {
		if action.Current != nil && (action.Current.Lifecycle != nil || hasRoutedPlaceholders(action.Current.Outputs)) {
			return nil, fmt.Errorf("%w: missing durable cleanup state for %q", interfaces.ErrValidation, action.Resource.Name)
		}
		return nil, nil
	}
	if action.Current != nil {
		if state.ProviderID != action.Current.ProviderID || state.Type != action.Current.Type || resourceStateProviderRef(*state) != resourceStateProviderRef(*action.Current) {
			return nil, fmt.Errorf("%w: resource identity changed before cleanup for %q", interfaces.ErrValidation, state.Name)
		}
		if action.Current.Lifecycle != nil && (state.Lifecycle == nil || state.Lifecycle.Generation != action.Current.Lifecycle.Generation) {
			return nil, fmt.Errorf("%w: resource generation changed before cleanup for %q", interfaces.ErrValidation, state.Name)
		}
	}
	if state.Lifecycle == nil {
		refs, err := routedReferencesFromState(*state, provider)
		if err != nil {
			return nil, err
		}
		if len(refs) == 0 {
			return nil, nil
		}
		state.Lifecycle = &interfaces.ResourceLifecycle{Generation: uuid.NewString(), Phase: interfaces.ResourcePhaseActive, Secrets: refs}
	}
	if state.Lifecycle.Generation == "" {
		return nil, fmt.Errorf("%w: missing cleanup generation for %q", interfaces.ErrValidation, state.Name)
	}
	if state.Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
		state.Lifecycle.Phase = interfaces.ResourcePhaseCloudDeletePending
	}
	if err := saveCleanupResource(ctx, store, *state); err != nil {
		return nil, err
	}
	return state, nil
}

func routedReferencesFromState(state interfaces.ResourceState, provider secrets.Provider) ([]interfaces.RoutedSecretReference, error) {
	var refs []interfaces.RoutedSecretReference
	target := secrets.DescribeTarget(provider)
	seen := make(map[string]struct{})
	for _, value := range state.Outputs {
		if !sensitive.IsPlaceholder(value) {
			continue
		}
		key := strings.TrimPrefix(value.(string), sensitive.PlaceholderPrefix)
		if key == "" || provider == nil {
			return nil, fmt.Errorf("%w: routed cleanup identifier/provider missing for %q", interfaces.ErrValidation, state.Name)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, interfaces.RoutedSecretReference{Key: key, Provider: target.Provider, Scope: target.Scope, Subject: target.Subject})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key < refs[j].Key })
	return refs, nil
}

func hasRoutedPlaceholders(outputs map[string]any) bool {
	for _, value := range outputs {
		if sensitive.IsPlaceholder(value) {
			return true
		}
	}
	return false
}

func prepareResourceRoutingIntent(ctx context.Context, store infraStateStore, provider secrets.Provider, state interfaces.ResourceState, out interfaces.ResourceOutput, created bool) (interfaces.ResourceState, bool, error) {
	prior, err := loadCleanupResource(ctx, store, state.Name)
	if err != nil {
		return state, false, err
	}
	if prior != nil && prior.Lifecycle != nil {
		if prior.ProviderID != state.ProviderID || (prior.Lifecycle.Phase != interfaces.ResourcePhaseActive && prior.Lifecycle.Phase != interfaces.ResourcePhaseSecretRoutingPending) {
			return state, false, fmt.Errorf("%w: unresolved lifecycle debt/identity for %q", interfaces.ErrValidation, state.Name)
		}
		state.Lifecycle = cloneResourceLifecycle(prior.Lifecycle)
	} else if prior != nil && hasRoutedPlaceholders(prior.Outputs) {
		if prior.ProviderID != state.ProviderID || prior.Type != state.Type || resourceStateProviderRef(*prior) != resourceStateProviderRef(state) {
			return state, false, fmt.Errorf("%w: legacy routed resource identity changed for %q", interfaces.ErrValidation, state.Name)
		}
		refs, err := routedReferencesFromState(*prior, provider)
		if err != nil {
			return state, false, err
		}
		state.Lifecycle = &interfaces.ResourceLifecycle{Generation: uuid.NewString(), Phase: interfaces.ResourcePhaseActive, Secrets: refs}
	}
	sanitized := make(map[string]any, len(out.Outputs))
	for key, value := range out.Outputs {
		sanitized[key] = value
	}
	var references []interfaces.RoutedSecretReference
	target := secrets.DescribeTarget(provider)
	for key, sensitiveFlag := range out.Sensitive {
		if !sensitiveFlag {
			continue
		}
		if _, present := out.Outputs[key]; !present {
			continue
		}
		if provider == nil {
			return state, false, fmt.Errorf("%w: no secrets provider for sensitive output of %q", interfaces.ErrValidation, state.Name)
		}
		identifier := sensitive.SecretKey(state.Name, key)
		sanitized[key] = sensitive.PlaceholderPrefix + identifier
		references = append(references, interfaces.RoutedSecretReference{Key: identifier, Provider: target.Provider, Scope: target.Scope, Subject: target.Subject})
	}
	if len(references) == 0 {
		return state, false, nil
	}
	sort.Slice(references, func(i, j int) bool { return references[i].Key < references[j].Key })
	if state.Lifecycle == nil {
		state.Lifecycle = &interfaces.ResourceLifecycle{}
	}
	state.Lifecycle.Generation = uuid.NewString()
	state.Lifecycle.Phase = interfaces.ResourcePhaseSecretRoutingPending
	state.Lifecycle.RoutingCreated = created
	for _, ref := range references {
		found := false
		for i, old := range state.Lifecycle.Secrets {
			if old.Key == ref.Key && old.Store == ref.Store && old.Provider == ref.Provider && old.Scope == ref.Scope && old.Subject == ref.Subject {
				state.Lifecycle.Secrets[i] = ref
				found = true
				break
			}
		}
		if !found {
			state.Lifecycle.Secrets = append(state.Lifecycle.Secrets, ref)
		}
	}
	state.Outputs = sanitized
	if err := saveCleanupResource(ctx, store, state); err != nil {
		return state, true, err
	}
	return state, true, nil
}

func compensateCreatedResourceWithJournal(store infraStateStore, provider secrets.Provider, driver interfaces.ResourceDriver, state interfaces.ResourceState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	current, err := loadCleanupResource(ctx, store, state.Name)
	if err != nil {
		return err
	}
	if current == nil || current.Lifecycle == nil {
		// A journal write failed before any secret write. Legacy rollback is
		// safe here only without routed values; it never revokes guessed keys.
		return compensateUnjournaledCreation(driver, state)
	}
	if state.ProviderID != current.ProviderID || state.Lifecycle == nil || state.Lifecycle.Generation != current.Lifecycle.Generation {
		return fmt.Errorf("%w: failed-create cleanup identity changed for %q", interfaces.ErrValidation, state.Name)
	}
	prepared, err := prepareResourceSecretDeletion(ctx, store, provider, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type}, Current: current})
	if err != nil {
		return err
	}
	if driver == nil {
		return fmt.Errorf("%w: failed-create cleanup driver missing", interfaces.ErrValidation)
	}
	if err := driver.Delete(ctx, interfaces.ResourceRef{Name: state.Name, Type: state.Type, ProviderID: state.ProviderID}); err != nil && !interfaces.IsErrResourceNotFound(err) {
		return cleanupStateError{operation: "delete failed creation", name: state.Name, cause: err}
	}
	return reconcileResourceSecretDeletion(ctx, store, func(ref interfaces.RoutedSecretReference) (secrets.Provider, error) {
		if ref.Store != "" {
			return nil, fmt.Errorf("%w: failed-create cleanup cannot resolve named store", interfaces.ErrValidation)
		}
		return provider, nil
	}, *prepared)
}

func requireCleanupGeneration(ctx context.Context, store infraStateStore, expected interfaces.ResourceState) (*interfaces.ResourceState, error) {
	state, err := loadCleanupResource(ctx, store, expected.Name)
	if err != nil {
		return nil, err
	}
	if expected.Lifecycle == nil || expected.Lifecycle.Generation == "" || state == nil || state.Lifecycle == nil || state.Lifecycle.Generation != expected.Lifecycle.Generation || state.ProviderID != expected.ProviderID || state.Type != expected.Type {
		return nil, fmt.Errorf("%w: cleanup generation/identity changed for %q", interfaces.ErrValidation, expected.Name)
	}
	return state, nil
}

func resolveCleanupSecret(resolver cleanupSecretResolver, ref interfaces.RoutedSecretReference) (secrets.Provider, error) {
	if resolver == nil || ref.Key == "" {
		return nil, fmt.Errorf("%w: cleanup resolver/identifier missing", interfaces.ErrValidation)
	}
	provider, err := resolver(ref)
	if err != nil {
		return nil, cleanupStateError{operation: "resolve secret store", name: ref.Key, cause: err}
	}
	target := secrets.DescribeTarget(provider)
	if provider == nil || target.Provider != ref.Provider || target.Scope != ref.Scope || target.Subject != ref.Subject {
		return nil, fmt.Errorf("%w: cleanup secret namespace changed for %q", interfaces.ErrValidation, ref.Key)
	}
	return provider, nil
}

// Called only after cloud deletion succeeds. A restart may repeat revokes, but
// cannot forget the resource until every exact identifier reads back absent.
func reconcileResourceSecretDeletion(ctx context.Context, store infraStateStore, resolver cleanupSecretResolver, expected interfaces.ResourceState) error {
	state, err := requireCleanupGeneration(ctx, store, expected)
	if err != nil {
		return err
	}
	if state.Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletePending && state.Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
		return fmt.Errorf("%w: cleanup was not prepared for %q", interfaces.ErrValidation, state.Name)
	}
	if state.Lifecycle.Phase != interfaces.ResourcePhaseCloudDeletedSecretCleanupPending {
		state.Lifecycle.Phase = interfaces.ResourcePhaseCloudDeletedSecretCleanupPending
		if err := saveCleanupResource(ctx, store, *state); err != nil {
			return err
		}
	}
	for i, ref := range state.Lifecycle.Secrets {
		if ref.VerifiedAbsent {
			continue
		}
		if _, err := requireCleanupGeneration(ctx, store, *state); err != nil {
			return err
		}
		provider, err := resolveCleanupSecret(resolver, ref)
		if err != nil {
			return err
		}
		if err := sensitive.RevokeKeys(ctx, provider, []string{ref.Key}); err != nil {
			return err
		}
		if _, err := requireCleanupGeneration(ctx, store, *state); err != nil {
			return err
		}
		state.Lifecycle.Secrets[i].VerifiedAbsent = true
		if err := saveCleanupResource(ctx, store, *state); err != nil {
			return err
		}
	}
	for _, ref := range state.Lifecycle.Secrets {
		provider, err := resolveCleanupSecret(resolver, ref)
		if err != nil {
			return err
		}
		if err := sensitive.VerifyAbsent(ctx, provider, ref.Key); err != nil {
			return err
		}
	}
	if _, err := requireCleanupGeneration(ctx, store, *state); err != nil {
		return err
	}
	if err := store.DeleteResource(ctx, state.Name); err != nil {
		return cleanupStateError{operation: "remove completed tombstone", name: state.Name, cause: err}
	}
	return nil
}

type cleanupStateError struct {
	operation string
	name      string
	cause     error
}

func (e cleanupStateError) Error() string {
	return fmt.Sprintf("resource cleanup %s failed for %q", e.operation, e.name)
}
func (e cleanupStateError) Unwrap() error { return e.cause }
