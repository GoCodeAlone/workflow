package region

import (
	"context"
	"testing"
)

func TestRegionContext(t *testing.T) {
	ctx := context.Background()
	if got := RegionFromContext(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = ContextWithRegion(ctx, "us-east-1")
	if got := RegionFromContext(ctx); got != "us-east-1" {
		t.Errorf("expected us-east-1, got %q", got)
	}
}

func TestDataResidencyEnforcerCheck(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()

	// No rule = no restriction
	if err := enforcer.Check("t1", "us-east-1"); err != nil {
		t.Errorf("expected no error without rule: %v", err)
	}

	// Set rule
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"eu-west-1", "eu-central-1"},
		DataClass:      "pii",
	})

	// Allowed region
	if err := enforcer.Check("t1", "eu-west-1"); err != nil {
		t.Errorf("expected no error for allowed region: %v", err)
	}

	// Disallowed region
	if err := enforcer.Check("t1", "us-east-1"); err == nil {
		t.Error("expected error for disallowed region")
	}

	// Different tenant, no rule
	if err := enforcer.Check("t2", "us-east-1"); err != nil {
		t.Errorf("expected no error for tenant without rule: %v", err)
	}
}

func TestDataResidencyEnforcerGetRule(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()

	_, ok := enforcer.GetRule("t1")
	if ok {
		t.Error("should not find rule for unconfigured tenant")
	}

	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"eu-west-1"},
	})

	rule, ok := enforcer.GetRule("t1")
	if !ok {
		t.Fatal("expected to find rule")
	}
	if rule.TenantID != "t1" {
		t.Errorf("expected t1, got %s", rule.TenantID)
	}
}

func TestDataResidencyEnforcerRemoveRule(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"eu-west-1"},
	})

	enforcer.RemoveRule("t1")

	_, ok := enforcer.GetRule("t1")
	if ok {
		t.Error("should not find removed rule")
	}
}

func TestDataResidencyEnforcerCheckDataClass(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()

	// No rule = no restriction
	regionCfg := RegionConfig{
		Name:               "us-east-1",
		AllowedDataClasses: []string{"general"},
	}
	if err := enforcer.CheckDataClass("t1", regionCfg); err != nil {
		t.Errorf("expected no error without rule: %v", err)
	}

	// Set rule with PII data class
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"eu-west-1"},
		DataClass:      "pii",
	})

	// Region supports PII
	piiRegion := RegionConfig{
		Name:               "eu-west-1",
		AllowedDataClasses: []string{"pii", "general"},
	}
	if err := enforcer.CheckDataClass("t1", piiRegion); err != nil {
		t.Errorf("expected no error for PII-supporting region: %v", err)
	}

	// Region does not support PII
	generalRegion := RegionConfig{
		Name:               "us-east-1",
		AllowedDataClasses: []string{"general"},
	}
	if err := enforcer.CheckDataClass("t1", generalRegion); err == nil {
		t.Error("expected error for region not supporting PII")
	}

	// Tenant with empty data class = no restriction
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t2",
		AllowedRegions: []string{"us-east-1"},
		DataClass:      "",
	})
	if err := enforcer.CheckDataClass("t2", generalRegion); err != nil {
		t.Errorf("expected no error for empty data class: %v", err)
	}
}

func TestRegionRouterBasic(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()
	router := NewRegionRouter(enforcer)

	// No regions configured
	_, err := router.Route("t1")
	if err == nil {
		t.Error("expected error with no regions")
	}

	// Add regions
	router.AddRegion(RegionConfig{
		Name:     "us-east-1",
		Endpoint: "https://us-east-1.example.com",
		Primary:  true,
	})
	router.AddRegion(RegionConfig{
		Name:     "eu-west-1",
		Endpoint: "https://eu-west-1.example.com",
	})

	// Without residency rule, should route to primary
	cfg, err := router.Route("t1")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if cfg.Name != "us-east-1" {
		t.Errorf("expected us-east-1, got %s", cfg.Name)
	}
}

func TestRegionRouterWithResidency(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"eu-west-1"},
	})

	router := NewRegionRouter(enforcer)
	router.AddRegion(RegionConfig{
		Name:     "us-east-1",
		Endpoint: "https://us-east-1.example.com",
		Primary:  true,
	})
	router.AddRegion(RegionConfig{
		Name:     "eu-west-1",
		Endpoint: "https://eu-west-1.example.com",
	})

	cfg, err := router.Route("t1")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if cfg.Name != "eu-west-1" {
		t.Errorf("expected eu-west-1 for tenant with EU residency, got %s", cfg.Name)
	}
}

func TestRegionRouterNoAllowedRegionAvailable(t *testing.T) {
	enforcer := NewDataResidencyEnforcer()
	enforcer.SetRule(DataResidencyRule{
		TenantID:       "t1",
		AllowedRegions: []string{"ap-southeast-1"}, // not available
	})

	router := NewRegionRouter(enforcer)
	router.AddRegion(RegionConfig{
		Name:     "us-east-1",
		Endpoint: "https://us-east-1.example.com",
		Primary:  true,
	})

	_, err := router.Route("t1")
	if err == nil {
		t.Error("expected error when no allowed region is available")
	}
}

func TestRegionRouterRemoveRegion(t *testing.T) {
	router := NewRegionRouter(nil)
	router.AddRegion(RegionConfig{
		Name:     "us-east-1",
		Endpoint: "https://us-east-1.example.com",
		Primary:  true,
	})

	router.RemoveRegion("us-east-1")

	_, err := router.Route("t1")
	if err == nil {
		t.Error("expected error after removing only region")
	}

	if router.PrimaryRegion() != "" {
		t.Error("primary should be cleared after removing primary region")
	}
}

func TestRegionRouterGetRegion(t *testing.T) {
	router := NewRegionRouter(nil)
	router.AddRegion(RegionConfig{
		Name:     "us-east-1",
		Endpoint: "https://us-east-1.example.com",
	})

	cfg, ok := router.GetRegion("us-east-1")
	if !ok {
		t.Fatal("expected to find region")
	}
	if cfg.Endpoint != "https://us-east-1.example.com" {
		t.Errorf("unexpected endpoint: %s", cfg.Endpoint)
	}

	_, ok = router.GetRegion("nonexistent")
	if ok {
		t.Error("should not find nonexistent region")
	}
}

func TestRegionRouterRegions(t *testing.T) {
	router := NewRegionRouter(nil)
	router.AddRegion(RegionConfig{Name: "r1"})
	router.AddRegion(RegionConfig{Name: "r2"})

	regions := router.Regions()
	if len(regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(regions))
	}
}

func TestRegionRouterNilEnforcer(t *testing.T) {
	router := NewRegionRouter(nil)
	router.AddRegion(RegionConfig{
		Name:    "us-east-1",
		Primary: true,
	})

	cfg, err := router.Route("t1")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if cfg.Name != "us-east-1" {
		t.Errorf("expected us-east-1, got %s", cfg.Name)
	}
}
