package module

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ---- encryption helper tests ------------------------------------------------

func TestResolveEncryptionKey_LiteralRejected(t *testing.T) {
	// Literal key strings must now be rejected — only env-var references are accepted.
	_, err := resolveEncryptionKey("mysecretkey")
	if err == nil {
		t.Fatal("expected error for literal key_id; only env-var references are allowed")
	}
	if !strings.Contains(err.Error(), "environment variable reference") {
		t.Errorf("expected message about env-var reference, got: %v", err)
	}
}

func TestResolveEncryptionKey_EnvVar(t *testing.T) {
	t.Setenv("TEST_ENC_KEY", "env-secret")
	key, err := resolveEncryptionKey("$TEST_ENC_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32-byte key, got %d bytes", len(key))
	}
}

func TestResolveEncryptionKey_BraceEnvVar(t *testing.T) {
	t.Setenv("TEST_BRACE_KEY", "brace-secret")
	key, err := resolveEncryptionKey("${TEST_BRACE_KEY}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32-byte key, got %d bytes", len(key))
	}
}

func TestResolveEncryptionKey_EmptyEnvVar(t *testing.T) {
	os.Unsetenv("TEST_MISSING_KEY")
	_, err := resolveEncryptionKey("$TEST_MISSING_KEY")
	if err == nil {
		t.Fatal("expected error for empty resolved key")
	}
}

func TestResolveEncryptionKey_EmptyLiteral(t *testing.T) {
	_, err := resolveEncryptionKey("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestEncryptDecryptFieldRoundTrip(t *testing.T) {
	dek, err := generateDEK()
	if err != nil {
		t.Fatalf("generateDEK: %v", err)
	}
	plain := "sensitive-phone-number"
	enc, err := encryptFieldWithDEK(dek, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == plain {
		t.Fatal("encrypted value should differ from plaintext")
	}
	got, err := decryptFieldWithDEK(dek, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Errorf("round-trip failed: got %q, want %q", got, plain)
	}
}

func TestApplyEventFieldEncryption(t *testing.T) {
	t.Setenv("TEST_MASTER_KEY", "test-master-key-value")

	cfg := &EventEncryptionConfig{
		Provider:  "aes",
		KeyID:     "${TEST_MASTER_KEY}",
		Fields:    []string{"phone", "message_body"},
		Algorithm: "AES-256-GCM",
	}

	payload := map[string]any{
		"phone":        "+15551234567",
		"message_body": "I need help",
		"safe_field":   "not encrypted",
	}

	encrypted, meta, err := applyEventFieldEncryption(payload, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Encrypted fields should differ from originals.
	if encrypted["phone"] == payload["phone"] {
		t.Error("phone should be encrypted")
	}
	if encrypted["message_body"] == payload["message_body"] {
		t.Error("message_body should be encrypted")
	}
	// Non-targeted fields should be unchanged.
	if encrypted["safe_field"] != "not encrypted" {
		t.Errorf("safe_field should be unchanged, got %v", encrypted["safe_field"])
	}
	// Original payload must not be mutated.
	if payload["phone"] != "+15551234567" {
		t.Error("original payload should not be mutated")
	}

	// Metadata checks.
	if meta.Algorithm != "AES-256-GCM" {
		t.Errorf("expected algorithm AES-256-GCM, got %v", meta.Algorithm)
	}
	if meta.KeyID != "${TEST_MASTER_KEY}" {
		t.Errorf("expected keyID ${TEST_MASTER_KEY}, got %v", meta.KeyID)
	}
	if meta.EncryptedDEK == "" {
		t.Error("expected non-empty EncryptedDEK")
	}
	if len(meta.EncryptedFields) != 2 {
		t.Errorf("expected 2 encrypted fields, got %d", len(meta.EncryptedFields))
	}
}

func TestDecryptEventFields(t *testing.T) {
	t.Setenv("TEST_ROUND_TRIP_KEY", "round-trip-secret-value")

	cfg := &EventEncryptionConfig{
		Provider:  "aes",
		KeyID:     "${TEST_ROUND_TRIP_KEY}",
		Fields:    []string{"phone", "message_body"},
		Algorithm: "AES-256-GCM",
	}
	original := map[string]any{
		"phone":        "+15559876543",
		"message_body": "Help me please",
		"other":        "untouched",
	}

	encrypted, meta, err := applyEventFieldEncryption(original, cfg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := decryptEventFields(encrypted, meta.EncryptedDEK, strings.Join(meta.EncryptedFields, ","), meta.KeyID)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if decrypted["phone"] != original["phone"] {
		t.Errorf("phone: got %v, want %v", decrypted["phone"], original["phone"])
	}
	if decrypted["message_body"] != original["message_body"] {
		t.Errorf("message_body: got %v, want %v", decrypted["message_body"], original["message_body"])
	}
	if decrypted["other"] != "untouched" {
		t.Errorf("other should be untouched, got %v", decrypted["other"])
	}
}

// ---- step.event_publish with encryption -------------------------------------

func TestEventPublishStep_EncryptionConfig_CloudEventsEnvelope(t *testing.T) {
	t.Setenv("PUB_ENC_TEST_KEY", "test-key-secret-value")

	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-enc", map[string]any{
		"topic":      "sensitive.events",
		"broker":     "bus",
		"event_type": "user.contact",
		"source":     "/api/users",
		"payload": map[string]any{
			"phone":   "+15551234567",
			"message": "please help",
			"id":      "user-1",
		},
		"encryption": map[string]any{
			"provider":  "aes",
			"key_id":    "${PUB_ENC_TEST_KEY}",
			"fields":    []any{"phone", "message"},
			"algorithm": "AES-256-GCM",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["published"] != true {
		t.Errorf("expected published=true")
	}

	var envelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &envelope); err != nil {
		t.Fatalf("failed to unmarshal published message: %v", err)
	}

	// CloudEvents base attributes.
	if envelope["specversion"] != "1.0" {
		t.Errorf("expected specversion=1.0, got %v", envelope["specversion"])
	}
	if envelope["type"] != "user.contact" {
		t.Errorf("expected type=user.contact, got %v", envelope["type"])
	}

	// Encryption extensions.
	if envelope["encryption"] != "AES-256-GCM" {
		t.Errorf("expected encryption=AES-256-GCM, got %v", envelope["encryption"])
	}
	// keyid stores the original key_id config string (env-var reference).
	if envelope["keyid"] != "${PUB_ENC_TEST_KEY}" {
		t.Errorf("expected keyid=${PUB_ENC_TEST_KEY}, got %v", envelope["keyid"])
	}
	if envelope["encrypteddek"] == "" {
		t.Error("expected non-empty encrypteddek")
	}
	encryptedFields, _ := envelope["encryptedfields"].(string)
	if !strings.Contains(encryptedFields, "phone") || !strings.Contains(encryptedFields, "message") {
		t.Errorf("expected encryptedfields to contain phone,message; got %q", encryptedFields)
	}

	// Data fields should be encrypted (not equal to original values).
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data field in envelope")
	}
	if data["phone"] == "+15551234567" {
		t.Error("phone should be encrypted in published envelope")
	}
	if data["phone"] == nil {
		t.Error("phone field missing from published data; encryption may not have run")
	}
	if data["message"] == "please help" {
		t.Error("message should be encrypted in published envelope")
	}
	// Non-encrypted field stays unchanged.
	if data["id"] != "user-1" {
		t.Errorf("id should be unchanged, got %v", data["id"])
	}
}

func TestEventPublishStep_EncryptionConfig_EnvVarKey(t *testing.T) {
	t.Setenv("MY_EVENT_KEY", "runtime-secret-key")

	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-enc-env", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"payload": map[string]any{
			"phone": "+15550000001",
		},
		"encryption": map[string]any{
			"key_id": "${MY_EVENT_KEY}",
			"fields": []any{"phone"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// When encryption is enabled, buildEventEnvelope always wraps under "data".
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected envelope with data field; phone encrypted events always produce a wrapper")
	}
	if data["phone"] == nil {
		t.Error("phone field missing from data; encryption may not have run")
	}
	if data["phone"] == "+15550000001" {
		t.Error("phone should be encrypted, not plaintext")
	}
}

func TestEventPublishStep_NoEncryption_Unchanged(t *testing.T) {
	// Verify that when no encryption config is set, behaviour is identical to before.
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-no-enc", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"payload": map[string]any{
			"phone": "+15559999999",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["phone"] != "+15559999999" {
		t.Errorf("expected phone unchanged, got %v", payload["phone"])
	}
}

func TestEventPublishStep_EncryptionConfigMissingKey_Ignored(t *testing.T) {
	// Encryption config without key_id should be silently ignored (not configured).
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-no-keyid", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"payload": map[string]any{
			"phone": "+15558888888",
		},
		"encryption": map[string]any{
			"fields": []any{"phone"},
			// key_id intentionally missing
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Without encryption, phone should be plaintext.
	if payload["phone"] != "+15558888888" {
		t.Errorf("expected plaintext phone, got %v", payload["phone"])
	}
}

func TestEventPublishStep_UnsupportedProvider_Error(t *testing.T) {
	factory := NewEventPublishStepFactory()
	_, err := factory("pub-bad-provider", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"encryption": map[string]any{
			"provider": "kms", // unsupported
			"key_id":   "${SOME_KEY}",
			"fields":   []any{"phone"},
		},
	}, NewMockApplication())
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported encryption provider") {
		t.Errorf("expected 'unsupported encryption provider' error, got: %v", err)
	}
}

func TestEventPublishStep_UnsupportedAlgorithm_Error(t *testing.T) {
	factory := NewEventPublishStepFactory()
	_, err := factory("pub-bad-algo", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"encryption": map[string]any{
			"key_id":    "${SOME_KEY}",
			"fields":    []any{"phone"},
			"algorithm": "ChaCha20-Poly1305", // unsupported
		},
	}, NewMockApplication())
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported encryption algorithm") {
		t.Errorf("expected 'unsupported encryption algorithm' error, got: %v", err)
	}
}

func TestEventPublishStep_EncryptionWithoutBroker_Error(t *testing.T) {
	// Encryption requires a broker so the full envelope (with metadata) is published.
	factory := NewEventPublishStepFactory()
	_, err := factory("pub-enc-no-broker", map[string]any{
		"topic": "events",
		// no broker configured
		"encryption": map[string]any{
			"key_id": "${SOME_KEY}",
			"fields": []any{"phone"},
		},
	}, NewMockApplication())
	if err == nil {
		t.Fatal("expected error when encryption is configured without a broker")
	}
	if !strings.Contains(err.Error(), "'broker' or 'provider' is required when encryption") {
		t.Errorf("expected broker-required error, got: %v", err)
	}
}

// ---- step.event_decrypt -----------------------------------------------------

func TestEventDecryptStep_RoundTrip(t *testing.T) {
	t.Setenv("DECRYPT_STEP_KEY", "decrypt-step-secret-value")

	// Simulate the publish side: encrypt an event.
	cfg := &EventEncryptionConfig{
		Provider:  "aes",
		KeyID:     "${DECRYPT_STEP_KEY}",
		Fields:    []string{"phone", "message_body"},
		Algorithm: "AES-256-GCM",
	}
	originalData := map[string]any{
		"phone":        "+15551234567",
		"message_body": "I need help",
		"id":           "conv-1",
	}
	encData, meta, err := applyEventFieldEncryption(originalData, cfg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Build a CloudEvents envelope as published by step.event_publish.
	event := map[string]any{
		"specversion":     "1.0",
		"type":            "test.event",
		"source":          "/test",
		"id":              "evt-1",
		"time":            "2026-01-01T00:00:00Z",
		"encryption":      meta.Algorithm,
		"keyid":           meta.KeyID,
		"encrypteddek":    meta.EncryptedDEK,
		"encryptedfields": strings.Join(meta.EncryptedFields, ","),
		"data":            encData,
	}

	// Now decrypt with step.event_decrypt.
	factory := NewEventDecryptStepFactory()
	step, err := factory("decrypt-step", map[string]any{}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(event, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	output := result.Output
	data, ok := output["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data field in output, got %T", output["data"])
	}

	if data["phone"] != "+15551234567" {
		t.Errorf("phone: got %v, want +15551234567", data["phone"])
	}
	if data["message_body"] != "I need help" {
		t.Errorf("message_body: got %v, want 'I need help'", data["message_body"])
	}
	if data["id"] != "conv-1" {
		t.Errorf("id: got %v, want conv-1", data["id"])
	}

	// CloudEvents attributes should be preserved.
	if output["specversion"] != "1.0" {
		t.Errorf("specversion should be preserved, got %v", output["specversion"])
	}
	if output["type"] != "test.event" {
		t.Errorf("type should be preserved, got %v", output["type"])
	}
}

func TestEventDecryptStep_KeyIDOverride(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "override-master-key-secret")

	// Encrypt using the env-var key.
	cfg := &EventEncryptionConfig{
		Provider:  "aes",
		KeyID:     "${OVERRIDE_KEY}",
		Fields:    []string{"phone"},
		Algorithm: "AES-256-GCM",
	}
	encData, meta, err := applyEventFieldEncryption(map[string]any{"phone": "+15550001111"}, cfg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Simulate an event where the keyid extension has been tampered/replaced with
	// a wrong value — the step config key_id should override it.
	event := map[string]any{
		"encryption":      meta.Algorithm,
		"keyid":           "$SOME_OTHER_UNSET_VAR", // wrong key in event
		"encrypteddek":    meta.EncryptedDEK,
		"encryptedfields": strings.Join(meta.EncryptedFields, ","),
		"data":            encData,
	}

	factory := NewEventDecryptStepFactory()
	// The step uses key_id="${OVERRIDE_KEY}" which resolves to the correct value.
	step, err := factory("decrypt-override", map[string]any{
		"key_id": "${OVERRIDE_KEY}",
	}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(event, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	data := result.Output["data"].(map[string]any)
	if data["phone"] != "+15550001111" {
		t.Errorf("phone: got %v, want +15550001111", data["phone"])
	}
}

func TestEventDecryptStep_NoEncryptionMetadata_Passthrough(t *testing.T) {
	// An event without encryption metadata should be passed through unchanged.
	event := map[string]any{
		"specversion": "1.0",
		"type":        "plain.event",
		"data": map[string]any{
			"foo": "bar",
		},
	}

	factory := NewEventDecryptStepFactory()
	step, err := factory("decrypt-passthrough", map[string]any{}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(event, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	data, ok := result.Output["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data field in output")
	}
	if data["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %v", data["foo"])
	}
}

func TestEventDecryptStep_NilData_Passthrough(t *testing.T) {
	factory := NewEventDecryptStepFactory()
	step, err := factory("decrypt-nil", map[string]any{}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// NewPipelineContext always creates a non-nil Current map, even with nil trigger data.
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// No encryption metadata → passthrough of the empty map.
	if result.Output == nil {
		t.Error("expected non-nil output for empty event")
	}
}

func TestEventDecryptStep_WrongKey_Error(t *testing.T) {
	t.Setenv("CORRECT_ENC_KEY", "correct-secret-value-abc")
	t.Setenv("WRONG_ENC_KEY", "completely-different-value-xyz")

	cfg := &EventEncryptionConfig{
		Provider:  "aes",
		KeyID:     "${CORRECT_ENC_KEY}",
		Fields:    []string{"phone"},
		Algorithm: "AES-256-GCM",
	}
	encData, meta, err := applyEventFieldEncryption(map[string]any{"phone": "+15550002222"}, cfg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// The event's keyid extension points to a different key than was used for encryption.
	event := map[string]any{
		"encryption":      meta.Algorithm,
		"keyid":           "${WRONG_ENC_KEY}", // different key → DEK unwrap must fail
		"encrypteddek":    meta.EncryptedDEK,
		"encryptedfields": strings.Join(meta.EncryptedFields, ","),
		"data":            encData,
	}

	factory := NewEventDecryptStepFactory()
	step, err := factory("decrypt-wrong-key", map[string]any{}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(event, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when using wrong key")
	}
	if !strings.Contains(err.Error(), "unwrap DEK") {
		t.Errorf("expected 'unwrap DEK' error, got: %v", err)
	}
}

func TestEventDecryptStep_UnsupportedAlgorithm_Error(t *testing.T) {
	// An event with an unknown encryption algorithm should return a clear error.
	t.Setenv("SOME_TEST_KEY", "some-test-secret")

	event := map[string]any{
		"encryption":      "ChaCha20-Poly1305", // unsupported
		"keyid":           "${SOME_TEST_KEY}",
		"encrypteddek":    "dGVzdA==",
		"encryptedfields": "phone",
		"data": map[string]any{
			"phone": "some-value",
		},
	}

	factory := NewEventDecryptStepFactory()
	step, err := factory("decrypt-bad-algo", map[string]any{}, NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(event, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported encryption algorithm") {
		t.Errorf("expected 'unsupported encryption algorithm' error, got: %v", err)
	}
}

// TestEventPublishAndDecrypt_FullPipeline tests the full publish→decrypt round trip
// using both steps together.
func TestEventPublishAndDecrypt_FullPipeline(t *testing.T) {
	t.Setenv("PIPELINE_ENC_KEY", "full-pipeline-integration-secret")

	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	// Publish step with encryption.
	publishFactory := NewEventPublishStepFactory()
	publishStep, err := publishFactory("publish", map[string]any{
		"topic":      "healthcare.events",
		"broker":     "bus",
		"event_type": "patient.contact",
		"source":     "/api/healthcare",
		"payload": map[string]any{
			"phone":          "+15559990000",
			"responder_name": "Dr. Smith",
			"case_id":        "case-42",
		},
		"encryption": map[string]any{
			"provider":  "aes",
			"key_id":    "${PIPELINE_ENC_KEY}",
			"fields":    []any{"phone", "responder_name"},
			"algorithm": "AES-256-GCM",
		},
	}, app)
	if err != nil {
		t.Fatalf("publish factory: %v", err)
	}

	publishCtx := NewPipelineContext(nil, nil)
	_, err = publishStep.Execute(context.Background(), publishCtx)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Retrieve the published message from the broker.
	if len(broker.producer.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(broker.producer.published))
	}
	var publishedEnvelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &publishedEnvelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Simulate consumer receiving the message — decrypt step.
	decryptFactory := NewEventDecryptStepFactory()
	decryptStep, err := decryptFactory("decrypt", map[string]any{}, app)
	if err != nil {
		t.Fatalf("decrypt factory: %v", err)
	}

	decryptCtx := NewPipelineContext(publishedEnvelope, nil)
	decryptResult, err := decryptStep.Execute(context.Background(), decryptCtx)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	data, ok := decryptResult.Output["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data field in decrypt output, got type %T", decryptResult.Output["data"])
	}

	if data["phone"] != "+15559990000" {
		t.Errorf("phone: got %v, want +15559990000", data["phone"])
	}
	if data["responder_name"] != "Dr. Smith" {
		t.Errorf("responder_name: got %v, want 'Dr. Smith'", data["responder_name"])
	}
	if data["case_id"] != "case-42" {
		t.Errorf("case_id should be unencrypted, got %v", data["case_id"])
	}
}
