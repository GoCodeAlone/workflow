package module

// RegisterAdminSchemas registers all admin API request/response schemas on
// the given OpenAPI generator. Call after BuildSpec and before ApplySchemas.
func RegisterAdminSchemas(gen *OpenAPIGenerator) {
	registerComponentSchemas(gen)
	registerAuthOperationSchemas(gen)
	registerEngineOperationSchemas(gen)
	registerSchemaOperationSchemas(gen)
	registerAIOperationSchemas(gen)
	registerComponentOperationSchemas(gen)
	registerV1OperationSchemas(gen)
}

// --- Component Schemas (reusable definitions) ---

func registerComponentSchemas(gen *OpenAPIGenerator) {
	// --- Auth ---
	gen.RegisterComponentSchema("LoginRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"email", "password"},
		Properties: map[string]*OpenAPISchema{
			"email":    {Type: "string", Format: "email"},
			"password": {Type: "string", Format: "password"},
		},
	})

	gen.RegisterComponentSchema("RegisterRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"email", "password"},
		Properties: map[string]*OpenAPISchema{
			"email":    {Type: "string", Format: "email"},
			"name":     {Type: "string"},
			"password": {Type: "string", Format: "password"},
		},
	})

	gen.RegisterComponentSchema("AuthResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"token": {Type: "string"},
			"user":  {Ref: "#/components/schemas/UserProfile"},
		},
	})

	gen.RegisterComponentSchema("UserProfile", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":        {Type: "string"},
			"email":     {Type: "string", Format: "email"},
			"name":      {Type: "string"},
			"role":      {Type: "string", Enum: []string{"admin", "user", "viewer"}},
			"createdAt": {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("SetupStatusResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"needsSetup": {Type: "boolean"},
			"userCount":  {Type: "integer"},
		},
	})

	gen.RegisterComponentSchema("CreateUserRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"email", "password"},
		Properties: map[string]*OpenAPISchema{
			"email":    {Type: "string", Format: "email"},
			"name":     {Type: "string"},
			"password": {Type: "string", Format: "password"},
			"role":     {Type: "string", Enum: []string{"admin", "user", "viewer"}},
		},
	})

	gen.RegisterComponentSchema("UpdateRoleRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"role"},
		Properties: map[string]*OpenAPISchema{
			"role": {Type: "string", Enum: []string{"admin", "user", "viewer"}},
		},
	})

	// --- Engine ---
	gen.RegisterComponentSchema("ModuleConfig", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":      {Type: "string"},
			"type":      {Type: "string"},
			"config":    {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"dependsOn": {Type: "array", Items: &OpenAPISchema{Type: "string"}},
		},
	})

	gen.RegisterComponentSchema("WorkflowConfig", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"modules":   {Type: "array", Items: SchemaRef("ModuleConfig")},
			"workflows": {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"triggers":  {Type: "object", AdditionalProperties: &OpenAPISchema{}},
		},
	})

	gen.RegisterComponentSchema("EngineStatus", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"status": {Type: "string", Enum: []string{"running", "stopped", "error"}},
		},
	})

	gen.RegisterComponentSchema("ValidationResult", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"valid":  {Type: "boolean"},
			"errors": {Type: "array", Items: &OpenAPISchema{Type: "string"}},
		},
	})

	gen.RegisterComponentSchema("ModuleTypeInfo", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"type":     {Type: "string"},
			"label":    {Type: "string"},
			"category": {Type: "string"},
		},
	})

	// --- Module Schema ---
	gen.RegisterComponentSchema("ModuleSchema", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"type":          {Type: "string"},
			"label":         {Type: "string"},
			"category":      {Type: "string"},
			"description":   {Type: "string"},
			"configFields":  {Type: "array", Items: SchemaRef("ConfigFieldDef")},
			"inputs":        {Type: "array", Items: SchemaRef("IODef")},
			"outputs":       {Type: "array", Items: SchemaRef("IODef")},
			"defaultConfig": {Type: "object", AdditionalProperties: &OpenAPISchema{}},
		},
	})

	gen.RegisterComponentSchema("ConfigFieldDef", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"key":          {Type: "string"},
			"label":        {Type: "string"},
			"type":         {Type: "string", Enum: []string{"string", "number", "boolean", "select", "json", "duration", "array", "map", "filepath"}},
			"description":  {Type: "string"},
			"required":     {Type: "boolean"},
			"defaultValue": {},
			"options":      {Type: "array", Items: &OpenAPISchema{Type: "string"}},
			"placeholder":  {Type: "string"},
			"group":        {Type: "string"},
			"inheritFrom":  {Type: "string"},
			"sensitive":    {Type: "boolean"},
		},
	})

	gen.RegisterComponentSchema("IODef", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"type":        {Type: "string"},
			"description": {Type: "string"},
		},
	})

	// --- AI ---
	gen.RegisterComponentSchema("AIGenerateRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"intent"},
		Properties: map[string]*OpenAPISchema{
			"intent":   {Type: "string", Description: "Natural language description of the desired workflow"},
			"provider": {Type: "string", Enum: []string{"anthropic", "copilot"}},
		},
	})

	gen.RegisterComponentSchema("AIGenerateResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"workflow":    SchemaRef("WorkflowConfig"),
			"explanation": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("AIComponentRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name", "description"},
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"interfaces":  {Type: "array", Items: &OpenAPISchema{Type: "string"}},
			"provider":    {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("AIComponentResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"source": {Type: "string", Description: "Generated Go source code"},
		},
	})

	gen.RegisterComponentSchema("AISuggestRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"useCase"},
		Properties: map[string]*OpenAPISchema{
			"useCase":  {Type: "string"},
			"provider": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("AISuggestion", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"confidence":  {Type: "number", Format: "float"},
		},
	})

	gen.RegisterComponentSchema("AIProvider", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":      {Type: "string"},
			"available": {Type: "boolean"},
		},
	})

	gen.RegisterComponentSchema("AIDeployRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"intent"},
		Properties: map[string]*OpenAPISchema{
			"intent":   {Type: "string"},
			"provider": {Type: "string"},
		},
	})

	// --- Dynamic Components ---
	gen.RegisterComponentSchema("ComponentInfo", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":     {Type: "string"},
			"type":   {Type: "string"},
			"status": {Type: "string", Enum: []string{"loaded", "running", "stopped", "error"}},
		},
	})

	gen.RegisterComponentSchema("CreateComponentRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"id", "source"},
		Properties: map[string]*OpenAPISchema{
			"id":     {Type: "string"},
			"source": {Type: "string", Description: "Go source code for the component"},
		},
	})

	gen.RegisterComponentSchema("UpdateComponentRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"source"},
		Properties: map[string]*OpenAPISchema{
			"source": {Type: "string", Description: "Updated Go source code"},
		},
	})

	gen.RegisterComponentSchema("ComponentDetail", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":     {Type: "string"},
			"type":   {Type: "string"},
			"status": {Type: "string"},
			"source": {Type: "string"},
		},
	})

	// --- V1 Entities ---
	gen.RegisterComponentSchema("Company", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":          {Type: "string", Format: "uuid"},
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"createdAt":   {Type: "string", Format: "date-time"},
			"updatedAt":   {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("CreateCompanyRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("Organization", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":          {Type: "string", Format: "uuid"},
			"companyId":   {Type: "string", Format: "uuid"},
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"createdAt":   {Type: "string", Format: "date-time"},
			"updatedAt":   {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("CreateOrganizationRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("Project", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":             {Type: "string", Format: "uuid"},
			"organizationId": {Type: "string", Format: "uuid"},
			"name":           {Type: "string"},
			"description":    {Type: "string"},
			"createdAt":      {Type: "string", Format: "date-time"},
			"updatedAt":      {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("CreateProjectRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("Workflow", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":          {Type: "string", Format: "uuid"},
			"projectId":   {Type: "string", Format: "uuid"},
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"config":      {Type: "string", Description: "YAML workflow configuration"},
			"status":      {Type: "string", Enum: []string{"draft", "active", "stopped", "error"}},
			"version":     {Type: "integer"},
			"createdAt":   {Type: "string", Format: "date-time"},
			"updatedAt":   {Type: "string", Format: "date-time"},
			"createdBy":   {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("CreateWorkflowRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"config":      {Type: "string"},
			"projectId":   {Type: "string", Format: "uuid"},
		},
	})

	gen.RegisterComponentSchema("UpdateWorkflowRequest", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"config":      {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("WorkflowVersion", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"version":   {Type: "integer"},
			"config":    {Type: "string"},
			"createdAt": {Type: "string", Format: "date-time"},
			"createdBy": {Type: "string"},
			"comment":   {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("WorkflowStatus", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"workflowId": {Type: "string", Format: "uuid"},
			"status":     {Type: "string"},
			"version":    {Type: "integer"},
			"deployedAt": {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("TriggerRequest", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"input": {Type: "object", AdditionalProperties: &OpenAPISchema{}, Description: "Input data for the workflow execution"},
		},
	})

	gen.RegisterComponentSchema("Execution", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":         {Type: "string", Format: "uuid"},
			"workflowId": {Type: "string", Format: "uuid"},
			"status":     {Type: "string", Enum: []string{"pending", "running", "completed", "failed", "cancelled"}},
			"input":      {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"output":     {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"startedAt":  {Type: "string", Format: "date-time"},
			"finishedAt": {Type: "string", Format: "date-time", Nullable: true},
			"error":      {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("ExecutionStep", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name":       {Type: "string"},
			"status":     {Type: "string"},
			"startedAt":  {Type: "string", Format: "date-time"},
			"finishedAt": {Type: "string", Format: "date-time", Nullable: true},
			"output":     {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"error":      {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("DashboardData", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"totalWorkflows":     {Type: "integer"},
			"activeWorkflows":    {Type: "integer"},
			"totalExecutions":    {Type: "integer"},
			"recentExecutions":   {Type: "array", Items: SchemaRef("Execution")},
			"totalCompanies":     {Type: "integer"},
			"totalOrganizations": {Type: "integer"},
			"totalProjects":      {Type: "integer"},
		},
	})

	gen.RegisterComponentSchema("AuditEntry", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":        {Type: "string"},
			"action":    {Type: "string"},
			"actor":     {Type: "string"},
			"target":    {Type: "string"},
			"details":   {Type: "string"},
			"timestamp": {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("PermissionEntry", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"userId":    {Type: "string"},
			"email":     {Type: "string", Format: "email"},
			"role":      {Type: "string"},
			"grantedAt": {Type: "string", Format: "date-time"},
			"grantedBy": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("ShareRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"email", "role"},
		Properties: map[string]*OpenAPISchema{
			"email": {Type: "string", Format: "email"},
			"role":  {Type: "string", Enum: []string{"viewer", "editor", "admin"}},
		},
	})

	gen.RegisterComponentSchema("LogEntry", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"timestamp": {Type: "string", Format: "date-time"},
			"level":     {Type: "string", Enum: []string{"debug", "info", "warn", "error"}},
			"message":   {Type: "string"},
			"source":    {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("EventEntry", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":        {Type: "string"},
			"type":      {Type: "string"},
			"source":    {Type: "string"},
			"data":      {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"timestamp": {Type: "string", Format: "date-time"},
		},
	})

	// --- IAM ---
	gen.RegisterComponentSchema("IAMProvider", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":        {Type: "string", Format: "uuid"},
			"type":      {Type: "string", Enum: []string{"oidc", "saml", "ldap"}},
			"name":      {Type: "string"},
			"config":    {Type: "object", AdditionalProperties: &OpenAPISchema{}},
			"enabled":   {Type: "boolean"},
			"createdAt": {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("CreateIAMProviderRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"type", "name", "config"},
		Properties: map[string]*OpenAPISchema{
			"type":   {Type: "string", Enum: []string{"oidc", "saml", "ldap"}},
			"name":   {Type: "string"},
			"config": {Type: "object", AdditionalProperties: &OpenAPISchema{}},
		},
	})

	gen.RegisterComponentSchema("IAMMapping", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":         {Type: "string", Format: "uuid"},
			"providerId": {Type: "string", Format: "uuid"},
			"externalId": {Type: "string"},
			"localRole":  {Type: "string"},
			"createdAt":  {Type: "string", Format: "date-time"},
		},
	})

	gen.RegisterComponentSchema("CreateIAMMappingRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"externalId", "localRole"},
		Properties: map[string]*OpenAPISchema{
			"externalId": {Type: "string"},
			"localRole":  {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("ErrorResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"error": {Type: "string"},
		},
	})

	gen.RegisterComponentSchema("SuccessResponse", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"message": {Type: "string"},
		},
	})
}

// --- Operation Schema Mappings ---

func registerAuthOperationSchemas(gen *OpenAPIGenerator) {
	gen.SetOperationSchema("GET", "/api/v1/auth/setup-status", nil, SchemaRef("SetupStatusResponse"))
	gen.SetOperationSchema("POST", "/api/v1/auth/setup", SchemaRef("RegisterRequest"), SchemaRef("AuthResponse"))
	gen.SetOperationSchema("POST", "/api/v1/auth/login", SchemaRef("LoginRequest"), SchemaRef("AuthResponse"))
	gen.SetOperationSchema("POST", "/api/v1/auth/register", SchemaRef("RegisterRequest"), SchemaRef("AuthResponse"))
	gen.SetOperationSchema("POST", "/api/v1/auth/refresh", nil, SchemaRef("AuthResponse"))
	gen.SetOperationSchema("GET", "/api/v1/auth/me", nil, SchemaRef("UserProfile"))
	gen.SetOperationSchema("PUT", "/api/v1/auth/me", SchemaRef("UserProfile"), SchemaRef("UserProfile"))
	gen.SetOperationSchema("GET", "/api/v1/auth/users", nil, SchemaArray(SchemaRef("UserProfile")))
	gen.SetOperationSchema("POST", "/api/v1/auth/users", SchemaRef("CreateUserRequest"), SchemaRef("UserProfile"))
	gen.SetOperationSchema("PUT", "/api/v1/auth/users/{id}/role", SchemaRef("UpdateRoleRequest"), SchemaRef("UserProfile"))
}

func registerEngineOperationSchemas(gen *OpenAPIGenerator) {
	gen.SetOperationSchema("GET", "/api/v1/admin/engine/config", nil, SchemaRef("WorkflowConfig"))
	gen.SetOperationSchema("PUT", "/api/v1/admin/engine/config", SchemaRef("WorkflowConfig"), SchemaRef("WorkflowConfig"))
	gen.SetOperationSchema("GET", "/api/v1/admin/engine/status", nil, SchemaRef("EngineStatus"))
	gen.SetOperationSchema("GET", "/api/v1/admin/engine/modules", nil, SchemaArray(SchemaRef("ModuleTypeInfo")))
	gen.SetOperationSchema("POST", "/api/v1/admin/engine/validate", SchemaRef("WorkflowConfig"), SchemaRef("ValidationResult"))
	gen.SetOperationSchema("POST", "/api/v1/admin/engine/reload", nil, SchemaRef("SuccessResponse"))
}

func registerSchemaOperationSchemas(gen *OpenAPIGenerator) {
	gen.SetOperationSchema("GET", "/api/v1/admin/schemas", nil, &OpenAPISchema{Type: "object", Description: "Workflow JSON schema"})
	gen.SetOperationSchema("GET", "/api/v1/admin/schemas/modules", nil, &OpenAPISchema{
		Type:                 "object",
		AdditionalProperties: SchemaRef("ModuleSchema"),
		Description:          "Map of module type to schema definition",
	})
}

func registerAIOperationSchemas(gen *OpenAPIGenerator) {
	gen.SetOperationSchema("GET", "/api/v1/admin/ai/providers", nil, SchemaArray(SchemaRef("AIProvider")))
	gen.SetOperationSchema("POST", "/api/v1/admin/ai/generate", SchemaRef("AIGenerateRequest"), SchemaRef("AIGenerateResponse"))
	gen.SetOperationSchema("POST", "/api/v1/admin/ai/component", SchemaRef("AIComponentRequest"), SchemaRef("AIComponentResponse"))
	gen.SetOperationSchema("POST", "/api/v1/admin/ai/suggest", SchemaRef("AISuggestRequest"), SchemaArray(SchemaRef("AISuggestion")))
	gen.SetOperationSchema("POST", "/api/v1/admin/ai/deploy", SchemaRef("AIDeployRequest"), SchemaRef("AIGenerateResponse"))
	gen.SetOperationSchema("POST", "/api/v1/admin/ai/deploy/component", SchemaRef("AIComponentRequest"), SchemaRef("ComponentInfo"))
}

func registerComponentOperationSchemas(gen *OpenAPIGenerator) {
	gen.SetOperationSchema("GET", "/api/v1/admin/components", nil, SchemaArray(SchemaRef("ComponentInfo")))
	gen.SetOperationSchema("POST", "/api/v1/admin/components", SchemaRef("CreateComponentRequest"), SchemaRef("ComponentInfo"))
	gen.SetOperationSchema("GET", "/api/v1/admin/components/{id}", nil, SchemaRef("ComponentDetail"))
	gen.SetOperationSchema("PUT", "/api/v1/admin/components/{id}", SchemaRef("UpdateComponentRequest"), SchemaRef("ComponentInfo"))
}

func registerV1OperationSchemas(gen *OpenAPIGenerator) {
	// Dashboard
	gen.SetOperationSchema("GET", "/api/v1/admin/dashboard", nil, SchemaRef("DashboardData"))

	// Companies
	gen.SetOperationSchema("GET", "/api/v1/admin/companies", nil, SchemaArray(SchemaRef("Company")))
	gen.SetOperationSchema("POST", "/api/v1/admin/companies", SchemaRef("CreateCompanyRequest"), SchemaRef("Company"))
	gen.SetOperationSchema("GET", "/api/v1/admin/companies/{id}", nil, SchemaRef("Company"))
	gen.SetOperationSchema("GET", "/api/v1/admin/companies/{id}/organizations", nil, SchemaArray(SchemaRef("Organization")))

	// Organizations
	gen.SetOperationSchema("POST", "/api/v1/admin/companies/{id}/organizations", SchemaRef("CreateOrganizationRequest"), SchemaRef("Organization"))
	gen.SetOperationSchema("GET", "/api/v1/admin/organizations/{id}/projects", nil, SchemaArray(SchemaRef("Project")))
	gen.SetOperationSchema("POST", "/api/v1/admin/organizations/{id}/projects", SchemaRef("CreateProjectRequest"), SchemaRef("Project"))

	// Projects
	gen.SetOperationSchema("GET", "/api/v1/admin/projects/{id}/workflows", nil, SchemaArray(SchemaRef("Workflow")))
	gen.SetOperationSchema("POST", "/api/v1/admin/projects/{id}/workflows", SchemaRef("CreateWorkflowRequest"), SchemaRef("Workflow"))

	// Workflows
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows", nil, SchemaArray(SchemaRef("Workflow")))
	gen.SetOperationSchema("POST", "/api/v1/admin/workflows", SchemaRef("CreateWorkflowRequest"), SchemaRef("Workflow"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}", nil, SchemaRef("Workflow"))
	gen.SetOperationSchema("PUT", "/api/v1/admin/workflows/{id}", SchemaRef("UpdateWorkflowRequest"), SchemaRef("Workflow"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/versions", nil, SchemaArray(SchemaRef("WorkflowVersion")))
	gen.SetOperationSchema("POST", "/api/v1/admin/workflows/{id}/deploy", nil, SchemaRef("WorkflowStatus"))
	gen.SetOperationSchema("POST", "/api/v1/admin/workflows/{id}/stop", nil, SchemaRef("WorkflowStatus"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/status", nil, SchemaRef("WorkflowStatus"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/dashboard", nil, SchemaRef("DashboardData"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/executions", nil, SchemaArray(SchemaRef("Execution")))
	gen.SetOperationSchema("POST", "/api/v1/admin/workflows/{id}/trigger", SchemaRef("TriggerRequest"), SchemaRef("Execution"))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/logs", nil, SchemaArray(SchemaRef("LogEntry")))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/events", nil, SchemaArray(SchemaRef("EventEntry")))
	gen.SetOperationSchema("GET", "/api/v1/admin/workflows/{id}/permissions", nil, SchemaArray(SchemaRef("PermissionEntry")))
	gen.SetOperationSchema("POST", "/api/v1/admin/workflows/{id}/permissions", SchemaRef("ShareRequest"), SchemaRef("PermissionEntry"))

	// Executions
	gen.SetOperationSchema("GET", "/api/v1/admin/executions/{id}", nil, SchemaRef("Execution"))
	gen.SetOperationSchema("GET", "/api/v1/admin/executions/{id}/steps", nil, SchemaArray(SchemaRef("ExecutionStep")))
	gen.SetOperationSchema("POST", "/api/v1/admin/executions/{id}/cancel", nil, SchemaRef("Execution"))

	// Audit
	gen.SetOperationSchema("GET", "/api/v1/admin/audit", nil, SchemaArray(SchemaRef("AuditEntry")))

	// IAM
	gen.SetOperationSchema("GET", "/api/v1/admin/iam/providers/{id}", nil, SchemaRef("IAMProvider"))
	gen.SetOperationSchema("POST", "/api/v1/admin/iam/providers", SchemaRef("CreateIAMProviderRequest"), SchemaRef("IAMProvider"))
	gen.SetOperationSchema("PUT", "/api/v1/admin/iam/providers/{id}", SchemaRef("CreateIAMProviderRequest"), SchemaRef("IAMProvider"))
	gen.SetOperationSchema("POST", "/api/v1/admin/iam/providers/{id}/test", nil, SchemaRef("SuccessResponse"))
	gen.SetOperationSchema("GET", "/api/v1/admin/iam/providers/{id}/mappings", nil, SchemaArray(SchemaRef("IAMMapping")))
	gen.SetOperationSchema("POST", "/api/v1/admin/iam/providers/{id}/mappings", SchemaRef("CreateIAMMappingRequest"), SchemaRef("IAMMapping"))
}
