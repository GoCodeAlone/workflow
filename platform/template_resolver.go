package platform

import (
	"fmt"
	"regexp"
	"strings"
)

// placeholderRe matches ${param_name} patterns in strings.
var placeholderRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// TemplateResolver resolves a WorkflowTemplate with concrete parameter values,
// producing a list of CapabilityDeclarations with all placeholders substituted.
type TemplateResolver struct{}

// NewTemplateResolver creates a new TemplateResolver.
func NewTemplateResolver() *TemplateResolver {
	return &TemplateResolver{}
}

// Resolve substitutes parameter values into the template's capability declarations.
// It validates that all required parameters are present, applies defaults for
// optional parameters, and performs deep substitution in nested structures.
func (r *TemplateResolver) Resolve(template *WorkflowTemplate, params map[string]any) ([]CapabilityDeclaration, error) {
	if template == nil {
		return nil, fmt.Errorf("template is nil")
	}

	// Build effective parameter values: start with defaults, overlay provided params.
	effective, err := r.buildEffectiveParams(template.Parameters, params)
	if err != nil {
		return nil, err
	}

	// Validate parameter values against their declared validation patterns.
	if err := r.validateParams(template.Parameters, effective); err != nil {
		return nil, err
	}

	// Deep-copy and substitute capabilities.
	result := make([]CapabilityDeclaration, 0, len(template.Capabilities))
	for _, cap := range template.Capabilities {
		resolved, err := r.resolveCapability(cap, effective)
		if err != nil {
			return nil, fmt.Errorf("resolving capability %q: %w", cap.Name, err)
		}
		result = append(result, resolved)
	}

	return result, nil
}

// buildEffectiveParams merges defaults and provided params, checking required fields.
func (r *TemplateResolver) buildEffectiveParams(paramDefs []TemplateParameter, provided map[string]any) (map[string]any, error) {
	effective := make(map[string]any)

	for _, p := range paramDefs {
		val, ok := provided[p.Name]
		if !ok || val == nil {
			if p.Required {
				return nil, fmt.Errorf("required parameter %q is missing", p.Name)
			}
			if p.Default != nil {
				effective[p.Name] = p.Default
			}
			continue
		}
		effective[p.Name] = val
	}

	return effective, nil
}

// validateParams checks parameter values against their Validation regex patterns.
func (r *TemplateResolver) validateParams(paramDefs []TemplateParameter, effective map[string]any) error {
	for _, p := range paramDefs {
		if p.Validation == "" {
			continue
		}
		val, ok := effective[p.Name]
		if !ok {
			continue
		}
		// Validation only applies to string values.
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		re, err := regexp.Compile(p.Validation)
		if err != nil {
			return fmt.Errorf("parameter %q has invalid validation pattern %q: %w", p.Name, p.Validation, err)
		}
		if !re.MatchString(strVal) {
			return fmt.Errorf("parameter %q value %q does not match validation pattern %q", p.Name, strVal, p.Validation)
		}
	}
	return nil
}

// resolveCapability substitutes parameters into a single CapabilityDeclaration.
func (r *TemplateResolver) resolveCapability(cap CapabilityDeclaration, params map[string]any) (CapabilityDeclaration, error) {
	resolved := CapabilityDeclaration{
		Type:        r.substituteString(cap.Type, params),
		Tier:        cap.Tier,
		Constraints: cap.Constraints,
	}

	resolved.Name = r.substituteString(cap.Name, params)

	// Deep-substitute properties.
	if cap.Properties != nil {
		props, err := r.substituteMap(cap.Properties, params)
		if err != nil {
			return CapabilityDeclaration{}, err
		}
		resolved.Properties = props
	}

	// Copy DependsOn with substitution.
	if cap.DependsOn != nil {
		deps := make([]string, len(cap.DependsOn))
		for i, d := range cap.DependsOn {
			deps[i] = r.substituteString(d, params)
		}
		resolved.DependsOn = deps
	}

	return resolved, nil
}

// substituteString replaces ${param_name} placeholders in a string with param values.
// If the entire string is a single placeholder and the param value is not a string,
// the original type is preserved (this is handled at the caller level for non-string values).
func (r *TemplateResolver) substituteString(s string, params map[string]any) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1] // strip ${ and }
		if val, ok := params[key]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
}

// substituteAny recursively substitutes parameters in any value.
// For strings, it performs placeholder substitution. If a string is entirely
// a single placeholder like "${name}", the parameter's native type is returned.
func (r *TemplateResolver) substituteAny(v any, params map[string]any) (any, error) {
	switch val := v.(type) {
	case string:
		return r.substituteStringValue(val, params), nil
	case map[string]any:
		return r.substituteMap(val, params)
	case []any:
		return r.substituteSlice(val, params)
	default:
		return v, nil
	}
}

// substituteStringValue handles string substitution, preserving native types
// when the entire string is a single placeholder.
func (r *TemplateResolver) substituteStringValue(s string, params map[string]any) any {
	// Check if the entire string is a single placeholder.
	trimmed := strings.TrimSpace(s)
	if placeholderRe.MatchString(trimmed) {
		matches := placeholderRe.FindAllStringIndex(trimmed, -1)
		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(trimmed) {
			key := trimmed[2 : len(trimmed)-1]
			if val, ok := params[key]; ok {
				return val
			}
		}
	}

	// Otherwise do string interpolation.
	return r.substituteString(s, params)
}

// substituteMap deep-substitutes all values in a map.
func (r *TemplateResolver) substituteMap(m map[string]any, params map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(m))
	for k, v := range m {
		resolvedKey := r.substituteString(k, params)
		resolvedVal, err := r.substituteAny(v, params)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		result[resolvedKey] = resolvedVal
	}
	return result, nil
}

// substituteSlice deep-substitutes all elements in a slice.
func (r *TemplateResolver) substituteSlice(s []any, params map[string]any) ([]any, error) {
	result := make([]any, len(s))
	for i, v := range s {
		resolved, err := r.substituteAny(v, params)
		if err != nil {
			return nil, fmt.Errorf("index %d: %w", i, err)
		}
		result[i] = resolved
	}
	return result, nil
}
