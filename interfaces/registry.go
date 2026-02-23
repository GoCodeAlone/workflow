package interfaces

// SchemaRegistrar is implemented by any service that can register admin
// schemas into an OpenAPI specification and apply them. Using this interface
// in cmd/server allows the server to enrich the OpenAPI spec without holding
// a concrete *module.OpenAPIGenerator pointer.
//
// *module.OpenAPIGenerator satisfies this interface.
type SchemaRegistrar interface {
	// Name returns the module name (used for logging).
	Name() string

	// RegisterAdminSchemas registers all admin API request/response schemas
	// onto this generator. Equivalent to calling module.RegisterAdminSchemas(gen).
	RegisterAdminSchemas()

	// ApplySchemas applies all previously registered component schemas and
	// operation schema overrides to the current OpenAPI spec.
	ApplySchemas()
}

// WorkflowStoreProvider is implemented by any service that exposes a workflow
// data store. Using this interface in cmd/server decouples the server startup
// code from the concrete *module.WorkflowRegistry type.
//
// *module.WorkflowRegistry satisfies this interface.
type WorkflowStoreProvider interface {
	// Name returns the module name (used for logging).
	Name() string

	// WorkflowStore returns the underlying workflow data store as an opaque
	// value. The caller is responsible for asserting the concrete type
	// (typically *module.V1Store) when further operations are required.
	WorkflowStore() any
}
