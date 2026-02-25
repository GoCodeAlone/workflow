package module

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
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
