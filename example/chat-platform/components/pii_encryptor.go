//go:build ignore

package component

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

func Name() string {
	return "pii-encryptor"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

// deriveKey creates a 32-byte AES-256 key from an arbitrary string using SHA-256.
func deriveKey(keyStr string) []byte {
	hash := sha256.Sum256([]byte(keyStr))
	return hash[:]
}

func encryptField(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
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
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptField(encoded string, key []byte) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}
	block, err := aes.NewCipher(key)
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

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}

	keyStr, _ := params["key"].(string)
	if keyStr == "" {
		return nil, fmt.Errorf("missing required parameter: key")
	}
	key := deriveKey(keyStr)

	data, _ := params["data"].(map[string]interface{})
	if data == nil {
		return nil, fmt.Errorf("missing required parameter: data")
	}

	fieldsList, _ := params["fields"].([]interface{})
	if len(fieldsList) == 0 {
		return nil, fmt.Errorf("missing required parameter: fields")
	}

	fields := make([]string, len(fieldsList))
	for i, f := range fieldsList {
		fields[i], _ = f.(string)
	}

	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = v
	}
	processedFields := make([]interface{}, 0)

	switch action {
	case "encrypt":
		for _, field := range fields {
			val, ok := result[field]
			if !ok {
				continue
			}
			plaintext := fmt.Sprintf("%v", val)
			encrypted, err := encryptField(plaintext, key)
			if err != nil {
				return nil, fmt.Errorf("encrypt field %s: %w", field, err)
			}
			result[field] = encrypted
			processedFields = append(processedFields, field)
		}
		return map[string]interface{}{
			"data":            result,
			"action":          "encrypt",
			"processedFields": processedFields,
		}, nil

	case "decrypt":
		for _, field := range fields {
			val, ok := result[field]
			if !ok {
				continue
			}
			encoded, ok := val.(string)
			if !ok {
				continue
			}
			decrypted, err := decryptField(encoded, key)
			if err != nil {
				return nil, fmt.Errorf("decrypt field %s: %w", field, err)
			}
			result[field] = decrypted
			processedFields = append(processedFields, field)
		}
		return map[string]interface{}{
			"data":            result,
			"action":          "decrypt",
			"processedFields": processedFields,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'encrypt' or 'decrypt')", action)
	}
}
