package fieldcrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := "sensitive data here"
	encrypted, err := Encrypt(plaintext, key, 1)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if !strings.HasPrefix(encrypted, "epf:v1:") {
		t.Fatalf("expected epf:v1: prefix, got %q", encrypted)
	}

	decrypted, err := Decrypt(encrypted, func(version int) ([]byte, error) {
		if version != 1 {
			t.Fatalf("expected version 1, got %d", version)
		}
		return key, nil
	})
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecryptVersion(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	encrypted, err := Encrypt("hello", key, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, "epf:v42:") {
		t.Fatalf("expected epf:v42: prefix, got %q", encrypted)
	}

	decrypted, err := Decrypt(encrypted, func(version int) ([]byte, error) {
		if version != 42 {
			t.Fatalf("expected version 42, got %d", version)
		}
		return key, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "hello" {
		t.Fatalf("expected hello, got %q", decrypted)
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"epf:v1:abc", true},
		{"enc::abc", true},
		{"plaintext", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsEncrypted(tt.value); got != tt.want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestLegacyEncDecrypt(t *testing.T) {
	// Simulate the old FieldEncryptor: SHA256(masterKey) -> AES-256-GCM.
	masterKey := []byte("my-secret-key")
	hash := sha256.Sum256(masterKey)

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	ct := aead.Seal(nonce, nonce, []byte("legacy secret"), nil)
	legacyEncoded := "enc::" + base64.StdEncoding.EncodeToString(ct)

	decrypted, err := Decrypt(legacyEncoded, func(version int) ([]byte, error) {
		if version != 0 {
			t.Fatalf("expected version 0 for legacy, got %d", version)
		}
		return masterKey, nil
	})
	if err != nil {
		t.Fatalf("Decrypt legacy: %v", err)
	}
	if decrypted != "legacy secret" {
		t.Fatalf("expected 'legacy secret', got %q", decrypted)
	}
}

func TestMaskEmail(t *testing.T) {
	got := MaskEmail("john@example.com")
	if got != "j***@e***.com" {
		t.Errorf("MaskEmail = %q, want %q", got, "j***@e***.com")
	}
}

func TestMaskPhone(t *testing.T) {
	got := MaskPhone("555-123-4567")
	if got != "***-***-4567" {
		t.Errorf("MaskPhone = %q, want %q", got, "***-***-4567")
	}
}

func TestHashValue(t *testing.T) {
	h := HashValue("test")
	if len(h) != 64 {
		t.Errorf("expected 64 char hex, got %d chars", len(h))
	}
}

func TestRedactValue(t *testing.T) {
	if got := RedactValue(); got != "[REDACTED]" {
		t.Errorf("RedactValue = %q", got)
	}
}

func TestMaskValueBehaviors(t *testing.T) {
	if MaskValue("secret", LogRedact, "") != "[REDACTED]" {
		t.Error("LogRedact failed")
	}
	if MaskValue("secret", LogAllow, "") != "secret" {
		t.Error("LogAllow failed")
	}
	h := MaskValue("secret", LogHash, "")
	if len(h) != 64 {
		t.Error("LogHash failed")
	}
}

func TestScanAndEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry([]ProtectedField{
		{Name: "ssn", Classification: ClassPII, Encryption: true, LogBehavior: LogRedact},
		{Name: "email", Classification: ClassPII, Encryption: true, LogBehavior: LogMask},
	})

	data := map[string]any{
		"ssn":   "123-45-6789",
		"email": "test@example.com",
		"name":  "John",
		"nested": map[string]any{
			"ssn": "987-65-4321",
		},
		"items": []any{
			map[string]any{"email": "a@b.com"},
		},
	}

	err := ScanAndEncrypt(data, registry, func() ([]byte, int, error) {
		return key, 1, nil
	}, 10)
	if err != nil {
		t.Fatalf("ScanAndEncrypt: %v", err)
	}

	// Verify fields are encrypted.
	if !IsEncrypted(data["ssn"].(string)) {
		t.Error("ssn should be encrypted")
	}
	if !IsEncrypted(data["email"].(string)) {
		t.Error("email should be encrypted")
	}
	if data["name"] != "John" {
		t.Error("name should not be modified")
	}
	nested := data["nested"].(map[string]any)
	if !IsEncrypted(nested["ssn"].(string)) {
		t.Error("nested ssn should be encrypted")
	}
	items := data["items"].([]any)
	item := items[0].(map[string]any)
	if !IsEncrypted(item["email"].(string)) {
		t.Error("array item email should be encrypted")
	}

	// Now decrypt.
	err = ScanAndDecrypt(data, registry, func(version int) ([]byte, error) {
		return key, nil
	}, 10)
	if err != nil {
		t.Fatalf("ScanAndDecrypt: %v", err)
	}

	if data["ssn"] != "123-45-6789" {
		t.Errorf("ssn = %q, want 123-45-6789", data["ssn"])
	}
	if data["email"] != "test@example.com" {
		t.Errorf("email = %q, want test@example.com", data["email"])
	}
	if nested["ssn"] != "987-65-4321" {
		t.Errorf("nested ssn = %q", nested["ssn"])
	}
	item = items[0].(map[string]any)
	if item["email"] != "a@b.com" {
		t.Errorf("array item email = %q", item["email"])
	}
}

func TestScanAndMask(t *testing.T) {
	registry := NewRegistry([]ProtectedField{
		{Name: "ssn", Classification: ClassPII, LogBehavior: LogRedact},
		{Name: "email", Classification: ClassPII, LogBehavior: LogMask},
	})

	data := map[string]any{
		"ssn":   "123-45-6789",
		"email": "test@example.com",
		"name":  "John",
	}

	masked := ScanAndMask(data, registry, 10)

	if masked["ssn"] != "[REDACTED]" {
		t.Errorf("ssn mask = %q", masked["ssn"])
	}
	if masked["email"] == "test@example.com" {
		t.Error("email should be masked")
	}
	if masked["name"] != "John" {
		t.Error("name should be unchanged")
	}

	// Original should be unmodified.
	if data["ssn"] != "123-45-6789" {
		t.Error("original ssn was modified")
	}
}

func TestKeyRingTenantIsolation(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	kr := NewLocalKeyRing(masterKey)
	ctx := context.Background()

	keyA, verA, err := kr.CurrentKey(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if verA != 1 {
		t.Fatalf("expected version 1, got %d", verA)
	}

	keyB, verB, err := kr.CurrentKey(ctx, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if verB != 1 {
		t.Fatalf("expected version 1, got %d", verB)
	}

	// Keys should be different for different tenants.
	if string(keyA) == string(keyB) {
		t.Error("tenant keys should differ")
	}
}

func TestKeyRingRotation(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	kr := NewLocalKeyRing(masterKey)
	ctx := context.Background()

	key1, ver1, err := kr.CurrentKey(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}

	key2, ver2, err := kr.Rotate(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if ver2 != ver1+1 {
		t.Fatalf("expected version %d, got %d", ver1+1, ver2)
	}
	if string(key1) == string(key2) {
		t.Error("rotated key should differ")
	}

	// Old key should still be retrievable.
	oldKey, err := kr.KeyByVersion(ctx, "t1", ver1)
	if err != nil {
		t.Fatal(err)
	}
	if string(oldKey) != string(key1) {
		t.Error("old key mismatch")
	}

	// Current should return new key.
	curKey, curVer, err := kr.CurrentKey(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if curVer != ver2 {
		t.Fatalf("current version = %d, want %d", curVer, ver2)
	}
	if string(curKey) != string(key2) {
		t.Error("current key mismatch")
	}
}

func TestKeyRingVersionLookup(t *testing.T) {
	masterKey := []byte("deterministic-master-key-for-test")
	kr := NewLocalKeyRing(masterKey)
	ctx := context.Background()

	// Create v1 by calling CurrentKey.
	_, _, err := kr.CurrentKey(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}

	// Rotate to v2.
	_, _, err = kr.Rotate(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}

	// Lookup v1 by version.
	k1, err := kr.KeyByVersion(ctx, "t", 1)
	if err != nil {
		t.Fatal(err)
	}

	// Lookup v2 by version.
	k2, err := kr.KeyByVersion(ctx, "t", 2)
	if err != nil {
		t.Fatal(err)
	}

	if string(k1) == string(k2) {
		t.Error("v1 and v2 keys should differ")
	}
}
