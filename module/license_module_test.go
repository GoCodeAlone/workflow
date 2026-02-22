package module_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/licensing"
	"github.com/GoCodeAlone/workflow/module"
)

// licenseValidateHandler returns a simple handler that always returns a valid professional licensing.
func licenseValidateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"valid": true,
			"license": map[string]any{
				"key":           "test-key",
				"tier":          "professional",
				"organization":  "Test Corp",
				"expires_at":    time.Now().Add(30 * 24 * time.Hour),
				"max_workflows": 10,
				"max_plugins":   20,
				"features":      []string{"feature-a"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func TestNewLicenseModule_DefaultConfig(t *testing.T) {
	m, err := module.NewLicenseModule("license", map[string]any{})
	if err != nil {
		t.Fatalf("NewLicenseModule error: %v", err)
	}
	if m.Name() != "license" {
		t.Errorf("expected name=license, got %s", m.Name())
	}
	if len(m.ProvidesServices()) == 0 {
		t.Error("expected at least one provided service")
	}
}

func TestNewLicenseModule_InvalidDuration(t *testing.T) {
	_, err := module.NewLicenseModule("license", map[string]any{
		"cache_ttl": "not-a-duration",
	})
	if err == nil {
		t.Error("expected error for invalid cache_ttl")
	}
}

func TestLicenseModule_StartStop(t *testing.T) {
	srv := httptest.NewServer(licenseValidateHandler())
	defer srv.Close()

	m, err := module.NewLicenseModule("license", map[string]any{
		"server_url":       srv.URL,
		"license_key":      "test-key",
		"cache_ttl":        "1m",
		"grace_period":     "1h",
		"refresh_interval": "1h",
	})
	if err != nil {
		t.Fatalf("NewLicenseModule: %v", err)
	}

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestLicenseModule_Validator(t *testing.T) {
	srv := httptest.NewServer(licenseValidateHandler())
	defer srv.Close()

	m, err := module.NewLicenseModule("license", map[string]any{
		"server_url":  srv.URL,
		"license_key": "test-key",
		"cache_ttl":   "1m",
	})
	if err != nil {
		t.Fatalf("NewLicenseModule: %v", err)
	}

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(ctx) }()

	v := m.Validator()
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	info := v.GetLicenseInfo()
	if info == nil {
		t.Fatal("expected non-nil license info after Start")
	}
	if info.Tier != "professional" {
		t.Errorf("expected tier=professional, got %s", info.Tier)
	}
}

func TestLicenseModule_ServeHTTP(t *testing.T) {
	srv := httptest.NewServer(licenseValidateHandler())
	defer srv.Close()

	m, err := module.NewLicenseModule("license", map[string]any{
		"server_url":  srv.URL,
		"license_key": "test-key",
		"cache_ttl":   "1m",
	})
	if err != nil {
		t.Fatalf("NewLicenseModule: %v", err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(ctx) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/license/status", nil)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if valid, _ := resp["valid"].(bool); !valid {
		t.Errorf("expected valid=true in response, got: %v", resp)
	}
}

func TestLicenseModule_ProvidesServices(t *testing.T) {
	m, err := module.NewLicenseModule("my-license", map[string]any{})
	if err != nil {
		t.Fatalf("NewLicenseModule: %v", err)
	}
	services := m.ProvidesServices()
	if len(services) < 2 {
		t.Fatalf("expected at least 2 services, got %d", len(services))
	}
	names := make(map[string]bool)
	for _, s := range services {
		names[s.Name] = true
	}
	if !names["my-license"] {
		t.Error("expected service named 'my-license'")
	}
	if !names["license-validator"] {
		t.Error("expected canonical 'license-validator' service")
	}
}

func TestLicenseModule_CanLoadPlugin(t *testing.T) {
	srv := httptest.NewServer(licenseValidateHandler())
	defer srv.Close()

	m, err := module.NewLicenseModule("license", map[string]any{
		"server_url":  srv.URL,
		"license_key": "test-key",
		"cache_ttl":   "1m",
	})
	if err != nil {
		t.Fatalf("NewLicenseModule: %v", err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(ctx) }()

	v := m.Validator()

	tests := []struct {
		tier     string
		expected bool
	}{
		{"core", true},
		{"community", true},
		{"premium", true}, // professional license allows premium
		{"unknown", false},
	}
	for _, tt := range tests {
		got := v.CanLoadPlugin(tt.tier)
		if got != tt.expected {
			t.Errorf("CanLoadPlugin(%q): got %v, want %v", tt.tier, got, tt.expected)
		}
	}
}

// Ensure HTTPValidator satisfies the Validator interface
var _ licensing.Validator = (*licensing.HTTPValidator)(nil)
