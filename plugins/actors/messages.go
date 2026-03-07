package actors

// ActorMessage is the standard message envelope for actor communication.
// All messages sent to bridge actors use this type.
type ActorMessage struct {
	// Type identifies which handler pipeline to invoke.
	Type string `json:"type"`
	// Payload is the data passed to the handler pipeline as trigger data.
	Payload map[string]any `json:"payload"`
}

// HandlerPipeline defines a message handler as an ordered set of step configs.
type HandlerPipeline struct {
	// Description is an optional human-readable description.
	Description string
	// Steps is an ordered list of step configs (each is a map with "type" and other fields).
	Steps []map[string]any
}
