package external

import (
	"context"

	goplugin "github.com/GoCodeAlone/go-plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// GRPCPlugin implements go-plugin's Plugin and GRPCPlugin interfaces.
// It bridges between go-plugin's plugin system and our gRPC services.
type GRPCPlugin struct {
	goplugin.Plugin
	// CallbackServer is the host-side callback implementation.
	// When non-nil, it will be registered on the broker for plugin access.
	CallbackServer *CallbackServer
}

// GRPCServer registers the plugin service on the gRPC server (plugin side).
// This is called by the plugin process.
func (p *GRPCPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	// Plugin side: the actual PluginService implementation is registered
	// by the SDK's Serve function, not here. This method is only needed
	// to satisfy the interface.
	return nil
}

// GRPCClient returns the client wrapper (host side).
// This is called by the host process to get a client that talks to the plugin.
func (p *GRPCPlugin) GRPCClient(_ context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	client := pb.NewPluginServiceClient(c)

	// If we have a callback server, start serving it via the broker
	// so the plugin can call back to the host.
	if p.CallbackServer != nil {
		brokerID := broker.NextId()
		go broker.AcceptAndServe(brokerID, func(opts []grpc.ServerOption) *grpc.Server {
			s := grpc.NewServer(opts...)
			pb.RegisterEngineCallbackServiceServer(s, p.CallbackServer)
			return s
		})
		// The plugin will connect back to us on this broker ID.
		// We pass the broker ID via the initial GetManifest metadata or
		// a dedicated setup call. For simplicity, we embed it in the client wrapper.
		return &PluginClient{
			client:           client,
			broker:           broker,
			callbackBrokerID: brokerID,
		}, nil
	}

	return &PluginClient{
		client: client,
		broker: broker,
	}, nil
}

// PluginClient wraps the gRPC client for the plugin service.
type PluginClient struct {
	client           pb.PluginServiceClient
	broker           *goplugin.GRPCBroker
	callbackBrokerID uint32
}
