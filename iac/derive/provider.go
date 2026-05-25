package derive

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/iac/requirements"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

type ExternalProviderMapper struct {
	Client pb.IaCProviderRequirementMapperClient
}

func (m ExternalProviderMapper) MapRequirements(ctx context.Context, req MapRequest) (MapResult, error) {
	if m.Client == nil {
		return MapResult{}, fmt.Errorf("iac provider requirement mapper client is nil")
	}
	protoReqs := make([]*pb.IaCRequirement, 0, len(req.Requirements))
	for i := range req.Requirements {
		protoReq, err := req.Requirements[i].ToProto()
		if err != nil {
			return MapResult{}, err
		}
		protoReqs = append(protoReqs, protoReq)
	}
	resp, err := m.Client.MapRequirements(ctx, &pb.MapRequirementsRequest{
		Provider:     req.Provider,
		Runtime:      runtimeToProto(req.Runtime),
		Environment:  req.Environment,
		Requirements: protoReqs,
	})
	if err != nil {
		return MapResult{}, err
	}
	return mapResultFromProto(resp)
}

func mapResultFromProto(resp *pb.MapRequirementsResponse) (MapResult, error) {
	if resp == nil {
		return MapResult{}, fmt.Errorf("map requirements response is nil")
	}
	out := MapResult{
		AcceptedKeys: append([]string(nil), resp.GetAcceptedKeys()...),
		Rejected:     make([]Diagnostic, 0, len(resp.GetRejected())),
		Notes:        make([]Note, 0, len(resp.GetNotes())),
		Modules:      make([]GeneratedModule, 0, len(resp.GetModules())),
	}
	for _, diag := range resp.GetRejected() {
		out.Rejected = append(out.Rejected, Diagnostic{
			Key:     diag.GetKey(),
			Code:    diag.GetCode(),
			Message: diag.GetMessage(),
		})
	}
	for _, note := range resp.GetNotes() {
		out.Notes = append(out.Notes, Note{
			Key:         note.GetKey(),
			Message:     note.GetMessage(),
			Interactive: note.GetInteractive(),
		})
	}
	for _, mod := range resp.GetModules() {
		cfg := make(map[string]any)
		if len(mod.GetConfigJson()) > 0 {
			if err := json.Unmarshal(mod.GetConfigJson(), &cfg); err != nil {
				return MapResult{}, fmt.Errorf("derived module %q config_json: %w", mod.GetName(), err)
			}
		}
		out.Modules = append(out.Modules, GeneratedModule{
			Name:      mod.GetName(),
			Type:      mod.GetType(),
			Satisfies: append([]string(nil), mod.GetSatisfies()...),
			Config:    cfg,
			DependsOn: append([]string(nil), mod.GetDependsOn()...),
		})
	}
	return out, nil
}

func runtimeToProto(runtime requirements.Runtime) pb.RequirementRuntime {
	switch runtime {
	case requirements.RuntimeKubernetes:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_KUBERNETES
	case requirements.RuntimeECS:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_ECS
	case requirements.RuntimeCloudRun:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_CLOUD_RUN
	case requirements.RuntimeAzureContainerApps:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_AZURE_CONTAINER_APPS
	case requirements.RuntimeDigitalOceanAppPlatform:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_DIGITALOCEAN_APP_PLATFORM
	default:
		return pb.RequirementRuntime_REQUIREMENT_RUNTIME_UNSPECIFIED
	}
}
