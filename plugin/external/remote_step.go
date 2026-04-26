package external

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// RemoteStep implements module.PipelineStep by delegating to a gRPC plugin.
type RemoteStep struct {
	name     string
	handleID string
	config   map[string]any
	client   pb.PluginServiceClient
	contract *pb.ContractDescriptor
	tmpl     *module.TemplateEngine
}

// NewRemoteStep creates a remote step proxy.
// config holds the raw (possibly template-containing) step configuration that
// will be resolved against the live pipeline context on each Execute call.
func NewRemoteStep(name, handleID string, client pb.PluginServiceClient, config map[string]any, contracts ...*pb.ContractDescriptor) *RemoteStep {
	var contract *pb.ContractDescriptor
	if len(contracts) > 0 {
		contract = contracts[0]
	}
	return &RemoteStep{
		name:     name,
		handleID: handleID,
		config:   config,
		client:   client,
		contract: contract,
		tmpl:     module.NewTemplateEngine(),
	}
}

func (s *RemoteStep) Name() string {
	return s.name
}

func (s *RemoteStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	// Resolve template expressions in the step config against the current
	// pipeline context so that dynamic values (e.g. outputs of earlier steps)
	// are available to the plugin. When no config was provided, skip resolution
	// and leave resolvedConfig nil so the Config proto field is omitted.
	var resolvedConfig map[string]any
	if s.config != nil {
		var err error
		resolvedConfig, err = s.tmpl.ResolveMap(s.config, pc)
		if err != nil {
			return nil, fmt.Errorf("remote step %q (handle %s) config resolve: %w", s.name, s.handleID, err)
		}
	}

	// Convert step outputs to proto map
	stepOutputs := make(map[string]*structpb.Struct)
	for k, v := range pc.StepOutputs {
		stepOutputs[k] = mapToStruct(v)
	}

	req, err := s.executeRequest(pc, resolvedConfig, stepOutputs)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.ExecuteStep(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote step execute: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote step execute: %s", resp.Error)
	}

	output := structToMap(resp.Output)
	if resp.TypedOutput != nil && s.contract != nil && s.contract.OutputMessage != "" {
		output, err = typedAnyToMap(resp.TypedOutput, s.contract.OutputMessage)
		if err != nil {
			return nil, fmt.Errorf("remote step %q typed output decode: %w", s.name, err)
		}
	}

	return &module.StepResult{
		Output: output,
		Stop:   resp.StopPipeline,
	}, nil
}

func (s *RemoteStep) executeRequest(pc *module.PipelineContext, resolvedConfig map[string]any, stepOutputs map[string]*structpb.Struct) (*pb.ExecuteStepRequest, error) {
	req := &pb.ExecuteStepRequest{
		HandleId:    s.handleID,
		TriggerData: mapToStruct(pc.TriggerData),
		StepOutputs: stepOutputs,
		Current:     mapToStruct(pc.Current),
		Metadata:    mapToStruct(pc.Metadata),
		Config:      mapToStruct(resolvedConfig),
	}
	if s.contract == nil || s.contract.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED {
		return req, nil
	}
	if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
		return req, nil
	}
	typedConfig, err := mapToTypedAny(s.contract.ConfigMessage, resolvedConfig)
	if err != nil {
		if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			return nil, fmt.Errorf("remote step %q STRICT_PROTO config message %q cannot use legacy Struct fallback: %w", s.name, s.contract.ConfigMessage, err)
		}
		return req, nil
	}
	typedInput, err := mapToTypedAny(s.contract.InputMessage, pc.Current)
	if err != nil {
		if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			return nil, fmt.Errorf("remote step %q STRICT_PROTO input message %q cannot use legacy Struct fallback: %w", s.name, s.contract.InputMessage, err)
		}
		return req, nil
	}
	req.Config = nil
	req.Current = nil
	req.TypedConfig = typedConfig
	req.TypedInput = typedInput
	return req, nil
}

// Destroy releases the remote step resources.
func (s *RemoteStep) Destroy() error {
	resp, err := s.client.DestroyStep(context.Background(), &pb.HandleRequest{
		HandleId: s.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote step destroy: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote step destroy: %s", resp.Error)
	}
	return nil
}

// Ensure RemoteStep satisfies module.PipelineStep at compile time.
var _ module.PipelineStep = (*RemoteStep)(nil)
