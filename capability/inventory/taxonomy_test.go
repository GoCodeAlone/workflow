package inventory

import (
	"strings"
	"testing"
)

func TestLoadTaxonomyMapsAliases(t *testing.T) {
	tax, err := LoadTaxonomy("testdata/taxonomy.yaml")
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	got, ok := tax.MatchType("module", "http.server")
	if !ok {
		t.Fatal("expected module alias match for http.server")
	}
	if got.ID != "http.server" {
		t.Fatalf("MatchType ID = %q, want http.server", got.ID)
	}

	got, ok = tax.MatchType("step", "step.authz_check")
	if !ok {
		t.Fatal("expected step alias match for step.authz_check")
	}
	if got.ID != "auth.authz" {
		t.Fatalf("MatchType ID = %q, want auth.authz", got.ID)
	}
}

func TestLoadTaxonomyRejectsDuplicateIDs(t *testing.T) {
	_, err := LoadTaxonomy("testdata/taxonomy-duplicate.yaml")
	if err == nil || !strings.Contains(err.Error(), "duplicate capability id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestLoadTaxonomyRejectsDuplicateAliases(t *testing.T) {
	_, err := LoadTaxonomy("testdata/taxonomy-duplicate-alias.yaml")
	if err == nil || !strings.Contains(err.Error(), "duplicate alias") {
		t.Fatalf("expected duplicate alias error, got %v", err)
	}
}
