package dynamic

import (
	"fmt"
	"strings"
	"sync"
)

// FieldType describes the expected type of a field in a contract.
type FieldType string

const (
	FieldTypeString FieldType = "string"
	FieldTypeInt    FieldType = "int"
	FieldTypeBool   FieldType = "bool"
	FieldTypeFloat  FieldType = "float"
	FieldTypeMap    FieldType = "map"
	FieldTypeSlice  FieldType = "slice"
	FieldTypeAny    FieldType = "any"
)

// FieldSpec describes a single field in a contract.
type FieldSpec struct {
	Type        FieldType `json:"type"`
	Description string    `json:"description,omitempty"`
	Default     any       `json:"default,omitempty"`
}

// FieldContract declares the input/output field requirements for a dynamic component.
type FieldContract struct {
	RequiredInputs map[string]FieldSpec `json:"required_inputs,omitempty"`
	OptionalInputs map[string]FieldSpec `json:"optional_inputs,omitempty"`
	Outputs        map[string]FieldSpec `json:"outputs,omitempty"`
}

// NewFieldContract creates an empty FieldContract.
func NewFieldContract() *FieldContract {
	return &FieldContract{
		RequiredInputs: make(map[string]FieldSpec),
		OptionalInputs: make(map[string]FieldSpec),
		Outputs:        make(map[string]FieldSpec),
	}
}

// ValidateInputs checks params against the given contract. It returns an error
// describing all missing required fields and type mismatches found.
func ValidateInputs(contract *FieldContract, params map[string]any) error {
	if contract == nil {
		return nil
	}

	var errs []string

	// Check required inputs
	for name, spec := range contract.RequiredInputs {
		val, ok := params[name]
		if !ok || val == nil {
			errs = append(errs, fmt.Sprintf("missing required field %q", name))
			continue
		}
		if err := checkType(name, val, spec.Type); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// Check optional inputs that are present
	for name, spec := range contract.OptionalInputs {
		val, ok := params[name]
		if !ok || val == nil {
			continue
		}
		if err := checkType(name, val, spec.Type); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("contract validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ApplyDefaults fills in default values from the contract for any missing optional
// fields in params. It returns a new map with defaults applied (does not mutate the original).
func ApplyDefaults(contract *FieldContract, params map[string]any) map[string]any {
	if contract == nil {
		return params
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		result[k] = v
	}
	for name, spec := range contract.OptionalInputs {
		if _, ok := result[name]; !ok && spec.Default != nil {
			result[name] = spec.Default
		}
	}
	return result
}

// checkType verifies that val is compatible with the declared FieldType.
func checkType(name string, val any, ft FieldType) error {
	if ft == FieldTypeAny {
		return nil
	}

	switch ft {
	case FieldTypeString:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", name, val)
		}
	case FieldTypeInt:
		switch val.(type) {
		case int, int8, int16, int32, int64, float64:
			// float64 accepted because JSON unmarshaling produces float64 for numbers
		default:
			return fmt.Errorf("field %q: expected int, got %T", name, val)
		}
	case FieldTypeFloat:
		switch val.(type) {
		case float32, float64, int, int64:
		default:
			return fmt.Errorf("field %q: expected float, got %T", name, val)
		}
	case FieldTypeBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected bool, got %T", name, val)
		}
	case FieldTypeMap:
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected map, got %T", name, val)
		}
	case FieldTypeSlice:
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("field %q: expected slice, got %T", name, val)
		}
	}
	return nil
}

// ContractRegistry stores field contracts indexed by component name.
// It is safe for concurrent access.
type ContractRegistry struct {
	mu        sync.RWMutex
	contracts map[string]*FieldContract
}

// NewContractRegistry creates an empty ContractRegistry.
func NewContractRegistry() *ContractRegistry {
	return &ContractRegistry{
		contracts: make(map[string]*FieldContract),
	}
}

// Register stores a contract for the given component name.
func (cr *ContractRegistry) Register(name string, contract *FieldContract) {
	if contract == nil {
		return
	}
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.contracts[name] = contract
}

// Get retrieves the contract for a component, if one exists.
func (cr *ContractRegistry) Get(name string) (*FieldContract, bool) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	c, ok := cr.contracts[name]
	return c, ok
}

// Unregister removes a contract for the given component name.
func (cr *ContractRegistry) Unregister(name string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	delete(cr.contracts, name)
}

// List returns all registered component names.
func (cr *ContractRegistry) List() []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	names := make([]string, 0, len(cr.contracts))
	for name := range cr.contracts {
		names = append(names, name)
	}
	return names
}
