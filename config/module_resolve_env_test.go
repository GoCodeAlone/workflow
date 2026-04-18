package config

import "testing"

func TestResolveForEnv_NoEnvironments_ReturnsTopLevel(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small"},
	}
	resolved, ok := m.ResolveForEnv("staging")
	if !ok {
		t.Fatal("want ok=true when no environments defined")
	}
	if resolved.Config["size"] != "small" {
		t.Fatalf("want size=small, got %v", resolved.Config["size"])
	}
}

func TestResolveForEnv_OverridesMerge(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small", "region": "nyc1"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"size": "large"}},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Config["size"] != "large" {
		t.Fatalf("want size=large, got %v", resolved.Config["size"])
	}
	if resolved.Config["region"] != "nyc1" {
		t.Fatalf("want region=nyc1 preserved, got %v", resolved.Config["region"])
	}
}

func TestResolveForEnv_NilEnvSkipsResource(t *testing.T) {
	m := &ModuleConfig{
		Name: "dns",
		Type: "infra.dns",
		Environments: map[string]*InfraEnvironmentResolution{
			"prod":    {Config: map[string]any{"domain": "example.com"}},
			"staging": nil, // explicit skip
		},
	}
	if _, ok := m.ResolveForEnv("staging"); ok {
		t.Fatal("want ok=false when env explicitly nil")
	}
	if _, ok := m.ResolveForEnv("prod"); !ok {
		t.Fatal("want ok=true for prod")
	}
}

func TestResolveForEnv_EnvNotListed_UsesTopLevel(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"size": "large"}},
		},
	}
	resolved, ok := m.ResolveForEnv("dev")
	if !ok {
		t.Fatal("want ok=true when env not listed (falls back to top-level)")
	}
	if resolved.Config["size"] != "small" {
		t.Fatalf("want size=small, got %v", resolved.Config["size"])
	}
}

func TestResolveForEnv_RegionPopulatedFromConfig(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"region": "nyc1"}},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Region != "nyc1" {
		t.Fatalf("want region=nyc1 populated from resolved config, got %q", resolved.Region)
	}
}
