package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ResolutionDiagnostic names a single ${...} reference that survived
// plan-time resolution unresolved. Surfaces as part of the plan output
// so operators see WHICH refs collapsed at plan time vs which remain
// templated for apply-time JIT.
type ResolutionDiagnostic struct {
	ResourceName string
	Ref          string
}

// resolveSpecsAgainstState applies plan-time lenient JIT substitution
// against existing state outputs. ${MODULE.field} refs whose source is
// in state collapse to literals; refs whose source isn't in state are
// LEFT UNTOUCHED for apply-time JIT. Hard errors only on malformed refs.
//
// `resolvedSecrets` is built from cfg.Secrets.Generate filtered to
// Type="infra_output" entries whose Source ("module.field") is
// resolvable from current state. The synthetic env-lookup closure
// consults this map first, then falls through to os.LookupEnv — but
// ONLY for keys that are NOT non-infra_output secret keys.
//
// Non-infra_output secrets (random_hex, random_base64, random_alphanumeric,
// provider_credential, etc.) MUST NOT be substituted at plan time even if
// they happen to be present in the process environment (e.g. injected as
// GitHub Secrets). Substituting their literal values into the plan causes
// security-check R4 to flag them as secret literals in env_vars. These
// secrets are runtime-resolved: the runtime JIT resolver substitutes them
// from env at apply time, and DO App Platform stores them as encrypted
// SECRET-typed env vars. See ADR 0014.
//
// envName threads through resolveInfraOutput so per-env module-name
// renaming (e.g., bmw-database → bmw-staging-db) is honored.
//
// Replace-cascade note: this resolver collapses ${MODULE.id} refs to
// literal ProviderIDs from current state. When a parent resource is also
// being replaced in the same plan, the dependent's resolved spec will
// carry the OLD ProviderID. However, if the parent is being replaced, the
// dependent's Diff will also see no change (both desired and state have the
// same old ProviderID) and NO action is emitted for the dependent — so
// there is no spec in the plan that needs the new ProviderID via ReplaceIDMap.
// The replace-cascade path (where parent.id changes AND dependent has an
// action) requires the dependent's config to differ from state for a non-id
// field, at which point ${parent.id} would stay unresolved (source module's
// ProviderID is the same as before until apply actually runs) and apply-time
// JIT handles the substitution via ReplaceIDMap. See ADR 0013.
func resolveSpecsAgainstState(
	specs []interfaces.ResourceSpec,
	current []interfaces.ResourceState,
	cfg *config.WorkflowConfig,
	envName string,
) ([]interfaces.ResourceSpec, []ResolutionDiagnostic, error) {
	syncedOutputs := buildSyncedOutputsFromState(current)
	resolvedSecrets := buildResolvedSecretsFromState(cfg, current, envName)
	runtimeOnlyKeys := buildRuntimeOnlySecretKeys(cfg)
	envLookup := planTimeEnvLookup(resolvedSecrets, runtimeOnlyKeys)

	out := make([]interfaces.ResourceSpec, len(specs))
	var diags []ResolutionDiagnostic
	for i, spec := range specs {
		resolved, unresolved, err := jitsubst.TryResolveSpec(spec, nil, syncedOutputs, envLookup)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve %q: %w", spec.Name, err)
		}
		out[i] = resolved
		for _, ref := range unresolved {
			diags = append(diags, ResolutionDiagnostic{
				ResourceName: spec.Name, Ref: ref,
			})
		}
	}
	return out, diags, nil
}

// buildSyncedOutputsFromState mirrors wfctlhelpers.buildInitialSyncedOutputs
// but takes []ResourceState (not []PlanAction). Same canonical "id"
// rule: ProviderID shadows any "id" in Outputs.
func buildSyncedOutputsFromState(states []interfaces.ResourceState) map[string]map[string]any {
	out := make(map[string]map[string]any, len(states))
	for i := range states {
		s := &states[i]
		m := make(map[string]any, len(s.Outputs)+1)
		for k, v := range s.Outputs {
			m[k] = v
		}
		if s.ProviderID != "" {
			m["id"] = s.ProviderID
		}
		out[s.Name] = m
	}
	return out
}

// buildResolvedSecretsFromState walks cfg.Secrets.Generate, filters to
// Type="infra_output" entries whose Source is resolvable from current
// state, and returns key → resolved-value. Unresolvable infra_output
// secrets (source module not in state, field missing, etc.) are SKIPPED
// silently — TryResolveSpec will leave the corresponding ${SECRET}
// reference templated, which the caller surfaces via diagnostics.
func buildResolvedSecretsFromState(
	cfg *config.WorkflowConfig,
	current []interfaces.ResourceState,
	envName string,
) map[string]string {
	if cfg == nil || cfg.Secrets == nil {
		return nil
	}
	stateOutputs := buildStateOutputsMap(current)
	out := make(map[string]string, len(cfg.Secrets.Generate))
	for _, gen := range cfg.Secrets.Generate {
		if gen.Type != "infra_output" {
			continue
		}
		// nil hydrated: this caller is offline drift-resolution / preview;
		// no same-process apply hand-off available. Sensitive placeholders
		// will fail-and-skip (the loop continues).
		val, err := resolveInfraOutput(cfg, gen.Source, envName, stateOutputs, nil)
		if err != nil {
			continue
		}
		out[gen.Key] = val
	}
	return out
}

// buildRuntimeOnlySecretKeys returns the set of declared secret keys that must
// resolve only at apply/runtime. Generated non-infra_output secrets and
// externally supplied required secrets MUST NOT be substituted at plan time —
// even if present in the process environment — because doing so would embed a
// literal secret value in the plan, which security-check R4 flags as a
// potential secret literal in env_vars. See ADR 0014.
func buildRuntimeOnlySecretKeys(cfg *config.WorkflowConfig) map[string]struct{} {
	if cfg == nil {
		return nil
	}
	out := make(map[string]struct{})
	if cfg.Secrets != nil {
		for _, gen := range cfg.Secrets.Generate {
			if gen.Type != "infra_output" && gen.Key != "" {
				out[gen.Key] = struct{}{}
			}
		}
		for _, entry := range cfg.Secrets.Entries {
			if entry.Name != "" {
				out[entry.Name] = struct{}{}
			}
		}
	}
	for _, m := range cfg.Modules {
		switch m.Type {
		case "secrets.requires":
			for _, key := range secretModuleKeys(m.Config, "requires") {
				out[key] = struct{}{}
			}
		case "secrets.generate":
			raw, ok := m.Config["generate"].([]any)
			if !ok {
				continue
			}
			for _, item := range raw {
				gen, ok := item.(map[string]any)
				if !ok {
					continue
				}
				key, _ := gen["key"].(string)
				typ, _ := gen["type"].(string)
				if key != "" && typ != "infra_output" {
					out[key] = struct{}{}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// planTimeEnvLookup wraps the standard os.LookupEnv with a precedence
// step that consults resolvedSecrets first. resolvedSecrets is the
// map of infra_output-typed secret-keys whose source is in state and
// whose value was resolved at plan-prep time. Without this wrapper the
// env-only path of TryResolveSpec wouldn't see the synthetic values.
//
// runtimeOnlyKeys is the set of SecretGen keys whose type is not
// "infra_output". If a key is in this set, os.LookupEnv is NOT called —
// the reference is left as ${VAR} for apply-time runtime JIT resolution.
// This prevents literal secret values (e.g. NATS_AUTH_TOKEN generated by
// random_hex bootstrap) from appearing in the plan, which would trigger
// security-check R4. See ADR 0014.
func planTimeEnvLookup(resolvedSecrets map[string]string, runtimeOnlyKeys map[string]struct{}) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if v, ok := resolvedSecrets[name]; ok {
			return v, true
		}
		// Do not resolve runtime-only secrets at plan time — they must remain
		// as ${VAR} template references so apply-time JIT substitution can
		// inject them from the live environment.
		if _, blocked := runtimeOnlyKeys[name]; blocked {
			return "", false
		}
		return os.LookupEnv(name)
	}
}
