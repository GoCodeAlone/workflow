package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

type infraTestFile struct {
	Config       string                   `yaml:"config"`
	Env          string                   `yaml:"env"`
	CurrentState []infraTestResourceState `yaml:"current_state"`
	Expect       infraTestExpect          `yaml:"expect"`
}

type infraTestResourceState struct {
	Name                string         `yaml:"name"`
	Type                string         `yaml:"type"`
	Provider            string         `yaml:"provider"`
	ProviderRef         string         `yaml:"provider_ref"`
	ProviderID          string         `yaml:"provider_id"`
	ConfigHash          string         `yaml:"config_hash"`
	AppliedConfig       map[string]any `yaml:"applied_config"`
	AppliedConfigSource string         `yaml:"applied_config_source"`
	Outputs             map[string]any `yaml:"outputs"`
	Dependencies        []string       `yaml:"dependencies"`
}

type infraTestExpect struct {
	ResourcesCount *int                     `yaml:"resources_count"`
	Resources      []infraResourceExpect    `yaml:"resources"`
	ProviderInputs infraProviderInputExpect `yaml:"provider_inputs"`
	Plan           infraPlanExpect          `yaml:"plan"`
}

type infraProviderInputExpect struct {
	Resources []infraResourceExpect `yaml:"resources"`
}

type infraResourceExpect struct {
	Name      string         `yaml:"name"`
	Type      string         `yaml:"type"`
	Config    map[string]any `yaml:"config"`
	DependsOn []string       `yaml:"depends_on"`
}

type infraPlanExpect struct {
	ActionCounts map[string]int          `yaml:"action_counts"`
	Actions      []infraPlanActionExpect `yaml:"actions"`
}

type infraPlanActionExpect struct {
	Action   string              `yaml:"action"`
	Resource infraResourceExpect `yaml:"resource"`
}

type infraTestResult struct {
	Resources int
	Actions   int
}

func runInfraTest(args []string) error {
	fs := flag.NewFlagSet("infra test", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl infra test <test.yaml> [test.yaml ...]

Validate expected infrastructure config and plan outcomes without contacting
live providers. Each test file names a workflow config and expected resources,
resolved provider inputs, and/or plan actions.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("at least one infra test file is required")
	}
	failures := 0
	for _, path := range fs.Args() {
		result, err := runInfraTestFile(path)
		if err != nil {
			failures++
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", path, err)
			continue
		}
		fmt.Printf("PASS %s (%d resources, %d plan actions)\n", path, result.Resources, result.Actions)
	}
	if failures > 0 {
		return fmt.Errorf("%d infra test(s) failed", failures)
	}
	return nil
}

func runInfraTestFile(path string) (infraTestResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return infraTestResult{}, fmt.Errorf("read test file: %w", err)
	}
	var tf infraTestFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return infraTestResult{}, fmt.Errorf("parse test file: %w", err)
	}
	if tf.Config == "" {
		return infraTestResult{}, errors.New("config is required")
	}
	cfgPath := tf.Config
	if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(filepath.Dir(path), cfgPath)
	}

	currentState := infraTestStates(tf.CurrentState)

	rendered, err := parseInfraResourceSpecsForEnv(cfgPath, tf.Env)
	if err != nil {
		return infraTestResult{}, fmt.Errorf("render resources: %w", err)
	}
	if err := validateUniqueInfraResourceNames(rendered); err != nil {
		return infraTestResult{}, err
	}
	if err := assertInfraResources("resources", tf.Expect.Resources, rendered); err != nil {
		return infraTestResult{}, err
	}
	if tf.Expect.ResourcesCount != nil && len(rendered) != *tf.Expect.ResourcesCount {
		return infraTestResult{}, fmt.Errorf("resources count: got %d, want %d", len(rendered), *tf.Expect.ResourcesCount)
	}

	providerInputs := rendered
	wfCfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return infraTestResult{}, fmt.Errorf("load config for plan-time resolver: %w", err)
	}
	providerInputs, _, err = resolveSpecsAgainstState(providerInputs, currentState, wfCfg, tf.Env)
	if err != nil {
		return infraTestResult{}, fmt.Errorf("resolve provider inputs: %w", err)
	}
	if err := assertInfraResources("provider_inputs.resources", tf.Expect.ProviderInputs.Resources, providerInputs); err != nil {
		return infraTestResult{}, err
	}

	plan, err := computeInfraPlan(context.Background(), nil, providerInputs, currentState)
	if err != nil {
		return infraTestResult{}, fmt.Errorf("compute hermetic plan: %w", err)
	}
	if err := assertInfraPlan(tf.Expect.Plan, plan); err != nil {
		return infraTestResult{}, err
	}
	return infraTestResult{Resources: len(rendered), Actions: len(plan.Actions)}, nil
}

func infraTestStates(in []infraTestResourceState) []interfaces.ResourceState {
	out := make([]interfaces.ResourceState, 0, len(in))
	for i := range in {
		s := &in[i]
		out = append(out, interfaces.ResourceState{
			Name:                s.Name,
			Type:                s.Type,
			Provider:            s.Provider,
			ProviderRef:         s.ProviderRef,
			ProviderID:          s.ProviderID,
			ConfigHash:          s.ConfigHash,
			AppliedConfig:       s.AppliedConfig,
			AppliedConfigSource: s.AppliedConfigSource,
			Outputs:             s.Outputs,
			Dependencies:        s.Dependencies,
		})
	}
	return out
}

func assertInfraResources(label string, expected []infraResourceExpect, actual []interfaces.ResourceSpec) error {
	for _, exp := range expected {
		var match *interfaces.ResourceSpec
		for i := range actual {
			if actual[i].Name == exp.Name {
				match = &actual[i]
				break
			}
		}
		if match == nil {
			return fmt.Errorf("%s: resource %q not found", label, exp.Name)
		}
		if exp.Type != "" && match.Type != exp.Type {
			return fmt.Errorf("%s[%s].type: got %q, want %q", label, exp.Name, match.Type, exp.Type)
		}
		if len(exp.DependsOn) > 0 && !reflect.DeepEqual(match.DependsOn, exp.DependsOn) {
			return fmt.Errorf("%s[%s].depends_on: got %v, want %v", label, exp.Name, match.DependsOn, exp.DependsOn)
		}
		if err := assertMapSubset(exp.Config, match.Config); err != nil {
			return fmt.Errorf("%s[%s].config: %w", label, exp.Name, err)
		}
	}
	return nil
}

func assertInfraPlan(expected infraPlanExpect, actual interfaces.IaCPlan) error {
	if len(expected.ActionCounts) > 0 {
		counts := map[string]int{}
		for i := range actual.Actions {
			counts[actual.Actions[i].Action]++
		}
		for action, want := range expected.ActionCounts {
			if got := counts[action]; got != want {
				return fmt.Errorf("plan action count for %s: got %d, want %d", action, got, want)
			}
		}
	}
	for _, exp := range expected.Actions {
		var match *interfaces.PlanAction
		for i := range actual.Actions {
			action := &actual.Actions[i]
			if exp.Action != "" && action.Action != exp.Action {
				continue
			}
			if exp.Resource.Name != "" && action.Resource.Name != exp.Resource.Name {
				continue
			}
			match = action
			break
		}
		if match == nil {
			return fmt.Errorf("plan action not found: action=%q resource=%q", exp.Action, exp.Resource.Name)
		}
		if exp.Resource.Type != "" && match.Resource.Type != exp.Resource.Type {
			return fmt.Errorf("plan action %s resource %s type: got %q, want %q", exp.Action, exp.Resource.Name, match.Resource.Type, exp.Resource.Type)
		}
		if err := assertMapSubset(exp.Resource.Config, match.Resource.Config); err != nil {
			return fmt.Errorf("plan action %s resource %s config: %w", exp.Action, exp.Resource.Name, err)
		}
	}
	return nil
}

func assertMapSubset(expected map[string]any, actual map[string]any) error {
	for key, want := range expected {
		got, ok := actual[key]
		if !ok {
			return fmt.Errorf("%s missing", key)
		}
		if wantMap, ok := want.(map[string]any); ok {
			gotMap, ok := got.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: got %#v, want map", key, got)
			}
			if err := assertMapSubset(wantMap, gotMap); err != nil {
				return fmt.Errorf("%s.%w", key, err)
			}
			continue
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("%s: got %#v, want %#v", key, got, want)
		}
	}
	return nil
}
