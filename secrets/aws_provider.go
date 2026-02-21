package secrets

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// HTTPClient is an interface for HTTP requests (allows testing).
// Used by the AWS Secrets Manager provider.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// AWSConfig holds configuration for AWS Secrets Manager.
type AWSConfig struct {
	Region          string `json:"region" yaml:"region"`
	AccessKeyID     string `json:"accessKeyId,omitempty" yaml:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty" yaml:"secretAccessKey,omitempty"`
}

// AWSSecretsManagerProvider reads secrets from AWS Secrets Manager using the
// HTTP API with AWS Signature V4 signing. No external AWS SDK is required.
type AWSSecretsManagerProvider struct {
	config     AWSConfig
	httpClient HTTPClient
}

// NewAWSSecretsManagerProvider creates a new AWS Secrets Manager provider.
// If AccessKeyID/SecretAccessKey are empty, it falls back to the environment
// variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
func NewAWSSecretsManagerProvider(cfg AWSConfig) (*AWSSecretsManagerProvider, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	// Fall back to environment variables for credentials
	if cfg.AccessKeyID == "" {
		cfg.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if cfg.SecretAccessKey == "" {
		cfg.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}

	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("%w: AWS credentials required (set AccessKeyID/SecretAccessKey or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY env vars)", ErrProviderInit)
	}

	return &AWSSecretsManagerProvider{
		config:     cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// NewAWSSecretsManagerProviderWithClient creates an AWS provider with a custom HTTP client (for testing).
func NewAWSSecretsManagerProviderWithClient(cfg AWSConfig, client HTTPClient) *AWSSecretsManagerProvider {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &AWSSecretsManagerProvider{
		config:     cfg,
		httpClient: client,
	}
}

func (p *AWSSecretsManagerProvider) Name() string { return "aws-sm" }

func (p *AWSSecretsManagerProvider) Get(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}

	// Parse key: "secret-name#field" or just "secret-name"
	secretName, field := parseAWSKey(key)

	val, err := p.getSecretValue(ctx, secretName)
	if err != nil {
		return "", err
	}

	if field != "" {
		return extractJSONField(val, field)
	}

	return val, nil
}

func (p *AWSSecretsManagerProvider) Set(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("%w: aws secrets manager provider is read-only", ErrUnsupported)
}

func (p *AWSSecretsManagerProvider) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("%w: aws secrets manager provider is read-only", ErrUnsupported)
}

func (p *AWSSecretsManagerProvider) List(_ context.Context) ([]string, error) {
	return nil, fmt.Errorf("%w: aws secrets manager list not implemented", ErrUnsupported)
}

// Config returns the provider's AWS configuration.
func (p *AWSSecretsManagerProvider) Config() AWSConfig { return p.config }

// getSecretValue calls the AWS Secrets Manager GetSecretValue API.
func (p *AWSSecretsManagerProvider) getSecretValue(ctx context.Context, secretID string) (string, error) {
	host := fmt.Sprintf("secretsmanager.%s.amazonaws.com", p.config.Region)
	endpoint := fmt.Sprintf("https://%s", host)

	// Build the JSON request body
	reqBody := fmt.Sprintf(`{"SecretId":%q}`, secretID)

	now := time.Now().UTC()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("secrets: failed to create AWS request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")
	req.Header.Set("Host", host)

	// Sign the request with AWS Signature V4
	p.signRequest(req, []byte(reqBody), now)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("secrets: AWS request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("secrets: failed to read AWS response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: AWS Secrets Manager returned status %d for secret %q: %s",
			ErrNotFound, resp.StatusCode, secretID, string(body))
	}

	var result awsGetSecretResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("secrets: failed to parse AWS response: %w", err)
	}

	if result.SecretString == "" {
		return "", fmt.Errorf("%w: secret %q has no string value", ErrNotFound, secretID)
	}

	return result.SecretString, nil
}

// awsGetSecretResponse represents the relevant fields of the GetSecretValue response.
type awsGetSecretResponse struct {
	SecretString string `json:"SecretString"`
	Name         string `json:"Name"`
	ARN          string `json:"ARN"`
}

// signRequest signs an HTTP request using AWS Signature V4.
func (p *AWSSecretsManagerProvider) signRequest(req *http.Request, payload []byte, now time.Time) {
	service := "secretsmanager"
	region := p.config.Region

	datestamp := now.Format("20060102")
	amzdate := now.Format("20060102T150405Z")

	req.Header.Set("X-Amz-Date", amzdate)

	// Step 1: Create canonical request
	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date;x-amz-target"
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\nx-amz-target:%s\n",
		req.Header.Get("Content-Type"),
		req.Header.Get("Host"),
		payloadHash,
		amzdate,
		req.Header.Get("X-Amz-Target"),
	)

	canonicalRequest := fmt.Sprintf("%s\n/\n\n%s\n%s\n%s",
		req.Method,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	// Step 2: Create string to sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzdate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	)

	// Step 3: Calculate signature
	signingKey := deriveSigningKey(p.config.SecretAccessKey, datestamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// Step 4: Add authorization header
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.config.AccessKeyID,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authHeader)
}

// parseAWSKey splits "secret-name#field" into (secretName, field).
func parseAWSKey(key string) (secretName, field string) {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}

// extractJSONField extracts a specific field from a JSON string.
func extractJSONField(jsonStr, field string) (string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", fmt.Errorf("secrets: failed to parse JSON for field extraction: %w", err)
	}
	val, ok := data[field]
	if !ok {
		return "", fmt.Errorf("%w: field %q not found in secret JSON", ErrNotFound, field)
	}
	return fmt.Sprintf("%v", val), nil
}

// sha256Hex computes hex-encoded SHA-256 hash.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// hmacSHA256 computes HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// deriveSigningKey derives the AWS Signature V4 signing key.
func deriveSigningKey(secret, datestamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}
