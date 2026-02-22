package sdk

import (
	"context"
	"encoding/json"
	"os"

	goplugin "github.com/GoCodeAlone/go-plugin"
	ext "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// Serve is the entry point for plugin authors. It starts the gRPC plugin server
// and blocks until the host process terminates the connection.
//
// If provider implements UIProvider, Serve writes a "ui.json" file to the
// current working directory (if one does not already exist). Plugin authors
// can also maintain "ui.json" manually without implementing UIProvider.
//
// Usage:
//
//	func main() {
//	    sdk.Serve(&myPlugin{})
//	}
func Serve(provider PluginProvider) {
	if up, ok := provider.(UIProvider); ok {
		writeUIManifestIfAbsent(up.UIManifest())
	}

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

// writeUIManifestIfAbsent writes m to "ui.json" in the current working
// directory only when that file does not already exist.
func writeUIManifestIfAbsent(m ext.UIManifest) {
	const filename = "ui.json"
	if _, err := os.Stat(filename); err == nil {
		return // file already exists — do not overwrite
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filename, data, 0o600)
}
