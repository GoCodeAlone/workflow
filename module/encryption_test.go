package module

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewFieldEncryptor(t *testing.T) {
	t.Run("empty key disables encryption", func(t *testing.T) {
		enc := NewFieldEncryptor("")
		if enc.Enabled() {
			t.Error("expected encryption to be disabled with empty key")
		}
	})

	t.Run("non-empty key enables encryption", func(t *testing.T) {
		enc := NewFieldEncryptor("test-key-32-characters-long!!!!!")
		if !enc.Enabled() {
			t.Error("expected encryption to be enabled with non-empty key")
		}
	})
}

func TestEncryptDecryptValue(t *testing.T) {
	enc := NewFieldEncryptor("test-encryption-key-for-unit-tests")

	t.Run("encrypt and decrypt round-trip", func(t *testing.T) {
		original := "Hello, World! This is PII data."
		encrypted, err := enc.EncryptValue(original)
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}

		if encrypted == original {
			t.Error("encrypted value should differ from plaintext")
		}
		if !strings.HasPrefix(encrypted, encryptedPrefix) {
			t.Errorf("encrypted value should start with %q prefix", encryptedPrefix)
		}

		decrypted, err := enc.DecryptValue(encrypted)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}
		if decrypted != original {
			t.Errorf("got %q, want %q", decrypted, original)
		}
	})

	t.Run("empty string passthrough", func(t *testing.T) {
		encrypted, err := enc.EncryptValue("")
		if err != nil {
			t.Fatalf("encrypt empty failed: %v", err)
		}
		if encrypted != "" {
			t.Error("empty string should pass through")
		}
	})

	t.Run("already encrypted value is not double-encrypted", func(t *testing.T) {
		original := "sensitive data"
		encrypted, _ := enc.EncryptValue(original)
		doubleEncrypted, _ := enc.EncryptValue(encrypted)
		if doubleEncrypted != encrypted {
			t.Error("double encryption should be idempotent")
		}
	})

	t.Run("plaintext passthrough on decrypt", func(t *testing.T) {
		plaintext := "not encrypted"
		result, err := enc.DecryptValue(plaintext)
		if err != nil {
			t.Fatalf("decrypt plaintext failed: %v", err)
		}
		if result != plaintext {
			t.Error("plaintext should pass through decrypt unchanged")
		}
	})

	t.Run("disabled encryptor passes through", func(t *testing.T) {
		disabled := NewFieldEncryptor("")
		original := "some data"
		encrypted, _ := disabled.EncryptValue(original)
		if encrypted != original {
			t.Error("disabled encryptor should pass through")
		}
		decrypted, _ := disabled.DecryptValue(original)
		if decrypted != original {
			t.Error("disabled encryptor should pass through")
		}
	})

	t.Run("wrong key fails decryption", func(t *testing.T) {
		enc2 := NewFieldEncryptor("different-key-for-testing-wrong-key")
		encrypted, _ := enc.EncryptValue("secret")
		_, err := enc2.DecryptValue(encrypted)
		if err == nil {
			t.Error("decryption with wrong key should fail")
		}
	})

	t.Run("each encryption produces unique ciphertext", func(t *testing.T) {
		enc1, _ := enc.EncryptValue("same input")
		enc2, _ := enc.EncryptValue("same input")
		if enc1 == enc2 {
			t.Error("same plaintext should produce different ciphertext (random nonce)")
		}
	})
}

func TestEncryptDecryptPIIFields(t *testing.T) {
	enc := NewFieldEncryptor("test-key-for-pii-fields")

	t.Run("encrypts known PII fields", func(t *testing.T) {
		data := map[string]interface{}{
			"name":    "John Doe",
			"email":   "john@example.com",
			"From":    "+15551234567",
			"state":   "active",
			"id":      "conv-1",
			"message": "I need help",
		}

		encrypted, err := enc.EncryptPIIFields(data)
		if err != nil {
			t.Fatalf("encrypt PII failed: %v", err)
		}

		// PII fields should be encrypted
		for _, field := range []string{"name", "email", "From", "message"} {
			val, ok := encrypted[field].(string)
			if !ok {
				t.Errorf("field %q missing from encrypted data", field)
				continue
			}
			if !strings.HasPrefix(val, encryptedPrefix) {
				t.Errorf("field %q should be encrypted, got %q", field, val)
			}
		}

		// Non-PII fields should be unchanged
		if encrypted["state"] != "active" {
			t.Error("non-PII field 'state' should be unchanged")
		}
		if encrypted["id"] != "conv-1" {
			t.Error("non-PII field 'id' should be unchanged")
		}
	})

	t.Run("round-trip preserves data", func(t *testing.T) {
		original := map[string]interface{}{
			"name":      "Jane Smith",
			"email":     "jane@example.com",
			"phone":     "+15559876543",
			"state":     "queued",
			"id":        "conv-2",
			"body":      "Help me please",
			"riskLevel": "high",
		}

		encrypted, err := enc.EncryptPIIFields(original)
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}

		decrypted, err := enc.DecryptPIIFields(encrypted)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}

		for _, field := range []string{"name", "email", "phone", "body"} {
			if decrypted[field] != original[field] {
				t.Errorf("field %q: got %q, want %q", field, decrypted[field], original[field])
			}
		}
		// Non-PII unchanged
		if decrypted["state"] != "queued" {
			t.Error("state should be unchanged")
		}
		if decrypted["riskLevel"] != "high" {
			t.Error("riskLevel should be unchanged")
		}
	})

	t.Run("encrypts messages array", func(t *testing.T) {
		data := map[string]interface{}{
			"id":    "conv-3",
			"state": "active",
			"messages": []interface{}{
				map[string]interface{}{
					"body":      "I feel terrible",
					"from":      "texter",
					"direction": "inbound",
					"timestamp": "2026-01-01T00:00:00Z",
				},
				map[string]interface{}{
					"body":      "I hear you. Tell me more.",
					"from":      "user-001",
					"direction": "outbound",
					"timestamp": "2026-01-01T00:01:00Z",
				},
			},
		}

		encrypted, err := enc.EncryptPIIFields(data)
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}

		msgs := encrypted["messages"].([]interface{})
		for i, m := range msgs {
			msg := m.(map[string]interface{})
			body := msg["body"].(string)
			if !strings.HasPrefix(body, encryptedPrefix) {
				t.Errorf("message[%d].body should be encrypted", i)
			}
			from := msg["from"].(string)
			if !strings.HasPrefix(from, encryptedPrefix) {
				t.Errorf("message[%d].from should be encrypted", i)
			}
			// direction and timestamp should be unchanged
			if msg["direction"] != data["messages"].([]interface{})[i].(map[string]interface{})["direction"] {
				t.Errorf("message[%d].direction should be unchanged", i)
			}
		}

		// Round-trip
		decrypted, err := enc.DecryptPIIFields(encrypted)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}
		decMsgs := decrypted["messages"].([]interface{})
		origMsgs := data["messages"].([]interface{})
		for i, m := range decMsgs {
			msg := m.(map[string]interface{})
			orig := origMsgs[i].(map[string]interface{})
			if msg["body"] != orig["body"] {
				t.Errorf("message[%d].body: got %q, want %q", i, msg["body"], orig["body"])
			}
			if msg["from"] != orig["from"] {
				t.Errorf("message[%d].from: got %q, want %q", i, msg["from"], orig["from"])
			}
		}
	})

	t.Run("nil data passthrough", func(t *testing.T) {
		result, err := enc.EncryptPIIFields(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("nil data should return nil")
		}
	})
}

func TestEncryptDecryptJSON(t *testing.T) {
	enc := NewFieldEncryptor("test-key-for-json-encryption")

	t.Run("round-trip JSON payload", func(t *testing.T) {
		original := map[string]interface{}{
			"conversationId": "conv-1",
			"name":           "John Doe",
			"phone":          "+15551234567",
			"message":        "Help me",
		}
		data, _ := json.Marshal(original)

		encrypted, err := enc.EncryptJSON(data)
		if err != nil {
			t.Fatalf("encrypt JSON failed: %v", err)
		}

		// Should be a JSON envelope with _encrypted key
		var envelope map[string]string
		if err := json.Unmarshal(encrypted, &envelope); err != nil {
			t.Fatalf("encrypted output should be valid JSON: %v", err)
		}
		if _, ok := envelope["_encrypted"]; !ok {
			t.Error("encrypted output should have _encrypted key")
		}

		decrypted, err := enc.DecryptJSON(encrypted)
		if err != nil {
			t.Fatalf("decrypt JSON failed: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(decrypted, &result); err != nil {
			t.Fatalf("decrypted output should be valid JSON: %v", err)
		}

		if result["name"] != "John Doe" {
			t.Errorf("name: got %q, want %q", result["name"], "John Doe")
		}
		if result["phone"] != "+15551234567" {
			t.Errorf("phone: got %q, want %q", result["phone"], "+15551234567")
		}
	})

	t.Run("non-encrypted JSON passthrough", func(t *testing.T) {
		plain := []byte(`{"hello":"world"}`)
		result, err := enc.DecryptJSON(plain)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result) != string(plain) {
			t.Error("non-encrypted JSON should pass through")
		}
	})

	t.Run("empty data passthrough", func(t *testing.T) {
		result, err := enc.EncryptJSON(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("nil data should return nil")
		}
	})

	t.Run("disabled encryptor passthrough", func(t *testing.T) {
		disabled := NewFieldEncryptor("")
		data := []byte(`{"secret":"value"}`)
		encrypted, _ := disabled.EncryptJSON(data)
		if string(encrypted) != string(data) {
			t.Error("disabled encryptor should pass through")
		}
	})
}
