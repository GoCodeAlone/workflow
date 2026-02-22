package license

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func validLicenseResponse(tier string) validateResponse {
	return validateResponse{
		Valid: true,
		License: LicenseInfo{
			Key:          "test-key-123",
			Tier:         tier,
			Organization: "Test Corp",
			ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
			MaxWorkflows: 10,
			MaxPlugins:   20,
			Features:     []string{"feature-a", "feature-b"},
		},
	}
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPValidator_ValidLicense(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("professional"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "test-key-123",
		CacheTTL:   5 * time.Minute,
	}, nil)

	result, err := v.Validate(context.Background(), "test-key-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid=true, got false: %s", result.Error)
	}
	if result.License.Tier != "professional" {
		t.Errorf("expected tier=professional, got %s", result.License.Tier)
	}
}

func TestHTTPValidator_InvalidLicense(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validateResponse{
			Valid: false,
			Error: "license key not found",
		})
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "bad-key",
		CacheTTL:   5 * time.Minute,
	}, nil)

	result, err := v.Validate(context.Background(), "bad-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Errorf("expected valid=false, got true")
	}
}

func TestHTTPValidator_CachedResult(t *testing.T) {
	callCount := 0
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("enterprise"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "test-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	// First call — hits server
	_, err := v.Validate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("first validate: %v", err)
	}

	// Second call — should use cache
	_, err = v.Validate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("second validate: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 server call, got %d", callCount)
	}
}

func TestHTTPValidator_GracePeriod(t *testing.T) {
	// First response is valid, then server goes away
	var mu sync.Mutex
	serverDown := false

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		down := serverDown
		mu.Unlock()
		if down {
			http.Error(w, "gone", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("professional"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:   srv.URL,
		LicenseKey:  "test-key",
		CacheTTL:    1 * time.Millisecond, // expire immediately
		GracePeriod: 1 * time.Hour,
	}, nil)

	// Populate cache with valid result
	result, err := v.Validate(context.Background(), "test-key")
	if err != nil || !result.Valid {
		t.Fatalf("initial validation failed: err=%v valid=%v", err, result.Valid)
	}

	// Record last validated time (already set internally)
	// Simulate server going down
	mu.Lock()
	serverDown = true
	mu.Unlock()

	// Wait for cache to expire
	time.Sleep(5 * time.Millisecond)

	// Should still be valid due to grace period
	result, err = v.Validate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("grace period validate error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid=true during grace period, got false: %s", result.Error)
	}
}

func TestHTTPValidator_GracePeriodExpired(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("professional"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:   srv.URL,
		LicenseKey:  "test-key",
		CacheTTL:    1 * time.Millisecond,
		GracePeriod: 1 * time.Millisecond, // expire immediately
	}, nil)

	// Populate cache
	_, err := v.Validate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("initial validation: %v", err)
	}

	// Take server down and wait for grace period to expire
	srv.Close()
	time.Sleep(10 * time.Millisecond)

	result, err := v.Validate(context.Background(), "test-key")
	if err != nil {
		// Depending on timing, error or invalid result are both acceptable
		return
	}
	if result.Valid {
		t.Errorf("expected valid=false after grace period expiry, got true")
	}
}

func TestHTTPValidator_CheckFeature(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("enterprise"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "test-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	if _, err := v.Validate(context.Background(), "test-key"); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !v.CheckFeature("feature-a") {
		t.Error("expected feature-a to be available")
	}
	if v.CheckFeature("feature-z") {
		t.Error("expected feature-z to not be available")
	}
}

func TestHTTPValidator_ConcurrentAccess(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("professional"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "test-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := v.Validate(context.Background(), "test-key")
			if err != nil {
				t.Errorf("concurrent validate error: %v", err)
				return
			}
			if !result.Valid {
				t.Errorf("expected valid result in concurrent access")
			}
		}()
	}
	wg.Wait()
}

func TestHTTPValidator_NoServerURL(t *testing.T) {
	// No server URL → starter license is synthesized
	v := NewHTTPValidator(ValidatorConfig{
		LicenseKey: "any-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	result, err := v.Validate(context.Background(), "any-key")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid=true for no-server mode")
	}
	if result.License.Tier != "starter" {
		t.Errorf("expected tier=starter, got %s", result.License.Tier)
	}
}

func TestHTTPValidator_CanLoadPlugin(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validLicenseResponse("professional"))
	})

	v := NewHTTPValidator(ValidatorConfig{
		ServerURL:  srv.URL,
		LicenseKey: "test-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	if _, err := v.Validate(context.Background(), "test-key"); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !v.CanLoadPlugin("core") {
		t.Error("core plugins should always be loadable")
	}
	if !v.CanLoadPlugin("community") {
		t.Error("community plugins should always be loadable")
	}
	if !v.CanLoadPlugin("premium") {
		t.Error("professional license should allow premium plugins")
	}
}

func TestHTTPValidator_CanLoadPlugin_StarterTier(t *testing.T) {
	v := NewHTTPValidator(ValidatorConfig{
		LicenseKey: "any-key",
		CacheTTL:   10 * time.Minute,
	}, nil)

	if _, err := v.Validate(context.Background(), "any-key"); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !v.CanLoadPlugin("core") {
		t.Error("core plugins should always be loadable")
	}
	if !v.CanLoadPlugin("community") {
		t.Error("community plugins should always be loadable")
	}
	if v.CanLoadPlugin("premium") {
		t.Error("starter license should not allow premium plugins")
	}
}
