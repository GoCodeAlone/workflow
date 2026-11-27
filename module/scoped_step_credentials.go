package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

const StepCredentialsService = "step.credentials"

var credentialFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var credentialKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*(\.[A-Za-z0-9_][A-Za-z0-9_-]*)*$`)

// DecodeStepCredentialGrants deliberately ignores schema sensitivity and names.
func DecodeStepCredentialGrants(raw any) ([]interfaces.StepCredentialGrant, error) {
	data, err := json.Marshal(raw)
	if err != nil || bytes.Equal(data, []byte("null")) {
		return nil, errors.New("invalid step credential grants")
	}
	var grants []interfaces.StepCredentialGrant
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&grants); err != nil {
		return nil, errors.New("invalid step credential grants")
	}
	if err := validateStepCredentialGrants(grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func validateStepCredentialGrants(grants []interfaces.StepCredentialGrant) error {
	type identity struct{ stepType, stepName, field string }
	type reference struct{ stepType, stepName, ref string }
	fields := make(map[identity]bool)
	refs := make(map[reference]bool)
	for _, g := range grants {
		for _, name := range []string{g.Plugin, g.StepType, g.StepName} {
			if name == "" || strings.ContainsAny(name, " \t\r\n{}$") {
				return errors.New("invalid step credential target")
			}
		}
		if g.Scope != "application" || !credentialFieldPattern.MatchString(g.Field) ||
			!strings.HasPrefix(g.Ref, "config:") || !credentialKeyPattern.MatchString(strings.TrimPrefix(g.Ref, "config:")) {
			return errors.New("invalid step credential grant")
		}
		f := identity{g.StepType, g.StepName, g.Field}
		r := reference{g.StepType, g.StepName, g.Ref}
		if fields[f] || refs[r] {
			return errors.New("duplicate or ambiguous step credential grants")
		}
		fields[f], refs[r] = true, true
	}
	return nil
}

type stepCredentialBinder struct {
	registry *ConfigRegistry
	grants   []interfaces.StepCredentialGrant
}

func NewStepCredentialBinder(registry *ConfigRegistry, grants []interfaces.StepCredentialGrant) (interfaces.StepCredentialBinder, error) {
	if registry == nil || registry == globalConfigRegistry {
		return nil, errors.New("step credentials require a private frozen registry")
	}
	registry.mu.RLock()
	frozen := registry.frozen
	registry.mu.RUnlock()
	if !frozen {
		return nil, errors.New("step credentials require a private frozen registry")
	}
	if err := validateStepCredentialGrants(grants); err != nil {
		return nil, err
	}
	return &stepCredentialBinder{registry: registry, grants: append([]interfaces.StepCredentialGrant(nil), grants...)}, nil
}

func (b *stepCredentialBinder) BindStep(target interfaces.StepCredentialTarget, cfg map[string]any) (*interfaces.BoundStepCredentials, error) {
	var bound []interfaces.StepCredentialGrant
	for _, g := range b.grants {
		if g.Plugin != target.Plugin || g.StepType != target.StepType || g.StepName != target.StepName {
			continue
		}
		literal, ok := cfg[g.Field].(string)
		if !ok || literal != g.Ref {
			return nil, errors.New("step credential literal does not match grant")
		}
		// Strings and grants are copied; the caller's config map is not retained.
		g.Ref = literal
		bound = append(bound, g)
	}
	if len(bound) == 0 {
		return nil, nil
	}
	return &interfaces.BoundStepCredentials{
		ConfigLookup: b.registry.Get,
		ValidateConfig: func(cfg map[string]any) error {
			for _, g := range bound {
				literal, ok := cfg[g.Field].(string)
				if !ok || literal != g.Ref {
					return errors.New("step credential literal does not match grant")
				}
			}
			return nil
		},
		Resolve: func(ctx context.Context) ([]interfaces.ResolvedStepCredential, error) {
			if ctx == nil {
				return nil, errors.New("step credential context is required")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			resolved := make([]interfaces.ResolvedStepCredential, 0, len(bound))
			for _, g := range bound {
				value, ok := b.registry.Get(strings.TrimPrefix(g.Ref, "config:"))
				if !ok || value == "" {
					return nil, errors.New("step credential value is unavailable")
				}
				resolved = append(resolved, interfaces.ResolvedStepCredential{Ref: g.Ref, Value: value})
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return resolved, nil
		},
	}, nil
}
