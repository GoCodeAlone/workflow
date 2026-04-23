package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/secrets"
)

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
	default:
		return infraUsage()
	}
}

func infraUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl infra <action> [options] [config.yaml]

Manage infrastructure defined in a workflow config.

Actions:
  plan      Show planned infrastructure changes
  apply     Apply infrastructure changes
  status    Show current infrastructure status
  drift     Detect configuration drift
  destroy   Tear down infrastructure
  import    Import an existing cloud resource into state
  state     Manage IaC state (list, export, import)

Options:
  --config <file>      Config file (default: infra.yaml or config/infra.yaml)
  --auto-approve       Skip confirmation prompt (apply/destroy only)
  --format <fmt>       Output format: table (default) or markdown (plan only)
  --output <file>      Write plan to JSON file (plan only)
  --show-sensitive/-S  Show sensitive values in plaintext (plan/apply only)
`)
	return fmt.Errorf("missing or unknown action")
}

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

	current := loadCurrentState(cfgFile)

	plan, err := platform.ComputePlan(desired, current)
	if err != nil {
		return fmt.Errorf("compute plan: %w", err)
	}

	switch *format {
	case "markdown":
		fmt.Print(formatPlanMarkdown(plan, showSensitive))
	default:
		fmt.Printf("Infrastructure Plan — %s\n\n", cfgFile)
		fmt.Print(formatPlanTable(plan, showSensitive))
	}

	if *output != "" {
		if err := writePlanJSON(plan, *output); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
		fmt.Printf("\nPlan saved to %s\n", *output)
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

// parseInfraResourceSpecs reads an infra config (resolving imports:) and
// returns ResourceSpecs for all infra.* and platform.* modules.
func parseInfraResourceSpecs(cfgFile string) ([]interfaces.ResourceSpec, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	var specs []interfaces.ResourceSpec
	for _, m := range cfg.Modules {
		if !isInfraType(m.Type) {
			continue
		}
		r := &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: config.ExpandEnvInMap(m.Config)}
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
	var out []*config.ResolvedModule
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !isInfraType(m.Type) {
			continue
		}
		if envName == "" {
			out = append(out, &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: config.ExpandEnvInMap(m.Config)})
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
		resolved.Config = config.ExpandEnvInMap(resolved.Config)
		out = append(out, resolved)
	}
	return out, nil
}

func isContainerType(t string) bool {
	return t == "infra.container_service" || t == "platform.do_app"
}

// loadCurrentState loads ResourceStates from the configured iac.state backend.
// Returns nil on any error (first run or unconfigured backend). Uses
// resolveStateStore so that remote backends (Spaces, S3, etc.) are supported.
func loadCurrentState(cfgFile string) []interfaces.ResourceState {
	store, err := resolveStateStore(cfgFile)
	if err != nil {
		return nil
	}
	states, err := store.ListResources(context.Background())
	if err != nil {
		return nil
	}
	return states
}

// configHashMap computes a deterministic SHA-256 hex hash of a config map.
func configHashMap(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	data, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(data))
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

	creates, updates, deletes := countActions(plan)
	fmt.Fprintf(&sb, "Plan: %d to create, %d to update, %d to destroy.\n",
		creates, updates, deletes)
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

	creates, updates, deletes := countActions(plan)
	fmt.Fprintf(&sb, "**Plan: %d to create, %d to update, %d to destroy.**\n",
		creates, updates, deletes)
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
	case "delete":
		return "-"
	default:
		return " "
	}
}

func countActions(plan interfaces.IaCPlan) (creates, updates, deletes int) {
	for i := range plan.Actions {
		switch plan.Actions[i].Action {
		case "create":
			creates++
		case "update":
			updates++
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
	var providerVal, resTypeVal, cloudIDVal string
	fs.StringVar(&providerVal, "provider", "", "Provider name (aws, gcp, azure, digitalocean)")
	fs.StringVar(&providerVal, "p", "", "Provider name (short for --provider)")
	fs.StringVar(&resTypeVal, "type", "", "Abstract resource type (e.g. infra.database)")
	fs.StringVar(&resTypeVal, "t", "", "Abstract resource type (short for --type)")
	fs.StringVar(&cloudIDVal, "id", "", "Cloud-provider resource ID")
	// Note: --env is intentionally absent from import. wfctl infra import is not
	// yet config-aware; env-scoped imports will be added in a follow-up.
	provider := &providerVal
	resType := &resTypeVal
	cloudID := &cloudIDVal
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *provider == "" || *resType == "" || *cloudID == "" {
		return fmt.Errorf("import requires --provider, --type, and --id\n\nExample:\n  wfctl infra import --provider aws --type infra.database --id db-abc123")
	}
	fmt.Printf("Import: provider=%s type=%s id=%s\n\n", *provider, *resType, *cloudID)
	fmt.Println("NOTE: Provider plugins (Phase 2) are required to call provider.Import().")
	fmt.Println("Once a provider plugin is installed, this command will query the cloud API")
	fmt.Println("and record the resource in the IaC state store.")
	return nil
}

func runInfraApply(args []string) error {
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
	autoApprove := &autoApproveVal
	showSensitive := showSensitiveVal
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = showSensitive // used in apply progress output when provider integration is complete

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			return err
		}
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

	fmt.Printf("Applying infrastructure from %s...\n", cfgFile)

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
		if err := applyInfraModules(ctx, cfgFile, envName); err != nil {
			return err
		}
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

	// Post-apply: sync infra_output secrets from the now-written state.
	secretsCfg, err := parseSecretsConfig(cfgFile)
	if err != nil || secretsCfg == nil {
		return err
	}
	secretsProvider, err := resolveSecretsProvider(secretsCfg)
	if err != nil {
		return fmt.Errorf("resolve secrets provider for infra_output sync: %w", err)
	}
	states := loadCurrentState(cfgFile)
	return syncInfraOutputSecrets(ctx, secretsCfg, secretsProvider, states)
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
