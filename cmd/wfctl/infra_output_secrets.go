package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/sensitive"
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

// resolveInfraOutput resolves a single "module.field" source string against the
// pre-loaded state outputs map, applying per-env module name resolution so that
// a source like "bmw-database.uri" finds the state keyed by the env-resolved
// name (e.g. "bmw-staging-db") when --env staging renames the module.
//
// wfCfg may be nil (e.g. tests that only care about base-name resolution).
// When envName is empty no resolution is performed and the source module name
// is used verbatim.
//
// hydrated is the in-memory routed-secret map from the same-process apply
// (may be nil). When the state field is a sensitive.PlaceholderPrefix
// string, resolveInfraOutput rehydrates from hydrated FIRST; if the
// secret is not in hydrated, it returns a documented error explaining
// the cold-start constraint (write-only providers cannot rehydrate
// without same-process hand-off).
func resolveInfraOutput(wfCfg *config.WorkflowConfig, source, envName string, stateOutputs map[string]map[string]any, hydrated map[string]string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("infra_output: source is required (format: \"module.field\")")
	}
	dot := strings.Index(source, ".")
	if dot < 1 || dot >= len(source)-1 {
		return "", fmt.Errorf("infra_output: invalid source %q: expected \"module.field\" format", source)
	}
	moduleName := source[:dot]
	field := source[dot+1:]

	// Apply env resolution: the state was persisted under the env-resolved name.
	if envName != "" && wfCfg != nil {
		for i := range wfCfg.Modules {
			m := &wfCfg.Modules[i]
			if m.Name != moduleName {
				continue
			}
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				return "", fmt.Errorf("infra_output: module %q is explicitly disabled for environment %q — cannot read infra_output from a disabled module", moduleName, envName)
			}
			if resolved.Name != "" {
				moduleName = resolved.Name
			}
			break
		}
	}

	if stateOutputs == nil {
		return "", fmt.Errorf("infra_output: state outputs not available for source %q — did infra apply succeed?", source)
	}
	outputs, ok := stateOutputs[moduleName]
	if !ok {
		return "", fmt.Errorf("infra_output: module %q not found in state (available: %s)", moduleName, strings.Join(stateKeys(stateOutputs), ", "))
	}
	val, ok := outputs[field]
	if !ok {
		return "", fmt.Errorf("infra_output: field %q not found in outputs of module %q", field, moduleName)
	}
	// If state has a routed-secret placeholder, prefer the hydrated map.
	if sensitive.IsPlaceholder(val) {
		secretName := strings.TrimPrefix(val.(string), sensitive.PlaceholderPrefix)
		if hv, hok := hydrated[secretName]; hok {
			return hv, nil
		}
		return "", fmt.Errorf(
			"infra_output: field %q of module %q is a routed-secret placeholder %q; not in same-process hydrated map "+
				"(write-only providers like GitHub Actions cannot rehydrate cold; rerun apply or reference the secret directly via secret://%s)",
			field, moduleName, val, secretName,
		)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("infra_output: output field %q of module %q is %T, expected string", field, moduleName, val)
	}
	return s, nil
}

// stateKeys returns the sorted keys of a state outputs map for error messages.
func stateKeys(m map[string]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// syncInfraOutputSecrets writes infra_output-typed secrets after a successful
// apply. It skips secrets that already exist in the provider so idempotent
// re-runs never overwrite live values.
// wfCfg and envName are used to resolve source module names through per-env
// overrides so that "bmw-database.uri" finds "bmw-staging-db" in state when
// --env staging renames the module.
//
// hydrated is the in-memory map of routed-secret values from the
// just-completed apply (populated by sensitive.Route). When a state output
// field is a sensitive.PlaceholderPrefix string, resolveInfraOutput
// rehydrates from this map so the generator can read the real value
// without going through provider.Get (which is unsupported on write-only
// providers like GitHub Actions). May be nil for callers that don't
// have a same-process apply hand-off (e.g., wfctl infra outputs CLI).
func syncInfraOutputSecrets(ctx context.Context, secretsCfg *SecretsConfig, provider secrets.Provider, states []interfaces.ResourceState, wfCfg *config.WorkflowConfig, envName string, hydrated map[string]string) error {
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

		value, err := resolveInfraOutput(wfCfg, gen.Source, envName, stateOutputs, hydrated)
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
