package module

import (
	"strings"
	"testing"
)

func TestDockerPushStep_MissingImage(t *testing.T) {
	_, err := NewDockerPushStepFactory()("push", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "'image' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDockerPushStep_ValidConfig(t *testing.T) {
	step, err := NewDockerPushStepFactory()("push", map[string]any{
		"image":    "myapp:latest",
		"registry": "registry.example.com",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "push" {
		t.Errorf("expected name 'push', got %q", step.Name())
	}
}

func TestDockerPushStep_MinimalConfig(t *testing.T) {
	_, err := NewDockerPushStepFactory()("push", map[string]any{
		"image": "alpine:latest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePushOutput_TextDigest(t *testing.T) {
	digest, err := parsePushOutput(strings.NewReader("latest: digest: sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff size: 856\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff" {
		t.Fatalf("unexpected digest %q", digest)
	}
}

func TestParsePushOutput_JSONDigest(t *testing.T) {
	digest, err := parsePushOutput(strings.NewReader(`{"status":"pushed"}
{"aux":{"Digest":"sha256:ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != "sha256:ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100" {
		t.Fatalf("unexpected digest %q", digest)
	}
}

func TestParsePushOutput_JSONError(t *testing.T) {
	_, err := parsePushOutput(strings.NewReader(`{"error":"denied"}`))
	if err == nil {
		t.Fatal("expected push error")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
