// Package contract contains host/plugin shared types that must stay free of
// workflow host runtime dependencies.
package contract

import goplugin "github.com/GoCodeAlone/go-plugin"

const (
	// ProtocolVersion is the plugin protocol version.
	// Increment this when making breaking changes to the gRPC interface.
	ProtocolVersion = 1

	// MagicCookieKey is the environment variable used for the handshake.
	MagicCookieKey = "WORKFLOW_PLUGIN"

	// MagicCookieValue is the expected value for the handshake cookie.
	MagicCookieValue = "workflow-external-plugin-v1"
)

// Handshake is the shared handshake configuration between host and plugins.
// Both the host client and plugin server must use identical values.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}
