package actors

import "github.com/GoCodeAlone/workflow/schema"

func actorSystemSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:     "actor.system",
		Label:    "Actor System",
		Category: "actor",
		Description: "Actor runtime for stateful, message-driven services. " +
			"Actors are lightweight, isolated units of computation that communicate through messages. " +
			"Each actor processes one message at a time, eliminating concurrency bugs.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:          "shutdownTimeout",
				Label:        "Shutdown Timeout",
				Type:         schema.FieldTypeDuration,
				Description:  "How long to wait for in-flight messages to drain before forcing shutdown",
				DefaultValue: "30s",
				Placeholder:  "30s",
			},
			{
				Key:         "defaultRecovery",
				Label:       "Default Recovery Policy",
				Type:        schema.FieldTypeJSON,
				Description: "What happens when any actor in this system crashes. Applied to pools that don't set their own recovery policy.",
				Group:       "Fault Tolerance",
			},
		},
		DefaultConfig: map[string]any{
			"shutdownTimeout": "30s",
		},
	}
}

func actorPoolSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:     "actor.pool",
		Label:    "Actor Pool",
		Category: "actor",
		Description: "Defines a group of actors that handle the same type of work. " +
			"Each actor has its own state and processes messages one at a time, " +
			"eliminating concurrency bugs. Use 'auto-managed' for actors identified by a " +
			"unique key (e.g. one per order) that activate on demand. " +
			"Use 'permanent' for a fixed pool of always-running workers.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "system",
				Label:       "Actor Cluster",
				Type:        schema.FieldTypeString,
				Description: "Name of the actor.system module this pool belongs to",
				Required:    true,
			},
			{
				Key:          "mode",
				Label:        "Lifecycle Mode",
				Type:         schema.FieldTypeSelect,
				Description:  "'auto-managed': actors activate on first message and deactivate after idle timeout, identified by a unique key. 'permanent': fixed pool that starts with the engine and runs until shutdown.",
				Options:      []string{"auto-managed", "permanent"},
				DefaultValue: "auto-managed",
			},
			{
				Key:          "idleTimeout",
				Label:        "Idle Timeout",
				Type:         schema.FieldTypeDuration,
				Description:  "How long an auto-managed actor stays in memory without messages before deactivating (auto-managed only)",
				DefaultValue: "10m",
				Placeholder:  "10m",
			},
			{
				Key:          "poolSize",
				Label:        "Pool Size",
				Type:         schema.FieldTypeNumber,
				Description:  "Number of actors in a permanent pool (permanent mode only)",
				DefaultValue: 10,
			},
			{
				Key:          "routing",
				Label:        "Load Balancing",
				Type:         schema.FieldTypeSelect,
				Description:  "How messages are distributed. 'round-robin': even distribution. 'random': random selection. 'broadcast': send to all. 'sticky': same key always goes to same actor.",
				Options:      []string{"round-robin", "random", "broadcast", "sticky"},
				DefaultValue: "round-robin",
			},
			{
				Key:         "routingKey",
				Label:       "Sticky Routing Key",
				Type:        schema.FieldTypeString,
				Description: "When routing is 'sticky', this message field determines which actor handles it. All messages with the same value go to the same actor.",
			},
			{
				Key:         "recovery",
				Label:       "Recovery Policy",
				Type:        schema.FieldTypeJSON,
				Description: "What happens when an actor crashes. Overrides the system default.",
				Group:       "Fault Tolerance",
			},
		},
		DefaultConfig: map[string]any{
			"mode":        "auto-managed",
			"idleTimeout": "10m",
			"routing":     "round-robin",
		},
	}
}

func actorSendStepSchema() *schema.StepSchema {
	return &schema.StepSchema{
		Type:   "step.actor_send",
		Plugin: "actors",
		Description: "Send a message to an actor without waiting for a response. " +
			"The actor processes it asynchronously. Use for fire-and-forget operations " +
			"like triggering background processing or updating actor state when the " +
			"pipeline doesn't need the result.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "pool",
				Label:       "Actor Pool",
				Type:        schema.FieldTypeString,
				Description: "Name of the actor.pool module to send to",
				Required:    true,
			},
			{
				Key:         "identity",
				Label:       "Actor Identity",
				Type:        schema.FieldTypeString,
				Description: "Unique key for auto-managed actors (e.g. '{{ .body.order_id }}'). Determines which actor instance receives the message.",
			},
			{
				Key:         "message",
				Label:       "Message",
				Type:        schema.FieldTypeJSON,
				Description: "Message to send. Must include 'type' (matched against receive handlers) and optional 'payload' map.",
				Required:    true,
			},
		},
		Outputs: []schema.StepOutputDef{
			{Key: "delivered", Type: "boolean", Description: "Whether the message was delivered"},
		},
	}
}

func actorAskStepSchema() *schema.StepSchema {
	return &schema.StepSchema{
		Type:   "step.actor_ask",
		Plugin: "actors",
		Description: "Send a message to an actor and wait for a response. " +
			"The actor's reply becomes this step's output, available to subsequent " +
			"steps via template expressions. If the actor doesn't respond within " +
			"the timeout, the step fails.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "pool",
				Label:       "Actor Pool",
				Type:        schema.FieldTypeString,
				Description: "Name of the actor.pool module to send to",
				Required:    true,
			},
			{
				Key:         "identity",
				Label:       "Actor Identity",
				Type:        schema.FieldTypeString,
				Description: "Unique key for auto-managed actors (e.g. '{{ .path.order_id }}')",
			},
			{
				Key:          "timeout",
				Label:        "Response Timeout",
				Type:         schema.FieldTypeDuration,
				Description:  "How long to wait for the actor's reply before failing",
				DefaultValue: "10s",
				Placeholder:  "10s",
			},
			{
				Key:         "message",
				Label:       "Message",
				Type:        schema.FieldTypeJSON,
				Description: "Message to send. Must include 'type' and optional 'payload' map.",
				Required:    true,
			},
		},
		Outputs: []schema.StepOutputDef{
			{Key: "*", Type: "any", Description: "The actor's reply — varies by message handler. The last step's output in the receive handler becomes the response."},
		},
	}
}
