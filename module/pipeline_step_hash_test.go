package module

import (
	"context"
	"crypto/md5" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"testing"
)

func TestHashStep_SHA256Default(t *testing.T) {
	factory := NewHashStepFactory()
	step, err := factory("hash-test", map[string]any{
		"input": "hello",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	h := sha256.Sum256([]byte("hello"))
	expected := hex.EncodeToString(h[:])

	if result.Output["hash"] != expected {
		t.Errorf("expected hash=%q, got %v", expected, result.Output["hash"])
	}
	if result.Output["algorithm"] != "sha256" {
		t.Errorf("expected algorithm=sha256, got %v", result.Output["algorithm"])
	}
}

func TestHashStep_MD5(t *testing.T) {
	factory := NewHashStepFactory()
	step, err := factory("hash-md5", map[string]any{
		"algorithm": "md5",
		"input":     "test",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	h := md5.Sum([]byte("test")) //nolint:gosec
	expected := hex.EncodeToString(h[:])

	if result.Output["hash"] != expected {
		t.Errorf("expected hash=%q, got %v", expected, result.Output["hash"])
	}
}

func TestHashStep_SHA512(t *testing.T) {
	factory := NewHashStepFactory()
	step, err := factory("hash-sha512", map[string]any{
		"algorithm": "sha512",
		"input":     "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	h := sha512.Sum512([]byte("data"))
	expected := hex.EncodeToString(h[:])

	if result.Output["hash"] != expected {
		t.Errorf("expected hash=%q, got %v", expected, result.Output["hash"])
	}
}

func TestHashStep_TemplateInput(t *testing.T) {
	factory := NewHashStepFactory()
	step, err := factory("hash-tmpl", map[string]any{
		"input": "{{ .val }}",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"val": "dynamic"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	h := sha256.Sum256([]byte("dynamic"))
	expected := hex.EncodeToString(h[:])

	if result.Output["hash"] != expected {
		t.Errorf("expected hash=%q, got %v", expected, result.Output["hash"])
	}
}

func TestHashStep_MissingInput(t *testing.T) {
	factory := NewHashStepFactory()
	_, err := factory("bad", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestHashStep_InvalidAlgorithm(t *testing.T) {
	factory := NewHashStepFactory()
	_, err := factory("bad", map[string]any{
		"algorithm": "sha1",
		"input":     "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}
