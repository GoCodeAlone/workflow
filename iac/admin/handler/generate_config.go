package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"gopkg.in/yaml.v3"
)

// moduleEntry is the YAML shape GenerateConfig emits — a single
// module entry under a host's `modules:` block. Field order is the
// canonical workflow config layout (name → type → config). Using a
// typed struct + yaml.Marshal is the strict-contract path the plan
// mandates: we never fmt.Sprintf user input into YAML, which would
// admit injection-of-arbitrary-keys via crafted field values.
type moduleEntry struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config,omitempty"`
}

// GenerateConfig implements InfraAdminService.GenerateConfig by
// type-coercing the form-builder's field_values map against the
// FieldSpecCatalog Kind dispatch, assembling a module config map,
// and yaml.Marshal-ing the result. Output is a single module entry
// (name + type + config) the user pastes under their existing
// `modules:` block. Per plan §Task 6.
//
// **Strict-contract invariant**: never fmt.Sprintf user input into
// YAML. All values flow through yaml.Marshal of a typed struct or
// map. TestGenerateConfig_NoFmtSprintfUserInput pins this against
// regression.
//
// Array encoding contract (cross-task, locked 2026-05-27):
// array_string + array_object field values arrive JSON-encoded
// (e.g. `field_values["ingress"] = "[\"rule a\", \"rule b, c\"]"`)
// so values containing commas survive the wire. Handler decodes
// via json.Unmarshal. Defensive fallback: a value that doesn't
// parse as a JSON array is wrapped into a one-element slice so a
// malformed UI submission doesn't crash the server. See
// TestGenerateConfig_ArrayValuesJSONDecoded +
// TestGenerateConfig_PlainStringNotJSONDecoded.
//
// Per design §Authz: default-deny via the shared authz guard.
func GenerateConfig(
	ctx context.Context,
	fieldCat *catalog.FieldSpecCatalog,
	in *adminpb.AdminGenerateConfigInput,
) (*adminpb.AdminGenerateConfigOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminGenerateConfigOutput{Error: msg}, nil
	}

	specs, ok := fieldCat.Get(in.GetResourceType())
	if !ok {
		return &adminpb.AdminGenerateConfigOutput{
			ValidationErrors: []string{
				fmt.Sprintf("unknown resource_type %q — not in FieldSpec catalog", in.GetResourceType()),
			},
		}, nil
	}

	cfg := map[string]any{}
	var validationErrors []string
	for i := range specs {
		spec := &specs[i] // gocritic rangeValCopy: avoid 176-byte copy per iteration
		raw, present := in.GetFieldValues()[spec.Name]
		if !present || raw == "" {
			if spec.Required {
				validationErrors = append(validationErrors,
					fmt.Sprintf("missing required field %q", spec.Name))
			}
			continue
		}
		coerced, verrs := coerceFieldValue(spec, raw)
		if len(verrs) > 0 {
			validationErrors = append(validationErrors, verrs...)
			continue
		}
		cfg[spec.Name] = coerced
	}

	entry := moduleEntry{
		Name:   in.GetResourceName(),
		Type:   in.GetResourceType(),
		Config: cfg,
	}
	yamlBytes, err := yaml.Marshal(entry)
	if err != nil {
		//nolint:nilerr // proto tag-100 convention; see list_resources.go for rationale
		return &adminpb.AdminGenerateConfigOutput{
			Error:            "marshal yaml: " + err.Error(),
			ValidationErrors: validationErrors,
		}, nil
	}

	return &adminpb.AdminGenerateConfigOutput{
		YamlSnippet:      string(yamlBytes),
		ValidationErrors: validationErrors,
	}, nil
}

// coerceFieldValue parses the string-encoded form value into the
// catalog-declared Kind. Returns (coercedValue, nil) on success or
// (nil, []validationError) on parse failure. The handler accumulates
// validation errors and continues processing other fields so the
// UI can surface every problem in one round-trip.
func coerceFieldValue(spec *catalog.FieldSpec, raw string) (any, []string) {
	switch spec.Kind {
	case "string":
		return raw, nil
	case "enum", "enum_dynamic":
		// Dropdowns submit a string value. Catalog cannot validate
		// enum_dynamic values without the live providers/regions
		// data; the host module can post-validate via T6 future
		// enhancement. v1 accepts the string verbatim.
		return raw, nil
	case "bool":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, []string{fmt.Sprintf("field %q: invalid bool %q", spec.Name, raw)}
		}
		return v, nil
	case "number":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, []string{fmt.Sprintf("field %q: invalid number %q", spec.Name, raw)}
		}
		// Bounds check: MaxCount/MinCount on number-kind fields carry
		// the value range per design's "number-with-bounds"
		// convention.
		if spec.MinCount != 0 && v < int64(spec.MinCount) {
			return nil, []string{fmt.Sprintf("field %q: %d below min %d", spec.Name, v, spec.MinCount)}
		}
		if spec.MaxCount != 0 && v > int64(spec.MaxCount) {
			return nil, []string{fmt.Sprintf("field %q: %d above max %d", spec.Name, v, spec.MaxCount)}
		}
		return v, nil
	case "array_string", "array_object", "array_enum_dynamic", "array_number":
		return coerceArrayValue(spec, raw)
	case "object":
		// Object kind: expect JSON-encoded payload, decode to
		// map[string]any so yaml.Marshal emits a nested map.
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, []string{fmt.Sprintf("field %q: invalid object JSON: %v", spec.Name, err)}
		}
		return m, nil
	default:
		// Unknown kind — accept verbatim with a validation warning so
		// the catalog can introduce new kinds without immediately
		// crashing in-flight requests.
		return raw, []string{fmt.Sprintf("field %q: unrecognized kind %q (accepted verbatim)", spec.Name, spec.Kind)}
	}
}

// coerceArrayValue handles the cross-task contract for array-shaped
// field_values: input arrives as a JSON-encoded array string (the
// canonical form) OR a plain literal (defensive fallback). Returns
// a []any whose elements are coerced per spec.ElementKind so yaml.
// Marshal emits a proper YAML sequence.
func coerceArrayValue(spec *catalog.FieldSpec, raw string) (any, []string) {
	trimmed := strings.TrimSpace(raw)
	var elements []any
	if strings.HasPrefix(trimmed, "[") {
		// JSON-encoded array — canonical shape per cross-task contract.
		var stringElems []string
		if err := json.Unmarshal([]byte(trimmed), &stringElems); err == nil {
			for _, s := range stringElems {
				elements = append(elements, s)
			}
		} else {
			// Try heterogeneous array (e.g. array_number).
			if err := json.Unmarshal([]byte(trimmed), &elements); err != nil {
				return nil, []string{fmt.Sprintf("field %q: invalid array JSON: %v", spec.Name, err)}
			}
		}
	} else {
		// Defensive fallback: plain literal becomes a one-element array.
		// TestGenerateConfig_PlainStringNotJSONDecoded pins this shape.
		elements = []any{raw}
	}
	// Element-kind coercion: array_number elements come back as
	// float64 from json.Unmarshal — coerce to int64 so YAML emits
	// integers rather than floats.
	if spec.ElementKind == "number" {
		coerced := make([]any, 0, len(elements))
		for _, e := range elements {
			switch v := e.(type) {
			case float64:
				coerced = append(coerced, int64(v))
			case string:
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					coerced = append(coerced, n)
				} else {
					return nil, []string{fmt.Sprintf("field %q: invalid number element %q", spec.Name, v)}
				}
			default:
				coerced = append(coerced, e)
			}
		}
		elements = coerced
	}
	return elements, nil
}
