package schema

// TypeCoercionRegistry defines which output types can connect to which input types
// beyond exact match. This is the single source of truth — the editor reads it
// via `wfctl editor-schemas`.
type TypeCoercionRegistry struct {
	rules map[string][]string
}

// NewTypeCoercionRegistry creates a registry with all built-in coercion rules.
func NewTypeCoercionRegistry() *TypeCoercionRegistry {
	r := &TypeCoercionRegistry{rules: map[string][]string{
		// Data types
		"http.Request":  {"any", "PipelineContext"},
		"http.Response": {"any", "JSON", "[]byte"},
		"JSON":          {"any", "[]byte", "string"},
		"[]byte":        {"any", "string"},
		"Event":         {"any", "[]byte", "JSON"},
		"CloudEvent":    {"any", "Event", "[]byte", "JSON"},
		"Transition":    {"any", "Event"},
		"State":         {"any"},
		"string":        {"any"},
		"boolean":       {"any"},
		"Token":         {"any", "string"},
		"Credentials":   {"any"},
		"Time":          {"any", "Event"},
		"SQL":           {"any", "string"},
		"Rows":          {"any", "JSON"},
		"HealthStatus":  {"any", "JSON"},
		"Metric[]":      {"any"},
		"LogEntry":      {"any", "JSON"},
		"LogEntry[]":    {"any"},
		"[]LogEntry":    {"any"},
		"Span[]":        {"any"},
		"Command":       {"any", "PipelineContext"},
		"RouteConfig":   {"any", "JSON"},
		"OpenAPISpec":   {"any", "JSON"},
		"SlackResponse": {"any", "JSON"},
		"SQLiteStorage": {"any", "sql.DB"},
		"func()":        {"any"},

		// Pipeline types
		"PipelineContext": {"any", "StepResult", "PipelineContext"},
		"StepResult":     {"any", "PipelineContext", "StepResult"},

		// Service/provider types
		"prometheus.Metrics": {"any"},
		"net.Listener":      {"any"},
		"Scheduler":         {"any"},
		"AuthService":       {"any"},
		"EventBus":          {"any"},
		"Cache":             {"any"},
		"http.Client":       {"any"},
		"sql.DB":            {"any"},
		"SchemaValidator":   {"any"},
		"StorageProvider":   {"any"},
		"SecretProvider":    {"any"},
		"PersistenceStore":  {"any"},
		"WorkflowRegistry":  {"any"},
		"ExternalAPIClient": {"any"},
		"FileStore":         {"any", "StorageProvider"},
		"ObjectStore":       {"any", "StorageProvider"},
		"UserStore":         {"any"},
		"trace.Span":        {"any"},
		"trace.Tracer":      {"any"},
	}}
	return r
}

// Rules returns the full coercion rules map.
func (r *TypeCoercionRegistry) Rules() map[string][]string {
	out := make(map[string][]string, len(r.rules))
	for k, v := range r.rules {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
