package external

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RemoteModule implements modular.Module by delegating to a gRPC plugin.
type RemoteModule struct {
	name             string
	handleID         string
	client           pb.PluginServiceClient
	contract         *pb.ContractDescriptor
	serviceContracts map[string]*pb.ContractDescriptor
	types            protoregistry.MessageTypeResolver

	// dependencies holds the yaml-level `dependsOn:` keys for this module.
	// Populated by the engine after the factory returns via SetDependencies
	// — see workflow#663. Returned from Dependencies() so modular's Init()
	// walker honours them when computing module init order. Without this,
	// every external-plugin module appeared as a root and modular sorted
	// them alphabetically, which broke any plugin where module A's Init()
	// registered runtime state in a plugin-local registry that module B's
	// Init() looked up.
	dependencies []string
}

type remoteModuleContracts struct {
	module   *pb.ContractDescriptor
	services map[string]*pb.ContractDescriptor
	types    protoregistry.MessageTypeResolver
}

// NewRemoteModule creates a remote module proxy.
func NewRemoteModule(name, handleID string, client pb.PluginServiceClient, contracts ...remoteModuleContracts) *RemoteModule {
	var contract *pb.ContractDescriptor
	var types protoregistry.MessageTypeResolver
	serviceContracts := map[string]*pb.ContractDescriptor{}
	if len(contracts) > 0 {
		contract = contracts[0].module
		if contracts[0].services != nil {
			serviceContracts = contracts[0].services
		}
		types = contracts[0].types
	}
	return &RemoteModule{
		name:             name,
		handleID:         handleID,
		client:           client,
		contract:         contract,
		serviceContracts: serviceContracts,
		types:            types,
	}
}

func (m *RemoteModule) Name() string {
	return m.name
}

func (m *RemoteModule) Dependencies() []string {
	return m.dependencies
}

// SetDependencies records the yaml-level `dependsOn:` keys declared for this
// module so modular's Init() walker can honour them. Called by the engine
// from BuildFromConfig immediately after the factory returns and before
// app.RegisterModule, but only when the module's `modCfg.DependsOn` is
// **non-empty after filtering** (filterResolvableDeps drops empty strings
// + names not present in cfg.Modules) AND the module satisfies
// `interface{ SetDependencies([]string) }` — modules with no resolvable
// dependsOn are skipped so a constructor-time default isn't clobbered with
// a SetDependencies(nil) call. See workflow#663.
//
// Defensive copy: although the engine already copies before calling, the
// setter is exported and Dependencies() exposes the same backing array to
// modular. Copying here too means any caller (engine, tests, future
// integration paths) can mutate its source slice after calling without
// silently corrupting this module's init graph.
func (m *RemoteModule) SetDependencies(deps []string) {
	cp := make([]string, len(deps))
	copy(cp, deps)
	m.dependencies = cp
}

func (m *RemoteModule) ProvidesServices() []string {
	return nil
}

func (m *RemoteModule) RequiresServices() []string {
	return nil
}

func (m *RemoteModule) RegisterConfig(app modular.Application) error {
	return nil
}

func (m *RemoteModule) Init(app modular.Application) error {
	resp, err := m.client.InitModule(context.Background(), &pb.HandleRequest{
		HandleId: m.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote init: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote init: %s", resp.Error)
	}
	return nil
}

func (m *RemoteModule) Start(ctx context.Context) error {
	resp, err := m.client.StartModule(ctx, &pb.HandleRequest{
		HandleId: m.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote start: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote start: %s", resp.Error)
	}
	return nil
}

func (m *RemoteModule) Stop(ctx context.Context) error {
	resp, err := m.client.StopModule(ctx, &pb.HandleRequest{
		HandleId: m.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote stop: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote stop: %s", resp.Error)
	}
	return nil
}

// Destroy releases the remote module resources.
func (m *RemoteModule) Destroy() error {
	resp, err := m.client.DestroyModule(context.Background(), &pb.HandleRequest{
		HandleId: m.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote destroy: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote destroy: %s", resp.Error)
	}
	return nil
}

// InvokeService calls a named method on the remote module's service interface.
func (m *RemoteModule) InvokeService(method string, args map[string]any) (map[string]any, error) {
	return m.InvokeServiceContext(context.Background(), method, args)
}

// InvokeServiceContext calls a named method on the remote module's service
// interface using the caller's context.
func (m *RemoteModule) InvokeServiceContext(ctx context.Context, method string, args map[string]any) (map[string]any, error) {
	req := &pb.InvokeServiceRequest{
		HandleId: m.handleID,
		Method:   method,
	}
	contract := m.serviceContracts[method]
	// Encode args as Struct only when the wire actually carries it. STRICT_PROTO
	// nils req.Args after a successful typed encode and relies on TypedInput;
	// failing early on Struct encoding for values JSON can marshal but Struct
	// cannot (e.g. time.Time targeting a strict typed payload) would break
	// otherwise-valid STRICT_PROTO calls — Copilot review #555 finding.
	encodeLegacyArgs := contract == nil ||
		contract.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED ||
		contract.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT ||
		contract.Mode == pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT
	if encodeLegacyArgs {
		argsStruct, err := mapToStruct(args)
		if err != nil {
			return nil, fmt.Errorf("remote invoke %s: encode args as Struct: %w", method, err)
		}
		req.Args = argsStruct
	}
	if contract != nil && contract.Mode != pb.ContractMode_CONTRACT_MODE_UNSPECIFIED && contract.Mode != pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
		typedInput, err := mapToTypedAny(contract.InputMessage, args, m.types)
		if err != nil {
			if contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
				return nil, fmt.Errorf("remote invoke %s STRICT_PROTO input message %q cannot use legacy Struct fallback: %w", method, contract.InputMessage, err)
			}
			// PROTO_WITH_LEGACY_STRUCT: typed encoding failed, fall back to
			// the legacy Struct already encoded above.
		} else {
			req.TypedInput = typedInput
			if contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
				req.Args = nil
			}
		}
	}
	resp, err := m.client.InvokeService(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote invoke %s: %w", method, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote invoke %s: %s", method, resp.Error)
	}
	usesTypedOutput := contract != nil && contract.OutputMessage != "" && contractModeUsesTyped(contract.Mode)
	if usesTypedOutput && resp.TypedOutput == nil && contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		return nil, fmt.Errorf("remote invoke %s STRICT_PROTO output message %q requires typed_output", method, contract.OutputMessage)
	}
	if usesTypedOutput && resp.TypedOutput != nil {
		output, err := typedAnyToMap(resp.TypedOutput, contract.OutputMessage, m.types)
		if err != nil {
			return nil, fmt.Errorf("remote invoke %s typed output decode: %w", method, err)
		}
		return output, nil
	}
	return structToMap(resp.Result), nil
}

// Ensure RemoteModule satisfies modular.Module at compile time.
var _ modular.Module = (*RemoteModule)(nil)

// Suppress unused import warning for emptypb.
var _ = (*emptypb.Empty)(nil)
