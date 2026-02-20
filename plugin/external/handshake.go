// Package external provides gRPC-based external plugin support for the workflow engine.
// External plugins run as separate processes communicating over gRPC via hashicorp/go-plugin.
package external

import (
	goplugin "github.com/GoCodeAlone/go-plugin"
)

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
// Both the host (client) and plugin (server) must use identical values.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}
