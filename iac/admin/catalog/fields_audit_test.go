package catalog_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// TestCatalog_NoUnannotatedFreeText enforces the selectable-over-free-text
// contract from the design (§FieldSpec Catalog). Every catalog field with
// Kind "string" or "array_string" MUST carry a matching FREEFORM_OK reason
// in freeformReasons[typeName][fieldName].
//
// New free-text fields added without a paired reason fail this test —
// the form-builder ergonomics depend on dropdowns being the default and
// text inputs being a deliberate exception with documented justification.
func TestCatalog_NoUnannotatedFreeText(t *testing.T) {
	cat := catalog.New()
	for _, typeName := range cat.AllTypes() {
		fields, ok := cat.Get(typeName)
		if !ok {
			t.Fatalf("AllTypes returned %q but Get returned !ok", typeName)
		}
		for _, f := range fields {
			if f.Kind != "string" && f.Kind != "array_string" {
				continue
			}
			reason, hasReason := catalog.FreeformReason(typeName, f.Name)
			if !hasReason || reason == "" {
				t.Errorf("%s.%s is kind=%s but has no FREEFORM_OK reason in freeformReasons",
					typeName, f.Name, f.Kind)
			}
		}
	}
}

// TestCatalog_AllExpectedTypesRegistered guards against accidental
// drop of one of the 13 typed Configs. The full proto-parity test
// (T9 catalog_proto_parity_test.go) walks the vendored proto and
// asserts the cross-product; this test is a fast unit-level
// canary that runs without filesystem dependencies.
func TestCatalog_AllExpectedTypesRegistered(t *testing.T) {
	expected := []string{
		"infra.api_gateway",
		"infra.cache",
		"infra.certificate",
		"infra.container_service",
		"infra.database",
		"infra.dns",
		"infra.firewall",
		"infra.iam_role",
		"infra.k8s_cluster",
		"infra.load_balancer",
		"infra.registry",
		"infra.storage",
		"infra.vpc",
	}
	cat := catalog.New()
	got := cat.AllTypes()
	if len(got) != len(expected) {
		t.Fatalf("AllTypes len=%d, want %d. got=%v", len(got), len(expected), got)
	}
	for i, name := range expected {
		if got[i] != name {
			t.Errorf("AllTypes[%d] = %q, want %q (full got=%v)", i, got[i], name, got)
		}
	}
}

// regionOptionalTypes lists types where the catalog deliberately omits
// the universal region field because the underlying resource is
// region-less in the design table (DNS is global per most providers).
// See infra.dns header comment in fields.go (spec-reviewer F2).
var regionOptionalTypes = map[string]bool{
	"infra.dns": true,
}

// TestCatalog_EveryTypeHasProviderAndRegion confirms the universal
// (provider, region) prefix is wired on every entry except those
// in regionOptionalTypes. The form-builder JS in new.js relies on
// the provider field being present everywhere (enum_dynamic source
// for dependent dropdowns); region presence is per-design.
func TestCatalog_EveryTypeHasProviderAndRegion(t *testing.T) {
	cat := catalog.New()
	for _, typeName := range cat.AllTypes() {
		fields, _ := cat.Get(typeName)
		var hasProvider, hasRegion bool
		for _, f := range fields {
			if f.Name == "provider" {
				hasProvider = true
				if f.Kind != "enum_dynamic" || f.EnumSource != "providers" {
					t.Errorf("%s.provider: kind=%s enum_source=%s, want enum_dynamic/providers",
						typeName, f.Kind, f.EnumSource)
				}
			}
			if f.Name == "region" {
				hasRegion = true
				if f.DependsOnField != "provider" {
					t.Errorf("%s.region: depends_on_field=%q, want \"provider\"",
						typeName, f.DependsOnField)
				}
			}
		}
		if !hasProvider {
			t.Errorf("%s: missing required `provider` field", typeName)
		}
		if !hasRegion && !regionOptionalTypes[typeName] {
			t.Errorf("%s: missing `region` field (not in regionOptionalTypes allowlist)", typeName)
		}
	}
}

// TestCatalog_EnumDynamicHasSource asserts every enum_dynamic field
// declares a non-empty EnumSource so the form-builder JS knows which
// resolver to invoke. An empty EnumSource produces an empty dropdown
// at render time, which is a footgun.
func TestCatalog_EnumDynamicHasSource(t *testing.T) {
	cat := catalog.New()
	for _, typeName := range cat.AllTypes() {
		fields, _ := cat.Get(typeName)
		for _, f := range fields {
			if f.Kind == "enum_dynamic" || f.Kind == "array_enum_dynamic" {
				if f.EnumSource == "" {
					t.Errorf("%s.%s: kind=%s but EnumSource is empty", typeName, f.Name, f.Kind)
				}
			}
		}
	}
}
