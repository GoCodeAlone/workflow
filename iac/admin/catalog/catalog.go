// Package catalog hosts the host-side FieldSpec catalog (covers all 13
// typed `infra.*` Configs from workflow-plugin-infra), the region
// catalog, and the engine catalog. The catalog drives the new-resource
// form-builder UI and feeds the typed AdminFieldSpec entries returned
// by InfraAdminService.ListResourceTypes (handler library T5/T6).
//
// Design: docs/plans/2026-05-27-infra-admin-dynamic-design.md §FieldSpec Catalog
// Plan:   docs/plans/2026-05-27-infra-admin-dynamic.md (Task 7a skeleton; T7b entries; T8 region/engine)
//
// This file (T7a) provides the package skeleton: types, the
// FieldSpecCatalog struct, the New() constructor returning an empty
// catalog, and the Get / AllTypes / FreeformReason accessors. T7b
// fills the 13 typed-Config entries in catalog/fields.go in parallel
// after T7a lands.
//
// The skeleton was split out of T7 per plan-adversarial C1: Lane A's
// T5 handler library imports *catalog.FieldSpecCatalog as a typed
// parameter, so the package + type + New() must exist before T5
// compiles. Splitting T7a → T7b resolves the hidden serial dependency.
package catalog

import "sort"

// FieldSpec describes one field of a typed `infra.*` Config so the
// new-resource form-builder UI can render the right input control.
// Mirrors workflow.iac.v1.AdminFieldSpec in iac/admin/proto/
// infra_admin.proto field-for-field so the handler library (T5/T6)
// can copy between the two without coercion. See the proto file for
// the per-field semantics documentation; this struct's tags double
// as the wire-rename contract for any future protojson-friendly
// serialization.
type FieldSpec struct {
	// Name is the YAML key the form-builder submits and the typed
	// Config message field name (lower_snake_case to match proto).
	Name string

	// Label is the human-readable form-field label.
	Label string

	// Kind is one of {"enum", "enum_dynamic", "string", "number",
	// "bool", "array_string", "array_object", "object"}. The
	// freeform-audit test (T7b) enforces that every "string" /
	// "array_string" entry carries a FREEFORM_OK reason via the
	// package-level reasons map.
	Kind string

	// Required indicates whether the form must collect a value.
	Required bool

	// EnumValues is the fixed option list for Kind=="enum".
	EnumValues []string

	// EnumSource is the dynamic-options provider for
	// Kind=="enum_dynamic": one of {"providers", "regions", "sizes",
	// "engines", "resource_types", "app_contexts", "k8s-versions"}.
	// The form-builder fetches the options at render time.
	EnumSource string

	// Description is the form-field help text (shown as tooltip or
	// inline help in the new-resource UI).
	Description string

	// DefaultValue is the initial form value (string-encoded per the
	// proto's map<string, string> field_values contract).
	DefaultValue string

	// Sensitive indicates the form should render a masked input AND
	// the value MUST be excluded from any rendered preview or audit
	// log entry.
	Sensitive bool

	// ElementKind is the per-element Kind for array_* entries.
	ElementKind string

	// MinCount + MaxCount bound array_* entry counts.
	MinCount int32
	MaxCount int32

	// DependsOnField filters this field's enum_dynamic options by the
	// value picked for another field (e.g. region options depend on
	// the chosen provider).
	DependsOnField string
}

// FieldSpecCatalog is the hand-maintained registry mapping resource
// type names (e.g. "infra.vpc") to their per-field specs. The
// handler library (T5/T6) holds one instance for the lifetime of the
// host-side infra.admin module; the catalog is read-only after
// New() returns.
//
// T7a ships the catalog skeleton with an empty entries map; T7b
// populates the map alongside the parallel freeformReasons table.
type FieldSpecCatalog struct {
	entries map[string][]FieldSpec
}

// New returns an empty FieldSpecCatalog. Per plan §Task 7a the
// skeleton catalog has zero entries so T5/T6 handler library can
// compile against the typed parameter while T7b fills the 13
// typed-Config entries in parallel.
//
// The returned catalog is safe for concurrent reads (Get / AllTypes
// never mutate state). T7b's filled catalog will populate entries at
// package init time (or via New() initialization) so no caller-side
// locking is required.
func New() *FieldSpecCatalog {
	return &FieldSpecCatalog{
		entries: catalogEntries(),
	}
}

// Get returns the field specs for typeName and ok=true when the type
// is registered. Returns (nil, false) for unknown types so callers
// can distinguish a missing-type from a registered-but-empty type
// (both return zero-length slices on the proto side; the boolean
// is the discriminator).
func (c *FieldSpecCatalog) Get(typeName string) ([]FieldSpec, bool) {
	fields, ok := c.entries[typeName]
	if !ok {
		return nil, false
	}
	// Defensive copy so callers cannot mutate the catalog's internal
	// state. Cost is minimal — entry slices average ~6 fields per
	// type.
	out := make([]FieldSpec, len(fields))
	copy(out, fields)
	return out, true
}

// AllTypes returns the sorted list of registered resource-type names.
// The sort is deterministic so callers (handler library +
// ListResourceTypes RPC response) emit a stable ordering for
// snapshot tests + diff-friendly downstream consumers.
func (c *FieldSpecCatalog) AllTypes() []string {
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// catalogEntries is the package-level seam T7b uses to populate the
// catalog. The skeleton ships with no entries; T7b's fields.go will
// override this variable with the hand-written table mapping
// {VPCConfig, ContainerServiceConfig, K8SClusterConfig, ...} to
// their per-field specs.
//
// Exposed as a package-level var (not a const, not a function
// literal embedded in New) so T7b's fields.go can reassign it from
// an init() function in the same package — Go forbids redeclaring
// a package-level var of the same name across files, so T7b's
// fields.go MUST take this shape:
//
//	package catalog
//	func init() {
//	    catalogEntries = func() map[string][]FieldSpec {
//	        return map[string][]FieldSpec{
//	            "infra.vpc": { ... },
//	            // ... 13 typed Configs ...
//	        }
//	    }
//	}
//
// Per spec-reviewer T7a comment-nit (commit ff0662602).
var catalogEntries = func() map[string][]FieldSpec {
	return map[string][]FieldSpec{}
}

// FreeformReason returns the FREEFORM_OK annotation reason for a
// catalog field whose Kind is "string" or "array_string". Returns
// ("", false) when no entry exists for {typeName, fieldName}.
//
// The plan §Task 7b audit test enumerates every catalog entry and
// asserts that every Kind ∈ {"string", "array_string"} field has a
// non-empty reason here. The reasons table is a parallel
// map[typeName]map[fieldName]string populated by hand alongside the
// catalog entries in T7b's fields.go. Skeleton ships with an empty
// map so the audit test runs (and trivially passes — there are no
// string-kind entries yet).
func FreeformReason(typeName, fieldName string) (string, bool) {
	byField, ok := freeformReasons[typeName]
	if !ok {
		return "", false
	}
	reason, ok := byField[fieldName]
	return reason, ok
}

// freeformReasons is the parallel annotation table populated by T7b.
// Skeleton ships empty so the package compiles + the smoke test
// passes; T7b reassigns this from a same-package init() in fields.go
// (same mechanism + Go-redeclaration constraint as catalogEntries
// above — assign via init(), don't re-`var`-declare). Per
// spec-reviewer T7a comment-nit (commit ff0662602).
var freeformReasons = map[string]map[string]string{}
