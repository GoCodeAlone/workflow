package validation

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateChallenge(t *testing.T) {
	token := GenerateChallenge("secret", "abc123", time.Now())
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	parts := strings.Split(token, "-")
	if len(parts) != 3 {
		t.Fatalf("expected 3-word token, got: %q", token)
	}
	for _, part := range parts {
		if part == "" {
			t.Errorf("token part is empty: %q", token)
		}
	}
}

func TestVerifyChallenge(t *testing.T) {
	now := time.Now()
	secret := "my-admin-secret"
	hash := "deadbeef"
	token := GenerateChallenge(secret, hash, now)
	if !VerifyChallenge(secret, hash, token, now) {
		t.Error("expected VerifyChallenge to return true for freshly generated token")
	}
}

func TestChallengeExpiry(t *testing.T) {
	secret := "test-secret"
	hash := "test-hash"
	now := time.Now()
	// Token generated 2 hours ago should fail verification at now
	oldToken := GenerateChallenge(secret, hash, now.Add(-2*time.Hour))
	if VerifyChallenge(secret, hash, oldToken, now) {
		t.Error("expected expired token to fail verification")
	}
}

func TestChallengeDeterministic(t *testing.T) {
	now := time.Now()
	secret := "test-secret"
	hash := "test-hash"
	token1 := GenerateChallenge(secret, hash, now)
	token2 := GenerateChallenge(secret, hash, now)
	if token1 != token2 {
		t.Errorf("expected same token for same inputs, got %q and %q", token1, token2)
	}
}

func TestChallengeDifferentHash(t *testing.T) {
	now := time.Now()
	secret := "test-secret"
	token1 := GenerateChallenge(secret, "hash-one", now)
	token2 := GenerateChallenge(secret, "hash-two", now)
	if token1 == token2 {
		t.Errorf("expected different tokens for different hashes, got %q", token1)
	}
}
