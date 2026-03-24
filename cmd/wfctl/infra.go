package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
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
func resolveInfraConfig(fs *flag.FlagSet) (string, error) {
	configFile := fs.Lookup("config").Value.String()
	if configFile != "" {
		return configFile, nil
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
	_ = fs.String("config", "", "Config file")
	format := fs.String("format", "table", "Output format: table or markdown")
	output := fs.String("output", "", "Write plan to JSON file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
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
		fmt.Print(formatPlanMarkdown(plan))
	default:
		fmt.Printf("Infrastructure Plan — %s\n\n", cfgFile)
		fmt.Print(formatPlanTable(plan))
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

// formatPlanTable renders an interfaces.IaCPlan as a human-readable table.
func formatPlanTable(plan interfaces.IaCPlan) string {
	if len(plan.Actions) == 0 {
		return "No changes. Infrastructure is up-to-date.\n"
	}

	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Action\tResource\tType")
	fmt.Fprintln(tw, "------\t--------\t----")
	for i := range plan.Actions {
		a := &plan.Actions[i]
		symbol := actionSymbol(a.Action)
		fmt.Fprintf(tw, "%s %s\t%s\t%s\n", symbol, a.Action, a.Resource.Name, a.Resource.Type)
	}
	tw.Flush()

	creates, updates, deletes := countActions(plan)
	fmt.Fprintf(&sb, "\nPlan: %d to create, %d to update, %d to destroy.\n",
		creates, updates, deletes)
	return sb.String()
}

// formatPlanMarkdown renders an interfaces.IaCPlan as a GitHub-flavored markdown
// table suitable for PR comments.
func formatPlanMarkdown(plan interfaces.IaCPlan) string {
	if len(plan.Actions) == 0 {
		return "## Infrastructure Plan\n\nNo changes. Infrastructure is up-to-date.\n"
	}

	var sb strings.Builder
	sb.WriteString("## Infrastructure Plan\n\n")
	sb.WriteString("| Action | Resource | Type |\n")
	sb.WriteString("|--------|----------|------|\n")
	for i := range plan.Actions {
		a := &plan.Actions[i]
		symbol := actionSymbol(a.Action)
		fmt.Fprintf(&sb, "| %s %s | `%s` | `%s` |\n",
			symbol, a.Action, a.Resource.Name, a.Resource.Type)
	}

	creates, updates, deletes := countActions(plan)
	fmt.Fprintf(&sb, "\n**Plan: %d to create, %d to update, %d to destroy.**\n",
		creates, updates, deletes)
	return sb.String()
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
	provider := fs.String("provider", "", "Provider name (aws, gcp, azure, digitalocean)")
	resType := fs.String("type", "", "Abstract resource type (e.g. infra.database)")
	cloudID := fs.String("id", "", "Cloud-provider resource ID")
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
	configFlag := fs.String("config", "", "Config file")
	autoApprove := fs.Bool("auto-approve", false, "Skip confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := *configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs)
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
	_ = fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
	if err != nil {
		return err
	}

	fmt.Printf("Infrastructure status from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "status"})
}

func runInfraDrift(args []string) error {
	fs := flag.NewFlagSet("infra drift", flag.ContinueOnError)
	_ = fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
	if err != nil {
		return err
	}

	fmt.Printf("Detecting drift for %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "drift"})
}

func runInfraDestroy(args []string) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	configFlag := fs.String("config", "", "Config file")
	autoApprove := fs.Bool("auto-approve", false, "Skip confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := *configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs)
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
