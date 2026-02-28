package license_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/pkg/license"
)

func newTestToken() *license.LicenseToken {
	return &license.LicenseToken{
		LicenseID:    "test-license-id",
		TenantID:     "tenant-123",
		Organization: "ACME Corp",
		Tier:         "enterprise",
		Features:     []string{"workflows", "plugins", "audit-log"},
		MaxWorkflows: 100,
		MaxPlugins:   50,
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    time.Now().Add(365 * 24 * time.Hour).Unix(),
	}
}

func TestRoundTrip(t *testing.T) {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := newTestToken()
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if !strings.HasPrefix(tokenStr, "wflic.v1.") {
		t.Errorf("unexpected token prefix: %s", tokenStr)
	}

	parsed, err := license.Parse(tokenStr)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if err := parsed.Verify(pub); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if parsed.LicenseID != tok.LicenseID {
		t.Errorf("LicenseID: got %q, want %q", parsed.LicenseID, tok.LicenseID)
	}
	if parsed.TenantID != tok.TenantID {
		t.Errorf("TenantID: got %q, want %q", parsed.TenantID, tok.TenantID)
	}
	if parsed.Organization != tok.Organization {
		t.Errorf("Organization: got %q, want %q", parsed.Organization, tok.Organization)
	}
	if parsed.Tier != tok.Tier {
		t.Errorf("Tier: got %q, want %q", parsed.Tier, tok.Tier)
	}
	if parsed.MaxWorkflows != tok.MaxWorkflows {
		t.Errorf("MaxWorkflows: got %d, want %d", parsed.MaxWorkflows, tok.MaxWorkflows)
	}
	if parsed.MaxPlugins != tok.MaxPlugins {
		t.Errorf("MaxPlugins: got %d, want %d", parsed.MaxPlugins, tok.MaxPlugins)
	}
	if len(parsed.Features) != len(tok.Features) {
		t.Errorf("Features length: got %d, want %d", len(parsed.Features), len(tok.Features))
	}
}

func TestExpiredToken(t *testing.T) {
	tok := newTestToken()
	tok.ExpiresAt = time.Now().Add(-time.Hour).Unix()

	if !tok.IsExpired() {
		t.Error("expected IsExpired() to return true for past ExpiresAt")
	}
}

func TestNotExpiredToken(t *testing.T) {
	tok := newTestToken()
	if tok.IsExpired() {
		t.Error("expected IsExpired() to return false for future ExpiresAt")
	}
}

func TestTamperedSignature(t *testing.T) {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := newTestToken()
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(tokenStr, ".")
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatal(err)
	}
	sigBytes[0] ^= 0xFF
	parts[3] = base64.RawURLEncoding.EncodeToString(sigBytes)

	parsed, err := license.Parse(strings.Join(parts, "."))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if err := parsed.Verify(pub); err == nil {
		t.Error("expected Verify to fail with tampered signature")
	}
}

func TestTamperedPayload(t *testing.T) {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := newTestToken()
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(tokenStr, ".")
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the JSON payload (avoid the first byte which is '{' to keep valid JSON structure-ish)
	payloadBytes[len(payloadBytes)/2] ^= 0xFF
	parts[2] = base64.RawURLEncoding.EncodeToString(payloadBytes)

	parsed, err := license.Parse(strings.Join(parts, "."))
	if err != nil {
		// Corrupted JSON is an acceptable failure path
		return
	}
	if err := parsed.Verify(pub); err == nil {
		t.Error("expected Verify to fail with tampered payload")
	}
}

func TestInvalidFormat(t *testing.T) {
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("{not valid json}"))
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"too few parts", "wflic.v1.abc"},
		{"too many parts", "wflic.v1.abc.def.ghi"},
		{"wrong prefix", "token.v1.abc.def"},
		{"wrong version", "wflic.v2.abc.def"},
		{"bad base64 payload", "wflic.v1.!!!invalid!!!.def"},
		{"bad json payload", "wflic.v1." + invalidJSON + ".def"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := license.Parse(tc.input)
			if err == nil {
				t.Errorf("expected Parse to fail for input %q", tc.input)
			}
		})
	}
}

func TestHasFeature(t *testing.T) {
	tok := newTestToken()
	// Features: []string{"workflows", "plugins", "audit-log"}

	if !tok.HasFeature("workflows") {
		t.Error(`expected HasFeature("workflows") to return true`)
	}
	if !tok.HasFeature("audit-log") {
		t.Error(`expected HasFeature("audit-log") to return true`)
	}
	if tok.HasFeature("nonexistent-feature") {
		t.Error(`expected HasFeature("nonexistent-feature") to return false`)
	}
}

func TestKeyPairPEMRoundTrip(t *testing.T) {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	pubPEM := license.MarshalPublicKeyPEM(pub)
	privPEM := license.MarshalPrivateKeyPEM(priv)

	recoveredPub, err := license.UnmarshalPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("UnmarshalPublicKeyPEM failed: %v", err)
	}
	recoveredPriv, err := license.UnmarshalPrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatalf("UnmarshalPrivateKeyPEM failed: %v", err)
	}

	if string(recoveredPub) != string(pub) {
		t.Error("public key mismatch after PEM round-trip")
	}
	if string(recoveredPriv) != string(priv) {
		t.Error("private key mismatch after PEM round-trip")
	}

	// Verify that a token signed with the original key verifies with the recovered key
	tok := newTestToken()
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := license.Parse(tokenStr)
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Verify(recoveredPub); err != nil {
		t.Errorf("Verify with recovered public key failed: %v", err)
	}

	// Also verify sign with recovered private key
	tokenStr2, err := tok.Sign(recoveredPriv)
	if err != nil {
		t.Fatal(err)
	}
	parsed2, err := license.Parse(tokenStr2)
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed2.Verify(pub); err != nil {
		t.Errorf("Verify (original pub) with recovered priv-signed token failed: %v", err)
	}
}

func TestVerifyWithoutParse(t *testing.T) {
	_, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pub2, _, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := newTestToken()
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := license.Parse(tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	// Verifying with a different public key should fail
	if err := parsed.Verify(pub2); err == nil {
		t.Error("expected Verify to fail with a different public key")
	}

	// Calling Verify on the original (non-parsed) token should fail
	if err := tok.Verify(pub2); err == nil {
		t.Error("expected Verify to fail on a token that was not produced by Parse")
	}
}
