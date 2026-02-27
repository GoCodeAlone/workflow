package module

import (
	"context"
	"encoding/base64"
	"testing"
)

// helper to produce a valid PNG data-URI (1x1 transparent PNG).
func testPNGDataURI() string {
	// Minimal valid 1x1 white PNG bytes (67 bytes).
	pngBytes := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR length + type
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // 8-bit RGB
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT length + type
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, // IDAT data
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, // IDAT data cont
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND length + type
		0x44, 0xae, 0x42, 0x60, 0x82, // IEND data
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
}

func testJPEGDataURI() string {
	// Minimal JPEG: SOI marker + EOI marker.
	jpegBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes)
}

// ---- Factory validation tests ----

func TestBase64DecodeStep_FactoryRequiresInputFrom(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	_, err := factory("test-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when input_from is missing")
	}
}

func TestBase64DecodeStep_FactoryRejectsUnknownFormat(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	_, err := factory("test-step", map[string]any{
		"input_from": "data",
		"format":     "hex",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestBase64DecodeStep_FactoryAcceptsValidFormats(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	for _, fmt := range []string{"data_uri", "raw_base64"} {
		_, err := factory("test-step", map[string]any{
			"input_from": "data",
			"format":     fmt,
		}, nil)
		if err != nil {
			t.Errorf("format %q should be accepted, got error: %v", fmt, err)
		}
	}
}

// ---- data_uri format tests ----

func TestBase64DecodeStep_DataURI_PNG(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-png", map[string]any{
		"input_from": "image_data",
		"format":     "data_uri",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"image_data": testPNGDataURI()}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v (reason: %v)", result.Output["valid"], result.Output["reason"])
	}
	if result.Output["content_type"] != "image/png" {
		t.Errorf("expected content_type='image/png', got %v", result.Output["content_type"])
	}
	if result.Output["extension"] != ".png" {
		t.Errorf("expected extension='.png', got %v", result.Output["extension"])
	}
	if result.Output["data"] == nil || result.Output["data"] == "" {
		t.Error("expected non-empty data field")
	}
	if sz, ok := result.Output["size_bytes"].(int); !ok || sz == 0 {
		t.Errorf("expected positive size_bytes, got %v", result.Output["size_bytes"])
	}
}

func TestBase64DecodeStep_DataURI_JPEG(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-jpeg", map[string]any{
		"input_from":    "image_data",
		"format":        "data_uri",
		"allowed_types": []any{"image/jpeg"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"image_data": testJPEGDataURI()}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true for JPEG, got %v (reason: %v)", result.Output["valid"], result.Output["reason"])
	}
	if result.Output["extension"] != ".jpg" {
		t.Errorf("expected extension='.jpg', got %v", result.Output["extension"])
	}
}

// ---- raw_base64 format tests ----

func TestBase64DecodeStep_RawBase64(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-raw", map[string]any{
		"input_from": "payload",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	original := "hello, world"
	encoded := base64.StdEncoding.EncodeToString([]byte(original))
	pc := NewPipelineContext(map[string]any{"payload": encoded}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v", result.Output["valid"])
	}
	// Verify that re-encoding the output produces the same base64
	if result.Output["data"] != encoded {
		t.Errorf("expected data=%q, got %v", encoded, result.Output["data"])
	}
}

// ---- allowed_types tests ----

func TestBase64DecodeStep_AllowedTypes_Accepts(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-img", map[string]any{
		"input_from":    "image_data",
		"format":        "data_uri",
		"allowed_types": []any{"image/png", "image/jpeg"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"image_data": testPNGDataURI()}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != true {
		t.Errorf("expected valid=true for allowed PNG, got %v (reason: %v)", result.Output["valid"], result.Output["reason"])
	}
}

func TestBase64DecodeStep_AllowedTypes_Rejects(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-img", map[string]any{
		"input_from":    "image_data",
		"format":        "data_uri",
		"allowed_types": []any{"image/jpeg"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"image_data": testPNGDataURI()}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false when PNG not in allowed_types (jpeg only), got %v", result.Output["valid"])
	}
}

// ---- max_size_bytes test ----

func TestBase64DecodeStep_MaxSizeBytes_Exceeded(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-size", map[string]any{
		"input_from":     "payload",
		"format":         "raw_base64",
		"max_size_bytes": 5,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("hello, world")) // 12 bytes
	pc := NewPipelineContext(map[string]any{"payload": encoded}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false when size exceeds max, got %v", result.Output["valid"])
	}
}

func TestBase64DecodeStep_MaxSizeBytes_WithinLimit(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-size", map[string]any{
		"input_from":     "payload",
		"format":         "raw_base64",
		"max_size_bytes": 100,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
	pc := NewPipelineContext(map[string]any{"payload": encoded}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != true {
		t.Errorf("expected valid=true within size limit, got %v", result.Output["valid"])
	}
}

// ---- invalid input tests ----

func TestBase64DecodeStep_InvalidBase64(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-bad", map[string]any{
		"input_from": "payload",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"payload": "not!!valid@base64$$"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false for invalid base64, got %v", result.Output["valid"])
	}
}

func TestBase64DecodeStep_InvalidDataURI_MissingComma(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-bad-uri", map[string]any{
		"input_from": "payload",
		"format":     "data_uri",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"payload": "data:image/png;base64:AAAA"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false for malformed data-URI, got %v", result.Output["valid"])
	}
}

func TestBase64DecodeStep_InvalidDataURI_NoBase64Tag(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-bad-uri", map[string]any{
		"input_from": "payload",
		"format":     "data_uri",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Missing ";base64" in data URI
	pc := NewPipelineContext(map[string]any{"payload": "data:image/png,AAAA"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false for data-URI without ;base64, got %v", result.Output["valid"])
	}
}

// ---- input_from path resolution tests ----

func TestBase64DecodeStep_InputFromStepOutput(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-from-step", map[string]any{
		"input_from": "steps.upload.encoded",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("data from step"))
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("upload", map[string]any{"encoded": encoded})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v", result.Output["valid"])
	}
}

func TestBase64DecodeStep_InputFrom_MissingPath(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-missing", map[string]any{
		"input_from": "steps.missing.encoded",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error (should return valid=false, not error): %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false for missing input_from path, got %v", result.Output["valid"])
	}
	if result.Output["reason"] == nil || result.Output["reason"] == "" {
		t.Error("expected non-empty reason when input_from path does not exist")
	}
}

func TestBase64DecodeStep_NonStringInput(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-non-string", map[string]any{
		"input_from": "payload",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Integer value at the input path — not a string
	pc := NewPipelineContext(map[string]any{"payload": 12345}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error (should return valid=false, not error): %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false for non-string input, got %v", result.Output["valid"])
	}
}

func TestBase64DecodeStep_InvalidResult_HasAllOutputKeys(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("decode-invalid-keys", map[string]any{
		"input_from": "steps.missing.value",
		"format":     "raw_base64",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["valid"] != false {
		t.Errorf("expected valid=false, got %v", result.Output["valid"])
	}

	// All output keys must be present even on failure to allow safe template access
	for _, key := range []string{"content_type", "extension", "size_bytes", "data", "valid", "reason"} {
		if _, exists := result.Output[key]; !exists {
			t.Errorf("expected output key %q to be present in invalid result", key)
		}
	}
}

// ---- name test ----

func TestBase64DecodeStep_Name(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	step, err := factory("my-decode-step", map[string]any{
		"input_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "my-decode-step" {
		t.Errorf("expected name 'my-decode-step', got %q", step.Name())
	}
}

// ---- helper function tests ----

func TestParseDataURI_Valid(t *testing.T) {
	mimeType, data, err := parseDataURI("data:image/jpeg;base64,/9j/abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mimeType != "image/jpeg" {
		t.Errorf("expected mime 'image/jpeg', got %q", mimeType)
	}
	if data != "/9j/abc123" {
		t.Errorf("expected data '/9j/abc123', got %q", data)
	}
}

func TestParseDataURI_MissingPrefix(t *testing.T) {
	_, _, err := parseDataURI("image/png;base64,abc")
	if err == nil {
		t.Error("expected error for missing data: prefix")
	}
}

func TestMimeAllowed(t *testing.T) {
	if !mimeAllowed("image/png", []string{"image/png", "image/jpeg"}) {
		t.Error("expected image/png to be allowed")
	}
	if mimeAllowed("image/gif", []string{"image/png", "image/jpeg"}) {
		t.Error("expected image/gif to not be allowed")
	}
}

func TestExtensionForMIME(t *testing.T) {
	tests := []struct {
		mime string
		ext  string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"application/pdf", ".pdf"},
		{"application/octet-stream", ".bin"},
	}
	for _, tt := range tests {
		got := extensionForMIME(tt.mime)
		if got != tt.ext {
			t.Errorf("extensionForMIME(%q) = %q, want %q", tt.mime, got, tt.ext)
		}
	}
}

func TestBase64DecodeStep_DefaultFormat(t *testing.T) {
	factory := NewBase64DecodeStepFactory()
	// No format specified — should default to data_uri
	step, err := factory("decode-default", map[string]any{
		"input_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error with default format: %v", err)
	}
	if step == nil {
		t.Fatal("expected non-nil step")
	}
}
