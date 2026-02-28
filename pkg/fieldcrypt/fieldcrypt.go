package fieldcrypt

// FieldClassification defines the sensitivity level.
type FieldClassification string

const (
	ClassPII FieldClassification = "pii"
	ClassPHI FieldClassification = "phi"
)

// LogBehavior defines how a field appears in logs.
type LogBehavior string

const (
	LogMask   LogBehavior = "mask"
	LogRedact LogBehavior = "redact"
	LogHash   LogBehavior = "hash"
	LogAllow  LogBehavior = "allow"
)

// ProtectedField defines a field that requires encryption/masking.
type ProtectedField struct {
	Name           string              `yaml:"name"`
	Classification FieldClassification `yaml:"classification"`
	Encryption     bool                `yaml:"encryption"`
	LogBehavior    LogBehavior         `yaml:"log_behavior"`
	MaskPattern    string              `yaml:"mask_pattern"`
}

// Registry holds the set of protected fields for lookup.
type Registry struct {
	fields map[string]ProtectedField
}

// NewRegistry creates a Registry from a slice of ProtectedField definitions.
func NewRegistry(fields []ProtectedField) *Registry {
	m := make(map[string]ProtectedField, len(fields))
	for _, f := range fields {
		m[f.Name] = f
	}
	return &Registry{fields: m}
}

// IsProtected returns true if the given field name is in the registry.
func (r *Registry) IsProtected(fieldName string) bool {
	_, ok := r.fields[fieldName]
	return ok
}

// GetField returns the ProtectedField definition for the given name.
func (r *Registry) GetField(fieldName string) (*ProtectedField, bool) {
	f, ok := r.fields[fieldName]
	if !ok {
		return nil, false
	}
	return &f, true
}

// Len returns the number of registered protected fields.
func (r *Registry) Len() int {
	return len(r.fields)
}

// ProtectedFields returns all registered protected fields.
func (r *Registry) ProtectedFields() []ProtectedField {
	out := make([]ProtectedField, 0, len(r.fields))
	for _, f := range r.fields {
		out = append(out, f)
	}
	return out
}
