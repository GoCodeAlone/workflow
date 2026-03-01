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
	tmpl     *module.TemplateEngine
}

// NewRemoteStep creates a remote step proxy.
// config holds the raw (possibly template-containing) step configuration that
// will be resolved against the live pipeline context on each Execute call.
func NewRemoteStep(name, handleID string, client pb.PluginServiceClient, config map[string]any) *RemoteStep {
	return &RemoteStep{
		name:     name,
		handleID: handleID,
		config:   config,
		client:   client,
		tmpl:     module.NewTemplateEngine(),
	}
}

func (s *RemoteStep) Name() string {
	return s.name
}

func (s *RemoteStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	// Resolve template expressions in the step config against the current
	// pipeline context so that dynamic values (e.g. outputs of earlier steps)
	// are available to the plugin.
	resolvedConfig, err := s.tmpl.ResolveMap(s.config, pc)
	if err != nil {
		return nil, fmt.Errorf("remote step config resolve: %w", err)
	}

	// Convert step outputs to proto map
	stepOutputs := make(map[string]*structpb.Struct)
	for k, v := range pc.StepOutputs {
		stepOutputs[k] = mapToStruct(v)
	}

	resp, err := s.client.ExecuteStep(ctx, &pb.ExecuteStepRequest{
		HandleId:    s.handleID,
		TriggerData: mapToStruct(pc.TriggerData),
		StepOutputs: stepOutputs,
		Current:     mapToStruct(pc.Current),
		Metadata:    mapToStruct(pc.Metadata),
		Config:      mapToStruct(resolvedConfig),
	})
	if err != nil {
		return nil, fmt.Errorf("remote step execute: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote step execute: %s", resp.Error)
	}

	return &module.StepResult{
		Output: structToMap(resp.Output),
		Stop:   resp.StopPipeline,
	}, nil
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
