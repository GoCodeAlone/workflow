package module

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
)

// eventEncryptionMeta holds the per-event encryption metadata that is stored
// as CloudEvents extension attributes alongside the encrypted payload.
type eventEncryptionMeta struct {
	// Algorithm is the encryption algorithm used (e.g. "AES-256-GCM").
	Algorithm string
	// KeyID is the key identifier used to wrap the DEK.
	KeyID string
	// EncryptedDEK is the data encryption key (DEK) encrypted with the master key,
	// base64-encoded.
	EncryptedDEK string
	// EncryptedFields is the list of field names that were encrypted.
	EncryptedFields []string
}

// resolveEncryptionKey resolves the master encryption key from the KeyID.
// Only env-var references are accepted ($VAR or ${VAR}) to prevent operators
// from accidentally treating a non-secret identifier (e.g. a KMS ARN) as key
// material. Literal key strings are rejected with a descriptive error.
func resolveEncryptionKey(keyID string) ([]byte, error) {
	var raw string

	switch {
	case strings.HasPrefix(keyID, "${") && strings.HasSuffix(keyID, "}"):
		raw = os.Getenv(keyID[2 : len(keyID)-1])
	case strings.HasPrefix(keyID, "$"):
		raw = os.Getenv(keyID[1:])
	default:
		return nil, fmt.Errorf("key_id %q must be an environment variable reference (use $VAR or ${VAR})", keyID)
	}

	if raw == "" {
		return nil, fmt.Errorf("encryption key is empty (key_id=%q resolved to empty string)", keyID)
	}
	return deriveAES256Key(raw), nil
}

// deriveAES256Key derives a 32-byte AES-256 key from an arbitrary string by
// taking the SHA-256 hash — matching the behaviour of NewFieldEncryptor.
func deriveAES256Key(raw string) []byte {
	// Reuse NewFieldEncryptor to get the same SHA-256 derivation.
	enc := NewFieldEncryptor(raw)
	return enc.key
}

// generateDEK generates a random 32-byte Data Encryption Key.
func generateDEK() ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("failed to generate DEK: %w", err)
	}
	return dek, nil
}

// aesGCMEncryptBytes encrypts plaintext bytes using AES-256-GCM with the given key.
// Returns nonce || ciphertext.
func aesGCMEncryptBytes(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// aesGCMDecryptBytes decrypts nonce || ciphertext using AES-256-GCM with the given key.
func aesGCMDecryptBytes(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	nonceSize := aead.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM decryption failed: %w", err)
	}
	return plaintext, nil
}

// encryptFieldWithDEK encrypts a string field value using the DEK.
// Returns the base64-encoded ciphertext.
func encryptFieldWithDEK(dek []byte, plaintext string) (string, error) {
	ciphertext, err := aesGCMEncryptBytes(dek, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptFieldWithDEK decrypts a base64-encoded field value using the DEK.
func decryptFieldWithDEK(dek []byte, encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	plaintext, err := aesGCMDecryptBytes(dek, ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// applyEventFieldEncryption encrypts the configured fields in the payload using a
// per-event DEK. The DEK is wrapped with the master key derived from KeyID.
// Returns the modified payload and the encryption metadata to attach to the event envelope.
func applyEventFieldEncryption(payload map[string]any, cfg *EventEncryptionConfig) (map[string]any, *eventEncryptionMeta, error) {
	masterKey, err := resolveEncryptionKey(cfg.KeyID)
	if err != nil {
		return nil, nil, err
	}

	dek, err := generateDEK()
	if err != nil {
		return nil, nil, err
	}

	// Wrap the DEK with the master key.
	wrappedDEK, err := aesGCMEncryptBytes(masterKey, dek)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to wrap DEK: %w", err)
	}

	// Copy the payload so we don't mutate the caller's map.
	result := make(map[string]any, len(payload))
	maps.Copy(result, payload)

	var encrypted []string
	for _, field := range cfg.Fields {
		val, ok := result[field]
		if !ok {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		enc, encErr := encryptFieldWithDEK(dek, strVal)
		if encErr != nil {
			return nil, nil, fmt.Errorf("failed to encrypt field %q: %w", field, encErr)
		}
		result[field] = enc
		encrypted = append(encrypted, field)
	}

	meta := &eventEncryptionMeta{
		Algorithm:       cfg.Algorithm,
		KeyID:           cfg.KeyID,
		EncryptedDEK:    base64.StdEncoding.EncodeToString(wrappedDEK),
		EncryptedFields: encrypted,
	}
	return result, meta, nil
}

// decryptEventFields decrypts fields in an event payload using the wrapped DEK stored
// in the CloudEvents extension attributes.
//
// Parameters:
//   - payload: the (possibly nested) data map containing encrypted field values.
//   - encryptedDEKB64: base64-encoded wrapped DEK from the "encrypteddek" extension.
//   - encryptedFields: comma-separated field names from the "encryptedfields" extension.
//   - keyID: master key identifier from the "keyid" extension.
//
// Returns a copy of payload with the specified fields decrypted.
func decryptEventFields(payload map[string]any, encryptedDEKB64, encryptedFields, keyID string) (map[string]any, error) {
	masterKey, err := resolveEncryptionKey(keyID)
	if err != nil {
		return nil, err
	}

	wrappedDEK, err := base64.StdEncoding.DecodeString(encryptedDEKB64)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode encrypteddek: %w", err)
	}

	dek, err := aesGCMDecryptBytes(masterKey, wrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap DEK: %w", err)
	}

	result := make(map[string]any, len(payload))
	maps.Copy(result, payload)

	for _, field := range strings.Split(encryptedFields, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		val, ok := result[field]
		if !ok {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		plain, decErr := decryptFieldWithDEK(dek, strVal)
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt field %q: %w", field, decErr)
		}
		result[field] = plain
	}

	return result, nil
}
