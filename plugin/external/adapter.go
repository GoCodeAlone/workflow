package external

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

// ExternalPluginAdapter wraps a gRPC plugin client to implement plugin.EnginePlugin.
// The engine sees this as a regular plugin — no changes to engine.go needed.
type ExternalPluginAdapter struct {
	name                string
	client              *PluginClient
	manifest            *pb.Manifest
	diskManifest        *plugin.PluginManifest // fallback when gRPC GetManifest is Unimplemented or returns empty Version
	contractRegistry    *pb.ContractRegistry
	contractRegistryErr error
	contracts           contractDescriptorCache
	contractTypes       *protoregistry.Types
	configFragment      []byte
	pluginDir           string
	triggerSetupErr     error
}

type contractDescriptorCache struct {
	modules  map[string]*pb.ContractDescriptor
	steps    map[string]*pb.ContractDescriptor
	services map[string]*pb.ContractDescriptor
}

type errorModule struct {
	name string
	err  error
}

func (m *errorModule) Name() string               { return m.name }
func (m *errorModule) Dependencies() []string     { return nil }
func (m *errorModule) ProvidesServices() []string { return nil }
func (m *errorModule) RequiresServices() []string { return nil }
func (m *errorModule) RegisterConfig(modular.Application) error {
	return nil
}
func (m *errorModule) Init(modular.Application) error { return m.err }
func (m *errorModule) Start(context.Context) error    { return m.err }
func (m *errorModule) Stop(context.Context) error     { return nil }

// AsModuleError returns the wrapped error if m was produced by a failed
// CreateModule call (i.e. the factory returned an *errorModule), or nil if m
// is a successfully-created module. Callers outside this package use this to
// surface plugin-reported errors without depending on the unexported type.
func AsModuleError(m modular.Module) error {
	if em, ok := m.(*errorModule); ok {
		return em.err
	}
	return nil
}

// NewErrorModule returns a Module that surfaces err from Init and Start.
// Exported for use in tests that need to simulate a plugin factory failure
// without importing the unexported errorModule type directly.
func NewErrorModule(name string, err error) modular.Module {
	return &errorModule{name: name, err: err}
}

// manifestFromDisk field-maps a canonical *plugin.PluginManifest into the
// *pb.Manifest the adapter caches. Used as the disk-manifest fallback when
// the plugin's gRPC GetManifest RPC returns codes.Unimplemented or an empty
// Version. Maps all 6 scalar fields of pb.Manifest.
func manifestFromDisk(m *plugin.PluginManifest) *pb.Manifest {
	if m == nil {
		return nil
	}
	return &pb.Manifest{
		Name:           m.Name,
		Version:        m.Version,
		Author:         m.Author,
		Description:    m.Description,
		ConfigMutable:  m.ConfigMutable,
		SampleCategory: m.SampleCategory,
	}
}

// NewExternalPluginAdapter creates an adapter from a connected plugin client.
// diskManifest is the *plugin.PluginManifest loaded by the manager at
// manager.go:108 via pluginpkg.LoadManifest + Validate. It is used as the
// canonical fallback when the plugin's gRPC GetManifest RPC returns
// codes.Unimplemented (strict-cutover IaC plugins) or an empty Version
// (defensive). Pass nil only in tests that exercise the no-disk fallback
// path; production callers must pass the manager-loaded manifest.
func NewExternalPluginAdapter(name string, client *PluginClient, diskManifest *plugin.PluginManifest) (*ExternalPluginAdapter, error) {
	ctx := context.Background()
	// Precedence rule (load-bearing): gRPC GetManifest is authoritative when it
	// returns a non-empty Version. Disk-manifest fallback fires only when gRPC
	// returns Unimplemented (strict-cutover IaC plugins) OR returns an empty
	// Version. EngineManifest() reads a.manifest directly — no second-layer
	// overlay, to avoid precedence ambiguity (see workflow ADR-0031 + plan F2).
	manifest, err := client.client.GetManifest(ctx, &emptypb.Empty{})
	if err != nil {
		if status.Code(err) != codes.Unimplemented {
			return nil, fmt.Errorf("get manifest from plugin %s: %w", name, err)
		}
		// Strict-cutover IaC plugins (e.g. workflow-plugin-digitalocean v1.0.0+)
		// register only PluginService.GetContractRegistry via the iacPluginServiceBridge
		// and leave GetManifest unimplemented. Prefer disk-loaded plugin.json fields;
		// fall back to a name-only synthesized manifest to preserve PR #627 tolerance.
		if dm := manifestFromDisk(diskManifest); dm != nil {
			manifest = dm
		} else {
			manifest = &pb.Manifest{Name: name}
		}
	} else if manifest != nil && manifest.Version == "" {
		// gRPC returned a manifest but Version is empty (auto-synthesized or
		// misconfigured plugin). Overlay missing fields from disk if available.
		if dm := manifestFromDisk(diskManifest); dm != nil {
			manifest = dm
		}
	}
	var triggerSetupErr error
	triggerTypes, triggerErr := client.client.GetTriggerTypes(ctx, &emptypb.Empty{})
	if triggerErr != nil && status.Code(triggerErr) != codes.Unimplemented {
		triggerSetupErr = fmt.Errorf("get trigger types from plugin %s before callback setup: %w", name, triggerErr)
	}
	if triggerTypes != nil && len(triggerTypes.Types) > 0 {
		if client.callbackBrokerID == 0 {
			triggerSetupErr = fmt.Errorf("configure callback for plugin %s: callback broker unavailable", name)
		} else {
			resp, callbackErr := client.client.ConfigureCallback(ctx, &pb.ConfigureCallbackRequest{
				BrokerId: client.callbackBrokerID,
			})
			if callbackErr != nil {
				triggerSetupErr = fmt.Errorf("configure callback for plugin %s: %w", name, callbackErr)
			} else if resp.Error != "" {
				triggerSetupErr = fmt.Errorf("configure callback for plugin %s: %s", name, resp.Error)
			}
		}
	}
	a := &ExternalPluginAdapter{
		name:            name,
		client:          client,
		manifest:        manifest,
		diskManifest:    diskManifest,
		triggerSetupErr: triggerSetupErr,
	}
	if registry, registryErr := client.client.GetContractRegistry(ctx, &emptypb.Empty{}); registryErr == nil {
		a.contractRegistry = registry
	} else if status.Code(registryErr) == codes.Unimplemented {
		a.contractRegistry = &pb.ContractRegistry{}
	} else {
		a.contractRegistryErr = fmt.Errorf("get contract registry from plugin %s: %w", name, registryErr)
	}
	a.contracts = buildContractDescriptorCache(a.contractRegistry)
	a.contractTypes, err = buildContractTypeResolver(a.contractRegistry)
	if err != nil {
		a.contractRegistryErr = fmt.Errorf("parse contract registry descriptors from plugin %s: %w", name, err)
	}
	// Fetch config fragment eagerly so it's available before BuildFromConfig runs.
	if resp, fragErr := client.client.GetConfigFragment(ctx, &emptypb.Empty{}); fragErr == nil && len(resp.YamlConfig) > 0 {
		a.configFragment = resp.YamlConfig
		a.pluginDir = resp.PluginDir
	}
	return a, nil
}

func newExternalPluginAdapterWithContractRegistry(manifest *pb.Manifest, registry *pb.ContractRegistry) *ExternalPluginAdapter {
	types, err := buildContractTypeResolver(registry)
	return &ExternalPluginAdapter{
		name:                manifest.Name,
		manifest:            manifest,
		contractRegistry:    registry,
		contractRegistryErr: err,
		contracts:           buildContractDescriptorCache(registry),
		contractTypes:       types,
	}
}

func buildContractDescriptorCache(registry *pb.ContractRegistry) contractDescriptorCache {
	cache := contractDescriptorCache{
		modules:  make(map[string]*pb.ContractDescriptor),
		steps:    make(map[string]*pb.ContractDescriptor),
		services: make(map[string]*pb.ContractDescriptor),
	}
	if registry == nil {
		return cache
	}
	for _, descriptor := range registry.Contracts {
		if descriptor == nil {
			continue
		}
		switch descriptor.Kind {
		case pb.ContractKind_CONTRACT_KIND_MODULE:
			if descriptor.ModuleType != "" {
				cache.modules[descriptor.ModuleType] = descriptor
			}
		case pb.ContractKind_CONTRACT_KIND_STEP:
			if descriptor.StepType != "" {
				cache.steps[descriptor.StepType] = descriptor
			}
		case pb.ContractKind_CONTRACT_KIND_SERVICE:
			if descriptor.Method != "" {
				cache.services[serviceContractKey(descriptor.ServiceName, descriptor.Method)] = descriptor
				if descriptor.ServiceName == "" {
					cache.services[descriptor.Method] = descriptor
				}
			}
		}
	}
	return cache
}

func buildContractTypeResolver(registry *pb.ContractRegistry) (*protoregistry.Types, error) {
	if registry == nil || registry.FileDescriptorSet == nil || len(registry.FileDescriptorSet.File) == 0 {
		return nil, nil
	}
	files, err := protodesc.NewFiles(registry.FileDescriptorSet)
	if err != nil {
		return nil, err
	}
	types := new(protoregistry.Types)
	var registerErr error
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		registerErr = registerFileMessages(types, file.Messages())
		return registerErr == nil
	})
	if registerErr != nil {
		return nil, fmt.Errorf("register contract message types: %w", registerErr)
	}
	return types, nil
}

func registerFileMessages(types *protoregistry.Types, messages protoreflect.MessageDescriptors) error {
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)
		if err := types.RegisterMessage(dynamicpb.NewMessageType(message)); err != nil {
			return fmt.Errorf("register message %q: %w", message.FullName(), err)
		}
		if err := registerFileMessages(types, message.Messages()); err != nil {
			return err
		}
	}
	return nil
}

func serviceContractKey(serviceName, method string) string {
	if serviceName == "" {
		return method
	}
	return serviceName + "\x00" + method
}

func (c contractDescriptorCache) module(typeName string) *pb.ContractDescriptor {
	return c.modules[typeName]
}

func (c contractDescriptorCache) step(typeName string) *pb.ContractDescriptor {
	return c.steps[typeName]
}

func (c contractDescriptorCache) servicesFor(moduleType string) map[string]*pb.ContractDescriptor {
	out := make(map[string]*pb.ContractDescriptor)
	for _, descriptor := range c.services {
		if descriptor == nil {
			continue
		}
		if descriptor.ModuleType == moduleType || descriptor.ServiceName == moduleType || (descriptor.ModuleType == "" && descriptor.ServiceName == "") {
			out[descriptor.Method] = descriptor
		}
	}
	return out
}

func createTypedConfigRequest(descriptor *pb.ContractDescriptor, cfg map[string]any, resolver protoregistry.MessageTypeResolver) (*structpb.Struct, *anypb.Any, error) {
	if descriptor == nil || descriptor.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED {
		s, err := mapToStruct(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode config as Struct: %w", err)
		}
		return s, nil, nil
	}
	if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
		s, err := mapToStruct(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode LEGACY_STRUCT config as Struct: %w", err)
		}
		return s, nil, nil
	}
	// Steps with STRICT_PROTO mode but no ConfigMessage are input-only
	// (eventbus.ack, eventbus.publish, etc.) — they declare InputMessage +
	// OutputMessage but no per-instance config schema. Encode cfg as legacy
	// struct only; typed payload is nil. The plugin's typed factory reads
	// data from the input message, not from the config.
	if descriptor.ConfigMessage == "" {
		s, err := mapToStruct(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode config as Struct (input-only typed contract): %w", err)
		}
		return s, nil, nil
	}
	// Strip engine-internal "_"-prefix keys before proto decode. STRICT_PROTO
	// and PROTO_WITH_LEGACY_STRUCT modules use protojson with DiscardUnknown
	// = false (convert.go:62), which rejects engine internals like
	// "_config_dir" as unknown fields. Strip is copy-on-clean — the caller's
	// original cfg map retains all keys for the legacy *structpb.Struct
	// path below.
	cleaned := stripInternalKeys(cfg)
	typed, err := mapToTypedAny(descriptor.ConfigMessage, cleaned, resolver)
	if err != nil {
		if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			return nil, nil, fmt.Errorf("STRICT_PROTO contract for config message %q cannot use legacy Struct fallback: %w", descriptor.ConfigMessage, err)
		}
		s, sErr := mapToStruct(cfg)
		if sErr != nil {
			return nil, nil, fmt.Errorf("encode config as Struct after typed fallback: %w", sErr)
		}
		return s, nil, nil
	}
	if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		return nil, typed, nil
	}
	s, err := mapToStruct(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("encode PROTO_WITH_LEGACY_STRUCT config as Struct: %w", err)
	}
	return s, typed, nil
}

func contractModeUsesTyped(mode pb.ContractMode) bool {
	return mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO || mode == pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT
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

// GetAsset fetches a static asset from the plugin by path.
func (a *ExternalPluginAdapter) GetAsset(path string) ([]byte, string, error) {
	resp, err := a.client.client.GetAsset(context.Background(), &pb.GetAssetRequest{Path: path})
	if err != nil {
		return nil, "", err
	}
	if resp.Error != "" {
		return nil, "", fmt.Errorf("%s", resp.Error)
	}
	return resp.Content, resp.ContentType, nil
}

// IsSamplePlugin returns true if the plugin declares a sample category.
func (a *ExternalPluginAdapter) IsSamplePlugin() bool {
	return a.manifest.SampleCategory != ""
}

// IsConfigMutable returns true if tenants can override the plugin's config fragment.
func (a *ExternalPluginAdapter) IsConfigMutable() bool {
	return a.manifest.ConfigMutable
}

// SampleCategory returns the sample category declared by the plugin.
func (a *ExternalPluginAdapter) SampleCategory() string {
	return a.manifest.SampleCategory
}

// ConfigFragmentBytes returns the raw YAML config fragment fetched from the plugin.
func (a *ExternalPluginAdapter) ConfigFragmentBytes() []byte {
	return a.configFragment
}

func (a *ExternalPluginAdapter) ContractRegistry() *pb.ContractRegistry {
	return a.contractRegistry
}

func (a *ExternalPluginAdapter) ContractRegistryError() error {
	return a.contractRegistryErr
}

// Conn returns the underlying gRPC client connection used to talk to
// the plugin process. Callers (notably wfctl's typed-IaC cutover in
// plan Task 16) construct additional typed gRPC service clients
// against this conn — for example
// `pb.NewIaCProviderRequiredClient(adapter.Conn())`.
//
// Returns nil in two cases:
//  1. The adapter was constructed without a backing PluginClient
//     (e.g. `newExternalPluginAdapterWithContractRegistry` test
//     fixtures populate manifest + registry directly without a
//     gRPC subprocess).
//  2. The adapter has a non-nil PluginClient but its underlying
//     PluginClient.Conn() is itself nil (in-process test plumbing
//     that wires only the PluginServiceClient interface without a
//     real *grpc.ClientConn).
//
// Callers MUST nil-check before constructing typed clients.
//
// The connection lifecycle is owned by the host's plugin manager —
// callers MUST NOT call Close() on the returned conn. The plugin
// shutdown path tears it down via the registered Closer.
func (a *ExternalPluginAdapter) Conn() *grpc.ClientConn {
	if a.client == nil {
		return nil
	}
	return a.client.Conn()
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
			config, typedConfig, configErr := createTypedConfigRequest(a.contracts.module(tn), cfg, a.contractTypes)
			if configErr != nil {
				return &errorModule{name: name, err: fmt.Errorf("create remote module %s: %w", tn, configErr)}
			}
			createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{
				Type:        tn,
				Name:        name,
				Config:      config,
				TypedConfig: typedConfig,
			})
			if createErr != nil {
				return &errorModule{name: name, err: fmt.Errorf("create remote module %s: %w", tn, createErr)}
			}
			if createResp.Error != "" {
				return &errorModule{name: name, err: fmt.Errorf("create remote module %s: plugin reported: %s", tn, createResp.Error)}
			}
			remote := NewRemoteModule(name, createResp.HandleId, a.client.client, remoteModuleContracts{
				module:   a.contracts.module(tn),
				services: a.contracts.servicesFor(tn),
				types:    a.contractTypes,
			})
			if tn == "security.scanner" {
				return NewSecurityScannerRemoteModule(remote)
			}
			return remote
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
			contract := a.contracts.step(tn)
			config, typedConfig, configErr := createTypedConfigRequest(contract, cfg, a.contractTypes)
			if configErr != nil {
				return nil, fmt.Errorf("create remote step %s: %w", tn, configErr)
			}
			createResp, createErr := a.client.client.CreateStep(ctx, &pb.CreateStepRequest{
				Type:        tn,
				Name:        name,
				Config:      config,
				TypedConfig: typedConfig,
			})
			if createErr != nil {
				return nil, fmt.Errorf("create remote step %s: %w", tn, createErr)
			}
			if createResp.Error != "" {
				return nil, fmt.Errorf("create remote step %s: %s", tn, createResp.Error)
			}
			return NewRemoteStepWithContractTypes(name, createResp.HandleId, a.client.client, cfg, contract, a.contractTypes), nil
		}
	}
	return factories
}

func (a *ExternalPluginAdapter) TriggerFactories() map[string]plugin.TriggerFactory {
	if a.triggerSetupErr != nil {
		return nil
	}
	ctx := context.Background()
	resp, err := a.client.client.GetTriggerTypes(ctx, &emptypb.Empty{})
	if err != nil || resp == nil || len(resp.Types) == 0 {
		return nil
	}
	factories := make(map[string]plugin.TriggerFactory, len(resp.Types))
	for _, typeName := range resp.Types {
		tn := typeName // capture
		factories[tn] = func() any {
			return NewRemoteTrigger(tn, tn, a.client.client)
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

func (a *ExternalPluginAdapter) StepSchemas() []*schema.StepSchema {
	// External plugins provide step schemas via their plugin.json manifest (StepSchemas field).
	// The manifest is loaded by the PluginLoader, so we return nil here.
	return nil
}

func (a *ExternalPluginAdapter) WiringHooks() []plugin.WiringHook {
	return nil
}

func (a *ExternalPluginAdapter) DeployTargets() map[string]deploy.DeployTarget {
	return nil
}

func (a *ExternalPluginAdapter) SidecarProviders() map[string]deploy.SidecarProvider {
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
