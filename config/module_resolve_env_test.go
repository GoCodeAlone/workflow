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

func TestResolveForEnv_RegionFromTopLevelConfigNoEnvs(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"region": "sfo3"},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Region != "sfo3" {
		t.Fatalf("want region=sfo3 from module config, got %q", resolved.Region)
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

func TestResolveForEnv_DeepMergesNestedMaps(t *testing.T) {
	m := &ModuleConfig{
		Name: "app",
		Type: "infra.container_service",
		Config: map[string]any{
			"env_vars": map[string]any{"PORT": "8080"},
		},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{
				"env_vars": map[string]any{"LOG_LEVEL": "info"},
			}},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	ev, _ := resolved.Config["env_vars"].(map[string]any)
	if ev["PORT"] != "8080" {
		t.Fatalf("want PORT=8080 preserved after deep merge, got %v", ev["PORT"])
	}
	if ev["LOG_LEVEL"] != "info" {
		t.Fatalf("want LOG_LEVEL=info from env override, got %v", ev["LOG_LEVEL"])
	}
}

func TestResolveForEnv_RegionWrittenToConfig(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "large"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"region": "nyc1"}},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Config["region"] != "nyc1" {
		t.Fatalf("want region in Config[region], got %v", resolved.Config["region"])
	}
	if resolved.Region != "nyc1" {
		t.Fatalf("want Region field set, got %q", resolved.Region)
	}
}

func TestResolveForEnv_ProviderWrittenToConfig(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "large"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Provider: "digitalocean"},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Provider != "digitalocean" {
		t.Fatalf("want Provider field set, got %q", resolved.Provider)
	}
	if resolved.Config["provider"] != "digitalocean" {
		t.Fatalf("want provider written to Config map, got %v", resolved.Config["provider"])
	}
}

func TestResolveForEnv_ProviderOverrideWins(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"provider": "do"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Provider: "aws"},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Config["provider"] != "aws" {
		t.Fatalf("env provider override should win over base Config, got %v", resolved.Config["provider"])
	}
	if resolved.Provider != "aws" {
		t.Fatalf("want Provider=aws, got %q", resolved.Provider)
	}
}
