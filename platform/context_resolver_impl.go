package platform

import (
	"context"
	"fmt"
)

// StdContextResolver implements the ContextResolver interface. It builds
// PlatformContext instances by reading tier outputs from a StateStore and
// assembling the parent context chain with accumulated constraints.
type StdContextResolver struct {
	store     StateStore
	validator *ConstraintValidator
}

// NewStdContextResolver creates a new StdContextResolver backed by the given StateStore.
func NewStdContextResolver(store StateStore) *StdContextResolver {
	return &StdContextResolver{
		store:     store,
		validator: NewConstraintValidator(),
	}
}

// ResolveContext builds a PlatformContext for the given tier and identifiers.
// It reads parent tier outputs from the state store, assembles them into the
// ParentOutputs map, and collects applicable constraints from all parent tiers.
//
// Context flows downward through the tier hierarchy:
//   - Tier 1 (Infrastructure): no parent outputs or constraints
//   - Tier 2 (SharedPrimitive): receives Tier 1 outputs and constraints
//   - Tier 3 (Application): receives Tier 1 + Tier 2 outputs and constraints
func (r *StdContextResolver) ResolveContext(ctx context.Context, org, env, app string, tier Tier) (*PlatformContext, error) {
	if !tier.Valid() {
		return nil, fmt.Errorf("invalid tier: %d", tier)
	}

	pctx := &PlatformContext{
		Org:           org,
		Environment:   env,
		Application:   app,
		Tier:          tier,
		ParentOutputs: make(map[string]*ResourceOutput),
		Constraints:   nil,
		Credentials:   make(map[string]string),
		Labels:        make(map[string]string),
		Annotations:   make(map[string]string),
	}

	// Collect outputs and constraints from all parent tiers.
	// Parent tiers are those with a lower tier number.
	for parentTier := TierInfrastructure; parentTier < tier; parentTier++ {
		parentPath := contextPathForTier(org, env, app, parentTier)

		outputs, err := r.store.ListResources(ctx, parentPath)
		if err != nil {
			return nil, fmt.Errorf("resolving tier %s outputs at %q: %w", parentTier, parentPath, err)
		}

		for _, output := range outputs {
			pctx.ParentOutputs[output.Name] = output
		}

		// Collect constraints defined on those parent outputs.
		// Constraints are stored as a special resource named "__constraints__"
		// in the tier's context path.
		constraintOutput, err := r.store.GetResource(ctx, parentPath, "__constraints__")
		if err != nil {
			// Not found is fine — tier may not define constraints.
			if !isNotFound(err) {
				return nil, fmt.Errorf("resolving tier %s constraints at %q: %w", parentTier, parentPath, err)
			}
		} else if constraintOutput != nil && constraintOutput.Properties != nil {
			constraints := extractConstraints(constraintOutput.Properties, parentTier.String())
			pctx.Constraints = append(pctx.Constraints, constraints...)
		}
	}

	return pctx, nil
}

// PropagateOutputs writes resource outputs into the state store so that
// downstream tiers can resolve them via ResolveContext. It also stores any
// constraints defined in the context for downstream consumption.
func (r *StdContextResolver) PropagateOutputs(ctx context.Context, pctx *PlatformContext, outputs []*ResourceOutput) error {
	contextPath := pctx.ContextPath()

	for _, output := range outputs {
		if err := r.store.SaveResource(ctx, contextPath, output); err != nil {
			return fmt.Errorf("saving output %q to %q: %w", output.Name, contextPath, err)
		}
	}

	return nil
}

// RegisterConstraints stores constraints in the state store for a given tier context.
// These constraints will be picked up by ResolveContext when building contexts
// for downstream tiers.
func (r *StdContextResolver) RegisterConstraints(ctx context.Context, pctx *PlatformContext, constraints []Constraint) error {
	if len(constraints) == 0 {
		return nil
	}

	contextPath := pctx.ContextPath()

	// Store constraints as properties on a special resource.
	props := make(map[string]any, len(constraints))
	for i, c := range constraints {
		key := fmt.Sprintf("constraint_%d", i)
		props[key] = map[string]any{
			"field":    c.Field,
			"operator": c.Operator,
			"value":    c.Value,
			"source":   c.Source,
		}
	}

	constraintOutput := &ResourceOutput{
		Name:       "__constraints__",
		Type:       "constraints",
		Properties: props,
		Status:     ResourceStatusActive,
	}

	return r.store.SaveResource(ctx, contextPath, constraintOutput)
}

// ValidateTierBoundary ensures a workflow operating at the given tier does not
// attempt to modify resources outside its scope. Returns any constraint
// violations found.
func (r *StdContextResolver) ValidateTierBoundary(pctx *PlatformContext, declarations []CapabilityDeclaration) []ConstraintViolation {
	var violations []ConstraintViolation

	for _, decl := range declarations {
		// Check that the declaration's tier matches the context tier.
		if decl.Tier != pctx.Tier {
			violations = append(violations, ConstraintViolation{
				Constraint: Constraint{
					Field:    "tier",
					Operator: "==",
					Value:    pctx.Tier,
					Source:   "tier_boundary",
				},
				Actual: decl.Tier,
				Message: fmt.Sprintf("capability %q declares tier %s but context operates at tier %s",
					decl.Name, decl.Tier, pctx.Tier),
			})
			continue
		}

		// Validate properties against accumulated constraints.
		if decl.Properties != nil && len(pctx.Constraints) > 0 {
			propViolations := r.validator.Validate(decl.Properties, pctx.Constraints)
			for i := range propViolations {
				propViolations[i].Message = fmt.Sprintf("capability %q: %s", decl.Name, propViolations[i].Message)
			}
			violations = append(violations, propViolations...)
		}
	}

	return violations
}

// contextPathForTier builds the context path for a specific tier.
// Tier 1 and Tier 2 use "org/env", Tier 3 uses "org/env/app".
func contextPathForTier(org, env, app string, tier Tier) string {
	switch tier {
	case TierInfrastructure:
		return org + "/" + env + "/tier1"
	case TierSharedPrimitive:
		return org + "/" + env + "/tier2"
	case TierApplication:
		if app != "" {
			return org + "/" + env + "/" + app + "/tier3"
		}
		return org + "/" + env + "/tier3"
	default:
		return org + "/" + env
	}
}

// extractConstraints converts the properties map from a __constraints__ resource
// back into a slice of Constraint values.
func extractConstraints(props map[string]any, source string) []Constraint {
	var constraints []Constraint
	for _, v := range props {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		c := Constraint{
			Source: source,
		}
		if f, ok := m["field"].(string); ok {
			c.Field = f
		}
		if op, ok := m["operator"].(string); ok {
			c.Operator = op
		}
		if val, exists := m["value"]; exists {
			c.Value = val
		}
		if src, ok := m["source"].(string); ok {
			c.Source = src
		}
		constraints = append(constraints, c)
	}
	return constraints
}

// isNotFound checks if an error is a ResourceNotFoundError.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*ResourceNotFoundError)
	return ok
}
