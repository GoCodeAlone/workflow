package schema

import "sort"

// StepOutputDef describes a single output key produced by a pipeline step.
type StepOutputDef struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// StepSchema describes the full schema for a pipeline step type,
// including config fields, outputs, and context keys the step reads.
type StepSchema struct {
	Type         string           `json:"type"`
	Plugin       string           `json:"plugin,omitempty"`
	Description  string           `json:"description"`
	ConfigFields []ConfigFieldDef `json:"configFields"`
	Outputs      []StepOutputDef  `json:"outputs,omitempty"`
	ReadKeys     []string         `json:"readKeys,omitempty"` // template keys this step typically reads (e.g. ".body", ".current")
}

// StepSchemaRegistry holds all known step configuration schemas.
type StepSchemaRegistry struct {
	schemas map[string]*StepSchema
}

// NewStepSchemaRegistry creates a new registry with all built-in step schemas pre-registered.
func NewStepSchemaRegistry() *StepSchemaRegistry {
	r := &StepSchemaRegistry{schemas: make(map[string]*StepSchema)}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a step schema.
func (r *StepSchemaRegistry) Register(s *StepSchema) {
	r.schemas[s.Type] = s
}

// Unregister removes a step schema by type.
func (r *StepSchemaRegistry) Unregister(stepType string) {
	delete(r.schemas, stepType)
}

// Get returns the schema for a step type, or nil if not found.
func (r *StepSchemaRegistry) Get(stepType string) *StepSchema {
	return r.schemas[stepType]
}

// All returns all registered schemas as a slice.
func (r *StepSchemaRegistry) All() []*StepSchema {
	out := make([]*StepSchema, 0, len(r.schemas))
	for _, s := range r.schemas {
		out = append(out, s)
	}
	return out
}

// AllMap returns all registered schemas as a map keyed by step type.
func (r *StepSchemaRegistry) AllMap() map[string]*StepSchema {
	out := make(map[string]*StepSchema, len(r.schemas))
	for k, v := range r.schemas {
		out[k] = v
	}
	return out
}

// Types returns a sorted list of all registered step type identifiers.
func (r *StepSchemaRegistry) Types() []string {
	types := make([]string, 0, len(r.schemas))
	for t := range r.schemas {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
