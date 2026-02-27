package module

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// computeTestHMAC is a test helper to compute HMAC-SHA256.
func computeTestHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerifyStep_ValidGitHub(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-gh", map[string]any{
		"provider": "github",
		"secret":   "my-secret",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"action":"opened","number":1}`)
	sig := "sha256=" + computeTestHMAC("my-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false on valid signature, got true (reason: %v)", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_InvalidGitHub(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-gh-bad", map[string]any{
		"provider": "github",
		"secret":   "my-secret",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"action":"opened"}`)
	badSig := "sha256=" + computeTestHMAC("wrong-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", badSig)

	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true on invalid signature")
	}
	if result.Output["verified"] != false {
		t.Errorf("expected verified=false, got %v", result.Output["verified"])
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", w.Code)
	}
}

func TestWebhookVerifyStep_MissingGitHubHeader(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-gh-missing", map[string]any{
		"provider": "github",
		"secret":   "my-secret",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	// No X-Hub-Signature-256 header

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true on missing signature header")
	}
}

func TestWebhookVerifyStep_ValidStripe(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-stripe", map[string]any{
		"provider": "stripe",
		"secret":   "whsec_test",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"type":"payment_intent.succeeded"}`)
	timestamp := time.Now().Unix()
	signedPayload := fmt.Sprintf("%d.%s", timestamp, string(body))
	sig := computeTestHMAC("whsec_test", signedPayload)
	stripeHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, sig)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", stripeHeader)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false on valid Stripe signature, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_StripeExpiredTimestamp(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-stripe-expired", map[string]any{
		"provider": "stripe",
		"secret":   "whsec_test",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"type":"payment_intent.succeeded"}`)
	// Timestamp 10 minutes in the past — beyond the 5-minute tolerance
	timestamp := time.Now().Add(-10 * time.Minute).Unix()
	signedPayload := fmt.Sprintf("%d.%s", timestamp, string(body))
	sig := computeTestHMAC("whsec_test", signedPayload)
	stripeHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, sig)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", stripeHeader)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true for expired Stripe timestamp")
	}
	reason, _ := result.Output["reason"].(string)
	if !strings.Contains(reason, "too old") {
		t.Errorf("expected 'too old' in reason, got: %q", reason)
	}
}

func TestWebhookVerifyStep_InvalidStripeSignature(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-stripe-bad", map[string]any{
		"provider": "stripe",
		"secret":   "whsec_test",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"type":"payment_intent.succeeded"}`)
	timestamp := time.Now().Unix()
	// Use wrong secret to generate signature
	sig := computeTestHMAC("wrong-secret", fmt.Sprintf("%d.%s", timestamp, string(body)))
	stripeHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, sig)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", stripeHeader)

	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true on invalid Stripe signature")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", w.Code)
	}
}

func TestWebhookVerifyStep_ValidGeneric(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-generic", map[string]any{
		"provider": "generic",
		"secret":   "generic-secret",
		"header":   "X-My-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	sig := computeTestHMAC("generic-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook/custom", bytes.NewReader(body))
	req.Header.Set("X-My-Signature", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false on valid generic signature, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_GenericDefaultHeader(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-generic-default", map[string]any{
		"provider": "generic",
		"secret":   "generic-secret",
		// no "header" field — should default to X-Signature
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	sig := computeTestHMAC("generic-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook/custom", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false on valid generic signature (default header), reason: %v", result.Output["reason"])
	}
}

func TestWebhookVerifyStep_MissingGenericHeader(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-generic-missing", map[string]any{
		"provider": "generic",
		"secret":   "generic-secret",
		"header":   "X-My-Sig",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/custom", strings.NewReader(`{}`))
	// No X-My-Sig header

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when signature header is missing")
	}
}

func TestWebhookVerifyStep_NoHTTPRequest(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-no-req", map[string]any{
		"provider": "github",
		"secret":   "my-secret",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No _http_request in metadata
	pc := NewPipelineContext(nil, nil)

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when no HTTP request is in context")
	}
}

func TestWebhookVerifyStep_FactoryRejectsMissingProvider(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("bad-verify", map[string]any{
		"secret": "my-secret",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'provider'")
	}
}

func TestWebhookVerifyStep_FactoryRejectsUnknownProvider(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("bad-verify", map[string]any{
		"provider": "unknown-provider",
		"secret":   "my-secret",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestWebhookVerifyStep_FactoryRejectsMissingSecret(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("bad-verify", map[string]any{
		"provider": "github",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'secret'")
	}
}

func TestWebhookVerifyStep_RawBodyCachedInMetadata(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-cached-body", map[string]any{
		"provider": "github",
		"secret":   "cached-secret",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"cached":"body"}`)
	sig := "sha256=" + computeTestHMAC("cached-secret", string(body))

	// Provide the body as raw bytes in metadata (simulating pre-read body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", http.NoBody)
	req.Header.Set("X-Hub-Signature-256", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
		"_raw_body":     body,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false when using cached body, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

// --- Scheme-based tests ---

func computeTestHMACSHA1Base64(secret, data string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerifyStep_SchemeHMACSHA1_Valid(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-twilio", map[string]any{
		"scheme":              "hmac-sha1",
		"secret":              "twilio-secret",
		"signature_header":    "X-Twilio-Signature",
		"url_reconstruction":  true,
		"include_form_params": true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Build form-encoded body
	formBody := "Body=Hello&From=%2B1234567890&To=%2B0987654321"
	// Twilio signing input: URL + sorted form params (key+value concatenated)
	// With url_reconstruction and X-Forwarded-Proto/Host:
	signingInput := "https://example.com/webhook" + "Body" + "Hello" + "From" + "+1234567890" + "To" + "+0987654321"
	sig := computeTestHMACSHA1Base64("twilio-secret", signingInput)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(formBody)))
	req.Header.Set("X-Twilio-Signature", sig)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "example.com")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false on valid Twilio signature, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_SchemeHMACSHA1_Invalid(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-twilio-bad", map[string]any{
		"scheme":              "hmac-sha1",
		"secret":              "twilio-secret",
		"signature_header":    "X-Twilio-Signature",
		"include_form_params": true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	formBody := "Body=Hello"
	sig := computeTestHMACSHA1Base64("wrong-secret", "http://example.com/webhook"+"Body"+"Hello")

	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhook", bytes.NewReader([]byte(formBody)))
	req.Header.Set("X-Twilio-Signature", sig)

	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true on invalid Twilio signature")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", w.Code)
	}
}

func TestWebhookVerifyStep_SchemeHMACSHA256_Valid(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-sha256", map[string]any{
		"scheme":           "hmac-sha256",
		"secret":           "sha256-secret",
		"signature_header": "X-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	sig := computeTestHMAC("sha256-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_SchemeHMACSHA256Hex_Valid(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-gh-scheme", map[string]any{
		"scheme":           "hmac-sha256-hex",
		"secret":           "gh-secret",
		"signature_header": "X-Hub-Signature-256",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"action":"opened"}`)
	sig := "sha256=" + computeTestHMAC("gh-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_SchemeHMACSHA256Hex_MissingPrefix(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-gh-no-prefix", map[string]any{
		"scheme":           "hmac-sha256-hex",
		"secret":           "gh-secret",
		"signature_header": "X-Hub-Signature-256",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"action":"opened"}`)
	// Missing "sha256=" prefix
	sig := computeTestHMAC("gh-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when sha256= prefix is missing")
	}
}

func TestWebhookVerifyStep_SecretFrom_Valid(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-secret-from", map[string]any{
		"scheme":           "hmac-sha256",
		"secret_from":      "steps.load-config.auth_token",
		"signature_header": "X-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	sig := computeTestHMAC("dynamic-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})
	// Simulate a previous step having produced the secret
	pc.StepOutputs["load-config"] = map[string]any{"auth_token": "dynamic-secret"}

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_SecretFrom_NotFound(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-secret-from-missing", map[string]any{
		"scheme":           "hmac-sha256",
		"secret_from":      "steps.missing-step.token",
		"signature_header": "X-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", "deadbeef")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when secret_from cannot be resolved")
	}
	reason, _ := result.Output["reason"].(string)
	if !strings.Contains(reason, "secret_from") {
		t.Errorf("expected reason to mention secret_from, got: %q", reason)
	}
}

func TestWebhookVerifyStep_ErrorStatus_Custom(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-custom-status", map[string]any{
		"scheme":           "hmac-sha256",
		"secret":           "my-secret",
		"signature_header": "X-Signature",
		"error_status":     403,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	// No X-Signature header → should fail

	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true on missing signature")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d", w.Code)
	}
}

func TestWebhookVerifyStep_URLReconstruction(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-url-recon", map[string]any{
		"scheme":              "hmac-sha1",
		"secret":              "test-secret",
		"signature_header":    "X-Twilio-Signature",
		"url_reconstruction":  true,
		"include_form_params": true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	formBody := "Param=Value"
	// URL reconstruction: X-Forwarded-Proto=https, X-Forwarded-Host=myapp.example.com
	expectedURL := "https://myapp.example.com/hook"
	signingInput := expectedURL + "Param" + "Value"
	sig := computeTestHMACSHA1Base64("test-secret", signingInput)

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader([]byte(formBody)))
	req.Header.Set("X-Twilio-Signature", sig)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "myapp.example.com")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_SchemeFactoryRejectsUnknownScheme(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("bad-scheme", map[string]any{
		"scheme":           "hmac-sha512",
		"secret":           "my-secret",
		"signature_header": "X-Sig",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
	if !strings.Contains(err.Error(), "unknown scheme") {
		t.Errorf("expected 'unknown scheme' error, got: %v", err)
	}
}

func TestWebhookVerifyStep_SchemeFactoryRejectsMissingSignatureHeader(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("no-header", map[string]any{
		"scheme": "hmac-sha256",
		"secret": "my-secret",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing signature_header")
	}
	if !strings.Contains(err.Error(), "signature_header") {
		t.Errorf("expected 'signature_header' error, got: %v", err)
	}
}

func TestWebhookVerifyStep_SchemeFactoryRejectsMissingSecret(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	_, err := factory("no-secret", map[string]any{
		"scheme":           "hmac-sha256",
		"signature_header": "X-Sig",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing secret and secret_from")
	}
}

func TestWebhookVerifyStep_SchemeMissingHeader(t *testing.T) {
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-missing-sig", map[string]any{
		"scheme":           "hmac-sha256",
		"secret":           "my-secret",
		"signature_header": "X-Custom-Sig",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	// No X-Custom-Sig header

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when signature header is missing")
	}
	reason, _ := result.Output["reason"].(string)
	if !strings.Contains(reason, "X-Custom-Sig") {
		t.Errorf("expected reason to mention X-Custom-Sig, got: %q", reason)
	}
}

func TestWebhookVerifyStep_SchemeNoFormParams(t *testing.T) {
	// When include_form_params is false, should use raw body
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-no-form", map[string]any{
		"scheme":           "hmac-sha256",
		"secret":           "raw-body-secret",
		"signature_header": "X-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`raw body content`)
	sig := computeTestHMAC("raw-body-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}
