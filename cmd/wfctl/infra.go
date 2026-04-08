package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/secrets"
	"gopkg.in/yaml.v3"
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
  --config <file>    Config file (default: infra.yaml or config/infra.yaml)
  --auto-approve     Skip confirmation prompt (apply/destroy only)
  --format <fmt>     Output format: table (default) or markdown (plan only)
  --output <file>    Write plan to JSON file (plan only)
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

// discoverInfraModules parses the config and finds IaC-related modules.
func discoverInfraModules(cfgFile string) (iacState []infraModuleEntry, platforms []infraModuleEntry, cloudAccounts []infraModuleEntry, err error) {
	data, readErr := os.ReadFile(cfgFile)
	if readErr != nil {
		return nil, nil, nil, fmt.Errorf("read %s: %w", cfgFile, readErr)
	}

	var parsed struct {
		Modules []infraModuleEntry `yaml:"modules"`
	}
	if yamlErr := yaml.Unmarshal(data, &parsed); yamlErr != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", cfgFile, yamlErr)
	}

	for _, m := range parsed.Modules {
		switch {
		case m.Type == "iac.state":
			iacState = append(iacState, m)
		case m.Type == "cloud.account":
			cloudAccounts = append(cloudAccounts, m)
		case strings.HasPrefix(m.Type, "platform."):
			platforms = append(platforms, m)
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

	desired, err := parseInfraResourceSpecs(cfgFile)
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
func parseInfraResourceSpecs(cfgFile string) ([]interfaces.ResourceSpec, error) {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cfgFile, err)
	}

	var parsed struct {
		Modules []struct {
			Name   string         `yaml:"name"`
			Type   string         `yaml:"type"`
			Config map[string]any `yaml:"config"`
		} `yaml:"modules"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cfgFile, err)
	}

	var specs []interfaces.ResourceSpec
	for _, m := range parsed.Modules {
		if !strings.HasPrefix(m.Type, "infra.") {
			continue
		}
		spec := interfaces.ResourceSpec{
			Name:   m.Name,
			Type:   m.Type,
			Config: m.Config,
		}
		// Extract size from config if present.
		if size, ok := m.Config["size"].(string); ok {
			spec.Size = interfaces.Size(size)
		}
		// Extract depends_on from config if present.
		if raw, ok := m.Config["depends_on"]; ok {
			switch v := raw.(type) {
			case []any:
				for _, d := range v {
					if s, ok := d.(string); ok {
						spec.DependsOn = append(spec.DependsOn, s)
					}
				}
			case []string:
				spec.DependsOn = v
			}
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// loadCurrentState attempts to load ResourceStates from the iac.state backend
// configured in cfgFile. Returns an empty slice on any error (first run).
func loadCurrentState(cfgFile string) []interfaces.ResourceState {
	iacStates, _, _, err := discoverInfraModules(cfgFile)
	if err != nil || len(iacStates) == 0 {
		return nil
	}
	m := iacStates[0]
	backend, _ := m.Config["backend"].(string)
	dir, _ := m.Config["directory"].(string)

	switch backend {
	case "filesystem":
		if dir == "" {
			dir = "/var/lib/workflow/iac-state"
		}
		return loadFSState(dir)
	default:
		// memory, spaces, gcs, azure, postgres — not accessible without credentials
		return nil
	}
}

// loadFSState reads IaC state records from a filesystem directory and converts
// them to interfaces.ResourceState values for use with the differ.
func loadFSState(dir string) []interfaces.ResourceState {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var states []interfaces.ResourceState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".lock.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s struct {
			ResourceID   string         `json:"resource_id"`
			ResourceType string         `json:"resource_type"`
			Provider     string         `json:"provider"`
			Config       map[string]any `json:"config"`
			Outputs      map[string]any `json:"outputs"`
		}
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, interfaces.ResourceState{
			ID:            s.ResourceID,
			Name:          s.ResourceID,
			Type:          s.ResourceType,
			Provider:      s.Provider,
			ProviderID:    s.ResourceID,
			ConfigHash:    configHashMap(s.Config),
			AppliedConfig: s.Config,
			Outputs:       s.Outputs,
		})
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

// formatFirewallRules produces a compact summary of firewall rule config (legacy, single-line).
func formatFirewallRules(v any) string {
	switch rules := v.(type) {
	case []any:
		if len(rules) == 0 {
			return ""
		}
		// Summarise first rule.
		first, ok := rules[0].(map[string]any)
		if !ok {
			return fmt.Sprintf("%d rule(s)", len(rules))
		}
		proto, _ := first["protocol"].(string)
		ports, _ := first["ports"].(string)
		src, _ := first["source"].(string)
		dst, _ := first["destination"].(string)
		var parts []string
		if proto != "" {
			parts = append(parts, strings.ToUpper(proto))
		}
		if ports != "" {
			parts = append(parts, ports)
		}
		if src != "" {
			parts = append(parts, "from "+src)
		}
		if dst != "" {
			parts = append(parts, "to "+dst)
		}
		summary := strings.Join(parts, " ")
		if len(rules) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(rules)-1)
		}
		return summary
	case string:
		return rules
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

	fmt.Printf("Applying infrastructure from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "apply"})
}

func runInfraStatus(args []string) error {
	fs := flag.NewFlagSet("infra status", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	fmt.Printf("Infrastructure status from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "status"})
}

func runInfraDrift(args []string) error {
	fs := flag.NewFlagSet("infra drift", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	fmt.Printf("Detecting drift for %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "drift"})
}

func runInfraDestroy(args []string) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	var configFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	var autoApproveVal bool
	fs.BoolVar(&autoApproveVal, "auto-approve", false, "Skip confirmation")
	fs.BoolVar(&autoApproveVal, "y", false, "Skip confirmation (short for --auto-approve)")
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
	return runPipelineRun([]string{"-c", cfgFile, "-p", "destroy"})
}
