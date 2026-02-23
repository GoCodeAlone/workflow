package admin

import (
	"testing"
)

func TestLoadConfig_Parses(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Modules) == 0 {
		t.Error("expected at least one module in admin config")
	}
}

func TestLoadConfigRaw_NonEmpty(t *testing.T) {
	raw, err := LoadConfigRaw()
	if err != nil {
		t.Fatalf("LoadConfigRaw: %v", err)
	}
	if len(raw) == 0 {
		t.Error("expected non-empty raw config data")
	}
}

func TestLoadConfig_HasExpectedModules(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	moduleNames := make(map[string]bool)
	for _, m := range cfg.Modules {
		moduleNames[m.Name] = true
	}

	required := []string{"admin-server", "admin-router", "admin-db", "admin-auth"}
	for _, name := range required {
		if !moduleNames[name] {
			t.Errorf("expected module %q in admin config", name)
		}
	}
}

func TestLoadConfig_HasWorkflows(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Workflows) == 0 {
		t.Error("expected at least one workflow in admin config")
	}
}
