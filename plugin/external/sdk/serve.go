package sdk

import (
	"context"
	"encoding/json"
	"os"

	goplugin "github.com/GoCodeAlone/go-plugin"
	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/external/contract"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// ServeOption configures Serve and ServePluginFull.
type ServeOption func(*grpcServer)

// WithManifestProvider wires a canonical *plugin.PluginManifest (typically
// loaded via sdk.EmbedManifest) into the gRPC GetManifest handler. When set,
// the disk-embedded manifest takes precedence over the provider's Manifest()
// method.
//
// Recommended pattern:
//
//	//go:embed plugin.json
//	var manifestJSON []byte
//	var manifest = sdk.MustEmbedManifest(manifestJSON)
//
//	func main() {
//	    sdk.Serve(&myProvider{}, sdk.WithManifestProvider(manifest))
//	}
func WithManifestProvider(m *pluginpkg.PluginManifest) ServeOption {
	return func(s *grpcServer) {
		s.diskManifest = m
	}
}

// WithBuildVersion sets the runtime build-version surfaced via GetManifest.
// Single-channel precedence: takes precedence over any ManifestProvider.Version
// or provider.Manifest().Version. Typically populated via
// sdk.ResolveBuildVersion(<plugin's ldflag-injected Version var>).
//
// Recommended pattern:
//
//	import (
//	    "github.com/<...>/internal"  // ldflag-injected Version var
//	    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
//	)
//
//	func main() {
//	    sdk.Serve(&myPlugin{},
//	        sdk.WithManifestProvider(manifest),
//	        sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
//	    )
//	}
//
// Goreleaser config injects the tag at build time:
//
//	ldflags:
//	  - -X github.com/<...>/internal.Version={{.Version}}
//
// Closes workflow#758.
func WithBuildVersion(v string) ServeOption {
	return func(s *grpcServer) {
		s.buildVersion = v
	}
}

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
//
// With disk-embedded manifest:
//
//	func main() {
//	    sdk.Serve(&myPlugin{}, sdk.WithManifestProvider(manifest))
//	}
func Serve(provider PluginProvider, opts ...ServeOption) {
	if up, ok := provider.(UIProvider); ok {
		writeUIManifestIfAbsent(up.UIManifest())
	}

	server := newGRPCServer(provider)
	for _, opt := range opts {
		opt(server)
	}

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: contract.Handshake,
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
// The broker is stored so that the server can dial back to the host's callback
// service on the first incoming request that carries the broker ID.
func (p *servePlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterPluginServiceServer(s, p.server)
	p.server.setBroker(broker)
	return nil
}

// GRPCClient is not used on the plugin side. Plugins only serve, they don't
// create clients back to the host via this interface.
func (p *servePlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, _ *grpc.ClientConn) (any, error) {
	return nil, nil
}

// writeUIManifestIfAbsent writes m to "ui.json" in the current working
// directory only when that file does not already exist.
func writeUIManifestIfAbsent(m UIManifest) {
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
