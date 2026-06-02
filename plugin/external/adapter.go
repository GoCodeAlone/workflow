package external

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
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
	// Contracts that declare a typed Mode (STRICT_PROTO or
	// PROTO_WITH_LEGACY_STRUCT) but leave ConfigMessage empty have no
	// per-instance config schema — primarily input-only steps like
	// step.eventbus.ack/publish/consume where data flows through the
	// InputMessage proto, but also applies to any contract Kind that
	// legitimately omits a config schema. Encode cfg as legacy struct
	// only; typed payload is nil. The plugin's typed factory reads data
	// from the input message (or other typed payload), not from config.
	if descriptor.ConfigMessage == "" {
		s, err := mapToStruct(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode config as Struct (no typed config schema): %w", err)
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

// iacProviderRequiredServiceName is the fully-qualified gRPC service a plugin's
// ContractRegistry must advertise for the adapter to be treated as an
// iac.provider — the analog of iacStateBackendServiceName. Sourced from the
// generated proto's ServiceDesc so it cannot drift if the proto package
// path/service name ever changes.
var iacProviderRequiredServiceName = pb.IaCProviderRequired_ServiceDesc.ServiceName

// advertisesIaCProviderRequiredService reports whether the adapter's
// ContractRegistry carries a CONTRACT_KIND_SERVICE descriptor for the
// IaCProviderRequired service. Mirrors advertisesIaCStateBackendService.
func (a *ExternalPluginAdapter) advertisesIaCProviderRequiredService() bool {
	if a.contractRegistry == nil {
		return false
	}
	for _, d := range a.contractRegistry.Contracts {
		if d == nil {
			continue
		}
		if d.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE && d.ServiceName == iacProviderRequiredServiceName {
			return true
		}
	}
	// Also check diskManifest.IaCServices as a fallback — the plugin may declare
	// the service there without advertising it in the ContractRegistry (e.g. when
	// GetContractRegistry returned Unimplemented). This mirrors the check in
	// IaCStateBackendClients which cross-checks against diskManifest.
	if a.diskManifest != nil {
		for _, svc := range a.diskManifest.IaCServices {
			if svc == iacProviderRequiredServiceName {
				return true
			}
		}
	}
	return false
}

// WiringHooks returns a WiringHook that registers the plugin as an
// interfaces.IaCProvider service in the modular application DI graph when this
// plugin advertises the IaCProviderRequired gRPC service.
//
// Registration key convention: the plugin name (a.name, as declared in the
// plugin manifest / app.yaml plugin entry). Steps that want to use this
// provider configure `provider: <pluginName>` and the engine resolves it via
// app.GetService(cfg.Provider, &provider). This key is the same name that
// appears in the scenario app.yaml's plugin entry for the iac.provider plugin.
//
// If the plugin does not advertise IaCProviderRequired this method returns nil
// (no wiring needed — not an iac.provider plugin).
func (a *ExternalPluginAdapter) WiringHooks() []plugin.WiringHook {
	if !a.advertisesIaCProviderRequiredService() {
		return nil
	}
	// Capture immutable fields before the closure. conn may be nil
	// (test adapters built without a real gRPC connection); the hook
	// guard handles that.
	name := a.name
	conn := a.Conn()
	return []plugin.WiringHook{
		{
			// Name follows the convention "<plugin>-iac-provider-registration".
			// Priority 50: runs after high-priority service wiring (e.g. OTEL at
			// 100, auth at 90) so the service registry is stable, but before lower-
			// priority wiring that might look up providers.
			Name:     name + "-iac-provider-registration",
			Priority: 50,
			Hook: func(app modular.Application, _ *config.WorkflowConfig) error {
				if conn == nil {
					return fmt.Errorf("plugin %q advertises IaCProviderRequired but has no gRPC connection", name)
				}
				adapter := providerclient.New(conn)
				if err := app.RegisterService(name, adapter); err != nil {
					return fmt.Errorf("plugin %q: register IaCProvider service: %w", name, err)
				}
				return nil
			},
		},
	}
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

// iacStateBackendServiceName is the fully-qualified gRPC service the plugin's
// ContractRegistry must advertise for the adapter to be treated as an
// iac.state backend provider. Sourced from the generated proto's ServiceDesc
// so it cannot drift if the proto package path/service name ever changes.
var iacStateBackendServiceName = pb.IaCStateBackend_ServiceDesc.ServiceName

// advertisesIaCStateBackendService reports whether the adapter's ContractRegistry
// carries a CONTRACT_KIND_SERVICE descriptor for the IaCStateBackend service.
func (a *ExternalPluginAdapter) advertisesIaCStateBackendService() bool {
	if a.contractRegistry == nil {
		return false
	}
	for _, d := range a.contractRegistry.Contracts {
		if d == nil {
			continue
		}
		if d.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE && d.ServiceName == iacStateBackendServiceName {
			return true
		}
	}
	return false
}

// IaCStateBackendClients implements plugin.IaCStateBackendProvider. At
// plugin-load the engine type-asserts the adapter against that interface and
// registers each returned (name → client) pair into module's iac.state backend
// registry. Amendment A2 (decisions/0035).
//
// Behaviour:
//   - If the plugin's ContractRegistry does not advertise the IaCStateBackend
//     service: when the disk manifest declares a non-empty IaCStateBackends
//     list, that is a silent misconfiguration (the plugin claims backends but
//     the host would register none) — return an error so plugin-load fails
//     loudly. When the manifest is also silent, the plugin genuinely serves no
//     state backend — return (nil, nil); the engine type-assert still succeeds
//     and just registers nothing.
//   - Otherwise call the live ListBackendNames RPC for the authoritative
//     backend-name list and cross-check it against the plugin's declared
//     PluginManifest.IaCStateBackends.
//
// Cross-check decision: the RPC is the live source of truth. The manifest is
// only consulted as a declared-vs-served consistency guard — when the manifest
// declares a non-empty backend set, it MUST match the RPC result exactly (a
// plugin whose live RPC contradicts its declared manifest is misconfigured and
// is rejected). When the manifest is silent (no diskManifest, or an empty
// IaCStateBackends list — e.g. a strict-cutover plugin that left GetManifest
// unimplemented and whose plugin.json omits the field) the RPC result is
// accepted on its own.
func (a *ExternalPluginAdapter) IaCStateBackendClients() (map[string]pb.IaCStateBackendClient, error) {
	if !a.advertisesIaCStateBackendService() {
		if a.diskManifest != nil && len(a.diskManifest.IaCStateBackends) > 0 {
			return nil, fmt.Errorf(
				"plugin %s: manifest declares iac.state backends %v but the plugin does not advertise the IaCStateBackend service",
				a.name, a.diskManifest.IaCStateBackends)
		}
		return nil, nil
	}
	conn := a.Conn()
	if conn == nil {
		return nil, fmt.Errorf("plugin %s advertises the IaCStateBackend service but has no gRPC connection", a.name)
	}
	client := pb.NewIaCStateBackendClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.ListBackendNames(ctx, &pb.ListBackendNamesRequest{})
	if err != nil {
		return nil, fmt.Errorf("plugin %s: ListBackendNames RPC: %w", a.name, err)
	}
	rpcNames := resp.GetBackendNames()
	if len(rpcNames) == 0 {
		return nil, fmt.Errorf("plugin %s advertises the IaCStateBackend service but ListBackendNames returned no names", a.name)
	}
	// Cross-check against the declared manifest when it declares any backends.
	if a.diskManifest != nil && len(a.diskManifest.IaCStateBackends) > 0 {
		if !sameStringSet(rpcNames, a.diskManifest.IaCStateBackends) {
			return nil, fmt.Errorf(
				"plugin %s: iac.state backend mismatch — ListBackendNames RPC returned %v but manifest declares %v",
				a.name, rpcNames, a.diskManifest.IaCStateBackends)
		}
	}
	clients := make(map[string]pb.IaCStateBackendClient, len(rpcNames))
	for _, name := range rpcNames {
		clients[name] = client
	}
	return clients, nil
}

// sameStringSet reports whether a and b contain the same set of strings,
// ignoring order and duplicates.
func sameStringSet(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	seen := make(map[string]struct{}, len(b))
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
		seen[s] = struct{}{}
	}
	return len(seen) == len(set)
}

// ─────────────────────────────────────────────────────────────────────────────
// KubernetesBackendProvider — plugin-served platform.kubernetes backends.
//
// Per ADR 0037 a kubernetes backend (gke) folds into the existing
// ResourceDriver contract — no new proto surface. A plugin serves the `gke`
// platform.kubernetes backend when it advertises the ResourceDriver service AND
// its live Capabilities RPC declares the infra.k8s_cluster resource type.
// ─────────────────────────────────────────────────────────────────────────────

// resourceDriverServiceName is the fully-qualified gRPC service a plugin's
// ContractRegistry must advertise for the adapter to be a potential
// kubernetes-backend provider. Sourced from the generated proto's ServiceDesc
// so it cannot drift if the proto package path/service name ever changes.
var resourceDriverServiceName = pb.ResourceDriver_ServiceDesc.ServiceName

// k8sClusterResourceType is the ResourceDriver resource type a plugin must
// declare (via the Capabilities RPC) for the adapter to register it as the
// platform.kubernetes `gke` backend. Mirrors module's gkeResourceType — kept
// local so the plugin/external package takes no dependency on module.
const k8sClusterResourceType = "infra.k8s_cluster"

// gkeKubernetesBackendType is the platform.kubernetes cluster type name the
// infra.k8s_cluster ResourceDriver is registered under in core.
const gkeKubernetesBackendType = "gke"

// advertisesResourceDriverService reports whether the adapter's ContractRegistry
// carries a CONTRACT_KIND_SERVICE descriptor for the ResourceDriver service.
func (a *ExternalPluginAdapter) advertisesResourceDriverService() bool {
	if a.contractRegistry == nil {
		return false
	}
	for _, d := range a.contractRegistry.Contracts {
		if d == nil {
			continue
		}
		if d.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE && d.ServiceName == resourceDriverServiceName {
			return true
		}
	}
	return false
}

// KubernetesBackendClients implements plugin.KubernetesBackendProvider. At
// plugin-load the engine type-asserts the adapter against that interface and
// registers each returned (cluster-type → ResourceDriver client) pair into
// module's kubernetes backend registry. Per ADR 0037.
//
// Behaviour:
//   - If the plugin does not advertise the ResourceDriver service it serves no
//     kubernetes backend — return (nil, nil); the engine type-assert still
//     succeeds and just registers nothing.
//   - Otherwise the live Capabilities RPC is the source of truth (mirroring how
//     IaCStateBackendClients trusts the ListBackendNames RPC): when it declares
//     the infra.k8s_cluster resource type, the plugin serves the `gke`
//     kubernetes backend and a ResourceDriver client is registered under that
//     name.
func (a *ExternalPluginAdapter) KubernetesBackendClients() (map[string]pb.ResourceDriverClient, error) {
	if !a.advertisesResourceDriverService() {
		return nil, nil
	}
	conn := a.Conn()
	if conn == nil {
		return nil, fmt.Errorf("plugin %s advertises the ResourceDriver service but has no gRPC connection", a.name)
	}
	provider := pb.NewIaCProviderRequiredClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	caps, err := provider.Capabilities(ctx, &pb.CapabilitiesRequest{})
	if err != nil {
		return nil, fmt.Errorf("plugin %s: Capabilities RPC: %w", a.name, err)
	}
	for _, decl := range caps.GetCapabilities() {
		if decl.GetResourceType() == k8sClusterResourceType {
			return map[string]pb.ResourceDriverClient{
				gkeKubernetesBackendType: pb.NewResourceDriverClient(conn),
			}, nil
		}
	}
	return nil, nil
}

// Ensure ExternalPluginAdapter satisfies plugin.EnginePlugin at compile time.
var _ plugin.EnginePlugin = (*ExternalPluginAdapter)(nil)

// Ensure ExternalPluginAdapter satisfies plugin.KubernetesBackendProvider — the
// engine type-asserts loaded plugins against it at plugin-load.
var _ plugin.KubernetesBackendProvider = (*ExternalPluginAdapter)(nil)

// Ensure ExternalPluginAdapter satisfies plugin.IaCStateBackendProvider at
// compile time — the engine type-asserts loaded plugins against it.
var _ plugin.IaCStateBackendProvider = (*ExternalPluginAdapter)(nil)
