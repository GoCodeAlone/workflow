package secrets

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAWSProvider_Get_PlainSecret(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Verify it's a POST request
		if req.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", req.Method)
		}

		// Verify headers
		if req.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" {
			t.Errorf("unexpected X-Amz-Target: %s", req.Header.Get("X-Amz-Target"))
		}
		if req.Header.Get("Content-Type") != "application/x-amz-json-1.1" {
			t.Errorf("unexpected Content-Type: %s", req.Header.Get("Content-Type"))
		}
		// Verify Authorization header is present
		if req.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		if !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Errorf("expected AWS4-HMAC-SHA256 auth, got %s", req.Header.Get("Authorization"))
		}

		body := `{"SecretString":"my-secret-value","Name":"test-secret","ARN":"arn:aws:secretsmanager:us-east-1:123456:secret:test-secret"}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}, mockClient)

	val, err := p.Get(context.Background(), "test-secret")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "my-secret-value" {
		t.Errorf("expected 'my-secret-value', got %q", val)
	}
}

func TestAWSProvider_Get_JSONFieldExtraction(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"SecretString":"{\"username\":\"admin\",\"password\":\"s3cret\"}","Name":"db-creds"}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, mockClient)

	// Extract specific field
	val, err := p.Get(context.Background(), "db-creds#password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "s3cret" {
		t.Errorf("expected 's3cret', got %q", val)
	}
}

func TestAWSProvider_Get_JSONFieldNotFound(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"SecretString":"{\"username\":\"admin\"}","Name":"db-creds"}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, mockClient)

	_, err := p.Get(context.Background(), "db-creds#nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestAWSProvider_Get_NotFound(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"__type":"ResourceNotFoundException","Message":"Secrets Manager can't find the specified secret."}`
		return &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, mockClient)

	_, err := p.Get(context.Background(), "nonexistent-secret")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestAWSProvider_Get_EmptyKey(t *testing.T) {
	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, nil)

	_, err := p.Get(context.Background(), "")
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestAWSProvider_Get_EmptySecretString(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"Name":"binary-secret","ARN":"arn:..."}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, mockClient)

	_, err := p.Get(context.Background(), "binary-secret")
	if err == nil {
		t.Fatal("expected error for empty secret string")
	}
}

func TestAWSProvider_Get_RegionInEndpoint(t *testing.T) {
	mockClient := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		expectedHost := "secretsmanager.eu-west-1.amazonaws.com"
		if req.URL.Host != expectedHost {
			t.Errorf("expected host %q, got %q", expectedHost, req.URL.Host)
		}
		body := `{"SecretString":"val","Name":"test"}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "eu-west-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, mockClient)

	_, err := p.Get(context.Background(), "test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestAWSProvider_SetDeleteListUnsupported(t *testing.T) {
	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, nil)
	ctx := context.Background()

	if err := p.Set(ctx, "key", "val"); err == nil {
		t.Error("expected error for Set")
	}
	if err := p.Delete(ctx, "key"); err == nil {
		t.Error("expected error for Delete")
	}
	if _, err := p.List(ctx); err == nil {
		t.Error("expected error for List")
	}
}

func TestAWSProvider_Name(t *testing.T) {
	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, nil)
	if p.Name() != "aws-sm" {
		t.Errorf("expected 'aws-sm', got %q", p.Name())
	}
}

func TestAWSProvider_Config(t *testing.T) {
	cfg := AWSConfig{
		Region:          "eu-west-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}
	p := NewAWSSecretsManagerProviderWithClient(cfg, nil)
	if p.Config().Region != "eu-west-1" {
		t.Errorf("expected 'eu-west-1', got %q", p.Config().Region)
	}
}

func TestNewAWSProvider_DefaultRegion(t *testing.T) {
	p := NewAWSSecretsManagerProviderWithClient(AWSConfig{
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}, nil)
	if p.Config().Region != "us-east-1" {
		t.Errorf("expected default region 'us-east-1', got %q", p.Config().Region)
	}
}

func TestNewAWSProvider_EnvFallback(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-akid")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")

	p, err := NewAWSSecretsManagerProvider(AWSConfig{Region: "us-west-2"})
	if err != nil {
		t.Fatalf("NewAWSSecretsManagerProvider: %v", err)
	}
	if p.Config().AccessKeyID != "env-akid" {
		t.Errorf("expected 'env-akid', got %q", p.Config().AccessKeyID)
	}
	if p.Config().SecretAccessKey != "env-secret" {
		t.Errorf("expected 'env-secret', got %q", p.Config().SecretAccessKey)
	}
}

func TestNewAWSProvider_MissingCredentials(t *testing.T) {
	// Clear env to ensure no fallback
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	_, err := NewAWSSecretsManagerProvider(AWSConfig{Region: "us-east-1"})
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestParseAWSKey(t *testing.T) {
	tests := []struct {
		input      string
		secretName string
		field      string
	}{
		{"my-secret", "my-secret", ""},
		{"my-secret#password", "my-secret", "password"},
		{"complex/path#field", "complex/path", "field"},
		{"no-hash", "no-hash", ""},
	}

	for _, tt := range tests {
		name, field := parseAWSKey(tt.input)
		if name != tt.secretName || field != tt.field {
			t.Errorf("parseAWSKey(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, field, tt.secretName, tt.field)
		}
	}
}

func TestExtractJSONField(t *testing.T) {
	tests := []struct {
		json    string
		field   string
		want    string
		wantErr bool
	}{
		{`{"user":"admin","pass":"secret"}`, "pass", "secret", false},
		{`{"user":"admin"}`, "missing", "", true},
		{`not-json`, "field", "", true},
		{`{"num":42}`, "num", "42", false},
	}

	for _, tt := range tests {
		val, err := extractJSONField(tt.json, tt.field)
		if (err != nil) != tt.wantErr {
			t.Errorf("extractJSONField(%q, %q): err=%v, wantErr=%v", tt.json, tt.field, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && val != tt.want {
			t.Errorf("extractJSONField(%q, %q) = %q, want %q", tt.json, tt.field, val, tt.want)
		}
	}
}

func TestDeriveSigningKey(t *testing.T) {
	// This is a basic sanity check that the signing key derivation produces
	// consistent output (deterministic).
	key1 := deriveSigningKey("secret", "20260101", "us-east-1", "secretsmanager")
	key2 := deriveSigningKey("secret", "20260101", "us-east-1", "secretsmanager")

	if len(key1) != 32 { // HMAC-SHA256 produces 32 bytes
		t.Errorf("expected 32 bytes, got %d", len(key1))
	}

	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("signing key derivation is not deterministic")
		}
	}

	// Different inputs should produce different keys
	key3 := deriveSigningKey("different", "20260101", "us-east-1", "secretsmanager")
	same := true
	for i := range key1 {
		if key1[i] != key3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different secrets should produce different signing keys")
	}
}
