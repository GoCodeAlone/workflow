package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- Config Tests ---

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := Config{
		Issuer:   "https://auth.example.com",
		ClientID: "my-client",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestConfig_Validate_MissingIssuer(t *testing.T) {
	cfg := Config{ClientID: "my-client"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing issuer")
	}
}

func TestConfig_Validate_MissingClientID(t *testing.T) {
	cfg := Config{Issuer: "https://auth.example.com"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

// --- Mock HTTP Client ---

type mockTransport struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.handler(req)
}

func newMockClient(handler func(req *http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{
		Transport: &mockTransport{handler: handler},
		Timeout:   5 * time.Second,
	}
}

// --- Discovery Tests ---

func TestProvider_Discover_Success(t *testing.T) {
	doc := DiscoveryDocument{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
		UserInfoEndpoint:      "https://auth.example.com/userinfo",
		JWKSURI:               "https://auth.example.com/.well-known/jwks.json",
	}
	docBytes, _ := json.Marshal(doc)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/.well-known/openid-configuration") {
			return &http.Response{StatusCode: http.StatusNotFound}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(docBytes))),
		}, nil
	})

	p, err := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, client)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	got, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if got.AuthorizationEndpoint != doc.AuthorizationEndpoint {
		t.Errorf("expected auth endpoint %q, got %q", doc.AuthorizationEndpoint, got.AuthorizationEndpoint)
	}
	if got.TokenEndpoint != doc.TokenEndpoint {
		t.Errorf("expected token endpoint %q, got %q", doc.TokenEndpoint, got.TokenEndpoint)
	}
}

func TestProvider_Discover_CachesResult(t *testing.T) {
	calls := 0
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		calls++
		doc := DiscoveryDocument{Issuer: "https://auth.example.com"}
		b, _ := json.Marshal(doc)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(b))),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, client)

	_, _ = p.Discover(context.Background())
	_, _ = p.Discover(context.Background())

	if calls != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", calls)
	}
}

func TestProvider_Discover_HTTPError(t *testing.T) {
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("error")),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, client)

	_, err := p.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

// --- Authorization URL Tests ---

func TestProvider_AuthorizationURL(t *testing.T) {
	doc := DiscoveryDocument{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
	}
	docBytes, _ := json.Marshal(doc)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(docBytes))),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:      "https://auth.example.com",
		ClientID:    "test-client",
		RedirectURI: "https://app.example.com/callback",
		Scopes:      []string{"openid", "profile"},
	}, client)

	url, err := p.AuthorizationURL(context.Background(), "test-state")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}

	if !strings.Contains(url, "client_id=test-client") {
		t.Error("expected client_id in URL")
	}
	if !strings.Contains(url, "state=test-state") {
		t.Error("expected state in URL")
	}
	if !strings.Contains(url, "response_type=code") {
		t.Error("expected response_type=code in URL")
	}
}

// --- Token Exchange Tests ---

func TestProvider_ExchangeCode_Success(t *testing.T) {
	tokenResp := TokenResponse{
		AccessToken: "access-token-123",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		IDToken:     "id-token-123",
	}
	tokenBytes, _ := json.Marshal(tokenResp)

	doc := DiscoveryDocument{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
	}
	docBytes, _ := json.Marshal(doc)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/token") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(tokenBytes))),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(docBytes))),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:       "https://auth.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURI:  "https://app.example.com/callback",
	}, client)

	got, err := p.ExchangeCode(context.Background(), "auth-code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	if got.AccessToken != tokenResp.AccessToken {
		t.Errorf("expected access token %q, got %q", tokenResp.AccessToken, got.AccessToken)
	}
	if got.IDToken != tokenResp.IDToken {
		t.Errorf("expected ID token %q, got %q", tokenResp.IDToken, got.IDToken)
	}
}

func TestProvider_ExchangeCode_Error(t *testing.T) {
	doc := DiscoveryDocument{
		Issuer:        "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	docBytes, _ := json.Marshal(doc)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/token") {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(docBytes))),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:       "https://auth.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, client)

	_, err := p.ExchangeCode(context.Background(), "bad-code")
	if err == nil {
		t.Fatal("expected error for bad token exchange")
	}
}

// --- ParseIDTokenUnverified Tests ---

func TestParseIDTokenUnverified_ValidToken(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":            "user-123",
		"email":          "user@example.com",
		"email_verified": true,
		"name":           "Test User",
		"iss":            "https://auth.example.com",
		"aud":            "my-client",
		"exp":            float64(time.Now().Add(time.Hour).Unix()),
		"iat":            float64(time.Now().Unix()),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte("test-secret-key-for-signing-only"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	got, err := ParseIDTokenUnverified(tokenStr)
	if err != nil {
		t.Fatalf("ParseIDTokenUnverified: %v", err)
	}

	if got.Subject != "user-123" {
		t.Errorf("expected subject 'user-123', got %q", got.Subject)
	}
	if got.Email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", got.Email)
	}
	if !got.EmailVerified {
		t.Error("expected email_verified=true")
	}
	if got.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %q", got.Name)
	}
	if got.Issuer != "https://auth.example.com" {
		t.Errorf("expected issuer 'https://auth.example.com', got %q", got.Issuer)
	}
}

func TestParseIDTokenUnverified_WithGroups(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":    "user-456",
		"groups": []any{"admins", "devs"},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	got, err := ParseIDTokenUnverified(tokenStr)
	if err != nil {
		t.Fatalf("ParseIDTokenUnverified: %v", err)
	}

	if len(got.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got.Groups))
	}
	if got.Groups[0] != "admins" || got.Groups[1] != "devs" {
		t.Errorf("unexpected groups: %v", got.Groups)
	}
}

func TestParseIDTokenUnverified_InvalidToken(t *testing.T) {
	_, err := ParseIDTokenUnverified("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

// --- GenerateState Tests ---

func TestGenerateState(t *testing.T) {
	s1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if s1 == "" {
		t.Fatal("expected non-empty state")
	}

	s2, _ := GenerateState()
	if s1 == s2 {
		t.Error("expected different state values on successive calls")
	}
}

// --- CallbackHandler Tests ---

func TestCallbackHandler_Success(t *testing.T) {
	idClaims := jwt.MapClaims{
		"sub":   "user-789",
		"email": "callback@example.com",
	}
	idToken := jwt.NewWithClaims(jwt.SigningMethodHS256, idClaims)
	idTokenStr, _ := idToken.SignedString([]byte("test-key"))

	tokenResp := TokenResponse{
		AccessToken: "access-123",
		IDToken:     idTokenStr,
	}
	tokenBytes, _ := json.Marshal(tokenResp)

	doc := DiscoveryDocument{
		Issuer:        "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	docBytes, _ := json.Marshal(doc)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/token") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(tokenBytes))),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(docBytes))),
		}, nil
	})

	p, _ := NewProvider(Config{
		Issuer:       "https://auth.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURI:  "https://app.example.com/callback",
	}, client)

	var gotClaims *Claims
	handler := p.CallbackHandler(func(w http.ResponseWriter, r *http.Request, claims *Claims, tokens *TokenResponse) {
		gotClaims = claims
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/callback?code=auth-code&state=test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotClaims == nil {
		t.Fatal("expected claims to be set")
	}
	if gotClaims.Subject != "user-789" {
		t.Errorf("expected subject 'user-789', got %q", gotClaims.Subject)
	}
}

func TestCallbackHandler_MissingCode(t *testing.T) {
	p, _ := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}))

	handler := p.CallbackHandler(func(w http.ResponseWriter, r *http.Request, claims *Claims, tokens *TokenResponse) {
		t.Fatal("should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCallbackHandler_ErrorParam(t *testing.T) {
	p, _ := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}))

	handler := p.CallbackHandler(func(w http.ResponseWriter, r *http.Request, claims *Claims, tokens *TokenResponse) {
		t.Fatal("should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/callback?code=x&error=access_denied&error_description=user+denied", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- NewProvider Tests ---

func TestNewProvider_InvalidConfig(t *testing.T) {
	_, err := NewProvider(Config{}, nil)
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

func TestNewProvider_DefaultScopes(t *testing.T) {
	p, err := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test",
	}, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	cfg := p.Config()
	if len(cfg.Scopes) != 3 {
		t.Errorf("expected 3 default scopes, got %d", len(cfg.Scopes))
	}
}

func TestProvider_Discover_NetworkError(t *testing.T) {
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network error")
	})

	p, _ := NewProvider(Config{
		Issuer:   "https://auth.example.com",
		ClientID: "test-client",
	}, client)

	_, err := p.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}
