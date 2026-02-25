package module

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
)

const (
	webhookVerifyProviderGitHub  = "github"
	webhookVerifyProviderStripe  = "stripe"
	webhookVerifyProviderGeneric = "generic"

	// stripeTimestampTolerance is the maximum allowed age of a Stripe timestamp.
	stripeTimestampTolerance = 5 * time.Minute
)

// WebhookVerifyStep verifies HMAC signatures for incoming webhook requests.
type WebhookVerifyStep struct {
	name     string
	provider string
	secret   string
	header   string
}

// NewWebhookVerifyStepFactory returns a StepFactory that creates WebhookVerifyStep instances.
func NewWebhookVerifyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		provider, _ := config["provider"].(string)
		if provider == "" {
			return nil, fmt.Errorf("webhook_verify step %q: 'provider' is required (github, stripe, or generic)", name)
		}

		switch provider {
		case webhookVerifyProviderGitHub, webhookVerifyProviderStripe, webhookVerifyProviderGeneric:
			// valid
		default:
			return nil, fmt.Errorf("webhook_verify step %q: unknown provider %q (must be github, stripe, or generic)", name, provider)
		}

		secret, _ := config["secret"].(string)
		if secret == "" {
			return nil, fmt.Errorf("webhook_verify step %q: 'secret' is required", name)
		}

		// Expand environment variable references (e.g., "$MY_SECRET" or "${MY_SECRET}")
		secret = expandEnvSecret(secret)

		header, _ := config["header"].(string)

		return &WebhookVerifyStep{
			name:     name,
			provider: provider,
			secret:   secret,
			header:   header,
		}, nil
	}
}

// Name returns the step name.
func (s *WebhookVerifyStep) Name() string { return s.name }

// Execute verifies the webhook signature from the HTTP request in pipeline context metadata.
func (s *WebhookVerifyStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	req, _ := pc.Metadata["_http_request"].(*http.Request)
	if req == nil {
		return s.unauthorized(pc, "no HTTP request in pipeline context")
	}

	// Read the request body. Body may have been read already; use raw body from metadata if present.
	body, err := s.readBody(req, pc)
	if err != nil {
		return s.unauthorized(pc, fmt.Sprintf("failed to read request body: %v", err))
	}

	switch s.provider {
	case webhookVerifyProviderGitHub:
		return s.verifyGitHub(req, body, pc)
	case webhookVerifyProviderStripe:
		return s.verifyStripe(req, body, pc)
	case webhookVerifyProviderGeneric:
		return s.verifyGeneric(req, body, pc)
	default:
		return s.unauthorized(pc, fmt.Sprintf("unknown provider: %s", s.provider))
	}
}

// verifyGitHub checks the X-Hub-Signature-256 header (format: sha256=<hex>).
func (s *WebhookVerifyStep) verifyGitHub(req *http.Request, body []byte, pc *PipelineContext) (*StepResult, error) {
	sig := req.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return s.unauthorized(pc, "missing X-Hub-Signature-256 header")
	}

	if !strings.HasPrefix(sig, "sha256=") {
		return s.unauthorized(pc, "X-Hub-Signature-256 must have format sha256=<hex>")
	}

	sigHex := strings.TrimPrefix(sig, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return s.unauthorized(pc, "invalid hex in X-Hub-Signature-256")
	}

	expected := computeHMACSHA256([]byte(s.secret), body)
	if subtle.ConstantTimeCompare(expected, sigBytes) != 1 {
		return s.unauthorized(pc, "signature mismatch")
	}

	return &StepResult{
		Output: map[string]any{"verified": true},
	}, nil
}

// verifyStripe checks the Stripe-Signature header (format: t=<timestamp>,v1=<hex>).
func (s *WebhookVerifyStep) verifyStripe(req *http.Request, body []byte, pc *PipelineContext) (*StepResult, error) {
	sig := req.Header.Get("Stripe-Signature")
	if sig == "" {
		return s.unauthorized(pc, "missing Stripe-Signature header")
	}

	timestamp, v1Sigs, err := parseStripeSignature(sig)
	if err != nil {
		return s.unauthorized(pc, fmt.Sprintf("invalid Stripe-Signature: %v", err))
	}

	// Validate timestamp is within tolerance
	ts := time.Unix(timestamp, 0)
	age := time.Since(ts)
	if age < 0 {
		age = -age
	}
	if age > stripeTimestampTolerance {
		return s.unauthorized(pc, fmt.Sprintf("Stripe timestamp is too old or too far in the future (%v)", age))
	}

	// Stripe signed payload: "<timestamp>.<body>"
	signedPayload := fmt.Sprintf("%d.%s", timestamp, string(body))
	expected := computeHMACSHA256([]byte(s.secret), []byte(signedPayload))
	expectedHex := hex.EncodeToString(expected)

	// Check any of the v1 signatures
	for _, candidate := range v1Sigs {
		if subtle.ConstantTimeCompare([]byte(expectedHex), []byte(candidate)) == 1 {
			return &StepResult{
				Output: map[string]any{"verified": true, "timestamp": timestamp},
			}, nil
		}
	}

	return s.unauthorized(pc, "signature mismatch")
}

// verifyGeneric checks a configurable header (default: X-Signature) with raw hex HMAC-SHA256.
func (s *WebhookVerifyStep) verifyGeneric(req *http.Request, body []byte, pc *PipelineContext) (*StepResult, error) {
	headerName := s.header
	if headerName == "" {
		headerName = "X-Signature"
	}

	sig := req.Header.Get(headerName)
	if sig == "" {
		return s.unauthorized(pc, fmt.Sprintf("missing %s header", headerName))
	}

	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return s.unauthorized(pc, fmt.Sprintf("invalid hex in %s", headerName))
	}

	expected := computeHMACSHA256([]byte(s.secret), body)
	if subtle.ConstantTimeCompare(expected, sigBytes) != 1 {
		return s.unauthorized(pc, "signature mismatch")
	}

	return &StepResult{
		Output: map[string]any{"verified": true},
	}, nil
}

// unauthorized writes a 401 response if a response writer is available, and returns Stop: true.
func (s *WebhookVerifyStep) unauthorized(pc *PipelineContext, reason string) (*StepResult, error) {
	if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","reason":"webhook signature verification failed"}`))
	}
	return &StepResult{
		Stop:   true,
		Output: map[string]any{"verified": false, "reason": reason},
	}, nil
}

// readBody reads the request body, preferring a cached copy in pipeline metadata.
func (s *WebhookVerifyStep) readBody(req *http.Request, pc *PipelineContext) ([]byte, error) {
	// Check if raw body is already cached in metadata
	if raw, ok := pc.Metadata["_raw_body"].([]byte); ok {
		return raw, nil
	}

	if req.Body == nil {
		return []byte{}, nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	// Cache it for other steps that may need the raw body
	pc.Metadata["_raw_body"] = body

	return body, nil
}

// computeHMACSHA256 returns the HMAC-SHA256 of data using key.
func computeHMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// parseStripeSignature parses the Stripe-Signature header.
// Format: t=<unix_timestamp>,v1=<hex>[,v1=<hex>]...
func parseStripeSignature(sig string) (int64, []string, error) {
	var timestamp int64
	var v1Sigs []string

	parts := strings.Split(sig, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "t=") {
			tsStr := strings.TrimPrefix(part, "t=")
			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				return 0, nil, fmt.Errorf("invalid timestamp: %w", err)
			}
			timestamp = ts
		} else if strings.HasPrefix(part, "v1=") {
			v1Sigs = append(v1Sigs, strings.TrimPrefix(part, "v1="))
		}
	}

	if timestamp == 0 {
		return 0, nil, fmt.Errorf("missing timestamp (t=) in Stripe-Signature")
	}
	if len(v1Sigs) == 0 {
		return 0, nil, fmt.Errorf("missing v1 signature in Stripe-Signature")
	}

	return timestamp, v1Sigs, nil
}

// expandEnvSecret expands environment variable references in the secret string.
// Supports $VAR_NAME and ${VAR_NAME} formats.
func expandEnvSecret(secret string) string {
	return os.ExpandEnv(secret)
}
