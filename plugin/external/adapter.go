package external

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/schema"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v3"
)

// ExternalPluginAdapter wraps a gRPC plugin client to implement plugin.EnginePlugin.
// The engine sees this as a regular plugin — no changes to engine.go needed.
type ExternalPluginAdapter struct {
	name           string
	client         *PluginClient
	manifest       *pb.Manifest
	configFragment []byte
	pluginDir      string
}

// NewExternalPluginAdapter creates an adapter from a connected plugin client.
func NewExternalPluginAdapter(name string, client *PluginClient) (*ExternalPluginAdapter, error) {
	ctx := context.Background()
	manifest, err := client.client.GetManifest(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get manifest from plugin %s: %w", name, err)
	}
	a := &ExternalPluginAdapter{
		name:     name,
		client:   client,
		manifest: manifest,
	}
	// Fetch config fragment eagerly so it's available before BuildFromConfig runs.
	if resp, fragErr := client.client.GetConfigFragment(ctx, &emptypb.Empty{}); fragErr == nil && len(resp.YamlConfig) > 0 {
		a.configFragment = resp.YamlConfig
		a.pluginDir = resp.PluginDir
	}
	return a, nil
}

// --- NativePlugin interface ---

func (a *ExternalPluginAdapter) Name() string                            { return a.manifest.Name }
func (a *ExternalPluginAdapter) Version() string                         { return a.manifest.Version }
func (a *ExternalPluginAdapter) Description() string                     { return a.manifest.Description }
func (a *ExternalPluginAdapter) Dependencies() []plugin.PluginDependency { return nil }
func (a *ExternalPluginAdapter) UIPages() []plugin.UIPageDef             { return nil }
func (a *ExternalPluginAdapter) RegisterRoutes(_ *http.ServeMux)         {}
func (a *ExternalPluginAdapter) OnEnable(_ plugin.PluginContext) error   { return nil }
func (a *ExternalPluginAdapter) OnDisable(_ plugin.PluginContext) error  { return nil }

// --- EnginePlugin interface ---

func (a *ExternalPluginAdapter) EngineManifest() *plugin.PluginManifest {
	ctx := context.Background()

	modTypes, _ := a.client.client.GetModuleTypes(ctx, &emptypb.Empty{})
	stepTypes, _ := a.client.client.GetStepTypes(ctx, &emptypb.Empty{})
	triggerTypes, _ := a.client.client.GetTriggerTypes(ctx, &emptypb.Empty{})

	m := &plugin.PluginManifest{
		Name:        a.manifest.Name,
		Version:     a.manifest.Version,
		Author:      a.manifest.Author,
		Description: a.manifest.Description,
	}
	if modTypes != nil {
		m.ModuleTypes = modTypes.Types
	}
	if stepTypes != nil {
		m.StepTypes = stepTypes.Types
	}
	if triggerTypes != nil {
		m.TriggerTypes = triggerTypes.Types
	}
	return m
}

func (a *ExternalPluginAdapter) Capabilities() []capability.Contract {
	return nil
}

func (a *ExternalPluginAdapter) ModuleFactories() map[string]plugin.ModuleFactory {
	ctx := context.Background()
	resp, err := a.client.client.GetModuleTypes(ctx, &emptypb.Empty{})
	if err != nil || resp == nil {
		return nil
	}
	factories := make(map[string]plugin.ModuleFactory, len(resp.Types))
	for _, typeName := range resp.Types {
		tn := typeName // capture
		factories[tn] = func(name string, cfg map[string]any) modular.Module {
			createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{
				Type:   tn,
				Name:   name,
				Config: mapToStruct(cfg),
			})
			if createErr != nil || createResp.Error != "" {
				return nil
			}
			return NewRemoteModule(name, createResp.HandleId, a.client.client)
		}
	}
	return factories
}

func (a *ExternalPluginAdapter) StepFactories() map[string]plugin.StepFactory {
	ctx := context.Background()
	resp, err := a.client.client.GetStepTypes(ctx, &emptypb.Empty{})
	if err != nil || resp == nil {
		return nil
	}
	factories := make(map[string]plugin.StepFactory, len(resp.Types))
	for _, typeName := range resp.Types {
		tn := typeName // capture
		factories[tn] = func(name string, cfg map[string]any, _ modular.Application) (any, error) {
			createResp, createErr := a.client.client.CreateStep(ctx, &pb.CreateStepRequest{
				Type:   tn,
				Name:   name,
				Config: mapToStruct(cfg),
			})
			if createErr != nil {
				return nil, fmt.Errorf("create remote step %s: %w", tn, createErr)
			}
			if createResp.Error != "" {
				return nil, fmt.Errorf("create remote step %s: %s", tn, createResp.Error)
			}
			return NewRemoteStep(name, createResp.HandleId, a.client.client), nil
		}
	}
	return factories
}

func (a *ExternalPluginAdapter) TriggerFactories() map[string]plugin.TriggerFactory {
	ctx := context.Background()
	resp, err := a.client.client.GetTriggerTypes(ctx, &emptypb.Empty{})
	if err != nil || resp == nil || len(resp.Types) == 0 {
		return nil
	}
	factories := make(map[string]plugin.TriggerFactory, len(resp.Types))
	for _, typeName := range resp.Types {
		tn := typeName // capture
		factories[tn] = func() any {
			createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{
				Type:   tn,
				Name:   tn,
				Config: nil,
			})
			if createErr != nil || createResp.Error != "" {
				return nil
			}
			return NewRemoteTrigger(tn, tn, createResp.HandleId, a.client.client, nil)
		}
	}
	return factories
}

func (a *ExternalPluginAdapter) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return nil
}

func (a *ExternalPluginAdapter) ModuleSchemas() []*schema.ModuleSchema {
	ctx := context.Background()
	resp, err := a.client.client.GetModuleSchemas(ctx, &emptypb.Empty{})
	if err != nil || resp == nil {
		return nil
	}
	schemas := make([]*schema.ModuleSchema, 0, len(resp.Schemas))
	for _, ps := range resp.Schemas {
		schemas = append(schemas, protoSchemaToSchema(ps))
	}
	return schemas
}

func (a *ExternalPluginAdapter) WiringHooks() []plugin.WiringHook {
	return nil
}

func (a *ExternalPluginAdapter) ConfigTransformHooks() []plugin.ConfigTransformHook {
	if len(a.configFragment) == 0 {
		return nil
	}
	return []plugin.ConfigTransformHook{
		{
			Name:     a.manifest.Name + "-config-merge",
			Priority: 100,
			Hook: func(cfg *config.WorkflowConfig) error {
				var fragment config.WorkflowConfig
				if err := yaml.Unmarshal(a.configFragment, &fragment); err != nil {
					return fmt.Errorf("failed to parse config fragment from plugin %s: %w", a.manifest.Name, err)
				}
				// Resolve relative paths against plugin directory.
				if a.pluginDir != "" {
					for i := range fragment.Modules {
						if mc, ok := fragment.Modules[i].Config["root"].(string); ok && !filepath.IsAbs(mc) {
							fragment.Modules[i].Config["root"] = filepath.Join(a.pluginDir, mc)
						}
					}
				}
				config.MergeConfigs(cfg, &fragment)
				return nil
			},
		},
	}
}

// Ensure ExternalPluginAdapter satisfies plugin.EnginePlugin at compile time.
var _ plugin.EnginePlugin = (*ExternalPluginAdapter)(nil)
