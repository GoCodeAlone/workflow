package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DryRunApplyPlan is the structured dry-run output for infra apply.
type DryRunApplyPlan struct {
	Command     string                `json:"command"`
	Config      string                `json:"config"`
	Environment string                `json:"environment,omitempty"`
	Actions     []DryRunAction        `json:"actions"`
	Secrets     []DryRunSecretRef     `json:"secrets,omitempty"`
	Providers   []DryRunProviderGroup `json:"providers,omitempty"`
	Summary     DryRunSummary         `json:"summary"`
}

// DryRunAction describes a single planned infrastructure operation.
type DryRunAction struct {
	Action       string `json:"action"`
	ResourceName string `json:"resource_name"`
	ResourceType string `json:"resource_type"`
	Provider     string `json:"provider,omitempty"`
}

// DryRunSecretRef describes a secret key required by the operation (value never shown).
type DryRunSecretRef struct {
	Key      string `json:"key"`
	Store    string `json:"store,omitempty"`
	Required bool   `json:"required"`
}

// DryRunProviderGroup summarizes resources grouped by provider.
type DryRunProviderGroup struct {
	ModuleRef     string `json:"module_ref"`
	ProviderType  string `json:"provider_type"`
	ResourceCount int    `json:"resource_count"`
}

// DryRunSummary provides counts of planned operations.
type DryRunSummary struct {
	Creates  int `json:"creates"`
	Updates  int `json:"updates"`
	Replaces int `json:"replaces"`
	Deletes  int `json:"deletes"`
}

// runInfraApplyDryRun executes the same planning logic as a real apply
// (config resolution, environment overrides, provider selection) but
// prints the plan and exits without executing any provider mutations.
func runInfraApplyDryRun(cfgFile, envName, format string, showSensitive bool) error {
	if format != "table" && format != "json" {
		return fmt.Errorf("unknown --format %q: supported values are table, json", format)
	}

	desired, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("parse infra resource specs: %w", err)
	}
	if err := validateUniqueInfraResourceNames(desired); err != nil {
		return err
	}

	// Treat state-load failure as empty state so dry-run works even when the
	// remote state backend does not yet exist (e.g. a fresh config whose
	// iac.state Spaces bucket has not been bootstrapped yet). The same
	// first-run assumption is safe here because a missing backend yields an
	// all-create plan — the correct preview for that scenario.
	current, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not load current state (treating as empty for dry-run): %v\n", err)
		current = nil
	}

	// --include: apply the same scope filter as the live apply path.
	// currentApplyIncludeFlag is set by runInfraApply before this call
	// so dry-run sees the same resource subset as a live apply would.
	dryRunIncludeSet := parseIncludeFlag(currentApplyIncludeFlag)
	if err := validateIncludeSet(dryRunIncludeSet, desired, current); err != nil {
		return err
	}
	desired = filterSpecsByInclude(desired, dryRunIncludeSet)
	current = filterStatesByInclude(current, dryRunIncludeSet)

	plan, err := computePlanForInfraSpecs(context.Background(), cfgFile, envName, desired, current)
	if err != nil {
		return fmt.Errorf("compute plan: %w", err)
	}

	// Collect provider groups for the summary.
	providerGroups := collectProviderGroups(cfgFile, envName, desired)

	// Collect required secrets (keys only, never values).
	secretRefs := collectSecretRefs(cfgFile, envName)

	switch format {
	case "json":
		return printDryRunJSON(cfgFile, envName, plan, providerGroups, secretRefs)
	default:
		return printDryRunTable(cfgFile, envName, plan, providerGroups, secretRefs, showSensitive)
	}
}

func printDryRunTable(cfgFile, envName string, plan interfaces.IaCPlan, providerGroups []DryRunProviderGroup, secretRefs []DryRunSecretRef, showSensitive bool) error {
	fmt.Printf("Dry Run — infra apply\n")
	fmt.Printf("=====================\n")
	fmt.Printf("Config:      %s\n", cfgFile)
	if envName != "" {
		fmt.Printf("Environment: %s\n", envName)
	}
	fmt.Println()

	if len(providerGroups) > 0 {
		fmt.Printf("Providers:\n")
		for _, pg := range providerGroups {
			fmt.Printf("  - %s (%s): %d resource(s)\n", pg.ModuleRef, pg.ProviderType, pg.ResourceCount)
		}
		fmt.Println()
	}

	fmt.Print(formatPlanTable(plan, showSensitive))
	fmt.Println()

	if len(secretRefs) > 0 {
		fmt.Printf("Required Secrets:\n")
		for _, s := range secretRefs {
			store := ""
			if s.Store != "" {
				store = fmt.Sprintf(" (store: %s)", s.Store)
			}
			fmt.Printf("  - %s%s\n", s.Key, store)
		}
		fmt.Println()
	}

	fmt.Printf("Dry run complete. No changes were applied.\n")
	applyCmd := fmt.Sprintf("wfctl infra apply -c %s", cfgFile)
	if envName != "" {
		applyCmd += fmt.Sprintf(" --env %s", envName)
	}
	fmt.Printf("To apply, run: %s\n", applyCmd)
	return nil
}

func printDryRunJSON(cfgFile, envName string, plan interfaces.IaCPlan, providerGroups []DryRunProviderGroup, secretRefs []DryRunSecretRef) error {
	actions := make([]DryRunAction, 0, len(plan.Actions))
	for i := range plan.Actions {
		a := &plan.Actions[i]
		provRef, _ := a.Resource.Config["provider"].(string)
		actions = append(actions, DryRunAction{
			Action:       a.Action,
			ResourceName: a.Resource.Name,
			ResourceType: a.Resource.Type,
			Provider:     provRef,
		})
	}

	creates, updates, replaces, deletes := countActions(plan)

	output := DryRunApplyPlan{
		Command:     "infra apply",
		Config:      cfgFile,
		Environment: envName,
		Actions:     actions,
		Secrets:     secretRefs,
		Providers:   providerGroups,
		Summary: DryRunSummary{
			Creates:  creates,
			Updates:  updates,
			Replaces: replaces,
			Deletes:  deletes,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func collectProviderGroups(cfgFile, envName string, specs []interfaces.ResourceSpec) []DryRunProviderGroup {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil
	}

	// Build provider type lookup from iac.provider modules.
	providerTypes := map[string]string{}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				continue
			}
			modCfg = resolved.Config
		} else {
			modCfg = m.Config
		}
		pt, _ := modCfg["provider"].(string)
		providerTypes[m.Name] = pt
	}

	// Group specs by provider ref.
	type groupAcc struct {
		moduleRef string
		provType  string
		count     int
	}
	groups := map[string]*groupAcc{}
	var order []string
	for _, spec := range specs {
		if !strings.HasPrefix(spec.Type, "infra.") {
			continue
		}
		moduleRef, _ := spec.Config["provider"].(string)
		if moduleRef == "" {
			continue
		}
		if _, exists := groups[moduleRef]; !exists {
			groups[moduleRef] = &groupAcc{
				moduleRef: moduleRef,
				provType:  providerTypes[moduleRef],
			}
			order = append(order, moduleRef)
		}
		groups[moduleRef].count++
	}

	result := make([]DryRunProviderGroup, 0, len(order))
	for _, ref := range order {
		g := groups[ref]
		result = append(result, DryRunProviderGroup{
			ModuleRef:     g.moduleRef,
			ProviderType:  g.provType,
			ResourceCount: g.count,
		})
	}
	return result
}

func collectSecretRefs(cfgFile, envName string) []DryRunSecretRef {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil || cfg.Secrets == nil {
		return nil
	}

	var refs []DryRunSecretRef
	for _, entry := range cfg.Secrets.Entries {
		// Use ResolveSecretStore so env-level overrides (secretsStoreOverride)
		// and defaultStore are applied — matching the priority order the real
		// apply path uses when it calls injectSecrets.
		store := ResolveSecretStore(entry.Name, envName, cfg)
		refs = append(refs, DryRunSecretRef{
			Key:      entry.Name,
			Store:    store,
			Required: true,
		})
	}
	return refs
}
