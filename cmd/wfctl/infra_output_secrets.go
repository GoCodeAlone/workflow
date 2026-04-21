package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// buildStateOutputsMap converts a slice of ResourceState into a map keyed by
// module name so generators can look up outputs by "module.field" source.
// Modules with nil Outputs are excluded.
func buildStateOutputsMap(states []interfaces.ResourceState) map[string]map[string]any {
	m := make(map[string]map[string]any, len(states))
	for i := range states {
		s := &states[i]
		if s.Outputs != nil {
			m[s.Name] = s.Outputs
		}
	}
	return m
}

// syncInfraOutputSecrets writes infra_output-typed secrets after a successful
// apply. It skips secrets that already exist in the provider so idempotent
// re-runs never overwrite live values.
func syncInfraOutputSecrets(ctx context.Context, secretsCfg *SecretsConfig, provider secrets.Provider, states []interfaces.ResourceState) error {
	if secretsCfg == nil {
		return nil
	}
	var gens []SecretGen
	for _, g := range secretsCfg.Generate {
		if g.Type == "infra_output" {
			gens = append(gens, g)
		}
	}
	if len(gens) == 0 {
		return nil
	}

	// Lazy List() cache — same pattern as bootstrapSecrets for write-only
	// providers (GitHub Actions) that return ErrUnsupported on Get.
	var listSet map[string]struct{}
	var listErr error
	var listDone bool
	lookupViaList := func(key string) (bool, error) {
		if !listDone {
			names, err := provider.List(ctx)
			listErr = err
			if err == nil {
				listSet = make(map[string]struct{}, len(names))
				for _, n := range names {
					listSet[n] = struct{}{}
				}
			}
			listDone = true
		}
		if listErr != nil && !errors.Is(listErr, secrets.ErrUnsupported) {
			return false, fmt.Errorf("list secrets to check %q: %w", key, listErr)
		}
		_, ok := listSet[key]
		return ok, nil
	}
	secretExists := func(key string) (bool, error) {
		_, err := provider.Get(ctx, key)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, secrets.ErrNotFound):
			return false, nil
		case errors.Is(err, secrets.ErrUnsupported):
			return lookupViaList(key)
		default:
			return false, fmt.Errorf("check secret %q: %w", key, err)
		}
	}

	stateOutputs := buildStateOutputsMap(states)

	for _, gen := range gens {
		exists, err := secretExists(gen.Key)
		if err != nil {
			return err
		}
		if exists {
			fmt.Printf("  secret %q: already exists — skipped\n", gen.Key)
			continue
		}

		genConfig := map[string]any{
			"source":         gen.Source,
			"_state_outputs": stateOutputs,
		}
		value, err := generateSecret(ctx, "infra_output", genConfig)
		if err != nil {
			return fmt.Errorf("generate infra_output secret %q: %w", gen.Key, err)
		}
		if err := provider.Set(ctx, gen.Key, value); err != nil {
			return fmt.Errorf("store secret %q: %w", gen.Key, err)
		}
		fmt.Printf("  secret %q: created from infra output\n", gen.Key)
	}
	return nil
}
