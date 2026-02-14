package schema

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// ValidationError represents a single validation failure with the path to the
// offending field and a human-readable message.
type ValidationError struct {
	Path    string // dot-separated path (e.g. "modules[0].type")
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

// ValidationErrors collects multiple validation failures.
type ValidationErrors []*ValidationError

func (ve ValidationErrors) Error() string {
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("config validation failed with %d error(s):\n  - %s",
		len(ve), strings.Join(msgs, "\n  - "))
}

// ValidationOption configures validation behaviour.
type ValidationOption func(*validationOpts)

type validationOpts struct {
	extraModuleTypes      []string
	extraWorkflowTypes    []string
	extraTriggerTypes     []string
	allowEmptyModules     bool
	skipWorkflowTypeCheck bool
	skipTriggerTypeCheck  bool
}

// WithExtraModuleTypes registers additional module types as valid (e.g. from
// custom factories registered via AddModuleType).
func WithExtraModuleTypes(types ...string) ValidationOption {
	return func(o *validationOpts) {
		o.extraModuleTypes = append(o.extraModuleTypes, types...)
	}
}

// WithExtraWorkflowTypes registers additional workflow handler types as valid.
func WithExtraWorkflowTypes(types ...string) ValidationOption {
	return func(o *validationOpts) {
		o.extraWorkflowTypes = append(o.extraWorkflowTypes, types...)
	}
}

// WithExtraTriggerTypes registers additional trigger types as valid.
func WithExtraTriggerTypes(types ...string) ValidationOption {
	return func(o *validationOpts) {
		o.extraTriggerTypes = append(o.extraTriggerTypes, types...)
	}
}

// WithAllowEmptyModules disables the "at least one module" requirement.
func WithAllowEmptyModules() ValidationOption {
	return func(o *validationOpts) {
		o.allowEmptyModules = true
	}
}

// WithSkipWorkflowTypeCheck disables validation of workflow section keys.
// Useful when the engine resolves workflow types dynamically via handlers.
func WithSkipWorkflowTypeCheck() ValidationOption {
	return func(o *validationOpts) {
		o.skipWorkflowTypeCheck = true
	}
}

// WithSkipTriggerTypeCheck disables validation of trigger section keys.
// Useful when the engine resolves trigger types dynamically.
func WithSkipTriggerTypeCheck() ValidationOption {
	return func(o *validationOpts) {
		o.skipTriggerTypeCheck = true
	}
}

// ValidateConfig validates a parsed WorkflowConfig, returning all detected
// problems. Returns nil if the config is valid.
func ValidateConfig(cfg *config.WorkflowConfig, opts ...ValidationOption) error {
	var o validationOpts
	for _, fn := range opts {
		fn(&o)
	}

	var errs ValidationErrors

	// modules is required and must be non-empty (unless explicitly allowed)
	if len(cfg.Modules) == 0 && !o.allowEmptyModules {
		errs = append(errs, &ValidationError{
			Path:    "modules",
			Message: "at least one module is required",
		})
	}

	knownTypes := makeSet(KnownModuleTypes())
	for _, t := range o.extraModuleTypes {
		knownTypes[t] = true
	}
	seenNames := make(map[string]int) // name -> index of first occurrence

	for i, mod := range cfg.Modules {
		prefix := fmt.Sprintf("modules[%d]", i)

		// name is required
		if mod.Name == "" {
			errs = append(errs, &ValidationError{
				Path:    prefix + ".name",
				Message: "module name is required",
			})
		} else {
			if firstIdx, exists := seenNames[mod.Name]; exists {
				errs = append(errs, &ValidationError{
					Path:    prefix + ".name",
					Message: fmt.Sprintf("duplicate module name %q (first defined at modules[%d])", mod.Name, firstIdx),
				})
			} else {
				seenNames[mod.Name] = i
			}
		}

		// type is required and must be known
		if mod.Type == "" {
			errs = append(errs, &ValidationError{
				Path:    prefix + ".type",
				Message: "module type is required",
			})
		} else if !knownTypes[mod.Type] {
			errs = append(errs, &ValidationError{
				Path:    prefix + ".type",
				Message: fmt.Sprintf("unknown module type %q", mod.Type),
			})
		}

		// validate dependsOn references
		for j, dep := range mod.DependsOn {
			if dep == "" {
				errs = append(errs, &ValidationError{
					Path:    fmt.Sprintf("%s.dependsOn[%d]", prefix, j),
					Message: "dependency name must not be empty",
				})
			}
		}
	}

	// Cross-validate dependsOn references point to defined module names.
	// We do this in a second pass so all names are collected first.
	for i, mod := range cfg.Modules {
		prefix := fmt.Sprintf("modules[%d]", i)
		for j, dep := range mod.DependsOn {
			if _, exists := seenNames[dep]; !exists && dep != "" {
				errs = append(errs, &ValidationError{
					Path:    fmt.Sprintf("%s.dependsOn[%d]", prefix, j),
					Message: fmt.Sprintf("depends on undefined module %q", dep),
				})
			}
		}
	}

	// Validate module-type-specific required config fields
	for i, mod := range cfg.Modules {
		prefix := fmt.Sprintf("modules[%d]", i)
		validateModuleConfig(mod, prefix, &errs)
	}

	// Validate workflow section keys
	if !o.skipWorkflowTypeCheck {
		knownWorkflows := makeSet(KnownWorkflowTypes())
		for _, t := range o.extraWorkflowTypes {
			knownWorkflows[t] = true
		}
		for wfType := range cfg.Workflows {
			if !knownWorkflows[wfType] {
				errs = append(errs, &ValidationError{
					Path:    fmt.Sprintf("workflows.%s", wfType),
					Message: fmt.Sprintf("unknown workflow type %q", wfType),
				})
			}
		}
	}

	// Validate trigger section keys
	if !o.skipTriggerTypeCheck {
		knownTriggers := makeSet(KnownTriggerTypes())
		for _, t := range o.extraTriggerTypes {
			knownTriggers[t] = true
		}
		for trigType := range cfg.Triggers {
			if !knownTriggers[trigType] {
				errs = append(errs, &ValidationError{
					Path:    fmt.Sprintf("triggers.%s", trigType),
					Message: fmt.Sprintf("unknown trigger type %q", trigType),
				})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// schemaRegistry is used by validation for schema-driven config checks.
var schemaRegistry = NewModuleSchemaRegistry()

// validateModuleConfig checks type-specific required configuration using the
// module schema registry. For modules with a schema, required fields are
// validated automatically. Additional type-specific checks are preserved.
func validateModuleConfig(mod config.ModuleConfig, prefix string, errs *ValidationErrors) {
	// Schema-driven validation for required fields
	s := schemaRegistry.Get(mod.Type)
	if s != nil {
		for _, field := range s.ConfigFields {
			if !field.Required {
				continue
			}
			fieldPath := prefix + ".config." + field.Key
			if mod.Config == nil {
				*errs = append(*errs, &ValidationError{
					Path:    fieldPath,
					Message: fmt.Sprintf("required config field %q is missing (no config section)", field.Key),
				})
				continue
			}
			v, ok := mod.Config[field.Key]
			if !ok {
				*errs = append(*errs, &ValidationError{
					Path:    fieldPath,
					Message: fmt.Sprintf("required config field %q is missing", field.Key),
				})
				continue
			}
			// Check non-empty for string fields
			if field.Type == FieldTypeString || field.Type == FieldTypeDuration || field.Type == FieldTypeSelect {
				if str, ok := v.(string); ok && str == "" {
					*errs = append(*errs, &ValidationError{
						Path:    fieldPath,
						Message: fmt.Sprintf("required config field %q must be a non-empty string", field.Key),
					})
				}
			}
		}
	}

	// Additional type-specific structural checks beyond simple required fields
	switch mod.Type {
	case "messaging.kafka":
		if mod.Config != nil {
			if brokers, ok := mod.Config["brokers"]; ok {
				if arr, ok := brokers.([]any); ok && len(arr) == 0 {
					*errs = append(*errs, &ValidationError{
						Path:    prefix + ".config.brokers",
						Message: "brokers list must not be empty",
					})
				}
			}
		}
	case "http.simple_proxy":
		if mod.Config == nil {
			break
		}
		if targets, ok := mod.Config["targets"]; ok {
			if m, ok := targets.(map[string]any); ok {
				for k, v := range m {
					if _, ok := v.(string); !ok {
						*errs = append(*errs, &ValidationError{
							Path:    fmt.Sprintf("%s.config.targets.%s", prefix, k),
							Message: "proxy target must be a string URL",
						})
					}
				}
			}
		}
	}
}

// requireStringConfig checks that a key exists in the config map and is a non-empty string.
func requireStringConfig(cfg map[string]any, key, prefix string, errs *ValidationErrors) {
	if cfg == nil {
		*errs = append(*errs, &ValidationError{
			Path:    prefix + ".config." + key,
			Message: fmt.Sprintf("required config field %q is missing (no config section)", key),
		})
		return
	}
	v, ok := cfg[key]
	if !ok {
		*errs = append(*errs, &ValidationError{
			Path:    prefix + ".config." + key,
			Message: fmt.Sprintf("required config field %q is missing", key),
		})
		return
	}
	s, ok := v.(string)
	if !ok || s == "" {
		*errs = append(*errs, &ValidationError{
			Path:    prefix + ".config." + key,
			Message: fmt.Sprintf("required config field %q must be a non-empty string", key),
		})
	}
}

func makeSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
