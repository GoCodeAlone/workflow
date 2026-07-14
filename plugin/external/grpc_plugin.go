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
			conn:             c,
			client:           client,
			broker:           broker,
			callbackBrokerID: brokerID,
		}, nil
	}

	return &PluginClient{
		conn:   c,
		client: client,
		broker: broker,
	}, nil
}

// PluginClient wraps the gRPC client for the plugin service.
//
// The underlying *grpc.ClientConn is retained so callers that need to
// instantiate additional typed gRPC clients (e.g. the typed
// pb.IaCProviderRequiredClient that wfctl's typedIaCAdapter wraps in
// the strict-contracts cutover, plan Task 16) can do so without going
// through the legacy pb.PluginServiceClient string-dispatch path.
// Exposed via Conn() rather than as a public field so the rest of the
// PluginClient surface stays opaque.
type PluginClient struct {
	conn             *grpc.ClientConn
	client           pb.PluginServiceClient
	broker           *goplugin.GRPCBroker
	callbackBrokerID uint32
}

// Conn returns the underlying gRPC client connection to the plugin
// process. Callers MAY use it to construct additional typed gRPC
// service clients (for example pb.NewIaCProviderRequiredClient).
//
// The connection lifecycle is owned by the host's plugin manager —
// callers MUST NOT call Close() on it. The connection is shared across
// every typed-client constructed against it; closing it would tear
// down every other consumer too.
func (p *PluginClient) Conn() *grpc.ClientConn {
	return p.conn
}

// CredentialIssuerClient constructs the typed optional credential-issuer
// client over the same connection owned by the plugin manager. A nil result
// means this PluginClient has no live connection; callers must also require
// the CredentialIssuer ContractRegistry advertisement before dispatch.
func (p *PluginClient) CredentialIssuerClient() pb.CredentialIssuerClient {
	if p == nil || p.Conn() == nil {
		return nil
	}
	return pb.NewCredentialIssuerClient(p.Conn())
}

// CredentialResolverClient constructs the typed optional credential-resolver
// client over the same connection owned by the plugin manager. Hosts must also
// require the CredentialResolver ContractRegistry advertisement before use.
func (p *PluginClient) CredentialResolverClient() pb.CredentialResolverClient {
	if p == nil || p.Conn() == nil {
		return nil
	}
	return pb.NewCredentialResolverClient(p.Conn())
}
