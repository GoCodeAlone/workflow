package actors

// ActorMessage is the standard message envelope for actor communication.
// All messages sent to bridge actors use this type.
type ActorMessage struct {
	// Type identifies which handler pipeline to invoke.
	Type string `json:"type"`
	// Payload is the data passed to the handler pipeline as trigger data.
	Payload map[string]any `json:"payload"`
}

// ActorResponse is the standard response envelope returned by bridge actors.
type ActorResponse struct {
	// Type echoes the request message type.
	Type string `json:"type"`
	// Result is the merged output from the handler pipeline.
	Result map[string]any `json:"result"`
	// Error holds a non-nil error string if the handler pipeline failed.
	Error string `json:"error,omitempty"`
}

// HandlerPipeline defines a message handler as an ordered set of step configs.
type HandlerPipeline struct {
	// Description is an optional human-readable description.
	Description string
	// Steps is an ordered list of step configs (each is a map with "type" and other fields).
	Steps []map[string]any
}
