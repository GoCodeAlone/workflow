package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
)

const (
	pemTypePublicKey  = "ED25519 PUBLIC KEY"
	pemTypePrivateKey = "ED25519 PRIVATE KEY"
)

// GenerateKeyPair generates a new Ed25519 key pair using crypto/rand.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// MarshalPublicKeyPEM PEM-encodes an Ed25519 public key with type "ED25519 PUBLIC KEY".
func MarshalPublicKeyPEM(pub ed25519.PublicKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePublicKey,
		Bytes: []byte(pub),
	})
}

// UnmarshalPublicKeyPEM decodes a PEM-encoded Ed25519 public key.
func UnmarshalPublicKeyPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}
	if block.Type != pemTypePublicKey {
		return nil, fmt.Errorf("unexpected PEM type: %q", block.Type)
	}
	if len(block.Bytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d, want %d", len(block.Bytes), ed25519.PublicKeySize)
	}
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	copy(key, block.Bytes)
	return key, nil
}

// MarshalPrivateKeyPEM PEM-encodes an Ed25519 private key with type "ED25519 PRIVATE KEY".
func MarshalPrivateKeyPEM(priv ed25519.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePrivateKey,
		Bytes: []byte(priv),
	})
}

// UnmarshalPrivateKeyPEM decodes a PEM-encoded Ed25519 private key.
func UnmarshalPrivateKeyPEM(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}
	if block.Type != pemTypePrivateKey {
		return nil, fmt.Errorf("unexpected PEM type: %q", block.Type)
	}
	if len(block.Bytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: got %d, want %d", len(block.Bytes), ed25519.PrivateKeySize)
	}
	key := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(key, block.Bytes)
	return key, nil
}
