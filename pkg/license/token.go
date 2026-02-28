// Package license provides Ed25519-signed license token creation, parsing, and verification.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	tokenPrefix  = "wflic"
	tokenVersion = "v1"
)

// LicenseToken holds the claims embedded in a signed license token.
type LicenseToken struct {
	LicenseID    string   `json:"lid"`
	TenantID     string   `json:"tid"`
	Organization string   `json:"org"`
	Tier         string   `json:"tier"`
	Features     []string `json:"feat"`
	MaxWorkflows int      `json:"max_wf"`
	MaxPlugins   int      `json:"max_pl"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`

	// set by Parse to enable subsequent Verify calls
	rawPayload string
	rawSig     []byte
}

// Sign produces a signed license token string in the format:
// wflic.v1.<base64url-payload>.<base64url-signature>
func (t *LicenseToken) Sign(privateKey ed25519.PrivateKey) (string, error) {
	payloadBytes, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	message := tokenPrefix + "." + tokenVersion + "." + payload
	sig := ed25519.Sign(privateKey, []byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Parse splits tokenStr on dots, decodes the base64url payload, and returns
// the token. It does NOT verify the signature.
func Parse(tokenStr string) (*LicenseToken, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid token: expected 4 dot-separated parts, got %d", len(parts))
	}
	if parts[0] != tokenPrefix {
		return nil, fmt.Errorf("invalid token prefix: %q", parts[0])
	}
	if parts[1] != tokenVersion {
		return nil, fmt.Errorf("unsupported token version: %q", parts[1])
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var tok LicenseToken
	if err := json.Unmarshal(payloadBytes, &tok); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	tok.rawPayload = parts[2]
	tok.rawSig = sigBytes
	return &tok, nil
}

// Verify verifies the Ed25519 signature over the wflic.v1.<payload> prefix bytes.
// The token must have been produced by Parse.
func (t *LicenseToken) Verify(publicKey ed25519.PublicKey) error {
	if t.rawPayload == "" || t.rawSig == nil {
		return errors.New("token has no signature data: load the token via Parse before calling Verify")
	}
	message := tokenPrefix + "." + tokenVersion + "." + t.rawPayload
	if !ed25519.Verify(publicKey, []byte(message), t.rawSig) {
		return errors.New("invalid signature")
	}
	return nil
}

// IsExpired returns true if ExpiresAt is in the past.
func (t *LicenseToken) IsExpired() bool {
	return time.Now().Unix() > t.ExpiresAt
}

// HasFeature returns true if the given feature name is present in the Features slice.
func (t *LicenseToken) HasFeature(name string) bool {
	for _, f := range t.Features {
		if f == name {
			return true
		}
	}
	return false
}
