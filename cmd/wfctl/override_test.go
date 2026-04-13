package main

import (
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/validation"
)

func TestRunOverrideGenerate(t *testing.T) {
	t.Setenv("WFCTL_ADMIN_SECRET", "test-secret")
	err := runOverrideGenerate([]string{"deadbeef1234"})
	if err != nil {
		t.Fatalf("runOverrideGenerate failed: %v", err)
	}
}

func TestRunOverrideGenerateMissingSecret(t *testing.T) {
	os.Unsetenv("WFCTL_ADMIN_SECRET")
	err := runOverrideGenerate([]string{"deadbeef1234"})
	if err == nil {
		t.Fatal("expected error when WFCTL_ADMIN_SECRET is missing")
	}
	if !strings.Contains(err.Error(), "WFCTL_ADMIN_SECRET") {
		t.Errorf("expected error about WFCTL_ADMIN_SECRET, got: %v", err)
	}
}

func TestRunOverrideGenerateMissingHash(t *testing.T) {
	t.Setenv("WFCTL_ADMIN_SECRET", "test-secret")
	err := runOverrideGenerate([]string{})
	if err == nil {
		t.Fatal("expected error for missing hash argument")
	}
}

func TestRunOverrideVerifyInvalidToken(t *testing.T) {
	t.Setenv("WFCTL_ADMIN_SECRET", "test-secret")
	err := runOverrideVerify([]string{"testhash", "notarealtoken"})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunOverrideVerifyValidToken(t *testing.T) {
	const secret = "test-secret"
	const hash = "testhash"
	t.Setenv("WFCTL_ADMIN_SECRET", secret)
	token := validation.GenerateChallenge(secret, hash)
	err := runOverrideVerify([]string{hash, token})
	if err != nil {
		t.Fatalf("expected valid token to pass: %v", err)
	}
}
