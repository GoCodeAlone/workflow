// Package main implements an example external plugin that provides a single
// step type "step.uppercase" which converts input strings to uppercase.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// uppercasePlugin implements both sdk.PluginProvider and sdk.StepProvider.
type uppercasePlugin struct{}

func (p *uppercasePlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "uppercase-plugin",
		Version:     "0.1.0",
		Author:      "workflow-examples",
		Description: "Example plugin that provides a step to uppercase strings",
	}
}

func (p *uppercasePlugin) StepTypes() []string {
	return []string{"step.uppercase"}
}

func (p *uppercasePlugin) CreateStep(typeName, name string, _ map[string]any) (sdk.StepInstance, error) {
	if typeName != "step.uppercase" {
		return nil, fmt.Errorf("unknown step type: %s", typeName)
	}
	return &uppercaseStep{name: name}, nil
}

// uppercaseStep reads current["input"] and returns {"output": strings.ToUpper(input)}.
type uppercaseStep struct {
	name string
}

func (s *uppercaseStep) Execute(_ context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any) (*sdk.StepResult, error) {
	input, _ := current["input"].(string)
	return &sdk.StepResult{
		Output: map[string]any{
			"output": strings.ToUpper(input),
		},
	}, nil
}

func main() {
	sdk.Serve(&uppercasePlugin{})
}
