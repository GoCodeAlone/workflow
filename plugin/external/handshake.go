// Package external provides gRPC-based external plugin support for the workflow engine.
// External plugins run as separate processes communicating over gRPC via hashicorp/go-plugin.
package external

import "github.com/GoCodeAlone/workflow/plugin/external/contract"

const (
	// ProtocolVersion is the plugin protocol version.
	// Increment this when making breaking changes to the gRPC interface.
	ProtocolVersion = contract.ProtocolVersion

	// MagicCookieKey is the environment variable used for the handshake.
	MagicCookieKey = contract.MagicCookieKey

	// MagicCookieValue is the expected value for the handshake cookie.
	MagicCookieValue = contract.MagicCookieValue
)

// Handshake is the shared handshake configuration between host and plugins.
// Both the host (client) and plugin (server) must use identical values.
var Handshake = contract.Handshake
