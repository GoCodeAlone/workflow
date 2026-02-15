package schema

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// EventSchema describes the expected shape of event data for a specific event type and version.
type EventSchema struct {
	Type        string              `json:"type" yaml:"type"`                             // e.g., "order.created"
	Version     string              `json:"version" yaml:"version"`                       // semver
	Description string              `json:"description" yaml:"description"`               // human-readable description
	Fields      map[string]FieldDef `json:"fields" yaml:"fields"`                         // field name -> definition
	Required    []string            `json:"required" yaml:"required"`                     // required field names
	Examples    []map[string]any    `json:"examples,omitempty" yaml:"examples,omitempty"` // example data payloads
}

// FieldDef describes a single field within an event data payload.
type FieldDef struct {
	Type        string   `json:"type" yaml:"type"`                         // string, number, boolean, object, array
	Description string   `json:"description" yaml:"description"`           // human-readable description
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`     // allowed values (if constrained)
	Format      string   `json:"format,omitempty" yaml:"format,omitempty"` // email, uri, date-time, uuid
}

// EventSchemaRegistry stores and validates event schemas keyed by "type:version".
type EventSchemaRegistry struct {
	mu      sync.RWMutex
	schemas map[string]*EventSchema // key: "type:version"
}

// EventValidationError represents a single event data validation failure.
type EventValidationError struct {
	Field   string
	Message string
}

func (e *EventValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("field %q: %s", e.Field, e.Message)
	}
	return e.Message
}

// EventValidationErrors collects multiple event validation failures.
type EventValidationErrors []*EventValidationError

func (ve EventValidationErrors) Error() string {
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("event validation failed with %d error(s):\n  - %s",
		len(ve), strings.Join(msgs, "\n  - "))
}

// Pre-compiled format validation regexes.
var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	uuidRegex     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	dateTimeRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	uriRegex      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`)
)

// NewEventSchemaRegistry creates a new empty event schema registry.
func NewEventSchemaRegistry() *EventSchemaRegistry {
	return &EventSchemaRegistry{
		schemas: make(map[string]*EventSchema),
	}
}

// schemaKey returns the composite key for an event schema.
func schemaKey(eventType, version string) string {
	return eventType + ":" + version
}

// Register validates and stores an event schema. It returns an error if a
// schema with the same type and version is already registered, or if the
// schema is missing required metadata.
func (r *EventSchemaRegistry) Register(schema *EventSchema) error {
	if schema == nil {
		return fmt.Errorf("event schema must not be nil")
	}
	if schema.Type == "" {
		return fmt.Errorf("event schema type must not be empty")
	}
	if schema.Version == "" {
		return fmt.Errorf("event schema version must not be empty")
	}

	// Validate that required fields reference defined fields
	for _, req := range schema.Required {
		if _, ok := schema.Fields[req]; !ok {
			return fmt.Errorf("required field %q is not defined in fields", req)
		}
	}

	// Validate field definitions
	validTypes := map[string]bool{
		"string": true, "number": true, "boolean": true, "object": true, "array": true,
	}
	for name, field := range schema.Fields {
		if !validTypes[field.Type] {
			return fmt.Errorf("field %q has invalid type %q (must be string, number, boolean, object, or array)", name, field.Type)
		}
	}

	key := schemaKey(schema.Type, schema.Version)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.schemas[key]; exists {
		return fmt.Errorf("event schema %q version %q is already registered", schema.Type, schema.Version)
	}

	r.schemas[key] = schema
	return nil
}

// Get retrieves an event schema by type and version. Returns nil and false if
// not found.
func (r *EventSchemaRegistry) Get(eventType, version string) (*EventSchema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.schemas[schemaKey(eventType, version)]
	return s, ok
}

// GetLatest returns the latest version of the schema for a given event type,
// determined by lexicographic comparison of semantic version strings. Returns
// nil and false if no schema exists for the type.
func (r *EventSchemaRegistry) GetLatest(eventType string) (*EventSchema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var latest *EventSchema
	for _, s := range r.schemas {
		if s.Type == eventType {
			if latest == nil || compareSemver(s.Version, latest.Version) > 0 {
				latest = s
			}
		}
	}
	if latest == nil {
		return nil, false
	}
	return latest, true
}

// Validate validates the given data map against the latest schema for the
// specified event type. Returns nil if valid, or an EventValidationErrors
// containing all failures.
func (r *EventSchemaRegistry) Validate(eventType string, data map[string]any) error {
	schema, ok := r.GetLatest(eventType)
	if !ok {
		return fmt.Errorf("no schema registered for event type %q", eventType)
	}
	return validateEventData(schema, data)
}

// ValidateVersion validates data against a specific version of the schema.
func (r *EventSchemaRegistry) ValidateVersion(eventType, version string, data map[string]any) error {
	schema, ok := r.Get(eventType, version)
	if !ok {
		return fmt.Errorf("no schema registered for event type %q version %q", eventType, version)
	}
	return validateEventData(schema, data)
}

// List returns all registered schemas as a slice, sorted by type then version.
func (r *EventSchemaRegistry) List() []*EventSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*EventSchema, 0, len(r.schemas))
	for _, s := range r.schemas {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// ListTypes returns all unique registered event types, sorted alphabetically.
func (r *EventSchemaRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeSet := make(map[string]bool)
	for _, s := range r.schemas {
		typeSet[s.Type] = true
	}

	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// Remove deletes a schema identified by event type and version. Returns true
// if the schema was found and removed, false otherwise.
func (r *EventSchemaRegistry) Remove(eventType, version string) bool {
	key := schemaKey(eventType, version)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.schemas[key]; !exists {
		return false
	}
	delete(r.schemas, key)
	return true
}

// validateEventData performs full validation of data against a schema.
func validateEventData(schema *EventSchema, data map[string]any) error {
	var errs EventValidationErrors

	// Check required fields
	for _, req := range schema.Required {
		if _, ok := data[req]; !ok {
			errs = append(errs, &EventValidationError{
				Field:   req,
				Message: "required field is missing",
			})
		}
	}

	// Validate each field present in data that has a schema definition
	for name, value := range data {
		fieldDef, ok := schema.Fields[name]
		if !ok {
			// Fields not in the schema are allowed (open content model)
			continue
		}

		// Type check
		if err := checkFieldType(name, fieldDef.Type, value); err != nil {
			errs = append(errs, err)
			continue // skip further checks if type is wrong
		}

		// Enum check
		if len(fieldDef.Enum) > 0 {
			if err := checkEnum(name, fieldDef.Enum, value); err != nil {
				errs = append(errs, err)
			}
		}

		// Format check
		if fieldDef.Format != "" {
			if err := checkFormat(name, fieldDef.Format, value); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// checkFieldType validates that value matches the expected schema type.
func checkFieldType(field, expectedType string, value any) *EventValidationError {
	if value == nil {
		return nil // nil is accepted for any type (absence is handled by required check)
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return &EventValidationError{
				Field:   field,
				Message: fmt.Sprintf("expected type string, got %s", reflect.TypeOf(value).Kind()),
			}
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			// valid numeric types
		default:
			return &EventValidationError{
				Field:   field,
				Message: fmt.Sprintf("expected type number, got %s", reflect.TypeOf(value).Kind()),
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return &EventValidationError{
				Field:   field,
				Message: fmt.Sprintf("expected type boolean, got %s", reflect.TypeOf(value).Kind()),
			}
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return &EventValidationError{
				Field:   field,
				Message: fmt.Sprintf("expected type object (map), got %s", reflect.TypeOf(value).Kind()),
			}
		}
	case "array":
		v := reflect.ValueOf(value)
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return &EventValidationError{
				Field:   field,
				Message: fmt.Sprintf("expected type array (slice), got %s", v.Kind()),
			}
		}
	}
	return nil
}

// checkEnum validates that value is one of the allowed enum values.
func checkEnum(field string, allowed []string, value any) *EventValidationError {
	str, ok := value.(string)
	if !ok {
		return &EventValidationError{
			Field:   field,
			Message: "enum validation requires a string value",
		}
	}
	for _, a := range allowed {
		if str == a {
			return nil
		}
	}
	return &EventValidationError{
		Field:   field,
		Message: fmt.Sprintf("value %q is not in allowed enum values %v", str, allowed),
	}
}

// checkFormat validates that a string value matches the expected format.
func checkFormat(field, format string, value any) *EventValidationError {
	str, ok := value.(string)
	if !ok {
		return nil // format checks only apply to strings
	}

	var valid bool
	switch format {
	case "email":
		valid = emailRegex.MatchString(str)
	case "uuid":
		valid = uuidRegex.MatchString(str)
	case "date-time":
		valid = dateTimeRegex.MatchString(str)
	case "uri":
		valid = uriRegex.MatchString(str)
	default:
		return nil // unknown formats are silently accepted
	}

	if !valid {
		return &EventValidationError{
			Field:   field,
			Message: fmt.Sprintf("value %q does not match format %q", str, format),
		}
	}
	return nil
}

// compareSemver compares two semantic version strings. It returns:
//
//	-1 if a < b, 0 if a == b, 1 if a > b
//
// Versions are expected in "major.minor.patch" format. Non-parseable versions
// fall back to lexicographic comparison.
func compareSemver(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			_, _ = fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			_, _ = fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum < bNum {
			return -1
		}
		if aNum > bNum {
			return 1
		}
	}
	return 0
}
