package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- HostSelector ---

func TestHostSelector_Match(t *testing.T) {
	sel := &HostSelector{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "acme.example.com:8080"

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "acme.example.com" {
		t.Errorf("expected host without port, got %q", key)
	}
}

func TestHostSelector_MixedCase(t *testing.T) {
	sel := &HostSelector{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "ACME.EXAMPLE.COM:8080"

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "acme.example.com" {
		t.Errorf("expected lowercased host without port, got %q", key)
	}
}

func TestHostSelector_Empty(t *testing.T) {
	sel := &HostSelector{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("empty host should not match")
	}
}

// --- SubdomainSelector ---

func TestSubdomainSelector_Match(t *testing.T) {
	sel := &SubdomainSelector{RootDomain: "example.com"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "acme.example.com"

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "acme" {
		t.Errorf("expected subdomain 'acme', got %q", key)
	}
}

func TestSubdomainSelector_NoSubdomain(t *testing.T) {
	sel := &SubdomainSelector{RootDomain: "example.com"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("root domain without subdomain should not match")
	}
}

// --- HeaderSelector ---

func TestHeaderSelector_Match(t *testing.T) {
	sel := &HeaderSelector{Header: "X-Tenant-ID"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "tenant-42")

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "tenant-42" {
		t.Errorf("expected 'tenant-42', got %q", key)
	}
}

func TestHeaderSelector_Missing(t *testing.T) {
	sel := &HeaderSelector{Header: "X-Tenant-ID"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("missing header should not match")
	}
}

// --- CookieSelector ---

func TestCookieSelector_Match(t *testing.T) {
	sel := &CookieSelector{Cookie: "tenant_id"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "tenant_id", Value: "tenant-99"})

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "tenant-99" {
		t.Errorf("expected 'tenant-99', got %q", key)
	}
}

func TestCookieSelector_Missing(t *testing.T) {
	sel := &CookieSelector{Cookie: "tenant_id"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("missing cookie should not match")
	}
}

// --- JWTClaimSelector ---

func TestJWTClaimSelector_Match(t *testing.T) {
	sel := &JWTClaimSelector{Claim: "tenant_id"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Simulate auth middleware having put claims in context.
	claims := map[string]any{"tenant_id": "tenant-jwt", "sub": "user@example.com"}
	ctx := context.WithValue(req.Context(), authClaimsContextKey, claims)
	req = req.WithContext(ctx)

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "tenant-jwt" {
		t.Errorf("expected 'tenant-jwt', got %q", key)
	}
}

func TestJWTClaimSelector_NoClaims(t *testing.T) {
	sel := &JWTClaimSelector{Claim: "tenant_id"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("no claims in context should not match")
	}
}

// --- SessionSelector ---

func TestSessionSelector_Match(t *testing.T) {
	sel := &SessionSelector{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), sessionTenantIDContextKey, "tenant-session")
	req = req.WithContext(ctx)

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true")
	}
	if key != "tenant-session" {
		t.Errorf("expected 'tenant-session', got %q", key)
	}
}

func TestSessionSelector_NoSession(t *testing.T) {
	sel := &SessionSelector{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("missing session should not match")
	}
}

// --- StaticSelector ---

func TestStaticSelector_Match(t *testing.T) {
	sel := &StaticSelector{TenantID: "default-tenant"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	key, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected matched=true for static selector")
	}
	if key != "default-tenant" {
		t.Errorf("expected 'default-tenant', got %q", key)
	}
}

func TestStaticSelector_Empty(t *testing.T) {
	sel := &StaticSelector{TenantID: ""}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, matched, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("empty static tenant should not match")
	}
}
