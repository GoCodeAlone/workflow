package catalog_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// TestRegionCatalog_NonEmptyPerProvider pins the v1 region coverage
// contract: every catalogued provider returns at least one region.
// An empty list would render an empty region dropdown — a footgun
// for the new-resource form-builder.
func TestRegionCatalog_NonEmptyPerProvider(t *testing.T) {
	c := catalog.NewRegionCatalog()
	for _, p := range c.Providers() {
		regs := c.For(p)
		if len(regs) == 0 {
			t.Errorf("provider %q returned empty region slice", p)
		}
	}
}

// TestRegionCatalog_DefensiveCopy verifies callers cannot mutate the
// catalog by writing into the returned slice. The form-builder /
// handler library both receive the slice; an accidental sort or
// append-and-truncate elsewhere would otherwise corrupt subsequent
// invocations.
func TestRegionCatalog_DefensiveCopy(t *testing.T) {
	c := catalog.NewRegionCatalog()
	a := c.For("digitalocean")
	if len(a) == 0 {
		t.Fatal("digitalocean returned no regions")
	}
	a[0] = "MUTATED"
	b := c.For("digitalocean")
	if b[0] == "MUTATED" {
		t.Errorf("catalog mutated via caller-side slice write: b[0]=%q", b[0])
	}
}

// TestRegionCatalog_UncataloguedProviderReturnsNil pins the contract
// for unknown provider types — handler library degrades gracefully
// by falling back to free-text region input per design's
// populateProviderTypes degradation path.
func TestRegionCatalog_UncataloguedProviderReturnsNil(t *testing.T) {
	c := catalog.NewRegionCatalog()
	if got := c.For("nonexistent-cloud"); got != nil {
		t.Errorf("For(unknown) = %v, want nil", got)
	}
}

// TestRegionCatalog_DigitalOceanSet asserts the design's documented
// DO region set is present verbatim — guards against accidental
// drop / typo when refreshing.
func TestRegionCatalog_DigitalOceanSet(t *testing.T) {
	c := catalog.NewRegionCatalog()
	got := c.For("digitalocean")
	expected := map[string]bool{
		"nyc1": true, "nyc3": true, "sfo3": true, "ams3": true,
		"sgp1": true, "lon1": true, "fra1": true, "tor1": true,
		"blr1": true, "syd1": true,
	}
	if len(got) != len(expected) {
		t.Errorf("DO regions len=%d, want %d. got=%v", len(got), len(expected), got)
	}
	for _, r := range got {
		if !expected[r] {
			t.Errorf("unexpected DO region %q", r)
		}
	}
}

// TestRegionCatalog_NilReceiver guards against nil-pointer panics
// when callers pass a zero-value catalog (e.g. degradation mode).
func TestRegionCatalog_NilReceiver(t *testing.T) {
	var c *catalog.RegionCatalog
	if got := c.For("digitalocean"); got != nil {
		t.Errorf("nil receiver For returned %v, want nil", got)
	}
	if got := c.Providers(); got != nil {
		t.Errorf("nil receiver Providers returned %v, want nil", got)
	}
}
