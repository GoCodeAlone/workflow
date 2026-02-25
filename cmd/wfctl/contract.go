package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// Contract is a snapshot of what a workflow application config exposes.
type Contract struct {
	Version     string            `json:"version"`
	ConfigHash  string            `json:"configHash"`
	GeneratedAt string            `json:"generatedAt"`
	Endpoints   []EndpointContract `json:"endpoints"`
	Modules     []ModuleContract   `json:"modules"`
	Steps       []string           `json:"steps"`
	Events      []EventContract    `json:"events"`
}

// EndpointContract describes an HTTP endpoint in the contract.
type EndpointContract struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	AuthRequired bool   `json:"authRequired"`
	Pipeline     string `json:"pipeline"`
}

// ModuleContract describes a module in the contract.
type ModuleContract struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Stateful bool   `json:"stateful"`
}

// EventContract describes an event topic in the contract.
type EventContract struct {
	Topic     string `json:"topic"`
	Direction string `json:"direction"` // publish or subscribe
	Pipeline  string `json:"pipeline"`
}

// contractComparison holds the result of comparing two contracts.
type contractComparison struct {
	BaseVersion    string
	CurrentVersion string
	Endpoints      []endpointChange
	Modules        []moduleChange
	Events         []eventChange
	BreakingCount  int
}

type changeType string

const (
	changeAdded   changeType = "ADDED"
	changeRemoved changeType = "REMOVED"
	changeChanged changeType = "CHANGED"
	changeUnchanged changeType = "UNCHANGED"
)

type endpointChange struct {
	Method       string
	Path         string
	Pipeline     string
	Change       changeType
	Detail       string
	IsBreaking   bool
}

type moduleChange struct {
	Name    string
	Type    string
	Change  changeType
}

type eventChange struct {
	Topic     string
	Direction string
	Pipeline  string
	Change    changeType
}

// runContract dispatches contract subcommands.
func runContract(args []string) error {
	if len(args) < 1 {
		return contractUsage()
	}
	switch args[0] {
	case "test":
		return runContractTest(args[1:])
	default:
		return contractUsage()
	}
}

func contractUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl contract <subcommand> [options]

Subcommands:
  test   Generate a contract from a config and optionally compare to a baseline

Run 'wfctl contract test -h' for details.
`)
	return fmt.Errorf("contract subcommand is required")
}

// runContractTest generates a contract and optionally compares it to a baseline.
func runContractTest(args []string) error {
	fs2 := flag.NewFlagSet("contract test", flag.ContinueOnError)
	baseline := fs2.String("baseline", "", "Previous version's contract file for comparison")
	output := fs2.String("output", "", "Write contract file to this path")
	format := fs2.String("format", "text", "Output format: text or json")
	fs2.Usage = func() {
		fmt.Fprintf(fs2.Output(), `Usage: wfctl contract test [options] <config.yaml>

Generate a contract snapshot from a workflow config file.
Optionally compare against a baseline contract to detect breaking changes.

Options:
`)
		fs2.PrintDefaults()
	}
	if err := fs2.Parse(args); err != nil {
		return err
	}
	if fs2.NArg() < 1 {
		fs2.Usage()
		return fmt.Errorf("config.yaml path is required")
	}

	configPath := fs2.Arg(0)
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	contract := generateContract(cfg)

	// Write contract to output file if requested
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(contract); err != nil {
			return fmt.Errorf("failed to write contract: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Contract written to %s\n", *output)
	}

	// Compare to baseline if provided
	if *baseline != "" {
		baseData, err := os.ReadFile(*baseline)
		if err != nil {
			return fmt.Errorf("failed to read baseline: %w", err)
		}
		var baseContract Contract
		if err := json.Unmarshal(baseData, &baseContract); err != nil {
			return fmt.Errorf("failed to parse baseline contract: %w", err)
		}

		comparison := compareContracts(&baseContract, contract)

		switch strings.ToLower(*format) {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(comparison)
		default:
			printContractComparison(comparison)
		}

		if comparison.BreakingCount > 0 {
			return fmt.Errorf("%d breaking change(s) detected", comparison.BreakingCount)
		}
		return nil
	}

	// Print contract summary
	switch strings.ToLower(*format) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(contract)
	default:
		printContract(contract)
	}

	return nil
}

// generateContract builds a Contract from a WorkflowConfig.
func generateContract(cfg *config.WorkflowConfig) *Contract {
	knownModules := KnownModuleTypes()

	// Hash the config for version tracking
	cfgData, _ := json.Marshal(cfg)
	hash := fmt.Sprintf("%x", sha256.Sum256(cfgData))[:16]

	contract := &Contract{
		Version:     "1.0",
		ConfigHash:  hash,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Extract modules
	moduleSet := make(map[string]bool)
	for _, mod := range cfg.Modules {
		moduleSet[mod.Name] = true
		info, isKnown := knownModules[mod.Type]
		mc := ModuleContract{
			Name: mod.Name,
			Type: mod.Type,
		}
		if isKnown {
			mc.Stateful = info.Stateful
		}
		contract.Modules = append(contract.Modules, mc)
	}

	// Sort modules for determinism
	sort.Slice(contract.Modules, func(i, j int) bool {
		return contract.Modules[i].Name < contract.Modules[j].Name
	})

	// Extract pipeline endpoints, steps, and events
	stepSet := make(map[string]bool)
	for pipelineName, pipelineRaw := range cfg.Pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}

		// Extract trigger info for endpoints
		if triggerRaw, ok := pipelineMap["trigger"]; ok {
			if triggerMap, ok := triggerRaw.(map[string]any); ok {
				triggerType, _ := triggerMap["type"].(string)
				if triggerType == "http" {
					triggerCfg, _ := triggerMap["config"].(map[string]any)
					if triggerCfg != nil {
						path, _ := triggerCfg["path"].(string)
						method, _ := triggerCfg["method"].(string)
						if path != "" && method != "" {
							ep := EndpointContract{
								Method:   strings.ToUpper(method),
								Path:     path,
								Pipeline: pipelineName,
							}
							// Check for auth in steps
							if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]any); ok {
										stepType, _ := stepMap["type"].(string)
										if stepType == "step.auth_required" {
											ep.AuthRequired = true
										}
									}
								}
							}
							contract.Endpoints = append(contract.Endpoints, ep)
						}
					}
				}
			}
		}

		// Extract steps
		if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
			for _, stepRaw := range stepsRaw {
				if stepMap, ok := stepRaw.(map[string]any); ok {
					stepType, _ := stepMap["type"].(string)
					if stepType != "" {
						stepSet[stepType] = true
					}
					// Extract event publishes
					if stepType == "step.publish" {
						if stepCfg, ok := stepMap["config"].(map[string]any); ok {
							if topic, ok := stepCfg["topic"].(string); ok && topic != "" {
								contract.Events = append(contract.Events, EventContract{
									Topic:     topic,
									Direction: "publish",
									Pipeline:  pipelineName,
								})
							}
						}
					}
				}
			}
		}

		// Extract event subscriptions from trigger
		if triggerRaw, ok := pipelineMap["trigger"]; ok {
			if triggerMap, ok := triggerRaw.(map[string]any); ok {
				triggerType, _ := triggerMap["type"].(string)
				if triggerType == "event" {
					if triggerCfg, ok := triggerMap["config"].(map[string]any); ok {
						if topic, ok := triggerCfg["topic"].(string); ok && topic != "" {
							contract.Events = append(contract.Events, EventContract{
								Topic:     topic,
								Direction: "subscribe",
								Pipeline:  pipelineName,
							})
						}
					}
				}
			}
		}
	}

	// Sort endpoints and events for determinism
	sort.Slice(contract.Endpoints, func(i, j int) bool {
		if contract.Endpoints[i].Path != contract.Endpoints[j].Path {
			return contract.Endpoints[i].Path < contract.Endpoints[j].Path
		}
		return contract.Endpoints[i].Method < contract.Endpoints[j].Method
	})

	// Collect steps as sorted slice
	for st := range stepSet {
		contract.Steps = append(contract.Steps, st)
	}
	sort.Strings(contract.Steps)

	// Sort events
	sort.Slice(contract.Events, func(i, j int) bool {
		if contract.Events[i].Topic != contract.Events[j].Topic {
			return contract.Events[i].Topic < contract.Events[j].Topic
		}
		return contract.Events[i].Direction < contract.Events[j].Direction
	})

	return contract
}

// compareContracts compares a baseline contract to the current one.
func compareContracts(base, current *Contract) *contractComparison {
	comp := &contractComparison{
		BaseVersion:    base.Version,
		CurrentVersion: current.Version,
	}

	// Compare endpoints
	baseEPs := make(map[string]EndpointContract)
	for _, ep := range base.Endpoints {
		key := ep.Method + " " + ep.Path
		baseEPs[key] = ep
	}
	currentEPs := make(map[string]EndpointContract)
	for _, ep := range current.Endpoints {
		key := ep.Method + " " + ep.Path
		currentEPs[key] = ep
	}

	// Check base endpoints
	for key, baseEP := range baseEPs {
		if currentEP, exists := currentEPs[key]; exists {
			// Check for breaking changes
			if baseEP.AuthRequired != currentEP.AuthRequired && !baseEP.AuthRequired {
				// Auth was added to a public endpoint
				comp.Endpoints = append(comp.Endpoints, endpointChange{
					Method:     baseEP.Method,
					Path:       baseEP.Path,
					Pipeline:   currentEP.Pipeline,
					Change:     changeChanged,
					Detail:     "auth requirement added (clients without tokens will get 401)",
					IsBreaking: true,
				})
				comp.BreakingCount++
			} else {
				comp.Endpoints = append(comp.Endpoints, endpointChange{
					Method:   baseEP.Method,
					Path:     baseEP.Path,
					Pipeline: currentEP.Pipeline,
					Change:   changeUnchanged,
				})
			}
		} else {
			// Endpoint was removed - BREAKING
			comp.Endpoints = append(comp.Endpoints, endpointChange{
				Method:     baseEP.Method,
				Path:       baseEP.Path,
				Pipeline:   baseEP.Pipeline,
				Change:     changeRemoved,
				Detail:     "endpoint removed (clients calling this will get 404)",
				IsBreaking: true,
			})
			comp.BreakingCount++
		}
		delete(currentEPs, key)
	}

	// Check added endpoints (non-breaking)
	for _, currentEP := range currentEPs {
		comp.Endpoints = append(comp.Endpoints, endpointChange{
			Method:     currentEP.Method,
			Path:       currentEP.Path,
			Pipeline:   currentEP.Pipeline,
			Change:     changeAdded,
			IsBreaking: false,
		})
	}

	// Sort endpoint changes for stable output
	sort.Slice(comp.Endpoints, func(i, j int) bool {
		if comp.Endpoints[i].Path != comp.Endpoints[j].Path {
			return comp.Endpoints[i].Path < comp.Endpoints[j].Path
		}
		return comp.Endpoints[i].Method < comp.Endpoints[j].Method
	})

	// Compare modules
	baseModules := make(map[string]ModuleContract)
	for _, m := range base.Modules {
		baseModules[m.Name] = m
	}
	currentModules := make(map[string]ModuleContract)
	for _, m := range current.Modules {
		currentModules[m.Name] = m
	}

	for name, baseMod := range baseModules {
		if _, exists := currentModules[name]; exists {
			comp.Modules = append(comp.Modules, moduleChange{
				Name:   name,
				Type:   baseMod.Type,
				Change: changeUnchanged,
			})
		} else {
			comp.Modules = append(comp.Modules, moduleChange{
				Name:   name,
				Type:   baseMod.Type,
				Change: changeRemoved,
			})
		}
		delete(currentModules, name)
	}
	for name, currentMod := range currentModules {
		comp.Modules = append(comp.Modules, moduleChange{
			Name:   name,
			Type:   currentMod.Type,
			Change: changeAdded,
		})
	}
	sort.Slice(comp.Modules, func(i, j int) bool {
		return comp.Modules[i].Name < comp.Modules[j].Name
	})

	// Compare events
	baseEvents := make(map[string]EventContract)
	for _, e := range base.Events {
		key := e.Direction + ":" + e.Topic
		baseEvents[key] = e
	}
	currentEvents := make(map[string]EventContract)
	for _, e := range current.Events {
		key := e.Direction + ":" + e.Topic
		currentEvents[key] = e
	}

	for key, baseEv := range baseEvents {
		if _, exists := currentEvents[key]; exists {
			comp.Events = append(comp.Events, eventChange{
				Topic:     baseEv.Topic,
				Direction: baseEv.Direction,
				Pipeline:  baseEv.Pipeline,
				Change:    changeUnchanged,
			})
		} else {
			comp.Events = append(comp.Events, eventChange{
				Topic:     baseEv.Topic,
				Direction: baseEv.Direction,
				Pipeline:  baseEv.Pipeline,
				Change:    changeRemoved,
			})
		}
		delete(currentEvents, key)
	}
	for _, currentEv := range currentEvents {
		comp.Events = append(comp.Events, eventChange{
			Topic:     currentEv.Topic,
			Direction: currentEv.Direction,
			Pipeline:  currentEv.Pipeline,
			Change:    changeAdded,
		})
	}
	sort.Slice(comp.Events, func(i, j int) bool {
		return comp.Events[i].Topic < comp.Events[j].Topic
	})

	return comp
}

// printContract prints a human-readable contract summary.
func printContract(c *Contract) {
	fmt.Printf("Contract (hash: %s, generated: %s)\n", c.ConfigHash, c.GeneratedAt)
	fmt.Printf("\nEndpoints (%d):\n", len(c.Endpoints))
	for _, ep := range c.Endpoints {
		auth := ""
		if ep.AuthRequired {
			auth = " [auth]"
		}
		fmt.Printf("  %-7s %s%s  (pipeline: %s)\n", ep.Method, ep.Path, auth, ep.Pipeline)
	}

	fmt.Printf("\nModules (%d):\n", len(c.Modules))
	for _, m := range c.Modules {
		stateful := ""
		if m.Stateful {
			stateful = " [stateful]"
		}
		fmt.Printf("  %s (%s)%s\n", m.Name, m.Type, stateful)
	}

	if len(c.Steps) > 0 {
		fmt.Printf("\nStep types used (%d):\n", len(c.Steps))
		for _, s := range c.Steps {
			fmt.Printf("  %s\n", s)
		}
	}

	if len(c.Events) > 0 {
		fmt.Printf("\nEvents (%d):\n", len(c.Events))
		for _, e := range c.Events {
			fmt.Printf("  %s %s  (pipeline: %s)\n", e.Direction, e.Topic, e.Pipeline)
		}
	}
}

// printContractComparison prints a human-readable contract comparison.
func printContractComparison(comp *contractComparison) {
	fmt.Printf("Contract Comparison\n\n")

	if len(comp.Endpoints) > 0 {
		fmt.Println("Endpoints:")
		for _, ec := range comp.Endpoints {
			var sym string
			switch ec.Change {
			case changeAdded:
				sym = "+"
			case changeRemoved:
				sym = "-"
			case changeChanged:
				sym = "~"
			default:
				sym = "="
			}
			breaking := ""
			if ec.IsBreaking {
				breaking = "  BREAKING"
			}
			if ec.Detail != "" {
				fmt.Printf("  %s %-7s %s%s\n    -> %s\n", sym, ec.Method, ec.Path, breaking, ec.Detail)
			} else {
				fmt.Printf("  %s %-7s %s [%s]%s\n", sym, ec.Method, ec.Path, ec.Change, breaking)
			}
		}
	}

	if len(comp.Modules) > 0 {
		fmt.Println("\nModules:")
		for _, mc := range comp.Modules {
			var sym string
			switch mc.Change {
			case changeAdded:
				sym = "+"
			case changeRemoved:
				sym = "-"
			default:
				sym = "="
			}
			fmt.Printf("  %s %s (%s) [%s]\n", sym, mc.Name, mc.Type, mc.Change)
		}
	}

	if len(comp.Events) > 0 {
		fmt.Println("\nEvents:")
		for _, ec := range comp.Events {
			var sym string
			switch ec.Change {
			case changeAdded:
				sym = "+"
			case changeRemoved:
				sym = "-"
			default:
				sym = "="
			}
			fmt.Printf("  %s %s %s [%s]\n", sym, ec.Direction, ec.Topic, ec.Change)
		}
	}

	if comp.BreakingCount > 0 {
		fmt.Printf("\nBreaking Changes: %d\n", comp.BreakingCount)
		n := 1
		for _, ec := range comp.Endpoints {
			if ec.IsBreaking {
				fmt.Printf("  %d. %s %s: %s\n", n, ec.Method, ec.Path, ec.Detail)
				n++
			}
		}
	} else {
		fmt.Println("\nNo breaking changes detected.")
	}
}
