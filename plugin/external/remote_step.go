package external

import (
	"context"
	"fmt"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/module"
	"google.golang.org/protobuf/types/known/structpb"
)

// RemoteStep implements module.PipelineStep by delegating to a gRPC plugin.
type RemoteStep struct {
	name     string
	handleID string
	client   pb.PluginServiceClient
}

// NewRemoteStep creates a remote step proxy.
func NewRemoteStep(name, handleID string, client pb.PluginServiceClient) *RemoteStep {
	return &RemoteStep{
		name:     name,
		handleID: handleID,
		client:   client,
	}
}

func (s *RemoteStep) Name() string {
	return s.name
}

func (s *RemoteStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
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
