package module

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// FieldEncryptor provides AES-256-GCM encryption for PII fields in data maps.
// It encrypts specific fields before storage and decrypts them on retrieval,
// ensuring data at rest contains no plaintext PII.
type FieldEncryptor struct {
	key     []byte
	enabled bool
}

// encryptedPrefix marks a value as encrypted so we can detect and skip
// already-encrypted data during double-encrypt scenarios.
const encryptedPrefix = "enc::"

// NewFieldEncryptor creates a FieldEncryptor from a key string.
// If the key is empty, encryption is disabled (passthrough mode).
func NewFieldEncryptor(keyStr string) *FieldEncryptor {
	if keyStr == "" {
		return &FieldEncryptor{enabled: false}
	}
	hash := sha256.Sum256([]byte(keyStr))
	return &FieldEncryptor{
		key:     hash[:],
		enabled: true,
	}
}

// NewFieldEncryptorFromEnv creates a FieldEncryptor using the ENCRYPTION_KEY
// environment variable. Returns a disabled encryptor if the var is not set.
func NewFieldEncryptorFromEnv() *FieldEncryptor {
	return NewFieldEncryptor(os.Getenv("ENCRYPTION_KEY"))
}

// Enabled returns whether encryption is active.
func (e *FieldEncryptor) Enabled() bool {
	return e.enabled
}

// EncryptValue encrypts a single string value using AES-256-GCM.
// Returns the encrypted value prefixed with "enc::" for identification.
func (e *FieldEncryptor) EncryptValue(plaintext string) (string, error) {
	if !e.enabled || plaintext == "" {
		return plaintext, nil
	}
	// Already encrypted — skip
	if strings.HasPrefix(plaintext, encryptedPrefix) {
		return plaintext, nil
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptValue decrypts a single AES-256-GCM encrypted value.
// Values without the "enc::" prefix are returned as-is (plaintext passthrough).
func (e *FieldEncryptor) DecryptValue(encoded string) (string, error) {
	if !e.enabled || encoded == "" {
		return encoded, nil
	}
	// Not encrypted — passthrough
	if !strings.HasPrefix(encoded, encryptedPrefix) {
		return encoded, nil
	}

	raw := strings.TrimPrefix(encoded, encryptedPrefix)
	ciphertext, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}
	return string(plaintext), nil
}

// piiFields defines which top-level data map keys contain PII and should
// be encrypted at rest. Nested "messages" arrays are handled separately.
var piiFields = map[string]bool{
	"name":        true,
	"Name":        true,
	"email":       true,
	"phone":       true,
	"phoneNumber": true,
	"From":        true,
	"from":        true,
	"to":          true,
	"address":     true,
	"body":        true,
	"Body":        true,
	"message":     true,
	"reason":      true,
}

// EncryptPIIFields encrypts known PII fields in a data map.
// It handles nested "messages" arrays where each message may contain PII.
func (e *FieldEncryptor) EncryptPIIFields(data map[string]interface{}) (map[string]interface{}, error) {
	if !e.enabled || data == nil {
		return data, nil
	}

	result := make(map[string]interface{}, len(data))
	for k, v := range data {
		result[k] = v
	}

	// Encrypt top-level PII fields
	for field := range piiFields {
		val, ok := result[field]
		if !ok {
			continue
		}
		str, ok := val.(string)
		if !ok || str == "" {
			continue
		}
		encrypted, err := e.EncryptValue(str)
		if err != nil {
			return nil, fmt.Errorf("encrypt field %q: %w", field, err)
		}
		result[field] = encrypted
	}

	// Encrypt PII within messages array
	if msgs, ok := result["messages"].([]interface{}); ok {
		encMsgs := make([]interface{}, len(msgs))
		for i, m := range msgs {
			msg, ok := m.(map[string]interface{})
			if !ok {
				encMsgs[i] = m
				continue
			}
			encMsg := make(map[string]interface{}, len(msg))
			for k, v := range msg {
				encMsg[k] = v
			}
			for field := range piiFields {
				val, ok := encMsg[field]
				if !ok {
					continue
				}
				str, ok := val.(string)
				if !ok || str == "" {
					continue
				}
				encrypted, err := e.EncryptValue(str)
				if err != nil {
					return nil, fmt.Errorf("encrypt message[%d].%s: %w", i, field, err)
				}
				encMsg[field] = encrypted
			}
			encMsgs[i] = encMsg
		}
		result["messages"] = encMsgs
	}

	return result, nil
}

// DecryptPIIFields decrypts known PII fields in a data map.
// Values without the "enc::" prefix are returned as-is (backward compatible).
func (e *FieldEncryptor) DecryptPIIFields(data map[string]interface{}) (map[string]interface{}, error) {
	if !e.enabled || data == nil {
		return data, nil
	}

	result := make(map[string]interface{}, len(data))
	for k, v := range data {
		result[k] = v
	}

	// Decrypt top-level PII fields
	for field := range piiFields {
		val, ok := result[field]
		if !ok {
			continue
		}
		str, ok := val.(string)
		if !ok || str == "" {
			continue
		}
		decrypted, err := e.DecryptValue(str)
		if err != nil {
			return nil, fmt.Errorf("decrypt field %q: %w", field, err)
		}
		result[field] = decrypted
	}

	// Decrypt PII within messages array
	if msgs, ok := result["messages"].([]interface{}); ok {
		decMsgs := make([]interface{}, len(msgs))
		for i, m := range msgs {
			msg, ok := m.(map[string]interface{})
			if !ok {
				decMsgs[i] = m
				continue
			}
			decMsg := make(map[string]interface{}, len(msg))
			for k, v := range msg {
				decMsg[k] = v
			}
			for field := range piiFields {
				val, ok := decMsg[field]
				if !ok {
					continue
				}
				str, ok := val.(string)
				if !ok || str == "" {
					continue
				}
				decrypted, err := e.DecryptValue(str)
				if err != nil {
					return nil, fmt.Errorf("decrypt message[%d].%s: %w", i, field, err)
				}
				decMsg[field] = decrypted
			}
			decMsgs[i] = decMsg
		}
		result["messages"] = decMsgs
	}

	return result, nil
}

// EncryptJSON encrypts an entire JSON payload (for Kafka messages).
// The entire message is encrypted as a single blob.
func (e *FieldEncryptor) EncryptJSON(data []byte) ([]byte, error) {
	if !e.enabled || len(data) == 0 {
		return data, nil
	}

	encrypted, err := e.EncryptValue(string(data))
	if err != nil {
		return nil, err
	}
	// Wrap in a JSON envelope so consumers know it's encrypted
	envelope := map[string]string{
		"_encrypted": encrypted,
	}
	return json.Marshal(envelope)
}

// DecryptJSON decrypts an entire JSON payload (for Kafka messages).
// Non-encrypted payloads (no "_encrypted" key) are returned as-is.
func (e *FieldEncryptor) DecryptJSON(data []byte) ([]byte, error) {
	if !e.enabled || len(data) == 0 {
		return data, nil
	}

	// Check if this is an encrypted envelope
	var envelope map[string]string
	if err := json.Unmarshal(data, &envelope); err != nil {
		// Not JSON or not our envelope — return as-is
		return data, nil
	}

	encrypted, ok := envelope["_encrypted"]
	if !ok {
		// Not an encrypted envelope — return as-is
		return data, nil
	}

	decrypted, err := e.DecryptValue(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt kafka message: %w", err)
	}
	return []byte(decrypted), nil
}
