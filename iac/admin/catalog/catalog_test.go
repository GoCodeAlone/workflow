package catalog_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// TestNew_ReturnsPopulatedCatalog pins the post-T7b contract: New()
// returns a usable catalog populated by fields.go's init() with the
// 13 typed `infra.*` Configs. The original T7a skeleton expected an
// empty catalog; T7b's fields.go reassigns catalogEntries from init()
// so callers of New() see the full table. Per plan §Task 7b.
func TestNew_ReturnsPopulatedCatalog(t *testing.T) {
	cat := catalog.New()
	if cat == nil {
		t.Fatal("catalog.New() returned nil")
	}
	types := cat.AllTypes()
	if len(types) == 0 {
		t.Fatal("AllTypes() returned no entries; T7b should have populated 13 typed Configs")
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

// TestFreeformReason_MissingEntryReturnsFalse pins the package-level
// FreeformReason signature: when no FREEFORM_OK annotation exists for
// the requested {typeName, fieldName}, the function returns ("", false).
//
// Post-T7b inputs: use a genuinely missing pair (infra.vpc has no
// `nope` field) since T7b populated annotations for the previously
// empty (infra.vpc, cidr) pair used in the skeleton test.
func TestFreeformReason_MissingEntryReturnsFalse(t *testing.T) {
	reason, ok := catalog.FreeformReason("infra.vpc", "nope_missing_field")
	if ok {
		t.Errorf("FreeformReason for missing field returned ok=true (reason=%q)", reason)
	}
	if reason != "" {
		t.Errorf("FreeformReason returned reason=%q for missing field, want \"\"", reason)
	}
	// Cross-check: also asserts unknown typeName returns false.
	if r, ok := catalog.FreeformReason("infra.unknown_type", "anything"); ok || r != "" {
		t.Errorf("FreeformReason for unknown type returned ok=%v reason=%q, want false/\"\"", ok, r)
	}
}
