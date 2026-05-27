package catalog_test

import (
	"slices"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// TestEngineCatalog_NonEmptyPerProvider mirrors region catalog's
// non-empty invariant for database engines.
func TestEngineCatalog_NonEmptyPerProvider(t *testing.T) {
	c := catalog.NewEngineCatalog()
	for _, p := range c.Providers() {
		engs := c.For(p)
		if len(engs) == 0 {
			t.Errorf("provider %q returned empty engine slice", p)
		}
	}
}

// TestEngineCatalog_DefensiveCopy parallels RegionCatalog's slice-
// mutation guard.
func TestEngineCatalog_DefensiveCopy(t *testing.T) {
	c := catalog.NewEngineCatalog()
	a := c.For("aws")
	if len(a) == 0 {
		t.Fatal("aws returned no engines")
	}
	a[0] = "MUTATED"
	b := c.For("aws")
	if b[0] == "MUTATED" {
		t.Errorf("catalog mutated via caller-side slice write: b[0]=%q", b[0])
	}
}

// TestEngineCatalog_UncataloguedProviderReturnsNil mirrors region
// catalog's degradation contract.
func TestEngineCatalog_UncataloguedProviderReturnsNil(t *testing.T) {
	c := catalog.NewEngineCatalog()
	if got := c.For("nonexistent-cloud"); got != nil {
		t.Errorf("For(unknown) = %v, want nil", got)
	}
}

// TestEngineCatalog_AWSSuperset asserts AWS is a strict superset of
// the common postgres/mysql/mongodb/redis set per the design's engine
// matrix (AWS additionally has dynamodb + aurora).
func TestEngineCatalog_AWSSuperset(t *testing.T) {
	c := catalog.NewEngineCatalog()
	aws := c.For("aws")
	required := []string{"postgres", "mysql", "mongodb", "redis", "dynamodb", "aurora"}
	for _, want := range required {
		if !slices.Contains(aws, want) {
			t.Errorf("aws missing required engine %q. got=%v", want, aws)
		}
	}
}

// TestEngineCatalog_StubMinimal asserts the stub provider exposes
// the minimum engine surface for scenario tests — postgres only,
// per the design table.
func TestEngineCatalog_StubMinimal(t *testing.T) {
	c := catalog.NewEngineCatalog()
	stub := c.For("stub")
	if len(stub) != 1 || stub[0] != "postgres" {
		t.Errorf("stub engines = %v, want [postgres]", stub)
	}
}

// TestEngineCatalog_NilReceiver mirrors RegionCatalog nil-safety.
func TestEngineCatalog_NilReceiver(t *testing.T) {
	var c *catalog.EngineCatalog
	if got := c.For("aws"); got != nil {
		t.Errorf("nil receiver For returned %v, want nil", got)
	}
	if got := c.Providers(); got != nil {
		t.Errorf("nil receiver Providers returned %v, want nil", got)
	}
}
