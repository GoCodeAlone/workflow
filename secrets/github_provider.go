package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

const githubAPIBase = "https://api.github.com"

// GitHubSecretsProvider manages GitHub Actions repository secrets.
// Secrets are write-only on GitHub, so Get() returns ErrUnsupported.
type GitHubSecretsProvider struct {
	owner  string
	repo   string
	token  string
	client *http.Client
}

// NewGitHubSecretsProvider creates a provider for the given "owner/repo".
// tokenEnvVar is the name of the environment variable holding the GitHub token.
func NewGitHubSecretsProvider(repo string, tokenEnvVar string) (*GitHubSecretsProvider, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("secrets: github repo must be 'owner/repo', got %q", repo)
	}
	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return nil, fmt.Errorf("secrets: env var %q is empty or unset", tokenEnvVar)
	}
	return &GitHubSecretsProvider{
		owner:  parts[0],
		repo:   parts[1],
		token:  token,
		client: &http.Client{},
	}, nil
}

func (p *GitHubSecretsProvider) Name() string { return "github" }

// Get always returns ErrUnsupported because GitHub secrets are write-only.
func (p *GitHubSecretsProvider) Get(_ context.Context, _ string) (string, error) {
	return "", ErrUnsupported
}

// Set encrypts value with the repo's public key and stores it as a secret.
func (p *GitHubSecretsProvider) Set(ctx context.Context, key, value string) error {
	if key == "" {
		return ErrInvalidKey
	}
	pubKeyID, pubKeyB64, err := p.repoPublicKey(ctx)
	if err != nil {
		return fmt.Errorf("secrets: github get public key: %w", err)
	}
	encrypted, err := encryptSecret(pubKeyB64, value)
	if err != nil {
		return fmt.Errorf("secrets: github encrypt: %w", err)
	}

	payload := map[string]string{
		"encrypted_value": encrypted,
		"key_id":          pubKeyID,
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/repos/%s/%s/actions/secrets/%s", githubAPIBase, p.owner, p.repo, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("secrets: github set secret %q: HTTP %d%s", key, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// Delete removes a GitHub Actions secret.
func (p *GitHubSecretsProvider) Delete(ctx context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	url := fmt.Sprintf("%s/repos/%s/%s/actions/secrets/%s", githubAPIBase, p.owner, p.repo, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("secrets: github delete secret %q: HTTP %d%s", key, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// List returns the names of all GitHub Actions secrets for the repo.
func (p *GitHubSecretsProvider) List(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/secrets", githubAPIBase, p.owner, p.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("secrets: github list secrets: HTTP %d%s", resp.StatusCode, readErrorBody(resp))
	}
	var result struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("secrets: github list decode: %w", err)
	}
	names := make([]string, len(result.Secrets))
	for i, s := range result.Secrets {
		names[i] = s.Name
	}
	return names, nil
}

// readErrorBody reads up to 512 bytes from resp.Body and returns them as a
// trimmed string prefixed with ": " for appending to an error message.
// Returns "" when the body is empty, so callers don't emit a trailing ": ".
// resp.Body must not yet be closed; the caller is responsible for closing it.
func readErrorBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	return ": " + s
}

func (p *GitHubSecretsProvider) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

type repoPublicKeyResponse struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

func (p *GitHubSecretsProvider) repoPublicKey(ctx context.Context) (keyID, keyBase64 string, err error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/secrets/public-key", githubAPIBase, p.owner, p.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d%s", resp.StatusCode, readErrorBody(resp))
	}
	var pk repoPublicKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&pk); err != nil {
		return "", "", err
	}
	return pk.KeyID, pk.Key, nil
}

// encryptSecret implements libsodium's crypto_box_seal using NaCl box.
// This matches what GitHub expects per their docs.
// Format: ephemeral_pubkey (32 bytes) || box.Seal output
// Nonce = BLAKE2b(ephemeral_pubkey || recipient_pubkey)[:24]
func encryptSecret(pubKeyBase64, plaintext string) (string, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyBase64)
	if err != nil {
		return "", fmt.Errorf("decode public key: %w", err)
	}
	if len(pubKeyBytes) != 32 {
		return "", fmt.Errorf("public key must be 32 bytes, got %d", len(pubKeyBytes))
	}
	var recipientKey [32]byte
	copy(recipientKey[:], pubKeyBytes)

	// Generate ephemeral sender key pair.
	senderPub, senderPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Derive 24-byte nonce = BLAKE2b-192(senderPub || recipientKey).
	// Must use a native 24-byte BLAKE2b output to match libsodium's
	// crypto_box_seal: BLAKE2b parameterises the output size into the hash
	// itself, so blake2b(x, 24) != blake2b(x, 32)[:24]. GitHub rejects
	// the encrypted value ("improperly encrypted secret") if we truncate
	// a 32-byte digest instead of hashing to 24 bytes directly.
	h, err := blake2b.New(24, nil)
	if err != nil {
		return "", fmt.Errorf("blake2b init: %w", err)
	}
	h.Write(senderPub[:])
	h.Write(recipientKey[:])
	var nonce [24]byte
	copy(nonce[:], h.Sum(nil))

	// Encrypt and prepend the ephemeral public key.
	ciphertext := box.Seal(senderPub[:], []byte(plaintext), &nonce, &recipientKey, senderPriv)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
