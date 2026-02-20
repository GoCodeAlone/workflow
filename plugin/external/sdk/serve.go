package sdk

import (
	"context"

	goplugin "github.com/GoCodeAlone/go-plugin"
	ext "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// Serve is the entry point for plugin authors. It starts the gRPC plugin server
// and blocks until the host process terminates the connection.
//
// Usage:
//
//	func main() {
//	    sdk.Serve(&myPlugin{})
//	}
func Serve(provider PluginProvider) {
	server := newGRPCServer(provider)

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: ext.Handshake,
		GRPCServer:      goplugin.DefaultGRPCServer,
		Plugins: goplugin.PluginSet{
			"plugin": &servePlugin{server: server},
		},
	})
}

// servePlugin implements goplugin.Plugin for the plugin (server) side.
// It registers the gRPC PluginService implementation on the gRPC server.
type servePlugin struct {
	server *grpcServer
}

// GRPCServer registers the PluginService implementation on the gRPC server.
func (p *servePlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterPluginServiceServer(s, p.server)
	return nil
}

// GRPCClient is not used on the plugin side. Plugins only serve, they don't
// create clients back to the host via this interface.
func (p *servePlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, _ *grpc.ClientConn) (any, error) {
	return nil, nil
}
