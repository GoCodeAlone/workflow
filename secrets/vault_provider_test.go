package secrets

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc allows creating simple mock HTTP clients.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestVaultProviderHTTP_Get_FullData(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Verify request
		if req.Header.Get("X-Vault-Token") != "test-token" {
			t.Errorf("expected X-Vault-Token 'test-token', got %q", req.Header.Get("X-Vault-Token"))
		}
		if !strings.Contains(req.URL.Path, "/v1/secret/data/myapp/config") {
			t.Errorf("unexpected URL path: %s", req.URL.Path)
		}

		body := `{
			"data": {
				"data": {
					"password": "s3cret",
					"username": "admin"
				}
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address:   "https://vault.example.com",
		Token:     "test-token",
		MountPath: "secret",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	// Get full data (no #field)
	val, err := p.Get(context.Background(), "myapp/config")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Should be JSON
	if !strings.Contains(val, "password") || !strings.Contains(val, "s3cret") {
		t.Errorf("expected JSON with password:s3cret, got %q", val)
	}
}

func TestVaultProviderHTTP_Get_SpecificField(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"data": {
				"data": {
					"password": "s3cret",
					"username": "admin"
				}
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	// Get specific field via #field
	val, err := p.Get(context.Background(), "myapp/config#password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "s3cret" {
		t.Errorf("expected 's3cret', got %q", val)
	}
}

func TestVaultProviderHTTP_Get_MissingField(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"data": {
				"data": {
					"password": "s3cret"
				}
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	_, err = p.Get(context.Background(), "myapp/config#nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestVaultProviderHTTP_Get_NotFound(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"errors":[]}`)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	_, err = p.Get(context.Background(), "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestVaultProviderHTTP_Get_WithNamespace(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Vault-Namespace") != "admin" {
			t.Errorf("expected X-Vault-Namespace 'admin', got %q", req.Header.Get("X-Vault-Namespace"))
		}
		body := `{"data":{"data":{"key":"value"}}}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address:   "https://vault.example.com",
		Token:     "test-token",
		Namespace: "admin",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	_, err = p.Get(context.Background(), "myapp/secret#key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestVaultProviderHTTP_Get_EmptyKey(t *testing.T) {
	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}

	_, err = p.Get(context.Background(), "")
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProviderHTTP_Get_CustomMountPath(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/v1/kv/data/") {
			t.Errorf("expected mount path 'kv' in URL, got %s", req.URL.Path)
		}
		body := `{"data":{"data":{"val":"ok"}}}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address:   "https://vault.example.com",
		Token:     "test-token",
		MountPath: "kv",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	p.httpClient = mockClient

	val, err := p.Get(context.Background(), "path#val")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "ok" {
		t.Errorf("expected 'ok', got %q", val)
	}
}

func TestNewVaultProviderHTTP_MissingAddress(t *testing.T) {
	_, err := NewVaultProviderHTTP(VaultConfig{Token: "test"})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestNewVaultProviderHTTP_MissingToken(t *testing.T) {
	_, err := NewVaultProviderHTTP(VaultConfig{Address: "https://vault.example.com"})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestNewVaultProviderHTTP_DefaultMountPath(t *testing.T) {
	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	if p.config.MountPath != "secret" {
		t.Errorf("expected default mount path 'secret', got %q", p.config.MountPath)
	}
}

func TestParseVaultKey(t *testing.T) {
	tests := []struct {
		input string
		path  string
		field string
	}{
		{"secret/path#field", "secret/path", "field"},
		{"just/a/path", "just/a/path", ""},
		{"path#with#multiple#hashes", "path#with#multiple", "hashes"},
		{"#leading", "", "leading"},
	}

	for _, tt := range tests {
		path, field := parseVaultKey(tt.input)
		if path != tt.path || field != tt.field {
			t.Errorf("parseVaultKey(%q) = (%q, %q), want (%q, %q)",
				tt.input, path, field, tt.path, tt.field)
		}
	}
}
