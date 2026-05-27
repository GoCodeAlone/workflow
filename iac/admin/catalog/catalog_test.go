package catalog_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// TestNew_ReturnsNonNilEmptyCatalog pins the T7a skeleton contract:
// New() must return a usable catalog with zero entries. T7b fills the
// 13 typed Configs separately so this skeleton is callable from T5/T6
// while the entry table is still being authored. Per plan §Task 7a
// Step 1.
func TestNew_ReturnsNonNilEmptyCatalog(t *testing.T) {
	cat := catalog.New()
	if cat == nil {
		t.Fatal("catalog.New() returned nil")
	}
	types := cat.AllTypes()
	if len(types) != 0 {
		t.Errorf("expected empty AllTypes() on skeleton catalog, got %v", types)
	}
}

// TestGet_MissingTypeReturnsFalse pins the Get(missing) contract: the
// boolean must be false (not ok) when no entry exists for the given
// resource type. T5/T6 handler library code uses this signal to skip
// unknown types rather than crashing or returning empty results
// indistinguishable from a registered-but-empty type.
func TestGet_MissingTypeReturnsFalse(t *testing.T) {
	cat := catalog.New()
	fields, ok := cat.Get("infra.nonexistent")
	if ok {
		t.Errorf("Get(\"infra.nonexistent\") returned ok=true on empty catalog")
	}
	if fields != nil {
		t.Errorf("Get on missing type returned %v, want nil slice", fields)
	}
}

// TestFreeformReason_MissingEntryReturnsFalse pins the
// package-level FreeformReason signature used by the T7b audit test:
// when no FREEFORM_OK annotation exists for the requested field, the
// function returns ("", false). The skeleton always returns false
// since the annotation map is empty; T7b populates the map alongside
// its FieldSpec entries.
func TestFreeformReason_MissingEntryReturnsFalse(t *testing.T) {
	reason, ok := catalog.FreeformReason("infra.vpc", "cidr")
	if ok {
		t.Errorf("FreeformReason on empty catalog returned ok=true (reason=%q)", reason)
	}
	if reason != "" {
		t.Errorf("FreeformReason returned reason=%q on empty catalog, want \"\"", reason)
	}
}
