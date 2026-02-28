package fieldcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Prefix is the marker for encrypted protected field values.
const Prefix = "epf:"

// legacyPrefix is the old encryption prefix from the original FieldEncryptor.
const legacyPrefix = "enc::"

// Encrypt encrypts plaintext with AES-256-GCM, returning "epf:v{version}:{base64(nonce+ciphertext)}".
func Encrypt(plaintext string, key []byte, version int) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: create GCM: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("fieldcrypt: generate nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return fmt.Sprintf("%sv%d:%s", Prefix, version, encoded), nil
}

// Decrypt decrypts an epf:-prefixed value. It also handles legacy "enc::" prefix values.
// keyFn is called with the version number extracted from the prefix.
// For legacy enc:: values, keyFn(0) is called to obtain the raw master key,
// which is then SHA256-hashed to match the original FieldEncryptor behavior.
func Decrypt(ciphertext string, keyFn func(version int) ([]byte, error)) (string, error) {
	if strings.HasPrefix(ciphertext, legacyPrefix) {
		return decryptLegacy(ciphertext, keyFn)
	}
	if !strings.HasPrefix(ciphertext, Prefix) {
		return "", fmt.Errorf("fieldcrypt: not an encrypted value")
	}

	// Parse "epf:v{version}:{base64}"
	rest := strings.TrimPrefix(ciphertext, Prefix)
	idx := strings.Index(rest, ":")
	if idx < 0 || !strings.HasPrefix(rest, "v") {
		return "", fmt.Errorf("fieldcrypt: invalid format")
	}
	version, err := strconv.Atoi(rest[1:idx])
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: invalid version: %w", err)
	}
	encoded := rest[idx+1:]

	key, err := keyFn(version)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: key lookup: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: base64 decode: %w", err)
	}

	return decryptAESGCM(raw, key)
}

// IsEncrypted checks if a value has the epf: prefix or legacy enc:: prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix) || strings.HasPrefix(value, legacyPrefix)
}

// decryptLegacy handles the old "enc::" prefix format.
func decryptLegacy(ciphertext string, keyFn func(version int) ([]byte, error)) (string, error) {
	raw := strings.TrimPrefix(ciphertext, legacyPrefix)
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: legacy base64 decode: %w", err)
	}
	// keyFn(0) returns the raw master key; hash it with SHA256 to match original behavior.
	masterKey, err := keyFn(0)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: legacy key lookup: %w", err)
	}
	hash := sha256.Sum256(masterKey)
	return decryptAESGCM(decoded, hash[:])
}

// decryptAESGCM decrypts raw bytes (nonce + ciphertext) with the given AES-256-GCM key.
func decryptAESGCM(raw, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: create GCM: %w", err)
	}
	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("fieldcrypt: ciphertext too short")
	}
	nonce, ct := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: decryption failed: %w", err)
	}
	return string(plaintext), nil
}
