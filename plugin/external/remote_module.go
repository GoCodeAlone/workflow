package external

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RemoteModule implements modular.Module by delegating to a gRPC plugin.
type RemoteModule struct {
	name             string
	handleID         string
	client           pb.PluginServiceClient
	contract         *pb.ContractDescriptor
	serviceContracts map[string]*pb.ContractDescriptor
}

type remoteModuleContracts struct {
	module   *pb.ContractDescriptor
	services map[string]*pb.ContractDescriptor
}

// NewRemoteModule creates a remote module proxy.
func NewRemoteModule(name, handleID string, client pb.PluginServiceClient, contracts ...remoteModuleContracts) *RemoteModule {
	var contract *pb.ContractDescriptor
	serviceContracts := map[string]*pb.ContractDescriptor{}
	if len(contracts) > 0 {
		contract = contracts[0].module
		if contracts[0].services != nil {
			serviceContracts = contracts[0].services
		}
	}
	return &RemoteModule{
		name:             name,
		handleID:         handleID,
		client:           client,
		contract:         contract,
		serviceContracts: serviceContracts,
	}
}

func (m *RemoteModule) Name() string {
	return m.name
}

func (m *RemoteModule) Dependencies() []string {
	return nil
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
	req := &pb.InvokeServiceRequest{
		HandleId: m.handleID,
		Method:   method,
		Args:     mapToStruct(args),
	}
	contract := m.serviceContracts[method]
	if contract != nil && contract.Mode != pb.ContractMode_CONTRACT_MODE_UNSPECIFIED && contract.Mode != pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
		typedInput, err := mapToTypedAny(contract.InputMessage, args)
		if err != nil {
			if contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
				return nil, fmt.Errorf("remote invoke %s STRICT_PROTO input message %q cannot use legacy Struct fallback: %w", method, contract.InputMessage, err)
			}
		} else {
			req.Args = nil
			req.TypedInput = typedInput
		}
	}
	resp, err := m.client.InvokeService(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("remote invoke %s: %w", method, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote invoke %s: %s", method, resp.Error)
	}
	if resp.TypedOutput != nil && contract != nil && contract.OutputMessage != "" {
		output, err := typedAnyToMap(resp.TypedOutput, contract.OutputMessage)
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
