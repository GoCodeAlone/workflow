// Package main implements an example external plugin that provides a single
// step type "step.uppercase" which converts input strings to uppercase.
package main

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// uppercasePlugin implements sdk.PluginProvider, sdk.TypedStepProvider, and
// sdk.ContractProvider.
type uppercasePlugin struct{}

func (p *uppercasePlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "uppercase-plugin",
		Version:     "0.1.0",
		Author:      "workflow-examples",
		Description: "Example plugin that provides a step to uppercase strings",
	}
}

func (p *uppercasePlugin) TypedStepTypes() []string {
	return []string{"step.uppercase"}
}

func (p *uppercasePlugin) CreateTypedStep(typeName, name string, config *anypb.Any) (sdk.StepInstance, error) {
	if typeName != "step.uppercase" {
		return nil, fmt.Errorf("unknown step type: %s", typeName)
	}
	factory := sdk.NewTypedStepFactory(
		"step.uppercase",
		wrapperspb.String(""),
		wrapperspb.String(""),
		executeUppercase,
	)
	return factory.CreateTypedStep(typeName, name, config)
}

func (p *uppercasePlugin) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
		{
			Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
			StepType:      "step.uppercase",
			ConfigMessage: "google.protobuf.StringValue",
			InputMessage:  "google.protobuf.StringValue",
			OutputMessage: "google.protobuf.StringValue",
			Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		},
	}}
}

func executeUppercase(
	_ context.Context,
	req sdk.TypedStepRequest[*wrapperspb.StringValue, *wrapperspb.StringValue],
) (*sdk.TypedStepResult[*wrapperspb.StringValue], error) {
	input := ""
	if req.Input != nil {
		input = req.Input.Value
	}
	return &sdk.TypedStepResult[*wrapperspb.StringValue]{
		Output: wrapperspb.String(strings.ToUpper(input)),
	}, nil
}

func main() {
	sdk.Serve(&uppercasePlugin{})
}
