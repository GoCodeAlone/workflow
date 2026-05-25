package derive

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/requirements"
)

type Options struct {
	Provider       string
	Runtime        requirements.Runtime
	Environment    string
	NonInteractive bool
}

type GeneratedModule struct {
	Name      string
	Type      string
	Satisfies []string
	Config    map[string]any
	DependsOn []string
}

type Diagnostic struct {
	Key     string
	Code    string
	Message string
}

type Note struct {
	Key         string
	Message     string
	Interactive bool
}

type Result struct {
	Provider     string
	Runtime      requirements.Runtime
	Requirements []requirements.Requirement
	Modules      []GeneratedModule
	Rejected     []Diagnostic
	Notes        []Note
}

type MapRequest struct {
	Provider     string
	Runtime      requirements.Runtime
	Environment  string
	Requirements []requirements.Requirement
}

type MapResult struct {
	AcceptedKeys []string
	Modules      []GeneratedModule
	Rejected     []Diagnostic
	Notes        []Note
}

type ProviderMapper interface {
	MapRequirements(context.Context, MapRequest) (MapResult, error)
}

func Derive(ctx context.Context, cfg *config.WorkflowConfig, reqs []requirements.Requirement, mapper ProviderMapper, opts Options) (Result, error) {
	if mapper == nil {
		return Result{}, fmt.Errorf("iac requirement mapper is nil")
	}
	if len(reqs) == 0 {
		discovered, err := requirements.Discover(ctx, requirements.Input{
			Config:      cfg,
			Environment: opts.Environment,
		})
		if err != nil {
			return Result{}, err
		}
		reqs = discovered
	}
	provider, err := ResolveProvider(cfg, opts)
	if err != nil {
		return Result{}, err
	}
	runtime, err := resolveRuntime(reqs, opts)
	if err != nil {
		return Result{}, err
	}
	mapped, err := mapper.MapRequirements(ctx, MapRequest{
		Provider:     provider,
		Runtime:      runtime,
		Environment:  opts.Environment,
		Requirements: cloneRequirements(reqs),
	})
	if err != nil {
		return Result{}, err
	}
	modules := cloneModules(mapped.Modules)
	for i := range modules {
		if len(modules[i].Satisfies) == 0 {
			modules[i].Satisfies = append([]string(nil), mapped.AcceptedKeys...)
		}
		if err := rejectPlaintextSecrets(modules[i]); err != nil {
			return Result{}, err
		}
	}
	return Result{
		Provider:     provider,
		Runtime:      runtime,
		Requirements: cloneRequirements(reqs),
		Modules:      modules,
		Rejected:     append([]Diagnostic(nil), mapped.Rejected...),
		Notes:        append([]Note(nil), mapped.Notes...),
	}, nil
}

func ResolveProvider(cfg *config.WorkflowConfig, opts Options) (string, error) {
	if opts.Provider != "" {
		return opts.Provider, nil
	}
	choices := ProviderChoices(cfg, opts.Environment)
	switch len(choices) {
	case 0:
		return "", nil
	case 1:
		return choices[0], nil
	default:
		return "", fmt.Errorf("multiple iac providers available %v; pass --provider", choices)
	}
}

func ProviderChoices(cfg *config.WorkflowConfig, envName string) []string {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for i := range cfg.Modules {
		mod := &cfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		modCfg := mod.Config
		if envName != "" {
			if resolved, ok := mod.ResolveForEnv(envName); ok {
				modCfg = resolved.Config
			}
		}
		provider, _ := config.ExpandEnvInMap(modCfg)["provider"].(string)
		if provider != "" {
			seen[provider] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for provider := range seen {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

func resolveRuntime(reqs []requirements.Requirement, opts Options) (requirements.Runtime, error) {
	if opts.Runtime != "" {
		return opts.Runtime, nil
	}
	seen := make(map[requirements.Runtime]struct{})
	for i := range reqs {
		for _, runtime := range reqs[i].Runtimes {
			if runtime != "" {
				seen[runtime] = struct{}{}
			}
		}
	}
	switch len(seen) {
	case 0:
		return "", nil
	case 1:
		for runtime := range seen {
			return runtime, nil
		}
	}
	choices := make([]string, 0, len(seen))
	for runtime := range seen {
		choices = append(choices, string(runtime))
	}
	sort.Strings(choices)
	return "", fmt.Errorf("multiple requirement runtimes available %v; pass --runtime", choices)
}

func rejectPlaintextSecrets(module GeneratedModule) error {
	return scanSecretLikeValues(module.Config, func(path string) error {
		return fmt.Errorf("generated module %q contains plaintext secret-like config key %q", module.Name, path)
	})
}

func scanSecretLikeValues(value any, reject func(string) error) error {
	return scanSecretLikeValuesAt("", value, reject)
}

func scanSecretLikeValuesAt(path string, value any, reject func(string) error) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := joinPath(path, key)
			if err := scanSecretLikeValuesAt(childPath, typed[key], reject); err != nil {
				return err
			}
		}
	case []any:
		for i := range typed {
			if err := scanSecretLikeValuesAt(path, typed[i], reject); err != nil {
				return err
			}
		}
	case string:
		if secretLikeKey(path) && !isPlaceholder(typed) && typed != "" {
			return reject(path)
		}
	}
	return nil
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func secretLikeKey(key string) bool {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "-", "_")
	return strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey")
}

func isPlaceholder(value string) bool {
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}")
}

func cloneRequirements(in []requirements.Requirement) []requirements.Requirement {
	out := make([]requirements.Requirement, len(in))
	copy(out, in)
	return out
}

func cloneModules(in []GeneratedModule) []GeneratedModule {
	out := make([]GeneratedModule, len(in))
	for i := range in {
		out[i] = GeneratedModule{
			Name:      in[i].Name,
			Type:      in[i].Type,
			Satisfies: append([]string(nil), in[i].Satisfies...),
			Config:    cloneMap(in[i].Config),
			DependsOn: append([]string(nil), in[i].DependsOn...),
		}
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
