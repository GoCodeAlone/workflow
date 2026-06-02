package module

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// ─── step.iac_secret_reachability ────────────────────────────────────────────

// IaCSecretReachabilityStep pre-flights whether the secrets referenced in a set
// of IaC resource specs can be read from the chosen execution environment.
//
// It resolves the secrets provider from the app service registry (mirrors the
// resolveProvider pattern in SecretSetStep), gathers all distinct secret://
// references from the spec configs (including nested maps and slices), calls
// secrets.Reachability once for the provider, and reports per-ref results.
//
// Output shape:
//
//	{
//	  "secrets":       [ {ref, reachable, reason}, ... ],
//	  "all_reachable": bool,
//	}
//
// When there are zero secret:// refs, all_reachable is true with an empty list.
type IaCSecretReachabilityStep struct {
	name      string
	provider  string // secrets service name in the app registry
	execEnv   string
	specs     []interfaces.ResourceSpec
	specsFrom string // dotted context path; mutually exclusive with specs
	app       modular.Application
}

// NewIaCSecretReachabilityStepFactory returns a StepFactory for
// step.iac_secret_reachability.
func NewIaCSecretReachabilityStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_secret_reachability step %q: 'provider' is required", name)
		}

		execEnv, _ := cfg["exec_env"].(string)

		specsFrom, _ := cfg["specs_from"].(string)
		_, hasStaticSpecs := cfg["specs"]
		if specsFrom != "" && hasStaticSpecs {
			return nil, fmt.Errorf("iac_secret_reachability step %q: 'specs' and 'specs_from' are mutually exclusive", name)
		}

		var specs []interfaces.ResourceSpec
		if hasStaticSpecs {
			var err error
			specs, err = specparse.ParseResourceSpecs(cfg["specs"])
			if err != nil {
				return nil, fmt.Errorf("iac_secret_reachability step %q: parse specs: %w", name, err)
			}
		}

		return &IaCSecretReachabilityStep{
			name:      name,
			provider:  providerName,
			execEnv:   execEnv,
			specs:     specs,
			specsFrom: specsFrom,
			app:       app,
		}, nil
	}
}

func (s *IaCSecretReachabilityStep) Name() string { return s.name }

func (s *IaCSecretReachabilityStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve specs: dynamic path takes precedence when specsFrom is configured.
	specs := s.specs
	if s.specsFrom != "" {
		raw := resolveBodyFrom(s.specsFrom, pc)
		var err error
		specs, err = specparse.ParseResourceSpecs(raw)
		if err != nil {
			return nil, fmt.Errorf("iac_secret_reachability step %q: resolve specs_from %q: %w", s.name, s.specsFrom, err)
		}
	}

	// Resolve the secrets provider from the service registry.
	p, err := resolveSecretsProvider(s.app, s.provider, s.name)
	if err != nil {
		return nil, err
	}

	// Gather distinct secret:// refs across all spec configs.
	refs := collectSecretRefs(specs)

	// Short-path: no secret refs → trivially reachable.
	if len(refs) == 0 {
		return &StepResult{Output: map[string]any{
			"secrets":       []map[string]any{},
			"all_reachable": true,
		}}, nil
	}

	// Call Reachability once — the verdict is provider-level, not per-ref.
	// Propagate the Execute ctx so a slow/unreachable backend probe is bounded
	// by the pipeline/route deadline rather than hanging the pre-flight.
	verdict := secrets.Reachability(ctx, p, s.execEnv)

	secretEntries := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		entry := map[string]any{
			"ref":       ref,
			"reachable": verdict.Reachable,
		}
		if !verdict.Reachable {
			entry["reason"] = verdict.Reason
		}
		secretEntries = append(secretEntries, entry)
	}

	allReachable := verdict.Reachable

	return &StepResult{Output: map[string]any{
		"secrets":       secretEntries,
		"all_reachable": allReachable,
	}}, nil
}

// resolveSecretsProvider looks up a secrets.Provider from the application
// service registry by name. It mirrors the resolveProvider pattern in
// SecretSetStep: first checks if the service directly implements secrets.Provider;
// if not, checks for a Provider() accessor (used by SecretsAWSModule,
// SecretsVaultModule, etc.).
func resolveSecretsProvider(app modular.Application, providerName, stepName string) (secrets.Provider, error) {
	if app == nil {
		return nil, fmt.Errorf("iac_secret_reachability step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[providerName]
	if !ok {
		return nil, fmt.Errorf("iac_secret_reachability step %q: secrets service %q not found in registry", stepName, providerName)
	}

	// Direct: service itself implements secrets.Provider.
	if p, ok := svc.(secrets.Provider); ok {
		return p, nil
	}

	// Indirect: service exposes a Provider() accessor (e.g. SecretsAWSModule,
	// SecretsVaultModule).
	type providerAccessor interface {
		Provider() secrets.Provider
	}
	if acc, ok := svc.(providerAccessor); ok {
		p := acc.Provider()
		if p == nil {
			return nil, fmt.Errorf("iac_secret_reachability step %q: service %q exposes Provider() accessor but returned nil; module may not be started", stepName, providerName)
		}
		return p, nil
	}

	return nil, fmt.Errorf("iac_secret_reachability step %q: service %q does not implement secrets.Provider directly or via Provider() accessor", stepName, providerName)
}

// collectSecretRefs walks the Config map of each ResourceSpec and returns a
// sorted, deduplicated slice of all string values that start with secrets.SecretPrefix.
// It recurses into nested map[string]any, []any, and typed []string values so
// both YAML-decoded and programmatically-built spec configs are fully scanned.
func collectSecretRefs(specs []interfaces.ResourceSpec) []string {
	seen := make(map[string]struct{})
	for _, spec := range specs {
		collectFromValue(spec.Config, seen)
	}
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// collectFromValue recursively extracts secret:// refs from an arbitrary value.
// It handles both YAML-decoded shapes (map[string]any, []any) and typed Go
// shapes built programmatically (notably []string), since ResourceSpec.Config
// may carry either when specs are constructed in code rather than parsed.
func collectFromValue(v any, seen map[string]struct{}) {
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, secrets.SecretPrefix) {
			seen[val] = struct{}{}
		}
	case []string:
		for _, item := range val {
			if strings.HasPrefix(item, secrets.SecretPrefix) {
				seen[item] = struct{}{}
			}
		}
	case map[string]any:
		for _, child := range val {
			collectFromValue(child, seen)
		}
	case []any:
		for _, item := range val {
			collectFromValue(item, seen)
		}
	}
}
