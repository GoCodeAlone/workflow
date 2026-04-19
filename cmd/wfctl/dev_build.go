package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
)

// resolveBuildForEnv returns a CIBuildConfig for the given environment.
// If environments[envName].build is set, its targets override base targets
// by name (env target wins); remaining base targets are kept unchanged.
// If no env override exists, the base ci.build is returned unchanged.
func resolveBuildForEnv(cfg *config.WorkflowConfig, envName string) *config.CIBuildConfig {
	if cfg.CI == nil || cfg.CI.Build == nil {
		return nil
	}

	base := cfg.CI.Build
	envBuild := envBuildOverride(cfg, envName)
	if envBuild == nil {
		return applyLocalEnvDefaults(base, envName)
	}

	// Merge: build a map of env-override targets keyed by name.
	overrideByName := make(map[string]config.CITarget, len(envBuild.Targets))
	for _, t := range envBuild.Targets {
		overrideByName[t.Name] = t
	}

	// Apply overrides on top of base targets.
	merged := make([]config.CITarget, len(base.Targets))
	for i, t := range base.Targets {
		if ov, ok := overrideByName[t.Name]; ok {
			merged[i] = mergeTarget(t, ov)
		} else {
			merged[i] = t
		}
	}

	result := *base
	result.Targets = merged

	// Apply top-level security override if provided.
	if envBuild.Security != nil {
		result.Security = envBuild.Security
	}

	return applyLocalEnvDefaults(&result, envName)
}

// applyLocalEnvDefaults applies T38 (skip hardening) and T39 (local cache)
// when envName is "local" and no explicit override has already been set.
func applyLocalEnvDefaults(build *config.CIBuildConfig, envName string) *config.CIBuildConfig {
	if envName != "local" {
		return build
	}

	result := *build

	// T38: local env defaults to hardened=false, sbom=false unless explicitly overridden.
	if result.Security == nil || (result.Security.Hardened && result.Security.SBOM) {
		// Only override when security came from the global hardened defaults, not from
		// an explicit environments.local.build.security block (which sets Security != nil
		// with the user's choices).
		if build.Security == nil || isHardenedDefault(build.Security) {
			result.Security = &config.CIBuildSecurity{
				Hardened:   false,
				SBOM:       false,
				Provenance: "",
				NonRoot:    false,
			}
		}
	}

	// T39: inject local Docker layer cache into all containers that don't already have one.
	if len(result.Containers) > 0 {
		containers := make([]config.CIContainerTarget, len(result.Containers))
		copy(containers, result.Containers)
		for i := range containers {
			if containers[i].Cache == nil {
				containers[i].Cache = &config.CIContainerCache{
					From: []config.CIContainerCacheRef{{Type: "local"}},
				}
			}
		}
		result.Containers = containers
	}

	return &result
}

// isHardenedDefault returns true when sec matches the auto-applied hardened defaults,
// indicating no explicit user override was present.
func isHardenedDefault(sec *config.CIBuildSecurity) bool {
	return sec != nil && sec.Hardened && sec.SBOM && sec.Provenance == "slsa-3" && sec.NonRoot
}

// mergeTarget overlays env-specific config fields onto base, merging the
// config map so env keys win over base keys.
func mergeTarget(base, env config.CITarget) config.CITarget {
	out := base
	if env.Path != "" {
		out.Path = env.Path
	}
	if len(env.Config) > 0 {
		merged := make(map[string]any, len(base.Config)+len(env.Config))
		for k, v := range base.Config {
			merged[k] = v
		}
		for k, v := range env.Config {
			merged[k] = v
		}
		out.Config = merged
	}
	return out
}

// envBuildOverride returns the Build override for envName, or nil.
func envBuildOverride(cfg *config.WorkflowConfig, envName string) *config.CIBuildConfig {
	if cfg.Environments == nil {
		return nil
	}
	env, ok := cfg.Environments[envName]
	if !ok || env == nil {
		return nil
	}
	return env.Build
}

// runDevBuild resolves the build config for envName and invokes the build
// orchestrator. Called by runDevUp to trigger local builds.
func runDevBuild(cfgPath, envName string) error {
	cfg, err := loadDevConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	resolved := resolveBuildForEnv(cfg, envName)
	if resolved == nil {
		fmt.Fprintln(os.Stderr, "No ci.build config — skipping build phase")
		return nil
	}

	buildArgs := []string{"--config", cfgPath, "--env", envName}
	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		buildArgs = append(buildArgs, "--dry-run")
	}
	return runBuild(buildArgs)
}
