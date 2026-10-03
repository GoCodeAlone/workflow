package module

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type webhookInvokerSpy struct{ calls int }

func (*webhookInvokerSpy) Name() string { return "external-invoker" }

func (s *webhookInvokerSpy) Execute(context.Context, *PipelineContext) (*StepResult, error) {
	s.calls++
	return &StepResult{Output: map[string]any{"invoked": true}}, nil
}

func TestWebhookVerifyBodyLimitStopsPipeline(t *testing.T) {
	body := strings.Repeat("x", 64)
	step, err := NewWebhookVerifyStepFactory()("verify", map[string]any{
		"scheme": "hmac-sha256-hex", "secret": "test-secret", "signature_header": "X-Hub-Signature-256", "error_status": http.StatusTeapot, "max_body_bytes": 8,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	reader := &httpBodyReadSpy{Reader: strings.NewReader(body)}
	req.Body, req.ContentLength = reader, -1
	req.Header.Set("X-Hub-Signature-256", "sha256="+computeTestHMAC("test-secret", body))
	w := httptest.NewRecorder()
	ctx := context.WithValue(t.Context(), HTTPRequestContextKey, req)
	ctx = context.WithValue(ctx, HTTPResponseWriterContextKey, w)
	next := &webhookInvokerSpy{}
	pipeline := &Pipeline{Name: "webhook-limit", Steps: []PipelineStep{step, next}, OnError: ErrorStrategyStop}
	if _, err := pipeline.Execute(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusRequestEntityTooLarge || next.calls != 0 {
		t.Fatalf("oversized signed body reached subsequent execution: status=%d invocations=%d", w.Code, next.calls)
	}
	if reader.readBytes > 9 {
		t.Fatalf("verifier body read exceeded cap plus overflow probe: %d", reader.readBytes)
	}
}

func TestWebhookVerifyBodyLimitRejectsOversizedCache(t *testing.T) {
	body := []byte(strings.Repeat("x", 64))
	step, err := NewWebhookVerifyStepFactory()("verify", map[string]any{
		"provider": "github", "secret": "test-secret", "max_body_bytes": 8,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	req.Header.Set("X-Hub-Signature-256", "sha256="+computeTestHMAC("test-secret", string(body)))
	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{"_http_request": req, "_http_response_writer": w, "_raw_body": body})
	result, err := step.Execute(t.Context(), pc)
	if err != nil || result == nil || !result.Stop || result.Output["verified"] != false || w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized cached body bypassed the cap: result=%v error=%v status=%d", result, err, w.Code)
	}
}

func TestWebhookVerifyBodyLimitPreservesExactBytesForRequestParse(t *testing.T) {
	const body = " {\n  \"action\": \"opened\", \"number\": 1\n}\n"
	step, err := NewWebhookVerifyStepFactory()("verify", map[string]any{
		"provider": "github", "secret": "test-secret", "max_body_bytes": len(body),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256="+computeTestHMAC("test-secret", body))
	pc := NewPipelineContext(nil, map[string]any{"_http_request": req})
	result, err := step.Execute(t.Context(), pc)
	if err != nil || result == nil || result.Stop || result.Output["verified"] != true {
		t.Fatalf("signature over exact raw bytes was not accepted: result=%v error=%v", result, err)
	}
	if raw, ok := pc.Metadata["_raw_body"].([]byte); !ok || !bytes.Equal(raw, []byte(body)) {
		t.Fatalf("HMAC bytes were normalized or not cached: %#v", pc.Metadata["_raw_body"])
	}
	parse, err := NewRequestParseStepFactory()("parse", map[string]any{"format": "json"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parse.Execute(t.Context(), pc)
	if err != nil || parsed == nil {
		t.Fatalf("request_parse failed: result=%v error=%v", parsed, err)
	}
	parsedBody, _ := parsed.Output["body"].(map[string]any)
	if parsedBody["action"] != "opened" {
		t.Fatalf("request_parse did not consume the cached verified bytes: result=%v error=%v", parsed, err)
	}
}

func TestWebhookVerifyBodyLimitRejectsInvalidConfig(t *testing.T) {
	for _, value := range []any{nil, 0, -1, 1.5, math.NaN(), math.Inf(1), math.Exp2(63), "8", true} {
		t.Run(fmt.Sprintf("%T/%v", value, value), func(t *testing.T) {
			_, err := NewWebhookVerifyStepFactory()("verify", map[string]any{"provider": "github", "secret": "test-secret", "max_body_bytes": value}, nil)
			if err == nil {
				t.Fatalf("invalid max_body_bytes %v was silently ignored", value)
			}
		})
	}
}

func TestWebhookVerifyBodyLimitDefaultAlwaysEnforced(t *testing.T) {
	const limit = 1 << 20
	body := strings.Repeat("x", limit+1)
	for _, mode := range []string{"legacy provider", "scheme", "zero value"} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cached=%t", mode, cached), func(t *testing.T) {
				var step PipelineStep
				if mode == "zero value" {
					step = &WebhookVerifyStep{name: "verify", provider: "github", secret: "test-secret"}
				} else {
					cfg := map[string]any{"provider": "github", "secret": "test-secret"}
					if mode == "scheme" {
						cfg = map[string]any{"scheme": "hmac-sha256-hex", "secret": "test-secret", "signature_header": "X-Hub-Signature-256"}
					}
					var err error
					step, err = NewWebhookVerifyStepFactory()("verify", cfg, nil)
					if err != nil {
						t.Fatal(err)
					}
					if got := step.(*WebhookVerifyStep).maxBodyBytes; got != limit {
						t.Errorf("omitted cap must configure the enforced 1 MiB default, got %d", got)
					}
				}
				req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
				reader := &httpBodyReadSpy{Reader: strings.NewReader(body)}
				req.Body, req.ContentLength = reader, -1
				req.Header.Set("X-Hub-Signature-256", "sha256="+computeTestHMAC("test-secret", body))
				w := httptest.NewRecorder()
				metadata := map[string]any{"_http_request": req, "_http_response_writer": w}
				if cached {
					metadata["_raw_body"] = []byte(body)
				}
				next := &webhookInvokerSpy{}
				pipeline := &Pipeline{Name: "webhook-default-limit", Metadata: metadata, Steps: []PipelineStep{step, next}, OnError: ErrorStrategyStop}
				if _, err := pipeline.Execute(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusRequestEntityTooLarge || next.calls != 0 {
					t.Fatalf("default/zero cap allowed oversized body: status=%d invocations=%d", w.Code, next.calls)
				}
				if reader.readBytes > limit+1 || (cached && reader.readBytes != 0) {
					t.Fatalf("default body read was not bounded or ignored cache: read %d bytes", reader.readBytes)
				}
			})
		}
	}
}

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

func TestWebhookVerifyStep_SecretFrom_CannotBeOverriddenByCurrentData(t *testing.T) {
	// Verify that reserved keys (steps/trigger/meta) in resolveSecret cannot be overridden
	// by user-controlled data in pc.Current.
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-secret-override", map[string]any{
		"scheme":           "hmac-sha256",
		"secret_from":      "steps.load-config.auth_token",
		"signature_header": "X-Signature",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := []byte(`{"event":"test"}`)
	sig := computeTestHMAC("real-secret", string(body))

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)

	// Set the actual secret via StepOutputs
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})
	pc.StepOutputs["load-config"] = map[string]any{"auth_token": "real-secret"}
	// Attempt to override the "steps" key via Current — this should NOT take effect
	pc.Current["steps"] = map[string]any{
		"load-config": map[string]any{"auth_token": "attacker-controlled-secret"},
	}

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	// The real secret should be used (from StepOutputs), so signature verification should succeed.
	if result.Stop {
		t.Errorf("expected Stop=false; reserved keys must not be overrideable via Current, reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_URLReconstruction_CommaSeparatedHeaders(t *testing.T) {
	// Verify that comma-separated X-Forwarded-Proto and X-Forwarded-Host values
	// are handled correctly (first value is used).
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-url-comma", map[string]any{
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
	// First value from comma-separated headers should be used
	expectedURL := "https://myapp.example.com/hook"
	signingInput := expectedURL + "Param" + "Value"
	sig := computeTestHMACSHA1Base64("test-secret", signingInput)

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader([]byte(formBody)))
	req.Header.Set("X-Twilio-Signature", sig)
	// Comma-separated values — first should win
	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "myapp.example.com, internal.host")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false (first value from comma-separated headers), reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}

func TestWebhookVerifyStep_URLReconstruction_FallsBackToRequestScheme(t *testing.T) {
	// When X-Forwarded-Proto is absent, scheme should be inferred from the request (http for non-TLS).
	factory := NewWebhookVerifyStepFactory()
	step, err := factory("verify-url-fallback", map[string]any{
		"scheme":              "hmac-sha1",
		"secret":              "test-secret",
		"signature_header":    "X-Twilio-Signature",
		"url_reconstruction":  true,
		"include_form_params": true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	formBody := "Key=Val"
	// No X-Forwarded-Proto — req.TLS is nil so scheme should be "http"
	// httptest.NewRequest creates a request with no TLS so requestScheme returns "http"
	expectedURL := "http://example.com/hook"
	signingInput := expectedURL + "Key" + "Val"
	sig := computeTestHMACSHA1Base64("test-secret", signingInput)

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader([]byte(formBody)))
	req.Host = "example.com"
	req.Header.Set("X-Twilio-Signature", sig)
	// No X-Forwarded-Proto set

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Stop {
		t.Errorf("expected Stop=false (fallback to request scheme), reason: %v", result.Output["reason"])
	}
	if result.Output["verified"] != true {
		t.Errorf("expected verified=true, got %v", result.Output["verified"])
	}
}
