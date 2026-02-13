package module

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FieldMapping provides configurable field name resolution with fallback chains.
// Each logical field name maps to an ordered list of actual field names to try
// when reading from a data map. This eliminates hard-coded field references and
// allows YAML configuration to remap fields without code changes.
type FieldMapping struct {
	fields map[string][]string // logical name -> ordered actual field names
}

// NewFieldMapping creates a FieldMapping with no mappings defined.
func NewFieldMapping() *FieldMapping {
	return &FieldMapping{
		fields: make(map[string][]string),
	}
}

// Set defines the actual field name(s) for a logical field. The first name is the
// "primary" used for writes; all names are tried in order for reads.
func (fm *FieldMapping) Set(logical string, actual ...string) {
	if len(actual) == 0 {
		return
	}
	fm.fields[logical] = actual
}

// Resolve looks up a logical field name in data, trying each actual name in order.
// Returns the value and true if found, or nil and false if no actual name matched.
func (fm *FieldMapping) Resolve(data map[string]interface{}, logical string) (interface{}, bool) {
	names := fm.fieldNames(logical)
	for _, name := range names {
		if val, ok := data[name]; ok {
			return val, true
		}
	}
	return nil, false
}

// ResolveString resolves a logical field name to a string value.
// Returns empty string if not found or not a string.
func (fm *FieldMapping) ResolveString(data map[string]interface{}, logical string) string {
	val, ok := fm.Resolve(data, logical)
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

// ResolveSlice resolves a logical field name to a []interface{} value.
// Returns nil if not found or not a slice.
func (fm *FieldMapping) ResolveSlice(data map[string]interface{}, logical string) []interface{} {
	val, ok := fm.Resolve(data, logical)
	if !ok {
		return nil
	}
	s, _ := val.([]interface{})
	return s
}

// SetValue sets a value in data using the primary (first) field name for a logical field.
func (fm *FieldMapping) SetValue(data map[string]interface{}, logical string, value interface{}) {
	name := fm.Primary(logical)
	data[name] = value
}

// Primary returns the primary (first) field name for a logical field.
// If no mapping is defined, returns the logical name itself.
func (fm *FieldMapping) Primary(logical string) string {
	if names, ok := fm.fields[logical]; ok && len(names) > 0 {
		return names[0]
	}
	return logical
}

// Has returns true if a mapping is defined for the given logical name.
func (fm *FieldMapping) Has(logical string) bool {
	_, ok := fm.fields[logical]
	return ok
}

// fieldNames returns the actual field names for a logical name.
// If no mapping exists, returns a slice containing just the logical name.
func (fm *FieldMapping) fieldNames(logical string) []string {
	if names, ok := fm.fields[logical]; ok && len(names) > 0 {
		return names
	}
	return []string{logical}
}

// Merge copies all mappings from other into fm. Existing mappings are overwritten.
func (fm *FieldMapping) Merge(other *FieldMapping) {
	if other == nil {
		return
	}
	for k, v := range other.fields {
		fm.fields[k] = v
	}
}

// Clone returns a deep copy of the field mapping.
func (fm *FieldMapping) Clone() *FieldMapping {
	clone := NewFieldMapping()
	for k, v := range fm.fields {
		names := make([]string, len(v))
		copy(names, v)
		clone.fields[k] = names
	}
	return clone
}

// String returns a human-readable representation of the field mapping.
func (fm *FieldMapping) String() string {
	if len(fm.fields) == 0 {
		return "FieldMapping{}"
	}
	parts := make([]string, 0, len(fm.fields))
	for k, v := range fm.fields {
		parts = append(parts, fmt.Sprintf("%s:[%s]", k, strings.Join(v, ",")))
	}
	return fmt.Sprintf("FieldMapping{%s}", strings.Join(parts, ", "))
}

// FieldMappingFromConfig parses a field mapping from a config map.
// The config format is: {"logicalName": ["actual1", "actual2"]} or {"logicalName": "actual1"}
func FieldMappingFromConfig(cfg map[string]interface{}) *FieldMapping {
	fm := NewFieldMapping()
	if cfg == nil {
		return fm
	}
	for key, val := range cfg {
		switch v := val.(type) {
		case string:
			fm.Set(key, v)
		case []interface{}:
			names := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok {
					names = append(names, s)
				}
			}
			if len(names) > 0 {
				fm.Set(key, names...)
			}
		case []string:
			fm.Set(key, v...)
		}
	}
	return fm
}

// MarshalJSON implements json.Marshaler for FieldMapping.
func (fm *FieldMapping) MarshalJSON() ([]byte, error) {
	return json.Marshal(fm.fields)
}

// UnmarshalJSON implements json.Unmarshaler for FieldMapping.
func (fm *FieldMapping) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &fm.fields)
}

// --- Default field mapping presets ---

// DefaultRESTFieldMapping returns the default field mapping for REST API handlers.
// This matches the existing hard-coded behavior for full backwards compatibility.
func DefaultRESTFieldMapping() *FieldMapping {
	fm := NewFieldMapping()
	fm.Set("state", "state")
	fm.Set("lastUpdate", "lastUpdate")
	fm.Set("body", "body", "Body", "content", "message")
	fm.Set("riskLevel", "riskLevel")
	fm.Set("tags", "tags")
	fm.Set("messages", "messages")
	fm.Set("programId", "programId")
	fm.Set("programName", "programName")
	fm.Set("createdAt", "createdAt")
	fm.Set("responderId", "responderId")
	fm.Set("id", "id")
	fm.Set("userId", "userId")
	fm.Set("direction", "direction")
	fm.Set("from", "from")
	return fm
}

// DefaultTransitionMap returns the default sub-action to state machine transition mapping.
func DefaultTransitionMap() map[string]string {
	return map[string]string{
		"assign":    "assign_responder",
		"transfer":  "transfer_to_responder",
		"escalate":  "escalate_to_medical",
		"wrap-up":   "begin_wrap_up",
		"close":     "close_from_active",
		"follow-up": "schedule_follow_up",
		"survey":    "send_entry_survey",
	}
}

// DefaultSummaryFields returns the default list of fields to include in summary responses.
func DefaultSummaryFields() []string {
	return []string{
		"programId", "subProgram", "keyword", "responderId",
		"tags", "riskLevel", "summary", "aiSummary", "duration", "messageCount",
	}
}
