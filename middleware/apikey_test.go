package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	key := &store.APIKey{
		Name:        "valid-key",
		CompanyID:   uuid.New(),
		Permissions: []string{"read", "write"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var capturedTenant *TenantContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, ok := TenantFromContext(r.Context())
		if ok {
			capturedTenant = tc
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := APIKeyAuth(s, inner)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("valid key: expected 200, got %d", rec.Code)
	}
	if capturedTenant == nil {
		t.Fatal("TenantContext not set in request context")
	}
	if capturedTenant.CompanyID != key.CompanyID {
		t.Errorf("CompanyID: got %v, want %v", capturedTenant.CompanyID, key.CompanyID)
	}
	if len(capturedTenant.Permissions) != 2 {
		t.Errorf("Permissions: got %v, want [read write]", capturedTenant.Permissions)
	}
	if capturedTenant.APIKeyID != key.ID {
		t.Errorf("APIKeyID: got %v, want %v", capturedTenant.APIKeyID, key.ID)
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()

	handler := APIKeyAuth(s, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", "wf_invalid000000000000000000000000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid key: expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_NoHeader_Passthrough(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()

	var called bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Ensure no tenant context was set.
		_, ok := TenantFromContext(r.Context())
		if ok {
			t.Error("TenantContext should not be set when no API key header")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := APIKeyAuth(s, inner)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// No X-API-Key header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("no header: expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("inner handler was not called")
	}
}

func TestAPIKeyAuth_ExpiredKey(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	pastTime := time.Now().Add(-1 * time.Hour)
	key := &store.APIKey{
		Name:        "expired-key",
		CompanyID:   uuid.New(),
		Permissions: []string{"read"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
		ExpiresAt:   &pastTime,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	handler := APIKeyAuth(s, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired key: expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_InactiveKey(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	key := &store.APIKey{
		Name:        "inactive-key",
		CompanyID:   uuid.New(),
		Permissions: []string{"read"},
		CreatedBy:   uuid.New(),
		IsActive:    false,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	handler := APIKeyAuth(s, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("inactive key: expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_ScopedKey(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	orgID := uuid.New()
	projectID := uuid.New()

	key := &store.APIKey{
		Name:        "scoped-key",
		CompanyID:   uuid.New(),
		OrgID:       &orgID,
		ProjectID:   &projectID,
		Permissions: []string{"read", "write", "admin"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var capturedTenant *TenantContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, ok := TenantFromContext(r.Context())
		if ok {
			capturedTenant = tc
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := APIKeyAuth(s, inner)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("scoped key: expected 200, got %d", rec.Code)
	}
	if capturedTenant == nil {
		t.Fatal("TenantContext not set")
	}
	if capturedTenant.OrgID == nil || *capturedTenant.OrgID != orgID {
		t.Errorf("OrgID: got %v, want %v", capturedTenant.OrgID, orgID)
	}
	if capturedTenant.ProjectID == nil || *capturedTenant.ProjectID != projectID {
		t.Errorf("ProjectID: got %v, want %v", capturedTenant.ProjectID, projectID)
	}
}

func TestAPIKeyAuth_APIKeyInContext(t *testing.T) {
	s := store.NewInMemoryAPIKeyStore()
	ctx := context.Background()

	key := &store.APIKey{
		Name:        "context-key",
		CompanyID:   uuid.New(),
		Permissions: []string{"read"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var capturedKey *store.APIKey
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := r.Context().Value(APIKeyContextKey)
		if val != nil {
			capturedKey = val.(*store.APIKey)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := APIKeyAuth(s, inner)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedKey == nil {
		t.Fatal("APIKey not set in context")
	}
	if capturedKey.ID != key.ID {
		t.Errorf("APIKey ID: got %v, want %v", capturedKey.ID, key.ID)
	}
}
