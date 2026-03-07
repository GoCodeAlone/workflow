package actors

import "github.com/GoCodeAlone/workflow/module"

// ActorMessage is the standard message envelope for actor communication.
// All messages sent to bridge actors use this type.
type ActorMessage struct {
	// Type identifies which handler pipeline to invoke.
	Type string `json:"type" cbor:"type"`
	// Payload is the data passed to the handler pipeline as trigger data.
	Payload map[string]any `json:"payload" cbor:"payload"`
}

// HandlerPipeline defines a message handler as an ordered set of step configs.
type HandlerPipeline struct {
	// Description is an optional human-readable description.
	Description string
	// Steps is an ordered list of step configs (each is a map with "type" and other fields).
	Steps []map[string]any
	// BuiltSteps holds pre-built step instances (same length as Steps).
	// Built once during pool initialization to avoid concurrent map writes
	// when multiple actors share the same handler config.
	BuiltSteps []module.PipelineStep
}
