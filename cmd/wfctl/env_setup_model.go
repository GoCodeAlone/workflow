package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/secrets"
)

type envSetupInputKind string

const (
	envSetupInputSecret envSetupInputKind = "secret"
	envSetupInputVar    envSetupInputKind = "var"
)

func (k envSetupInputKind) String() string {
	if k == envSetupInputVar {
		return "var"
	}
	return "secret"
}

func parseNameMappings(raw []string) (map[string]string, error) {
	mappings := make(map[string]string, len(raw))
	storedNames := make(map[string]string, len(raw))
	for _, item := range raw {
		logical, stored, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("--name-map %q: expected LOGICAL=STORED", item)
		}
		logical = strings.TrimSpace(logical)
		stored = strings.TrimSpace(stored)
		if logical == "" || stored == "" {
			return nil, fmt.Errorf("--name-map %q: logical and stored names are required", item)
		}
		if existing := mappings[logical]; existing != "" {
			return nil, fmt.Errorf("--name-map %q: logical name %q is already mapped to %q", item, logical, existing)
		}
		if existing := storedNames[stored]; existing != "" {
			return nil, fmt.Errorf("--name-map %q: stored name %q is already used by logical name %q", item, stored, existing)
		}
		mappings[logical] = stored
		storedNames[stored] = logical
	}
	return mappings, nil
}

func applyManifestNameMappings(inputs []manifestDiscoveredSecret, mappings map[string]string) []manifestDiscoveredSecret {
	if len(mappings) == 0 {
		for i := range inputs {
			if inputs[i].StorageName == "" {
				inputs[i].StorageName = inputs[i].Name
			}
		}
		return inputs
	}
	for i := range inputs {
		logical := inputs[i].Name
		if stored := strings.TrimSpace(mappings[logical]); stored != "" {
			inputs[i].StorageName = stored
			continue
		}
		if inputs[i].StorageName == "" {
			inputs[i].StorageName = logical
		}
	}
	return inputs
}

func manifestInputStorageName(input manifestDiscoveredSecret) string {
	if name := strings.TrimSpace(input.StorageName); name != "" {
		return name
	}
	return strings.TrimSpace(input.Name)
}

func manifestInputDisplayName(input manifestDiscoveredSecret) string {
	stored := manifestInputStorageName(input)
	if stored == "" || stored == input.Name {
		return input.Name
	}
	return input.Name + " -> " + stored
}

func manifestInputValueLookupNames(input manifestDiscoveredSecret) []string {
	names := []string{manifestInputStorageName(input)}
	if input.Name != "" && input.Name != names[0] {
		names = append(names, input.Name)
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func manifestTargetVariableProvider(provider SecretsProvider) (secrets.VariableProvider, bool) {
	if adapter, ok := provider.(secretsProviderAdapter); ok {
		vars, ok := adapter.p.(secrets.VariableProvider)
		return vars, ok
	}
	vars, ok := provider.(secrets.VariableProvider)
	return vars, ok
}

func manifestTargetCheck(ctx context.Context, target manifestSecretTargetProvider, input manifestDiscoveredSecret) SecretStatus {
	storedName := manifestInputStorageName(input)
	status := SecretStatus{
		Name:  input.Name,
		Store: target.Store,
	}
	if input.Kind == envSetupInputVar {
		vars, ok := manifestTargetVariableProvider(target.Provider)
		if !ok {
			status.State = SecretUnconfigured
			status.Error = fmt.Sprintf("provider %q does not support variables", target.ProviderName())
			return status
		}
		meta, err := vars.CheckVariable(ctx, storedName)
		if err != nil {
			status.State = SecretFetchError
			status.Error = err.Error()
			return status
		}
		if meta.Exists {
			status.State = SecretSet
			status.IsSet = true
		} else {
			status.State = SecretNotSet
		}
		return status
	}
	state, err := target.Provider.Check(ctx, storedName)
	status.State = state
	status.IsSet = state == SecretSet
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func (p manifestSecretTargetProvider) ProviderName() string {
	if p.Provider == nil {
		return ""
	}
	if targeter, ok := p.Provider.(interface{ SecretTarget() secrets.ProviderTarget }); ok {
		if name := strings.TrimSpace(targeter.SecretTarget().Provider); name != "" {
			return name
		}
	}
	if adapter, ok := p.Provider.(secretsProviderAdapter); ok && adapter.p != nil {
		return adapter.p.Name()
	}
	return p.Store
}

func manifestTargetSet(ctx context.Context, target manifestSecretTarget, value string) error {
	storedName := manifestInputStorageName(target.Secret)
	if target.Secret.Kind == envSetupInputVar {
		vars, ok := manifestTargetVariableProvider(target.Provider)
		if !ok {
			return fmt.Errorf("provider target %s does not support variables", manifestSecretTargetScopeLabel(target))
		}
		return vars.SetVariable(ctx, storedName, value)
	}
	return target.Provider.Set(ctx, storedName, value)
}

func sortedManifestInputs(inputsByName map[string]*manifestDiscoveredSecret) []manifestDiscoveredSecret {
	names := make([]string, 0, len(inputsByName))
	for name := range inputsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]manifestDiscoveredSecret, 0, len(names))
	for _, name := range names {
		input := *inputsByName[name]
		if input.StorageName == "" {
			input.StorageName = input.Name
		}
		out = append(out, input)
	}
	return out
}
