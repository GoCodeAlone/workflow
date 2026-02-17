package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

// okHandler is a simple handler that returns 200 OK with a body.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
})

func TestAuthorizedRequestPassesThrough(t *testing.T) {
	auth := platform.NewStdTierAuthorizer()
	mw := NewTierBoundaryMiddleware(auth)
	handler := mw.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/tier3/resources", nil)
	req = req.WithContext(WithRole(req.Context(), platform.RoleTierAdmin))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUnauthorizedRequestReturns403(t *testing.T) {
	auth := platform.NewStdTierAuthorizer()
	mw := NewTierBoundaryMiddleware(auth)
	handler := mw.Wrap(okHandler)

	// Viewer trying to create (POST) on Tier 1.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tier1/resources", nil)
	req = req.WithContext(WithRole(req.Context(), platform.RoleTierViewer))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp errorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if resp.Code != http.StatusForbidden {
		t.Errorf("expected code 403 in body, got %d", resp.Code)
	}
	if resp.Message == "" {
		t.Error("expected non-empty error message")
	}
}

func TestMissingRoleReturns401(t *testing.T) {
	auth := platform.NewStdTierAuthorizer()
	mw := NewTierBoundaryMiddleware(auth)
	handler := mw.Wrap(okHandler)

	// No role in context.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/tier1/resources", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestTierExtractionFromPath(t *testing.T) {
	tests := []struct {
		path    string
		want    platform.Tier
		wantErr bool
	}{
		{"/api/v1/platform/tier1/resources", platform.TierInfrastructure, false},
		{"/api/v1/platform/tier2/resources", platform.TierSharedPrimitive, false},
		{"/api/v1/platform/tier3/resources", platform.TierApplication, false},
		{"/api/v1/platform/infrastructure/resources", platform.TierInfrastructure, false},
		{"/api/v1/platform/application/resources", platform.TierApplication, false},
		{"/api/v1/platform/shared-primitive/resources", platform.TierSharedPrimitive, false},
		{"/api/v1/other/path", 0, true},
		{"/", 0, true},
	}

	for _, tt := range tests {
		tier, err := TierFromPath(tt.path)
		if tt.wantErr {
			if err == nil {
				t.Errorf("TierFromPath(%q): expected error, got tier %v", tt.path, tier)
			}
			continue
		}
		if err != nil {
			t.Errorf("TierFromPath(%q): unexpected error: %v", tt.path, err)
			continue
		}
		if tier != tt.want {
			t.Errorf("TierFromPath(%q): got tier %v, want %v", tt.path, tier, tt.want)
		}
	}
}

func TestWithRealHTTPServer(t *testing.T) {
	auth := platform.NewStdTierAuthorizer()
	mw := NewTierBoundaryMiddleware(auth)

	// Build a mux that injects a role for testing, then wraps with the middleware.
	inner := mw.Wrap(okHandler)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/platform/", withTestRole(platform.RoleTierViewer, inner))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET should be allowed for viewer.
	resp, err := http.Get(srv.URL + "/api/v1/platform/tier3/resources")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", resp.StatusCode)
	}

	// DELETE should be denied for viewer.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/platform/tier3/resources/foo", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for DELETE, got %d", resp.StatusCode)
	}
}

func TestNoTierInPathPassesThrough(t *testing.T) {
	auth := platform.NewStdTierAuthorizer()
	mw := NewTierBoundaryMiddleware(auth)
	handler := mw.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req = req.WithContext(WithRole(req.Context(), platform.RoleTierViewer))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for path without tier, got %d", rr.Code)
	}
}

func TestHTTPMethodToOperation(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, "read"},
		{http.MethodHead, "read"},
		{http.MethodOptions, "read"},
		{http.MethodPost, "create"},
		{http.MethodPut, "update"},
		{http.MethodPatch, "update"},
		{http.MethodDelete, "delete"},
	}

	for _, tt := range tests {
		got := operationFromMethod(tt.method)
		if got != tt.want {
			t.Errorf("operationFromMethod(%q) = %q, want %q", tt.method, got, tt.want)
		}
	}
}

// withTestRole is a helper that injects a TierRole into the request context.
func withTestRole(role platform.TierRole, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithRole(r.Context(), role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
