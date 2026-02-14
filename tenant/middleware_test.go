package tenant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTenantFromContext(t *testing.T) {
	ctx := context.Background()
	if got := TenantFromContext(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = ContextWithTenant(ctx, "t1")
	if got := TenantFromContext(ctx); got != "t1" {
		t.Errorf("expected t1, got %q", got)
	}
}

func TestTenantIsolationMiddleware(t *testing.T) {
	iso := NewTenantIsolation()

	handler := iso.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := TenantFromContext(r.Context())
		w.Write([]byte(tenantID))
	}))

	t.Run("missing tenant header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("valid tenant header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "tenant-abc")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if rec.Body.String() != "tenant-abc" {
			t.Errorf("expected tenant-abc in body, got %q", rec.Body.String())
		}
	})

	t.Run("whitespace-only header treated as missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "  ")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})
}

func TestTenantIsolationAllowedTenants(t *testing.T) {
	iso := NewTenantIsolation()
	iso.SetAllowedTenants([]string{"t1", "t2"})

	handler := iso.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed tenant", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "t1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("forbidden tenant", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "t3")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
	})
}

func TestTenantIsolationOptional(t *testing.T) {
	iso := NewTenantIsolation()
	iso.RequireTenantID = false

	handler := iso.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := TenantFromContext(r.Context())
		w.Write([]byte(tenantID))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestQuotaEnforcerMiddleware(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1 << 30,
		MaxAPIRequestsPerMinute: 2,
	})

	enforcer := NewQuotaEnforcer(reg)

	handler := enforcer.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("within quota", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		ctx := ContextWithTenant(req.Context(), "t1")
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		// Exhaust the API quota
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			ctx := ContextWithTenant(req.Context(), "t1")
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}

		// This should be rate limited
		req := httptest.NewRequest("GET", "/test", nil)
		ctx := ContextWithTenant(req.Context(), "t1")
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["error"] == "" {
			t.Error("expected error message in body")
		}

		if rec.Header().Get("Retry-After") == "" {
			t.Error("expected Retry-After header")
		}
	})

	t.Run("no tenant passes through", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})
}
