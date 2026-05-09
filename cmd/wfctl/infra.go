package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/secrets"
)

// infraPlanSchemaVersion is the maximum on-disk plan format version this
// wfctl binary is willing to read. runInfraApply rejects plans with a
// higher version so a future schema bump fails fast rather than being
// silently mis-read by an older binary.
//
// runInfraPlan stamps either V1 (no JIT references in plan.Actions) or
// V2 (any ${MODULE.field} or ${MODULE.id} surviving in
// plan.Actions[*].Resource.Config). The choice is per-plan via
// jitsubst.HasModuleRefs; see runInfraPlan and the persisted-plan
// rejection in T5.5 (V2 plans cannot be persisted via -o; canonical path
// is `wfctl infra apply` without --plan).
const (
	infraPlanSchemaVersion    = 2 // max readable
	infraPlanSchemaVersionV1  = 1 // pre-JIT baseline
	infraPlanSchemaVersionJIT = 2 // bumped when plan has ${MODULE.field|id} refs
)

// planRequiresJITSubstitution returns true when any action in plan
// carries a ${MODULE.field} or ${MODULE.id} reference somewhere in its
// resolved Resource.Config. Plain ${VAR} env-var references do NOT
// count — see jitsubst.HasModuleRefs for the exact rule.
//
// Used by runInfraPlan (T5.4) to gate plan.SchemaVersion = 2 stamping
// and by runInfraPlan's persisted-plan rejection (T5.5).
func planRequiresJITSubstitution(plan *interfaces.IaCPlan) bool {
	if plan == nil {
		return false
	}
	for i := range plan.Actions {
		if jitsubst.HasModuleRefs(plan.Actions[i].Resource.Config) {
			return true
		}
	}
	return false
}

func runInfra(args []string) error {
	if len(args) < 1 {
		return infraUsage()
	}
	switch args[0] {
	case "plan":
		return runInfraPlan(args[1:])
	case "apply":
		return runInfraApply(args[1:])
	case "status":
		return runInfraStatus(args[1:])
	case "drift":
		return runInfraDrift(args[1:])
	case "destroy":
		return runInfraDestroy(args[1:])
	case "import":
		return runInfraImport(args[1:])
	case "state":
		return runInfraState(args[1:])
	case "bootstrap":
		return runInfraBootstrap(args[1:])
	case "outputs":
		return runInfraOutputs(args[1:])
	case "refresh-outputs":
		return runInfraRefreshOutputs(args[1:])
	case "align":
		return runInfraAlign(args[1:])
	case "security-check":
		return runInfraSecurityCheck(args[1:])
	case "cleanup":
		return runInfraCleanup(args[1:])
	case "audit-secrets":
		if rc := runInfraAuditSecrets(args[1:], os.Stdout); rc != 0 {
			return fmt.Errorf("audit-secrets exited with code %d", rc)
		}
		return nil
	default:
		return infraUsage()
	}
}

func infraUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl infra <action> [options] [config.yaml]

Manage infrastructure defined in a workflow config.

Actions:
  plan           Show planned infrastructure changes
  apply          Apply infrastructure changes
  status         Show current infrastructure status
  drift          Detect configuration drift
  destroy        Tear down infrastructure
  import         Import an existing cloud resource into state
  state          Manage IaC state (list, export, import)
  outputs        Print captured resource outputs from state
  refresh-outputs Read live outputs and reconcile state (no cloud writes)
  align          Validate IaC config + plan alignment (8 rule families)
  security-check Scan plan.json for security policy violations
  cleanup        Tag-based force-cleanup across providers (--tag NAME [--fix])
  audit-secrets  Report provider_credential anti-patterns in secrets.generate

Options:
  --config <file>      Config file (default: infra.yaml or config/infra.yaml)
  --env <name>         Environment name for config/state resolution
  --name <resource>    Desired resource name from config (import only)
  --id <provider-id>   Cloud-provider resource ID (import only; optional)
  --auto-approve       Skip confirmation prompt (apply/destroy only)
  --format <fmt>       Output format: table (default) or markdown (plan only)
  --output <file>      Write plan to JSON file (plan only)
  --show-sensitive/-S  Show sensitive values in plaintext (plan/apply only)
  --tag <name>         Tag to match resources (cleanup only; required)
  --dry-run            Preview only (cleanup; default true)
  --fix                Opt into deletion (cleanup; overrides --dry-run)
`)
	return fmt.Errorf("missing or unknown action")
}

// infraPreserveKeys lists the submap keys whose contents should be left
// as ${VAR} literals through plan serialization. Apply-time injection
// (per the existing pattern in deploy_providers.go + driver Apply
// methods) resolves them when the plugin actually creates/updates the
// resource.
//
// Why these three keys:
//   - env_vars: App Platform service env vars that downstream consumers
//     reference in YAML as ${VAR}.
//   - env_vars_secret: canonical secret-typed env vars per
//     workflow-plugin-digitalocean's envVarsFromConfig.
//   - secret_env_vars: legacy alias for env_vars_secret kept for
//     backwards compat (same source).
//
// This preservation is the fix for core-dump#154 (R4 fired on
// env_vars["NATS_AUTH_TOKEN"] because the secret had been eagerly
// resolved into the plan output). See
// docs/plans/2026-05-02-staging-deploy-blockers-design.md.
var infraPreserveKeys = []string{"env_vars", "env_vars_secret", "secret_env_vars"}

// resolveInfraConfig finds the config file from flags or defaults.
// configFile is the resolved value from --config / -c flags (may be empty).
func resolveInfraConfig(fs *flag.FlagSet, configFile ...string) (string, error) {
	// Accept optional pre-resolved value (from StringVar binding).
	cfg := ""
	if len(configFile) > 0 {
		cfg = configFile[0]
	}
	// Fall back to looking up the long flag name if no value was passed.
	if cfg == "" {
		if f := fs.Lookup("config"); f != nil {
			cfg = f.Value.String()
		}
	}
	if cfg != "" {
		return cfg, nil
	}
	for _, candidate := range []string{"infra.yaml", "config/infra.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Check remaining args for a positional config file
	for _, arg := range fs.Args() {
		if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
			return arg, nil
		}
	}
	return "", fmt.Errorf("no infrastructure config found (tried infra.yaml, config/infra.yaml)\n\nCreate an infra config with cloud.account and platform.* modules.\nRun 'wfctl init --template full-stack' for a starter config with infrastructure")
}

// infraModuleEntry is a minimal struct for parsing modules from YAML.
type infraModuleEntry struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

// discoverInfraModules parses the config (resolving imports) and finds IaC-related modules.
func discoverInfraModules(cfgFile string) (iacState []infraModuleEntry, platforms []infraModuleEntry, cloudAccounts []infraModuleEntry, err error) {
	cfg, loadErr := config.LoadFromFile(cfgFile)
	if loadErr != nil {
		return nil, nil, nil, fmt.Errorf("load %s: %w", cfgFile, loadErr)
	}
	for _, m := range cfg.Modules {
		entry := infraModuleEntry{Name: m.Name, Type: m.Type, Config: m.Config}
		switch {
		case m.Type == "iac.state":
			iacState = append(iacState, entry)
		case m.Type == "cloud.account":
			cloudAccounts = append(cloudAccounts, entry)
		case strings.HasPrefix(m.Type, "platform.") || strings.HasPrefix(m.Type, "infra."):
			platforms = append(platforms, entry)
		}
	}
	return
}

func runInfraPlan(args []string) error {
	fs := flag.NewFlagSet("infra plan", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var formatVal string
	fs.StringVar(&formatVal, "format", "table", "Output format: table or markdown")
	fs.StringVar(&formatVal, "f", "table", "Output format (short for --format)")
	var outputVal string
	fs.StringVar(&outputVal, "output", "", "Write plan to JSON file")
	fs.StringVar(&outputVal, "o", "", "Write plan to JSON file (short for --output)")
	var showSensitiveVal bool
	fs.BoolVar(&showSensitiveVal, "show-sensitive", false, "Show sensitive values in plaintext")
	fs.BoolVar(&showSensitiveVal, "S", false, "Show sensitive values in plaintext (short for --show-sensitive)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	var planIncludeFlag string
	fs.StringVar(&planIncludeFlag, "include", "",
		"Comma-separated list of resource names to scope this command to (filters both desired specs and current state)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	format := &formatVal
	output := &outputVal
	showSensitive := showSensitiveVal

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	desired, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return err
	}
	if err := validateUniqueInfraResourceNames(desired); err != nil {
		return err
	}

	current, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state: %w", err)
	}

	// --include: apply scope filter. Validate before filtering so unknown names
	// produce a descriptive error. State-only resources (eligible for delete)
	// are accepted in the include set. parseInfraResourceSpecsForEnv returns
	// both infra.* and platform.* specs; --include works across both.
	planIncludeSet := parseIncludeFlag(planIncludeFlag)
	if err := validateIncludeSet(planIncludeSet, desired, current); err != nil {
		return err
	}
	desired = filterSpecsByInclude(desired, planIncludeSet)
	current = filterStatesByInclude(current, planIncludeSet)

	// Plan-time JIT resolution (PR-1): substitute ${MODULE.field} and
	// ${SECRET} refs against current state so driver.Diff sees real
	// values instead of literal templates. Refs whose source isn't in
	// state stay templated; planRequiresJITSubstitution detects them
	// and SchemaVersion=2 stamping handles the apply-time path.
	var resolutionDiags []ResolutionDiagnostic
	{
		wfCfgForResolver, cfgLoadErr := config.LoadFromFile(cfgFile)
		if cfgLoadErr != nil {
			return fmt.Errorf("load config for plan-time resolver: %w", cfgLoadErr)
		}
		desired, resolutionDiags, err = resolveSpecsAgainstState(desired, current, wfCfgForResolver, envName)
		if err != nil {
			return fmt.Errorf("resolve specs against state: %w", err)
		}
	}

	// W-3b: load each iac.provider plugin and dispatch ComputePlan per
	// provider group. The provider is required so platform.ComputePlan can
	// invoke ResourceDriver.Diff for ForceNew-aware Replace classification
	// (T3.6e). Configs without any iac.provider module fall back to a nil
	// provider, which platform.ComputePlan tolerates with the legacy
	// ConfigHash compare path (preserves minimal test fixtures and
	// out-of-band scripts that never declared one).
	plan, err := computePlanForInfraSpecs(context.Background(), cfgFile, envName, desired, current)
	if err != nil {
		wrapped := fmt.Errorf("compute plan: %w", err)
		// Emit an actionable hint to stderr if the underlying driver Diff
		// rejected an AppSpec image that's missing from its registry. See
		// infra_image_presence_hint.go.
		emitImageNotInRegistryHint(os.Stderr, wrapped)
		return wrapped
	}

	// Capture env-var fingerprints so apply (persisted-plan path: T1.5; in-process
	// path: T3.1.5) can surface a per-key diagnostic when a referenced env var
	// changed between plan and apply. Bumped to schema version 1 so older
	// readers that predate this field can be detected and rejected.
	snap, err := computeInfraInputSnapshot(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("compute input snapshot: %w", err)
	}
	plan.InputSnapshot = snap
	// Stamp SchemaVersion based on whether any plan action's resolved
	// Config carries a JIT-required ${MODULE.field|id} reference. Plain
	// ${VAR} env-var refs (no dot in body) do NOT trigger the bump —
	// plan-time config.ExpandEnvInMapPreservingKeys has already
	// collapsed them outside preserved keys, and inside preserved keys
	// they remain operator-managed across plan/apply (drift detection
	// in plan.InputSnapshot covers the change-after-plan case).
	plan.SchemaVersion = infraPlanSchemaVersionV1
	if planRequiresJITSubstitution(&plan) {
		plan.SchemaVersion = infraPlanSchemaVersionJIT
	}

	switch *format {
	case "markdown":
		fmt.Print(formatPlanMarkdown(plan, showSensitive))
	default:
		fmt.Printf("Infrastructure Plan — %s\n\n", cfgFile)
		fmt.Print(formatPlanTable(plan, showSensitive))
	}

	// Print any plan-time JIT resolution diagnostics after the plan table
	// so they're visible only when the plan rendering succeeded.
	if len(resolutionDiags) > 0 {
		fmt.Println()
		fmt.Println("Pending JIT resolution (apply-time):")
		for _, d := range resolutionDiags {
			fmt.Printf("  %s: ${%s}\n", d.ResourceName, d.Ref)
		}
	}

	if *output != "" {
		// T5.5: persisted plan.json is the wfctl-infra-apply --plan
		// canonical input. JIT-style plans cannot be persisted because
		// every ${MODULE.field|id} ref needs apply-time resolution
		// against this-apply ReplaceIDMap + syncedOutputs (data that
		// does NOT exist at plan time and CANNOT be preserved across
		// the plan/apply boundary). Reject up-front with an exact
		// error string the operator can grep for. Stdout-only emission
		// (no -o) of a JIT-style plan IS allowed — it's a preview, not
		// a contract — and falls through this guard untouched.
		if plan.SchemaVersion == infraPlanSchemaVersionJIT {
			// Plan literal per docs/plans/2026-05-03-iac-conformance-and-replace.md
			// §T5.5 line 2104. NO leading "error:" — that's prepended by
			// cmd/wfctl/main.go's top-level wrapper. errors.New (rather than
			// fmt.Errorf) avoids govet's no-verbs noise and is canonical for
			// fixed-string error literals per Go convention.
			return errors.New("this plan requires JIT resolution; persisted plan.json is not supported. Run 'wfctl infra apply' directly without -o/--plan")
		}
		// Embed a hash of the desired-state inputs so wfctl infra apply --plan
		// can detect stale plans when the config changes after plan generation.
		plan.DesiredHash = desiredStateHash(desired)
		if err := writePlanJSON(plan, *output); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
		fmt.Printf("\nPlan saved to %s\n", *output)
		// Plan files carry semi-sensitive content (env-var fingerprints,
		// resolved configs); warn the operator when none of the reachable
		// .gitignore files cover the output path. Silent when the directory
		// is not under a tracked repo (no .gitignore present).
		warnIfPlanNotGitignored(os.Stderr, *output)
	}

	return nil
}

// parseInfraResourceSpecs reads an infra YAML file and returns the list of
// infra.* modules as ResourceSpecs for plan computation.
// isInfraType returns true for module types handled by wfctl infra commands.
func isInfraType(t string) bool {
	return strings.HasPrefix(t, "infra.") || strings.HasPrefix(t, "platform.")
}

// extractDependsOn pulls the depends_on value from a module config map.
func extractDependsOn(cfg map[string]any) []string {
	raw, ok := cfg["depends_on"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, d := range v {
			if s, ok := d.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// resourceSpecFromResolvedModule converts a ResolvedModule to a ResourceSpec,
// populating Size and DependsOn from the resolved Config. Used by both the
// --env and no-env paths so field extraction never diverges.
func resourceSpecFromResolvedModule(r *config.ResolvedModule) interfaces.ResourceSpec {
	spec := interfaces.ResourceSpec{
		Name:      r.Name,
		Type:      r.Type,
		Config:    r.Config,
		DependsOn: extractDependsOn(r.Config),
	}
	if size, ok := r.Config["size"].(string); ok {
		spec.Size = interfaces.Size(size)
	}
	return spec
}

// secretGenKeys returns the variable names declared in cfg.Secrets.Generate.
// These keys are preserved as literal ${VAR} references during plan-time
// config expansion so that desiredStateHash produces the same result
// regardless of whether the variable is currently set in the process
// environment. This fixes the "plan stale: config hash mismatch" error that
// occurs when a generated secret (e.g. STAGING_PG_PASSWORD) is referenced
// outside env_vars — for example in a Droplet user_data cloud-init script —
// where the variable is absent at plan time but present at apply time.
func secretGenKeys(cfg *config.WorkflowConfig) []string {
	if cfg == nil || cfg.Secrets == nil {
		return nil
	}
	keys := make([]string, 0, len(cfg.Secrets.Generate))
	for _, g := range cfg.Secrets.Generate {
		if g.Key != "" {
			keys = append(keys, g.Key)
		}
	}
	return keys
}

// parseInfraResourceSpecs reads an infra config (resolving imports:) and
// returns ResourceSpecs for all infra.* and platform.* modules.
func parseInfraResourceSpecs(cfgFile string) ([]interfaces.ResourceSpec, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	secretVars := secretGenKeys(cfg)
	var specs []interfaces.ResourceSpec
	for _, m := range cfg.Modules {
		if !isInfraType(m.Type) {
			continue
		}
		r := &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: config.ExpandEnvInMapPreservingVars(m.Config, infraPreserveKeys, secretVars)}
		specs = append(specs, resourceSpecFromResolvedModule(r))
	}
	return specs, nil
}

// parseInfraResourceSpecsForEnv returns ResourceSpecs for plan computation,
// applying per-environment resolution when envName is non-empty. Both the
// --env and no-env paths produce the same ResourceSpec shape so callers never
// need to duplicate the ResolvedModule->ResourceSpec mapping.
func parseInfraResourceSpecsForEnv(cfgFile, envName string) ([]interfaces.ResourceSpec, error) {
	if envName == "" {
		return parseInfraResourceSpecs(cfgFile)
	}
	resolved, err := planResourcesForEnv(cfgFile, envName)
	if err != nil {
		return nil, err
	}
	specs := make([]interfaces.ResourceSpec, 0, len(resolved))
	for _, r := range resolved {
		specs = append(specs, resourceSpecFromResolvedModule(r))
	}
	return specs, nil
}

// planResourcesForEnv loads the config at path and returns the list of
// resolved modules for envName. Resources whose environments[envName] is
// explicitly null are skipped. If envName is empty, all modules are returned
// with their top-level config. Top-level environments[envName] defaults
// (region, provider, envVars) are applied after per-module resolution.
func planResourcesForEnv(path, envName string) ([]*config.ResolvedModule, error) {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	var topEnv *config.EnvironmentConfig
	if envName != "" && cfg.Environments != nil {
		topEnv = cfg.Environments[envName]
	}
	secretVars := secretGenKeys(cfg)
	var out []*config.ResolvedModule
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !isInfraType(m.Type) {
			continue
		}
		if envName == "" {
			out = append(out, &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: config.ExpandEnvInMapPreservingVars(m.Config, infraPreserveKeys, secretVars)})
			continue
		}
		resolved, ok := m.ResolveForEnv(envName)
		if !ok {
			continue
		}
		if topEnv != nil {
			if resolved.Region == "" {
				resolved.Region = topEnv.Region
				if resolved.Region != "" {
					if resolved.Config == nil {
						resolved.Config = map[string]any{}
					}
					if _, present := resolved.Config["region"]; !present {
						resolved.Config["region"] = resolved.Region
					}
				}
			}
			if resolved.Provider == "" {
				resolved.Provider = topEnv.Provider
				if resolved.Provider != "" {
					if resolved.Config == nil {
						resolved.Config = map[string]any{}
					}
					if _, present := resolved.Config["provider"]; !present {
						resolved.Config["provider"] = resolved.Provider
					}
				}
			}
			if isContainerType(resolved.Type) && len(topEnv.EnvVars) > 0 {
				ev, _ := resolved.Config["env_vars"].(map[string]any)
				if ev == nil {
					ev = map[string]any{}
				}
				for k, v := range topEnv.EnvVars {
					if _, present := ev[k]; !present {
						ev[k] = v
					}
				}
				resolved.Config["env_vars"] = ev
			}
		}
		// Expand ${VAR} / $VAR references in the per-env resolved config so
		// that plan output and apply pipeline both see substituted values.
		// Use the preserving variant so that env_vars submaps retain their
		// ${VAR} literals through plan serialization (apply-time injection
		// resolves them when the plugin creates/updates the resource).
		// secretVars are also preserved so that fields like user_data that
		// reference generated secrets produce the same hash at plan time
		// (variable unset) and apply time (variable set).
		resolved.Config = config.ExpandEnvInMapPreservingVars(resolved.Config, infraPreserveKeys, secretVars)
		out = append(out, resolved)
	}
	return out, nil
}

func isContainerType(t string) bool {
	return t == "infra.container_service" || t == "platform.do_app"
}

// loadCurrentState loads ResourceStates from the configured iac.state backend.
// Returns an error when the state store cannot be resolved or read; callers
// that treat "no prior state" as a valid first-run condition should swallow the
// error explicitly with a comment. Uses resolveStateStore so that remote
// backends (Spaces, S3, etc.) are supported. envName is forwarded to
// resolveStateStore so per-env backend config (e.g. region, prefix) is applied
// when reading state.
func loadCurrentState(cfgFile, envName string) ([]interfaces.ResourceState, error) {
	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return nil, fmt.Errorf("resolve state store: %w", err)
	}
	states, err := store.ListResources(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list state resources: %w", err)
	}
	return states, nil
}

// configHashMap delegates to platform.ConfigHash so that the CLI always
// produces hashes byte-for-byte identical to those stored by ComputePlan.
// The local duplication that previously existed here has been removed.
func configHashMap(config map[string]any) string {
	return platform.ConfigHash(config)
}

// formatPlanTable renders an interfaces.IaCPlan as a human-readable table
// with per-resource config details shown as indented key-value lines.
func formatPlanTable(plan interfaces.IaCPlan, showSensitive bool) string {
	if len(plan.Actions) == 0 {
		return "No changes. Infrastructure is up-to-date.\n"
	}

	var sb strings.Builder
	for i := range plan.Actions {
		a := &plan.Actions[i]
		symbol := actionSymbol(a.Action)
		fmt.Fprintf(&sb, "%s %s  %s  (%s)\n", symbol, a.Action, a.Resource.Name, a.Resource.Type)
		keys := resourceSummaryKeys(a.Resource.Type, a.Resource.Config, showSensitive)
		if len(keys) > 0 {
			// Align values: find longest key.
			maxLen := 0
			for _, kv := range keys {
				if len(kv[0]) > maxLen {
					maxLen = len(kv[0])
				}
			}
			for _, kv := range keys {
				padding := strings.Repeat(" ", maxLen-len(kv[0]))
				fmt.Fprintf(&sb, "    %s:%s  %s\n", kv[0], padding, kv[1])
			}
		}
		fmt.Fprintln(&sb)
	}

	creates, updates, replaces, deletes := countActions(plan)
	fmt.Fprintf(&sb, "Plan: %d to create, %d to update, %d to replace, %d to destroy.\n",
		creates, updates, replaces, deletes)
	return sb.String()
}

// formatPlanMarkdown renders an interfaces.IaCPlan as GitHub-flavored markdown
// with collapsible sections per resource, suitable for PR comments.
func formatPlanMarkdown(plan interfaces.IaCPlan, showSensitive bool) string {
	if len(plan.Actions) == 0 {
		return "## Infrastructure Plan\n\nNo changes. Infrastructure is up-to-date.\n"
	}

	var sb strings.Builder
	sb.WriteString("### Infrastructure Plan\n\n")
	sb.WriteString("| Action | Resource | Type |\n")
	sb.WriteString("|--------|----------|------|\n")
	for i := range plan.Actions {
		a := &plan.Actions[i]
		symbol := actionSymbol(a.Action)
		fmt.Fprintf(&sb, "| %s %s | `%s` | `%s` |\n", symbol, a.Action, a.Resource.Name, a.Resource.Type)
	}
	sb.WriteString("\n")

	for i := range plan.Actions {
		a := &plan.Actions[i]
		symbol := actionSymbol(a.Action)
		fmt.Fprintf(&sb, "<details>\n<summary>%s %s %s (%s)</summary>\n\n",
			symbol, a.Action, a.Resource.Name, a.Resource.Type)

		keys := resourceSummaryKeys(a.Resource.Type, a.Resource.Config, showSensitive)
		if len(keys) > 0 {
			sb.WriteString("| Property | Value |\n")
			sb.WriteString("|----------|-------|\n")
			for _, kv := range keys {
				fmt.Fprintf(&sb, "| %s | %s |\n", kv[0], kv[1])
			}
			sb.WriteString("\n")
		}
		sb.WriteString("</details>\n\n")
	}

	creates, updates, replaces, deletes := countActions(plan)
	fmt.Fprintf(&sb, "**Plan: %d to create, %d to update, %d to replace, %d to destroy.**\n",
		creates, updates, replaces, deletes)
	return sb.String()
}

// resourceSummaryKeys returns the most relevant key-value pairs to display for
// a given resource type. Each entry is a [key, value] pair. Sensitive keys are
// masked as "(sensitive)" unless showSensitive is true.
func resourceSummaryKeys(resType string, cfg map[string]any, showSensitive bool) [][2]string {
	if len(cfg) == 0 {
		return nil
	}

	sensitiveSet := make(map[string]struct{})
	if !showSensitive {
		for _, k := range secrets.DefaultSensitiveKeys() {
			sensitiveSet[k] = struct{}{}
		}
	}

	// Helper to extract a string value from config, masking if sensitive.
	str := func(key string) string {
		if _, isSensitive := sensitiveSet[key]; isSensitive {
			if _, ok := cfg[key]; ok {
				return "(sensitive)"
			}
			return ""
		}
		if v, ok := cfg[key]; ok {
			switch s := v.(type) {
			case string:
				return s
			case int, int64, float64, bool:
				return fmt.Sprintf("%v", s)
			}
		}
		return ""
	}

	add := func(pairs *[][2]string, key, val string) {
		if val != "" {
			*pairs = append(*pairs, [2]string{key, val})
		}
	}

	var pairs [][2]string

	switch resType {
	case "infra.vpc":
		add(&pairs, "name", str("name"))
		add(&pairs, "region", str("region"))
		add(&pairs, "cidr", str("cidr"))

	case "infra.firewall":
		add(&pairs, "name", str("name"))
		// Inbound rules — try both key variants.
		for _, key := range []string{"inbound_rules", "inbound"} {
			if rules, ok := cfg[key]; ok {
				for _, line := range formatFirewallRulesList(rules) {
					add(&pairs, "allow inbound", line)
				}
				break
			}
		}
		// Outbound rules.
		for _, key := range []string{"outbound_rules", "outbound"} {
			if rules, ok := cfg[key]; ok {
				for _, line := range formatFirewallRulesList(rules) {
					add(&pairs, "allow outbound", line)
				}
				break
			}
		}

	case "infra.database":
		add(&pairs, "name", str("name"))
		engine := str("engine")
		ver := str("version")
		if engine != "" && ver != "" {
			add(&pairs, "engine", engine+" v"+ver)
		} else {
			add(&pairs, "engine", engine)
		}
		add(&pairs, "size", str("size"))
		add(&pairs, "nodes", str("nodes"))
		add(&pairs, "region", str("region"))

	case "infra.container_service":
		add(&pairs, "name", str("name"))
		add(&pairs, "image", str("image"))
		add(&pairs, "http_port", str("http_port"))
		add(&pairs, "instances", str("instances"))
		add(&pairs, "region", str("region"))

	case "infra.registry":
		add(&pairs, "name", str("name"))
		add(&pairs, "tier", str("tier"))
		add(&pairs, "region", str("region"))

	case "infra.cluster":
		add(&pairs, "name", str("name"))
		add(&pairs, "region", str("region"))
		add(&pairs, "node_size", str("node_size"))
		add(&pairs, "nodes", str("nodes"))

	case "infra.cache":
		add(&pairs, "name", str("name"))
		add(&pairs, "engine", str("engine"))
		add(&pairs, "size", str("size"))
		add(&pairs, "region", str("region"))

	case "infra.storage":
		add(&pairs, "name", str("name"))
		add(&pairs, "region", str("region"))

	case "infra.dns":
		add(&pairs, "name", str("name"))
		add(&pairs, "domain", str("domain"))

	case "infra.load_balancer":
		add(&pairs, "name", str("name"))
		add(&pairs, "region", str("region"))
		add(&pairs, "algorithm", str("algorithm"))

	default:
		// For unknown types, show up to 5 top-level string/numeric keys.
		count := 0
		for k, v := range cfg {
			if count >= 5 {
				break
			}
			if _, isSensitive := sensitiveSet[k]; isSensitive {
				add(&pairs, k, "(sensitive)")
				count++
				continue
			}
			switch s := v.(type) {
			case string:
				if s != "" {
					add(&pairs, k, s)
					count++
				}
			case int, int64, float64, bool:
				add(&pairs, k, fmt.Sprintf("%v", v))
				count++
			}
		}
	}
	return pairs
}

// formatFirewallRulesList returns one summary line per firewall rule.
func formatFirewallRulesList(v any) []string {
	rules, ok := v.([]any)
	if !ok {
		return nil
	}
	var lines []string
	for _, r := range rules {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		var parts []string
		if proto, ok := rm["protocol"].(string); ok && proto != "" {
			parts = append(parts, strings.ToUpper(proto))
		}
		if ports, ok := rm["ports"].(string); ok && ports != "" {
			parts = append(parts, ports)
		}
		// Handle both "source"/"sources" and "destination"/"destinations".
		if src := extractSources(rm, "source", "sources"); src != "" {
			parts = append(parts, "from "+src)
		}
		if dst := extractSources(rm, "destination", "destinations"); dst != "" {
			parts = append(parts, "to "+dst)
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, " "))
		}
	}
	return lines
}

// extractSources gets a string or []string from a map, trying both singular and plural keys.
func extractSources(m map[string]any, singular, plural string) string {
	// Try plural first (array of IPs).
	if v, ok := m[plural]; ok {
		switch sv := v.(type) {
		case []any:
			strs := make([]string, 0, len(sv))
			for _, s := range sv {
				if str, ok := s.(string); ok {
					strs = append(strs, str)
				}
			}
			return strings.Join(strs, ",")
		case []string:
			return strings.Join(sv, ",")
		case string:
			return sv
		}
	}
	// Try singular.
	if v, ok := m[singular]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func actionSymbol(action string) string {
	switch action {
	case "create":
		return "+"
	case "update":
		return "~"
	case "replace":
		return "±"
	case "delete":
		return "-"
	default:
		return " "
	}
}

func countActions(plan interfaces.IaCPlan) (creates, updates, replaces, deletes int) {
	for i := range plan.Actions {
		switch plan.Actions[i].Action {
		case "create":
			creates++
		case "update":
			updates++
		case "replace":
			replaces++
		case "delete":
			deletes++
		}
	}
	return
}

// writePlanJSON serialises the plan to a JSON file for later apply.
func writePlanJSON(plan interfaces.IaCPlan, path string) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// runInfraImport imports an existing cloud resource into the IaC state.
func runInfraImport(args []string) error {
	fs := flag.NewFlagSet("infra import", flag.ContinueOnError)
	var configFile, envName, nameVal, cloudIDVal string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name")
	fs.StringVar(&nameVal, "name", "", "Desired resource name from config")
	fs.StringVar(&cloudIDVal, "id", "", "Cloud-provider resource ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}
	if nameVal == "" {
		return fmt.Errorf("import requires --name with the desired resource name from config")
	}

	spec, err := findInfraSpecByName(cfgFile, envName, nameVal)
	if err != nil {
		return err
	}
	providerType, providerCfg, err := resolveProviderForSpec(cfgFile, envName, spec)
	if err != nil {
		return err
	}
	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("resolve state store: %w", err)
	}
	if isNoopStateStore(store) {
		return fmt.Errorf("infra import requires a writable iac.state backend; add an iac.state module before importing %q", spec.Name)
	}
	provider, closer, err := resolveIaCProvider(context.Background(), providerType, providerCfg)
	if err != nil {
		return fmt.Errorf("load provider %q: %w", providerType, err)
	}
	if closer != nil {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", providerType, cerr)
			}
		}()
	}

	var state interfaces.ResourceState
	if cloudIDVal != "" {
		imported, err := provider.Import(context.Background(), cloudIDVal, spec.Type)
		if err != nil {
			return fmt.Errorf("%s/%s: import provider id %q: %w", spec.Type, spec.Name, cloudIDVal, err)
		}
		state, err = resourceStateFromImportedState(spec, providerType, imported, cloudIDVal)
		if err != nil {
			return err
		}
	} else {
		driver, err := provider.ResourceDriver(spec.Type)
		if err != nil {
			return fmt.Errorf("%s/%s: resolve resource driver: %w", spec.Type, spec.Name, err)
		}
		ref, adoptable, err := adoptionRefForSpec(driver, spec)
		if err != nil {
			return err
		}
		if !adoptable {
			return fmt.Errorf("%s/%s: resource type is not importable without --id", spec.Type, spec.Name)
		}
		live, err := driver.Read(context.Background(), ref)
		if err != nil {
			return fmt.Errorf("%s/%s: read existing resource: %w", spec.Type, spec.Name, err)
		}
		if live == nil {
			return fmt.Errorf("%s/%s: read existing resource returned no state", spec.Type, spec.Name)
		}
		state, err = resourceStateFromLiveOutput(spec, providerType, live)
		if err != nil {
			return err
		}
	}
	if err := validateStateProviderID(provider, providerType, state); err != nil {
		return err
	}
	if err := store.SaveResource(context.Background(), state); err != nil {
		return fmt.Errorf("save imported state %q: %w", state.Name, err)
	}
	fmt.Printf("Imported %q (%s) id=%s provider=%s\n", state.Name, state.Type, state.ProviderID, state.Provider)
	return nil
}

func findInfraSpecByName(cfgFile, envName, name string) (interfaces.ResourceSpec, error) {
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return interfaces.ResourceSpec{}, err
	}
	for _, spec := range specs {
		if spec.Name == name {
			return spec, nil
		}
	}
	if envName != "" {
		return interfaces.ResourceSpec{}, fmt.Errorf("infra resource %q not found in %s for env %q", name, cfgFile, envName)
	}
	return interfaces.ResourceSpec{}, fmt.Errorf("infra resource %q not found in %s", name, cfgFile)
}

func resolveProviderForSpec(cfgFile, envName string, spec interfaces.ResourceSpec) (string, map[string]any, error) {
	moduleRef, _ := spec.Config["provider"].(string)
	if moduleRef == "" {
		return "", nil, fmt.Errorf("infra module %q (%s): missing required 'provider' field", spec.Name, spec.Type)
	}
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" || m.Name != moduleRef {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				return "", nil, fmt.Errorf("infra module %q references provider %q which is disabled for environment %q", spec.Name, moduleRef, envName)
			}
			modCfg = config.ExpandEnvInMapPreservingKeys(resolved.Config, infraPreserveKeys)
		} else {
			modCfg = config.ExpandEnvInMapPreservingKeys(m.Config, infraPreserveKeys)
		}
		providerType, _ := modCfg["provider"].(string)
		if providerType == "" {
			return "", nil, fmt.Errorf("provider module %q has no 'provider' type configured", moduleRef)
		}
		return providerType, modCfg, nil
	}
	return "", nil, fmt.Errorf("infra module %q references provider %q which is not declared as an iac.provider module", spec.Name, moduleRef)
}

func isNoopStateStore(store infraStateStore) bool {
	_, ok := store.(*noopStateStore)
	return ok
}

func resourceStateFromImportedState(spec interfaces.ResourceSpec, providerType string, imported *interfaces.ResourceState, providerIDOverride string) (interfaces.ResourceState, error) {
	if imported == nil {
		return interfaces.ResourceState{}, fmt.Errorf("%s/%s: provider import returned no state", spec.Type, spec.Name)
	}
	providerID := imported.ProviderID
	if providerID == "" {
		providerID = providerIDOverride
	}
	if providerID == "" {
		providerID = imported.ID
	}
	if providerID == "" {
		providerID = imported.Name
	}
	if providerID == "" {
		return interfaces.ResourceState{}, fmt.Errorf("%s/%s: imported resource returned empty ProviderID; state not persisted", spec.Type, spec.Name)
	}
	appliedConfig := cloneMap(imported.AppliedConfig)
	if appliedConfig == nil {
		appliedConfig = liveConfigFromOutputs(imported.Outputs)
	}
	cfgHash := imported.ConfigHash
	if cfgHash == "" {
		cfgHash = configHashMap(appliedConfig)
	}
	now := imported.CreatedAt
	if now.IsZero() {
		now = imported.UpdatedAt
	}
	if now.IsZero() {
		now = platformNow()
	}
	updated := imported.UpdatedAt
	if updated.IsZero() {
		updated = platformNow()
	}
	return interfaces.ResourceState{
		ID:            spec.Name,
		Name:          spec.Name,
		Type:          spec.Type,
		Provider:      providerType,
		ProviderRef:   resourceSpecProviderRef(spec),
		ProviderID:    providerID,
		ConfigHash:    cfgHash,
		AppliedConfig: appliedConfig,
		Outputs:       cloneMap(imported.Outputs),
		Dependencies:  append([]string(nil), spec.DependsOn...),
		CreatedAt:     now,
		UpdatedAt:     updated,
	}, nil
}

func platformNow() time.Time {
	return time.Now().UTC()
}

func runInfraApply(args []string) error {
	// runHydrated carries routed-secret values from the same-process apply
	// (sensitive.Route's hydrated map) to syncInfraOutputSecrets below.
	// Empty when no driver emitted sensitive outputs; nil for the
	// precomputed-plan branch unless threaded through. Required for
	// rehydration on write-only providers (GitHub Actions secrets are
	// write-only after Set).
	var runHydrated map[string]string
	fs := flag.NewFlagSet("infra apply", flag.ContinueOnError)
	var configFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	var autoApproveVal bool
	fs.BoolVar(&autoApproveVal, "auto-approve", false, "Skip confirmation")
	fs.BoolVar(&autoApproveVal, "y", false, "Skip confirmation (short for --auto-approve)")
	var showSensitiveVal bool
	fs.BoolVar(&showSensitiveVal, "show-sensitive", false, "Show sensitive values in plaintext")
	fs.BoolVar(&showSensitiveVal, "S", false, "Show sensitive values in plaintext (short for --show-sensitive)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "Show planned operations without executing provider mutations")
	var dryRunFormat string
	fs.StringVar(&dryRunFormat, "format", "table", "Dry-run output format: table, json")
	var planFile string
	fs.StringVar(&planFile, "plan", "", "Apply from a pre-emitted plan.json (skips ComputePlan)")
	var refreshFlag bool
	fs.BoolVar(&refreshFlag, "refresh", false, "Detect drift and prune ghost-in-state entries before applying")
	var allowProtectedPruneFlag bool
	fs.BoolVar(&allowProtectedPruneFlag, "allow-protected-prune", false, "Allow pruning state entries for resources marked protected: true (requires --refresh)")
	var refreshOutputsFlag bool
	fs.BoolVar(&refreshOutputsFlag, "refresh-outputs", false,
		"Refresh per-field Outputs from cloud truth before applying (recommended pair with --refresh for cutover-style operations)")
	var skipRefreshFlag bool
	fs.BoolVar(&skipRefreshFlag, "skip-refresh", false, "Skip the WFCTL_REFRESH_OUTPUTS pre-step refresh even if the env var is set (does NOT cancel explicit --refresh-outputs)")
	var allowReplaceFlag string
	fs.StringVar(&allowReplaceFlag, "allow-replace", "",
		"Comma-separated list of resource names whose protected: true status is overridden for this apply (replace/delete actions only)")
	var includeFlag string
	fs.StringVar(&includeFlag, "include", "",
		"Comma-separated list of resource names to scope this command to (filters both desired specs and current state)")
	autoApprove := &autoApproveVal
	showSensitive := showSensitiveVal
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = showSensitive // used in apply progress output when provider integration is complete

	// Pre-flight: --allow-protected-prune is only meaningful with --refresh.
	// Without --refresh, the flag is silently ignored, which could mislead
	// operators into believing they have authorized a dangerous prune operation.
	if allowProtectedPruneFlag && !refreshFlag {
		return fmt.Errorf("--allow-protected-prune requires --refresh")
	}

	// Pre-flight: --include + --plan is rejected. The plan already carries the
	// scope from the plan-time --include invocation; applying a scoped plan with
	// a different --include would produce confusing partial-apply behavior.
	if parseIncludeFlag(includeFlag) != nil && planFile != "" {
		return fmt.Errorf("--include cannot be combined with --plan (use --include at plan time, then apply with --plan; the plan already carries the scope)")
	}

	// W-6/T6.1: publish the parsed --allow-replace set for the apply
	// path's gate (validateAllowReplaceProtected, called from both
	// applyWithProviderAndStore and applyPrecomputedPlanWithStore).
	// Reset to nil at the top of every invocation so the gate fails
	// closed when subsequent runs do not pass the flag — package-level
	// state would otherwise leak override authorization across runs.
	applyAllowReplaceSet = parseAllowReplaceFlag(allowReplaceFlag)
	defer func() { applyAllowReplaceSet = nil }()

	// Publish the --include flag value for the apply path's filter helpers
	// (including dry-run). Reset to "" at the top of every invocation so the
	// filter fails open (all-resources) on subsequent invocations that do not
	// pass the flag. Must be set before the dry-run early return so the dry-run
	// planner can see the same include scope.
	currentApplyIncludeFlag = includeFlag
	defer func() { currentApplyIncludeFlag = "" }()

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			return err
		}
	}

	// --dry-run: compute and display the plan without executing any mutations.
	if dryRun {
		return runInfraApplyDryRun(cfgFile, envName, dryRunFormat, showSensitiveVal)
	}

	if !*autoApprove {
		fmt.Printf("Apply infrastructure changes from %s? [y/N]: ", cfgFile)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Auto-bootstrap first: generates secrets (secrets: generate:) and ensures
	// the state backend exists before we attempt to inject/use secrets.
	infraCfg, err := parseInfraConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("parse infra config: %w", err)
	}
	autoBootstrap := infraCfg == nil || infraCfg.AutoBootstrap == nil || *infraCfg.AutoBootstrap
	if autoBootstrap {
		fmt.Println("Running bootstrap before apply...")
		bootstrapArgs := []string{"--config", cfgFile}
		if envName != "" {
			bootstrapArgs = append(bootstrapArgs, "--env", envName)
		}
		if err := runInfraBootstrap(bootstrapArgs); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
	}

	ctx := context.Background()

	// Inject secrets after bootstrap so generated secrets are available.
	if envName != "" {
		wfCfg, loadErr := config.LoadFromFile(cfgFile)
		if loadErr == nil && wfCfg.Secrets != nil && len(wfCfg.Secrets.Entries) > 0 {
			secretVals, secretErr := injectSecrets(ctx, wfCfg, envName)
			if secretErr != nil {
				return fmt.Errorf("inject secrets for env %q: %w", envName, secretErr)
			}
			for k, v := range secretVals {
				os.Setenv(k, v)
			}
		}
	}

	// --refresh-outputs: read cloud-truth Outputs and persist field-level
	// changes to state. Runs as a pre-step to either --refresh ghost-prune
	// or the regular plan/apply path — so ghost-prune sees fresh Outputs.
	// NOT gated by skipRefreshFlag — that flag only cancels the env-var-
	// driven pre-step; explicit --refresh-outputs is operator-opt-in and
	// overrides skip semantics. Per ADR 0008: paired flag, not a semantic
	// change to --refresh.
	refreshOutputsRan := false
	if refreshOutputsFlag {
		if hasInfraModules(cfgFile) {
			if err := applyPreStepRefreshOutputs(ctx, cfgFile, envName, os.Stdout); err != nil {
				return fmt.Errorf("--refresh-outputs: %w", err)
			}
			refreshOutputsRan = true
		} else {
			fmt.Println("Refresh-outputs: --refresh-outputs requires infra.* modules; legacy platform.* config — no-op.")
		}
	}

	// --refresh: detect drift first and prune ghost-in-state entries (cloud 404s)
	// before running the normal plan + apply. Only applicable for infra.* configs;
	// silently skipped for legacy platform.* configs. Runs AFTER --refresh-outputs
	// so the drift check sees the freshest possible Outputs.
	if refreshFlag && hasInfraModules(cfgFile) {
		fmt.Println("Refreshing state (detecting drift)...")
		store, storeErr := resolveStateStore(cfgFile, envName)
		if storeErr != nil {
			return fmt.Errorf("open state store for refresh: %w", storeErr)
		}
		states, statesErr := store.ListResources(ctx)
		if statesErr != nil {
			return fmt.Errorf("list state for refresh: %w", statesErr)
		}
		groups, groupOrder := groupStatesByProvider(states, cfgFile, envName)
		// Wrap each group in a helper so the deferred closer fires after the
		// group finishes, not at runInfraApply exit. Without this, a config
		// with N provider groups would hold N connections open throughout the
		// rest of the apply path (same pattern as infra_plan_provider.go and
		// infra_apply.go).
		refreshGroup := func(moduleRef string, g *providerGroup) error {
			provider, closer, provErr := resolveIaCProvider(ctx, g.provType, g.provCfg)
			if provErr != nil {
				return fmt.Errorf("refresh: load provider %q: %w", moduleRef, provErr)
			}
			if closer != nil {
				provType := g.provType
				defer func() {
					if cerr := closer.Close(); cerr != nil {
						fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", provType, cerr)
					}
				}()
			}
			return runInfraApplyRefreshPhase(ctx, provider, g.refs, store,
				*autoApprove, allowProtectedPruneFlag, states, os.Stdout, os.Stderr)
		}
		for _, moduleRef := range groupOrder {
			if refreshErr := refreshGroup(moduleRef, groups[moduleRef]); refreshErr != nil {
				return fmt.Errorf("refresh phase: %w", refreshErr)
			}
		}
	}

	// WFCTL_REFRESH_OUTPUTS pre-step (T2.3): when opted in, read live
	// Outputs from each provider and persist any field-level changes
	// before computing the plan, so apply doesn't make decisions on
	// stale state. Default off; --skip-refresh always wins. Only
	// applicable for infra.* configs (legacy platform.* path doesn't
	// flow through iac/refreshoutputs). Skipped when --refresh-outputs
	// already ran (refreshOutputsRan guard prevents double-trigger).
	if !refreshOutputsRan && applyPreStepRefreshEnabled(skipRefreshFlag) && hasInfraModules(cfgFile) {
		if err := applyPreStepRefreshOutputs(ctx, cfgFile, envName, os.Stdout); err != nil {
			return fmt.Errorf("apply pre-step refresh-outputs: %w", err)
		}
	}

	fmt.Printf("Applying infrastructure from %s...\n", cfgFile)

	// --plan: dispatch actions from a pre-emitted plan file, skipping ComputePlan.
	if planFile != "" {
		plan, err := loadPlanFromFile(planFile)
		if err != nil {
			return err
		}
		// Reject plans whose on-disk schema is newer than this binary
		// understands. SchemaVersion == 0 (unset) is grandfathered in for
		// plans emitted by wfctl predating the field.
		if plan.SchemaVersion > infraPlanSchemaVersion {
			return fmt.Errorf("plan schema_version %d is newer than this wfctl supports (max %d) — upgrade wfctl or re-plan with the older format", plan.SchemaVersion, infraPlanSchemaVersion)
		}
		// Validate that the plan is still current relative to the config.
		desired, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
		if err != nil {
			return fmt.Errorf("parse infra resource specs: %w", err)
		}
		if plan.DesiredHash == "" {
			return fmt.Errorf("plan file has no hash — regenerate with: wfctl infra plan -o plan.json")
		}
		// Check the input-fingerprint drift first so the operator gets a
		// per-key diagnostic instead of the generic config-hash mismatch.
		// (Env-var changes are a strict subset of config-hash differences;
		// flagging them here yields the actionable message.) Names list is
		// derived from plan.InputSnapshot keys — no separate InputNames field.
		if len(plan.InputSnapshot) > 0 {
			names := make([]string, 0, len(plan.InputSnapshot))
			for k := range plan.InputSnapshot {
				names = append(names, k)
			}
			applySnap := inputsnapshot.Compute(names, inputsnapshot.OSEnvProvider)
			if drift := inputsnapshot.ComputeDrift(plan.InputSnapshot, applySnap); len(drift) > 0 {
				// *StaleError: Error() yields the canonical FormatStaleError
				// output (no sentinel prefix); Unwrap() yields ErrEnvVarChanged
				// so errors.Is(err, inputsnapshot.ErrEnvVarChanged) still matches.
				return inputsnapshot.NewStaleError(drift)
			}
		}
		// Mirror the plan-time resolver: apply resolveSpecsAgainstState before
		// hashing so that DesiredHash is computed on post-resolution specs, matching
		// what runInfraPlan recorded in plan.DesiredHash. Without this step, any ref
		// that resolved at plan time would cause a currentHash != plan.DesiredHash
		// mismatch on every --plan apply.
		{
			currentState, stateErr := loadCurrentState(cfgFile, envName)
			if stateErr != nil {
				return fmt.Errorf("load state for stale-check: %w", stateErr)
			}
			planApplyCfg, cfgErr := config.LoadFromFile(cfgFile)
			if cfgErr != nil {
				return fmt.Errorf("load config for stale-check: %w", cfgErr)
			}
			desired, _, err = resolveSpecsAgainstState(desired, currentState, planApplyCfg, envName)
			if err != nil {
				return fmt.Errorf("resolve specs for stale-check: %w", err)
			}
		}
		currentHash := desiredStateHash(desired)
		if plan.DesiredHash != currentHash {
			return fmt.Errorf("plan stale: config hash mismatch (run wfctl infra plan again)")
		}
		if err := applyFromPrecomputedPlan(ctx, plan, cfgFile, envName); err != nil {
			return err
		}
		// Fall through to post-apply infra_output secrets sync below —
		// same as the live-diff path so STAGING_DATABASE_URL and similar
		// infra_output secrets are always refreshed after a successful apply.
	} else {
		// Dispatch: infra.* modules use the direct IaCProvider path; legacy
		// platform.* configs fall back to the pipeline runner (pipelines.apply).
		// Mixing both types in the same config is not supported — fail fast with a
		// descriptive error rather than silently skipping one class of modules.
		if hasInfraModules(cfgFile) && hasPlatformModules(cfgFile) {
			return fmt.Errorf(
				"config %q mixes infra.* and platform.* module types — "+
					"use one style per config file, or split into separate configs",
				cfgFile,
			)
		}
		if hasInfraModules(cfgFile) {
			h, err := applyInfraModules(ctx, cfgFile, envName)
			if err != nil {
				return err
			}
			runHydrated = h
		} else {
			pipelineCfg := cfgFile
			if envName != "" {
				tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
				if resErr != nil {
					return resErr
				}
				defer os.Remove(tmp)
				pipelineCfg = tmp
			}
			if err := runPipelineRun([]string{"-c", pipelineCfg, "-p", "apply"}); err != nil {
				return err
			}
		}
	}

	// Post-apply: sync infra_output secrets from the now-written state.
	secretsCfg, err := parseSecretsConfig(cfgFile)
	if err != nil || secretsCfg == nil {
		return err
	}
	secretsProvider, err := resolveSecretsProvider(secretsCfg)
	if err != nil {
		return fmt.Errorf("resolve secrets provider for infra_output sync: %w", err)
	}
	states, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state for infra_output sync: %w", err)
	}
	// Only reload the workflow config when env resolution is actually needed:
	// it is needed only when --env is set AND at least one infra_output secret
	// generator is configured (otherwise syncInfraOutputSecrets is a no-op for
	// env resolution regardless).
	var wfCfg *config.WorkflowConfig
	if envName != "" {
		for _, g := range secretsCfg.Generate {
			if g.Type == "infra_output" {
				var loadErr error
				wfCfg, loadErr = config.LoadFromFile(cfgFile)
				if loadErr != nil {
					return fmt.Errorf("load config for infra_output env resolution: %w", loadErr)
				}
				break
			}
		}
	}
	return syncInfraOutputSecrets(ctx, secretsCfg, secretsProvider, states, wfCfg, envName, runHydrated)
}

func runInfraStatus(args []string) error {
	fs := flag.NewFlagSet("infra status", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	fmt.Printf("Infrastructure status from %s...\n", cfgFile)

	// Direct path for infra.* module configs; legacy pipeline path for platform.*.
	if hasInfraModules(cfgFile) {
		return statusInfraModules(context.Background(), cfgFile, envName)
	}

	pipelineCfg := cfgFile
	if envName != "" {
		tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
		if resErr != nil {
			return resErr
		}
		defer os.Remove(tmp)
		pipelineCfg = tmp
	}
	return runPipelineRun([]string{"-c", pipelineCfg, "-p", "status"})
}

func runInfraDrift(args []string) error {
	fs := flag.NewFlagSet("infra drift", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	fmt.Printf("Detecting drift for %s...\n", cfgFile)

	// Direct path for infra.* module configs; legacy pipeline path for platform.*.
	if hasInfraModules(cfgFile) {
		return driftInfraModules(context.Background(), cfgFile, envName)
	}

	pipelineCfg := cfgFile
	if envName != "" {
		tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
		if resErr != nil {
			return resErr
		}
		defer os.Remove(tmp)
		pipelineCfg = tmp
	}
	return runPipelineRun([]string{"-c", pipelineCfg, "-p", "drift"})
}

func runInfraDestroy(args []string) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	var configFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	var autoApproveVal bool
	fs.BoolVar(&autoApproveVal, "auto-approve", false, "Skip confirmation")
	fs.BoolVar(&autoApproveVal, "y", false, "Skip confirmation (short for --auto-approve)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	autoApprove := &autoApproveVal
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			return err
		}
	}

	if !*autoApprove {
		fmt.Printf("DESTROY all infrastructure defined in %s? This cannot be undone. [y/N]: ", cfgFile)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Destroying infrastructure from %s...\n", cfgFile)

	// Direct path for infra.* module configs; legacy pipeline path for platform.*.
	if hasInfraModules(cfgFile) {
		return destroyInfraModules(context.Background(), cfgFile, envName)
	}

	pipelineCfg := cfgFile
	if envName != "" {
		tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
		if resErr != nil {
			return resErr
		}
		defer os.Remove(tmp)
		pipelineCfg = tmp
	}
	return runPipelineRun([]string{"-c", pipelineCfg, "-p", "destroy"})
}
