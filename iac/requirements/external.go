package requirements

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

type ExternalDiscoveryProvider struct {
	Client           pb.IaCRequirementDiscoveryClient
	Context          *pb.RequirementContext
	ModuleConfigJSON []byte
}

func (p ExternalDiscoveryProvider) IaCRequirements(ctx context.Context, input Input) ([]Requirement, error) {
	if p.Client == nil {
		return nil, fmt.Errorf("iac requirement discovery client is nil")
	}
	req := &pb.DiscoverRequirementsRequest{
		Context:          p.Context,
		ModuleConfigJson: append([]byte(nil), p.ModuleConfigJSON...),
	}
	if req.Context == nil {
		req.Context = RequirementContextFromConfig(input.Config, input.Environment)
	}
	resp, err := p.Client.DiscoverRequirements(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]Requirement, 0, len(resp.GetRequirements()))
	for _, protoReq := range resp.GetRequirements() {
		req, err := FromProto(protoReq)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

func RequirementContextFromConfig(cfg *config.WorkflowConfig, environment string) *pb.RequirementContext {
	ctx := &pb.RequirementContext{Environment: environment}
	if cfg == nil {
		return ctx
	}
	refs := allModules(cfg)
	ctx.Modules = make([]*pb.ModuleRef, 0, len(refs))
	for _, mod := range refs {
		ctx.Modules = append(ctx.Modules, &pb.ModuleRef{
			Name:      mod.Name,
			Type:      mod.Type,
			Satisfies: append([]string(nil), mod.Satisfies...),
		})
	}
	if cfg.Plugins != nil {
		for _, plugin := range cfg.Plugins.External {
			ctx.PluginIds = append(ctx.PluginIds, plugin.Name)
		}
	}
	return ctx
}
