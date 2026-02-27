package schema

import (
	"fmt"
	"strings"
	"unicode"

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
	allowNoEntryPoints    bool
	skipModuleTypeCheck   bool
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

// WithAllowNoEntryPoints disables the check that requires at least one entry
// point (trigger, HTTP route, messaging subscription, or scheduler job).
func WithAllowNoEntryPoints() ValidationOption {
	return func(o *validationOpts) {
		o.allowNoEntryPoints = true
	}
}

// WithSkipModuleTypeCheck disables validation of module type identifiers.
// Useful for validating configs that use custom or placeholder module types.
func WithSkipModuleTypeCheck() ValidationOption {
	return func(o *validationOpts) {
		o.skipModuleTypeCheck = true
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
		switch {
		case mod.Name == "":
			errs = append(errs, &ValidationError{
				Path:    prefix + ".name",
				Message: "module name is required",
			})
		case !strings.HasPrefix(mod.Type, "step."):
			// Only check uniqueness for non-pipeline-step modules
			if firstIdx, exists := seenNames[mod.Name]; exists {
				errs = append(errs, &ValidationError{
					Path:    prefix + ".name",
					Message: fmt.Sprintf("duplicate module name %q (first defined at modules[%d])", mod.Name, firstIdx),
				})
			} else {
				seenNames[mod.Name] = i
			}
		default:
			// For step.* modules, still track name for dependency resolution
			if _, exists := seenNames[mod.Name]; !exists {
				seenNames[mod.Name] = i
			}
		}

		// type is required and must be known
		if mod.Type == "" {
			errs = append(errs, &ValidationError{
				Path:    prefix + ".type",
				Message: "module type is required",
			})
		} else if !o.skipModuleTypeCheck && !knownTypes[mod.Type] {
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

	// Check for entry points (unless in lenient mode or explicitly allowed)
	if !o.allowEmptyModules && !o.allowNoEntryPoints {
		checkEntryPoints(cfg, &errs)
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
		// Build snake_case → camelCase mapping for "did you mean" hints.
		snakeToCamel := make(map[string]string, len(s.ConfigFields))
		for i := range s.ConfigFields {
			if snake := camelToSnake(s.ConfigFields[i].Key); snake != s.ConfigFields[i].Key {
				snakeToCamel[snake] = s.ConfigFields[i].Key
			}
		}

		// Check each config key for snake_case/camelCase confusion.
		if mod.Config != nil {
			for key := range mod.Config {
				if camel, ok := snakeToCamel[key]; ok {
					*errs = append(*errs, &ValidationError{
						Path:    prefix + ".config." + key,
						Message: fmt.Sprintf("config field %q uses snake_case; use camelCase %q instead", key, camel),
					})
				}
			}
		}

		for i := range s.ConfigFields {
			if !s.ConfigFields[i].Required {
				continue
			}
			fieldPath := prefix + ".config." + s.ConfigFields[i].Key
			if mod.Config == nil {
				*errs = append(*errs, &ValidationError{
					Path:    fieldPath,
					Message: fmt.Sprintf("required config field %q is missing (no config section)", s.ConfigFields[i].Key),
				})
				continue
			}
			v, ok := mod.Config[s.ConfigFields[i].Key]
			if !ok {
				msg := fmt.Sprintf("required config field %q is missing", s.ConfigFields[i].Key)
				// Check if the snake_case form of the required key was provided instead.
				if snakeKey := camelToSnake(s.ConfigFields[i].Key); snakeKey != s.ConfigFields[i].Key {
					if _, snakeProvided := mod.Config[snakeKey]; snakeProvided {
						msg = fmt.Sprintf("required config field %q is missing; found snake_case %q — use camelCase instead", s.ConfigFields[i].Key, snakeKey)
					}
				}
				*errs = append(*errs, &ValidationError{
					Path:    fieldPath,
					Message: msg,
				})
				continue
			}
			// Check non-empty for string fields
			if s.ConfigFields[i].Type == FieldTypeString || s.ConfigFields[i].Type == FieldTypeDuration || s.ConfigFields[i].Type == FieldTypeSelect {
				if str, ok := v.(string); ok && str == "" {
					*errs = append(*errs, &ValidationError{
						Path:    fieldPath,
						Message: fmt.Sprintf("required config field %q must be a non-empty string", s.ConfigFields[i].Key),
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

// entryPointModuleTypes are module types that inherently serve as entry points
// because they listen for external input (HTTP servers, schedulers, event buses).
var entryPointModuleTypes = map[string]bool{
	"http.server":       true,
	"scheduler.modular": true,
	"messaging.broker":  true,
}

// checkEntryPoints validates that the config has at least one entry point:
// triggers, HTTP routes, messaging subscriptions, scheduler jobs, pipelines,
// or modules that inherently listen for external input.
func checkEntryPoints(cfg *config.WorkflowConfig, errs *ValidationErrors) {
	// Triggers count as entry points
	if len(cfg.Triggers) > 0 {
		return
	}

	// Pipelines with triggers count as entry points
	if len(cfg.Pipelines) > 0 {
		return
	}

	// Modules that inherently listen for external input count as entry points
	for _, mod := range cfg.Modules {
		if entryPointModuleTypes[mod.Type] {
			return
		}
	}

	// Check workflow sections for entry points
	if cfg.Workflows != nil {
		// HTTP routes
		if httpWF, ok := cfg.Workflows["http"]; ok {
			if m, ok := httpWF.(map[string]any); ok {
				if routes, ok := m["routes"]; ok {
					if arr, ok := routes.([]any); ok && len(arr) > 0 {
						return
					}
				}
			}
		}

		// Messaging subscriptions
		if msgWF, ok := cfg.Workflows["messaging"]; ok {
			if m, ok := msgWF.(map[string]any); ok {
				if subs, ok := m["subscriptions"]; ok {
					if arr, ok := subs.([]any); ok && len(arr) > 0 {
						return
					}
				}
			}
		}

		// Scheduler jobs
		if schedWF, ok := cfg.Workflows["scheduler"]; ok {
			if m, ok := schedWF.(map[string]any); ok {
				if jobs, ok := m["jobs"]; ok {
					if arr, ok := jobs.([]any); ok && len(arr) > 0 {
						return
					}
				}
			}
		}
	}

	*errs = append(*errs, &ValidationError{
		Message: "config has no entry points (no triggers, HTTP routes, messaging subscriptions, scheduler jobs, or pipelines); add entry points or set allow_no_entry_points for embeddable sub-workflows",
	})
}

func makeSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// camelToSnake converts a camelCase identifier to its snake_case equivalent.
// For example: "contentType" → "content_type", "dbPath" → "db_path".
func camelToSnake(s string) string {
	return CamelToSnake(s)
}

// CamelToSnake converts a camelCase identifier to its snake_case equivalent.
// For example: "contentType" → "content_type", "dbPath" → "db_path".
func CamelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
