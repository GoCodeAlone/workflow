package main

import (
	"os"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
)

// collectInfraEnvVarRefs returns a sorted, de-duplicated list of env-var
// names referenced via ${VAR} or $VAR in the raw (pre-substitution) Configs
// of all infra.* and platform.* modules in cfgFile.
//
// When envName is non-empty, per-environment overrides are applied via
// ModuleConfig.ResolveForEnv before scanning, so env-specific substitution
// references are captured.
//
// Preserved-key submaps (env_vars / env_vars_secret / secret_env_vars) are
// scanned just like any other map: their ${VAR} literals are kept verbatim
// in the persisted plan but the plan-time fingerprint of the underlying env
// var is still recorded so apply-time drift is detectable.
//
// LIMITATION (tracked, not addressed in W-1): top-level
// environments[env].envVars defaults that planResourcesForEnv merges into
// container-style modules are NOT applied here. References that originate
// solely from a top-level envVars default (without appearing in the
// module's own Config map) won't appear in InputSnapshot, so plan-stale
// drift detection will miss those vars changing between plan and apply.
// Closing the gap requires reusing planResourcesForEnv's merge logic
// before walkValueForEnvRefs; deferred to a follow-up that can extend
// ResolveForEnv to expose the merged form.
func collectInfraEnvVarRefs(cfgFile, envName string) ([]string, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	record := func(name string) string {
		if name != "" {
			seen[name] = struct{}{}
		}
		return ""
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !isInfraType(m.Type) {
			continue
		}
		if envName == "" {
			walkValueForEnvRefs(m.Config, record)
			continue
		}
		resolved, ok := m.ResolveForEnv(envName)
		if !ok {
			continue
		}
		walkValueForEnvRefs(resolved.Config, record)
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// walkValueForEnvRefs recursively scans v for ${VAR} / $VAR references in
// any string values, calling record(name) for each. Maps and slices are
// walked element-wise; non-string scalars are ignored.
func walkValueForEnvRefs(v any, record func(string) string) {
	switch val := v.(type) {
	case string:
		// os.Expand walks ${VAR} and $VAR references the same way os.ExpandEnv
		// does at substitution time, so the name set captured here matches the
		// set that ExpandEnvInMap[PreservingKeys] would actually substitute.
		os.Expand(val, record)
	case map[string]any:
		for _, vv := range val {
			walkValueForEnvRefs(vv, record)
		}
	case []any:
		for _, vv := range val {
			walkValueForEnvRefs(vv, record)
		}
	}
}

// computeInfraInputSnapshot returns the env-var fingerprint map for cfgFile's
// infra/platform modules. Returns (nil, nil) when no ${VAR} references exist.
func computeInfraInputSnapshot(cfgFile, envName string) (map[string]string, error) {
	names, err := collectInfraEnvVarRefs(cfgFile, envName)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	return inputsnapshot.Compute(names, inputsnapshot.OSEnvProvider), nil
}
