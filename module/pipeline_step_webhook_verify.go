package module

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"  //nolint:gosec // Required for Twilio HMAC-SHA1 webhook signature verification
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
)

const (
	webhookVerifyProviderGitHub  = "github"
	webhookVerifyProviderStripe  = "stripe"
	webhookVerifyProviderGeneric = "generic"

	// Scheme constants for scheme-based verification.
	webhookSchemeHMACSHA1      = "hmac-sha1"
	webhookSchemeHMACSHA256    = "hmac-sha256"
	webhookSchemeHMACSHA256Hex = "hmac-sha256-hex"

	// stripeTimestampTolerance is the maximum allowed age of a Stripe timestamp.
	stripeTimestampTolerance = 5 * time.Minute
)

// WebhookVerifyStep verifies HMAC signatures for incoming webhook requests.
type WebhookVerifyStep struct {
	name     string
	provider string
	secret   string
	header   string

	// scheme-based fields (new config model)
	scheme            string
	secretFrom        string
	signatureHeader   string
	urlReconstruction bool
	includeFormParams bool
	errorStatus       int
}

// NewWebhookVerifyStepFactory returns a StepFactory that creates WebhookVerifyStep instances.
func NewWebhookVerifyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		scheme, _ := config["scheme"].(string)
		provider, _ := config["provider"].(string)

		// Determine which mode to use: scheme-based or provider-based
		if scheme != "" {
			return newSchemeBasedStep(name, scheme, config)
		}

		if provider == "" {
			return nil, fmt.Errorf("webhook_verify step %q: 'scheme' or 'provider' is required", name)
		}

		return newProviderBasedStep(name, provider, config)
	}
}

// newSchemeBasedStep creates a WebhookVerifyStep using the scheme-based config model.
func newSchemeBasedStep(name, scheme string, config map[string]any) (PipelineStep, error) {
	switch scheme {
	case webhookSchemeHMACSHA1, webhookSchemeHMACSHA256, webhookSchemeHMACSHA256Hex:
		// valid
	default:
		return nil, fmt.Errorf("webhook_verify step %q: unknown scheme %q (must be hmac-sha1, hmac-sha256, or hmac-sha256-hex)", name, scheme)
	}

	secret, _ := config["secret"].(string)
	secretFrom, _ := config["secret_from"].(string)
	if secret == "" && secretFrom == "" {
		return nil, fmt.Errorf("webhook_verify step %q: 'secret' or 'secret_from' is required", name)
	}

	if secret != "" {
		secret = expandEnvSecret(secret)
	}

	signatureHeader, _ := config["signature_header"].(string)
	if signatureHeader == "" {
		return nil, fmt.Errorf("webhook_verify step %q: 'signature_header' is required when using scheme", name)
	}

	urlReconstruction, _ := config["url_reconstruction"].(bool)
	includeFormParams, _ := config["include_form_params"].(bool)

	errorStatus := http.StatusUnauthorized
	if es, ok := config["error_status"]; ok {
		switch v := es.(type) {
		case int:
			errorStatus = v
		case float64:
			errorStatus = int(v)
		}
	}

	return &WebhookVerifyStep{
		name:              name,
		scheme:            scheme,
		secret:            secret,
		secretFrom:        secretFrom,
		signatureHeader:   signatureHeader,
		urlReconstruction: urlReconstruction,
		includeFormParams: includeFormParams,
		errorStatus:       errorStatus,
	}, nil
}

// newProviderBasedStep creates a WebhookVerifyStep using the legacy provider-based config model.
func newProviderBasedStep(name, provider string, config map[string]any) (PipelineStep, error) {
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

	secret = expandEnvSecret(secret)
	header, _ := config["header"].(string)

	return &WebhookVerifyStep{
		name:        name,
		provider:    provider,
		secret:      secret,
		header:      header,
		errorStatus: http.StatusUnauthorized,
	}, nil
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

	// Scheme-based verification takes priority
	if s.scheme != "" {
		return s.verifyByScheme(req, body, pc)
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

// resolveSecret returns the signing secret, resolving from pipeline context if secret_from is set.
func (s *WebhookVerifyStep) resolveSecret(pc *PipelineContext) (string, error) {
	if s.secret != "" {
		return s.secret, nil
	}

	if s.secretFrom == "" {
		return "", fmt.Errorf("no secret configured")
	}

	// Build a data map for dot-path resolution.
	// Convert StepOutputs (map[string]map[string]any) to map[string]any for traversal.
	stepsMap := make(map[string]any, len(pc.StepOutputs))
	for k, v := range pc.StepOutputs {
		stepsMap[k] = v
	}

	// Start with the current context, then overlay reserved keys so they cannot be overridden
	// by user-controlled trigger data containing keys like "steps", "trigger", or "meta".
	data := make(map[string]any, len(pc.Current)+3)
	for k, v := range pc.Current {
		data[k] = v
	}
	data["steps"] = stepsMap
	data["trigger"] = pc.TriggerData
	data["meta"] = pc.Metadata

	val, err := resolveDottedPath(data, s.secretFrom)
	if err != nil {
		return "", fmt.Errorf("could not resolve secret_from %q: %w", s.secretFrom, err)
	}

	secretStr, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("secret_from %q resolved to non-string type %T", s.secretFrom, val)
	}
	return secretStr, nil
}

// verifyByScheme performs signature verification using the scheme-based config model.
func (s *WebhookVerifyStep) verifyByScheme(req *http.Request, body []byte, pc *PipelineContext) (*StepResult, error) {
	sig := req.Header.Get(s.signatureHeader)
	if sig == "" {
		return s.unauthorized(pc, fmt.Sprintf("missing %s header", s.signatureHeader))
	}

	secret, err := s.resolveSecret(pc)
	if err != nil {
		return s.unauthorized(pc, err.Error())
	}

	// Build signing input
	var signingInput []byte
	if s.includeFormParams {
		signingInput = s.buildTwilioSigningInput(req, body)
	} else {
		signingInput = body
	}

	switch s.scheme {
	case webhookSchemeHMACSHA1:
		return s.verifyHMACSHA1(sig, secret, signingInput, pc)
	case webhookSchemeHMACSHA256:
		return s.verifyHMACSHA256Hex(sig, secret, signingInput, pc)
	case webhookSchemeHMACSHA256Hex:
		// Expects sha256=<hex> prefix
		if !strings.HasPrefix(sig, "sha256=") {
			return s.unauthorized(pc, fmt.Sprintf("%s must have format sha256=<hex>", s.signatureHeader))
		}
		return s.verifyHMACSHA256Hex(strings.TrimPrefix(sig, "sha256="), secret, signingInput, pc)
	default:
		return s.unauthorized(pc, fmt.Sprintf("unknown scheme: %s", s.scheme))
	}
}

// verifyHMACSHA1 verifies a base64-encoded HMAC-SHA1 signature.
func (s *WebhookVerifyStep) verifyHMACSHA1(sig, secret string, data []byte, pc *PipelineContext) (*StepResult, error) {
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return s.unauthorized(pc, fmt.Sprintf("invalid base64 in %s", s.signatureHeader))
	}

	expected := computeHMACSHA1([]byte(secret), data)
	if subtle.ConstantTimeCompare(expected, sigBytes) != 1 {
		return s.unauthorized(pc, "signature mismatch")
	}

	return &StepResult{
		Output: map[string]any{"verified": true},
	}, nil
}

// verifyHMACSHA256Hex verifies a hex-encoded HMAC-SHA256 signature.
func (s *WebhookVerifyStep) verifyHMACSHA256Hex(sigHex, secret string, data []byte, pc *PipelineContext) (*StepResult, error) {
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return s.unauthorized(pc, fmt.Sprintf("invalid hex in %s", s.signatureHeader))
	}

	expected := computeHMACSHA256([]byte(secret), data)
	if subtle.ConstantTimeCompare(expected, sigBytes) != 1 {
		return s.unauthorized(pc, "signature mismatch")
	}

	return &StepResult{
		Output: map[string]any{"verified": true},
	}, nil
}

// buildTwilioSigningInput constructs the signing input for Twilio-style webhooks:
// the URL followed by POST form parameter values sorted alphabetically by key.
func (s *WebhookVerifyStep) buildTwilioSigningInput(req *http.Request, body []byte) []byte {
	requestURL := s.reconstructURL(req)

	// Parse form parameters from the body
	params, err := url.ParseQuery(string(body))
	if err != nil {
		return []byte(requestURL)
	}

	// Sort parameter keys and append key+value pairs
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	buf.WriteString(requestURL)
	for _, k := range keys {
		for _, v := range params[k] {
			buf.WriteString(k)
			buf.WriteString(v)
		}
	}

	return []byte(buf.String())
}

// reconstructURL returns the full URL used for signature verification.
// When url_reconstruction is enabled, it rebuilds from X-Forwarded-Proto and X-Forwarded-Host headers.
func (s *WebhookVerifyStep) reconstructURL(req *http.Request) string {
	if !s.urlReconstruction {
		return requestURL(req)
	}

	// Take the first value from comma-separated X-Forwarded-Proto header,
	// falling back to the scheme inferred from the request itself.
	scheme := firstHeaderValue(req.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = requestScheme(req)
	}

	// Take the first value from comma-separated X-Forwarded-Host header,
	// falling back to the Host from the request.
	host := firstHeaderValue(req.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = req.Host
	}

	return scheme + "://" + host + req.URL.RequestURI()
}

// firstHeaderValue returns the first comma-separated value from a header string.
func firstHeaderValue(h string) string {
	if h == "" {
		return ""
	}
	if idx := strings.IndexByte(h, ','); idx != -1 {
		return strings.TrimSpace(h[:idx])
	}
	return strings.TrimSpace(h)
}

// requestScheme returns the scheme of the request based on TLS state and URL.
func requestScheme(req *http.Request) string {
	if req.TLS != nil {
		return "https"
	}
	if s := req.URL.Scheme; s != "" {
		return s
	}
	return "http"
}

// requestURL reconstructs the URL from the request as-is.
func requestURL(req *http.Request) string {
	return requestScheme(req) + "://" + req.Host + req.URL.RequestURI()
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

// unauthorized writes an error response if a response writer is available, and returns Stop: true.
func (s *WebhookVerifyStep) unauthorized(pc *PipelineContext, reason string) (*StepResult, error) {
	status := s.errorStatus
	if status == 0 {
		status = http.StatusUnauthorized
	}
	if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
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

// computeHMACSHA1 returns the HMAC-SHA1 of data using key.
func computeHMACSHA1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
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
