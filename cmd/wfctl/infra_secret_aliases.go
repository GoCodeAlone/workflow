package main

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/google/uuid"
)

// Journal only aliases this apply is about to write. The durable source owns
// exact targets even if the generator is removed before the resource is deleted.
func journalInfraOutputAlias(ctx context.Context, store infraStateStore, observed []interfaces.ResourceState, cfg *config.WorkflowConfig, envName string, gen SecretGen, provider, defaultProvider secrets.Provider) error {
	if store == nil || provider == nil || gen.Key == "" {
		return fmt.Errorf("%w: infra_output alias journal requires a state store, provider, and key", interfaces.ErrValidation)
	}
	name, _, err := resolveInfraOutputSource(cfg, gen.Source, envName)
	if err != nil {
		return cleanupStateError{operation: "resolve alias source", name: gen.Key, cause: err}
	}
	var expected *interfaces.ResourceState
	for i := range observed {
		if observed[i].Name != name {
			continue
		}
		if expected != nil {
			return fmt.Errorf("%w: duplicate infra_output source %q", interfaces.ErrValidation, name)
		}
		expected = &observed[i]
	}
	state, err := loadCleanupResource(ctx, store, name)
	if err != nil {
		return err
	}
	if expected == nil || state == nil {
		return fmt.Errorf("%w: durable infra_output source missing for %q", interfaces.ErrValidation, name)
	}
	if state.ProviderID != expected.ProviderID || state.Type != expected.Type || resourceStateProviderRef(*state) != resourceStateProviderRef(*expected) {
		return fmt.Errorf("%w: infra_output source identity changed for %q", interfaces.ErrValidation, name)
	}
	if expected.Lifecycle != nil && (state.Lifecycle == nil || state.Lifecycle.Generation != expected.Lifecycle.Generation) {
		return fmt.Errorf("%w: infra_output source generation changed for %q", interfaces.ErrValidation, name)
	}
	if state.Lifecycle == nil {
		refs, err := routedReferencesFromState(*state, defaultProvider)
		if err != nil {
			return err
		}
		state.Lifecycle = &interfaces.ResourceLifecycle{Generation: uuid.NewString(), Phase: interfaces.ResourcePhaseActive, Secrets: refs}
	}
	if state.Lifecycle.Generation == "" || state.Lifecycle.Phase != interfaces.ResourcePhaseActive {
		return fmt.Errorf("%w: unresolved infra_output source lifecycle for %q", interfaces.ErrValidation, name)
	}
	target := secrets.DescribeTarget(provider)
	ref := interfaces.RoutedSecretReference{Key: gen.Key, Store: gen.Store, Provider: target.Provider, Scope: target.Scope, Subject: target.Subject}
	owners, err := store.ListResources(ctx)
	if err != nil {
		return cleanupStateError{operation: "read alias ownership", name: gen.Key, cause: err}
	}
	for i := range owners {
		owner := &owners[i]
		if owner.Name == state.Name || owner.Lifecycle == nil {
			continue
		}
		for _, owned := range owner.Lifecycle.Secrets {
			// Store names are config aliases; ownership is the concrete namespace/key.
			if owned.Key == ref.Key && owned.Provider == ref.Provider && owned.Scope == ref.Scope && owned.Subject == ref.Subject {
				return fmt.Errorf("%w: infra_output alias %q is owned by resource %q", interfaces.ErrValidation, gen.Key, owner.Name)
			}
		}
	}
	found := false
	for i, prior := range state.Lifecycle.Secrets {
		if prior.Key == ref.Key && prior.Store == ref.Store && prior.Provider == ref.Provider && prior.Scope == ref.Scope && prior.Subject == ref.Subject {
			state.Lifecycle.Secrets[i] = ref
			found = true
			break
		}
	}
	if !found {
		state.Lifecycle.Secrets = append(state.Lifecycle.Secrets, ref)
	}
	return saveCleanupResource(ctx, store, *state)
}
