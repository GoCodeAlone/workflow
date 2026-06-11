package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

const githubAPIBase = "https://api.github.com"

// GitHubSecretScope selects which GitHub secret namespace a provider
// writes to. Default zero value = repo (backwards-compat).
//
//	GitHubScopeRepo → /repos/{owner}/{repo}/actions/secrets/...
//	GitHubScopeEnv  → /repos/{owner}/{repo}/environments/{env}/secrets/...
//	GitHubScopeOrg  → /orgs/{org}/actions/secrets/...
type GitHubSecretScope string

const (
	GitHubScopeRepo GitHubSecretScope = "repo"
	GitHubScopeEnv  GitHubSecretScope = "env"
	GitHubScopeOrg  GitHubSecretScope = "org"
)

// GitHubOrgVisibility controls who can pull an org-scoped secret. Mirrors
// GitHub's API field; one of "all", "selected", "private".
type GitHubOrgVisibility string

const (
	OrgVisibilityAll      GitHubOrgVisibility = "all"
	OrgVisibilitySelected GitHubOrgVisibility = "selected"
	OrgVisibilityPrivate  GitHubOrgVisibility = "private"
)

// GitHubSecretsProvider manages GitHub Actions secrets at repo, env, or
// org scope. Secrets are write-only on GitHub, so Get() returns
// ErrUnsupported.
type GitHubSecretsProvider struct {
	scope           GitHubSecretScope
	owner           string // for repo/env scope
	repo            string // for repo/env scope
	env             string // for env scope
	org             string // for org scope
	orgVisibility   GitHubOrgVisibility
	selectedRepoIDs []int64 // required iff scope=org && visibility=selected
	token           string
	client          *http.Client
	baseURL         string // overridden in tests to point at an httptest.Server
}

// base returns the API base URL, using baseURL when set (for tests).
func (p *GitHubSecretsProvider) base() string {
	if p.baseURL != "" {
		return p.baseURL
	}
	return githubAPIBase
}

// NewGitHubSecretsProvider creates a repo-scoped provider for the given
// "owner/repo". tokenEnvVar is the name of the environment variable
// holding the GitHub token. Backwards-compatible — sets scope=repo.
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
		scope:  GitHubScopeRepo,
		owner:  parts[0],
		repo:   parts[1],
		token:  token,
		client: &http.Client{},
	}, nil
}

// NewGitHubOrgSecretsProvider creates an org-scoped provider. visibility
// is one of OrgVisibilityAll / Selected / Private. selectedRepoIDs is
// required iff visibility=Selected.
//
// Requires the token to have admin:org scope.
func NewGitHubOrgSecretsProvider(org string, tokenEnvVar string, visibility GitHubOrgVisibility, selectedRepoIDs []int64) (*GitHubSecretsProvider, error) {
	if org == "" {
		return nil, fmt.Errorf("secrets: github org name is required")
	}
	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return nil, fmt.Errorf("secrets: env var %q is empty or unset", tokenEnvVar)
	}
	if visibility == "" {
		visibility = OrgVisibilityAll
	}
	switch visibility {
	case OrgVisibilityAll, OrgVisibilitySelected, OrgVisibilityPrivate:
	default:
		return nil, fmt.Errorf("secrets: github org visibility must be all|selected|private, got %q", visibility)
	}
	if visibility == OrgVisibilitySelected && len(selectedRepoIDs) == 0 {
		return nil, fmt.Errorf("secrets: github org visibility=selected requires selected_repository_ids")
	}
	return &GitHubSecretsProvider{
		scope:           GitHubScopeOrg,
		org:             org,
		orgVisibility:   visibility,
		selectedRepoIDs: append([]int64(nil), selectedRepoIDs...),
		token:           token,
		client:          &http.Client{},
	}, nil
}

// Scope reports the current scope.
func (p *GitHubSecretsProvider) Scope() GitHubSecretScope { return p.scope }

func (p *GitHubSecretsProvider) Name() string { return "github" }

// SecretTarget describes the GitHub Actions secret namespace represented by
// this provider: repository, environment, or organization.
func (p *GitHubSecretsProvider) SecretTarget() ProviderTarget {
	switch p.scope {
	case GitHubScopeOrg:
		return ProviderTarget{
			Provider: "github",
			Scope:    string(GitHubScopeOrg),
			Subject:  p.org,
			Label:    "github org " + p.org,
		}
	case GitHubScopeEnv:
		subject := fmt.Sprintf("%s on %s/%s", p.env, p.owner, p.repo)
		return ProviderTarget{
			Provider: "github",
			Scope:    string(GitHubScopeEnv),
			Subject:  subject,
			Label:    "github env " + subject,
		}
	default:
		subject := p.owner + "/" + p.repo
		return ProviderTarget{
			Provider: "github",
			Scope:    string(GitHubScopeRepo),
			Subject:  subject,
			Label:    "github repo " + subject,
		}
	}
}

// SetEnvironment scopes subsequent operations to a GitHub Actions environment.
// Empty scope means repository-level secrets. Calling SetEnvironment with a
// non-empty value flips scope to env.
func (p *GitHubSecretsProvider) SetEnvironment(environment string) {
	p.env = strings.TrimSpace(environment)
	if p.env != "" {
		p.scope = GitHubScopeEnv
	} else if p.scope == GitHubScopeEnv {
		p.scope = GitHubScopeRepo
	}
}

// Environment returns the configured GitHub Actions environment scope.
func (p *GitHubSecretsProvider) Environment() string {
	return p.env
}

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

	payload := map[string]any{
		"encrypted_value": encrypted,
		"key_id":          pubKeyID,
	}
	if p.scope == GitHubScopeOrg {
		payload["visibility"] = string(p.orgVisibility)
		if p.orgVisibility == OrgVisibilitySelected {
			payload["selected_repository_ids"] = p.selectedRepoIDs
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.secretURL(key), bytes.NewReader(body))
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
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.secretURL(key), nil)
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

// ghSecretEntry is the JSON shape returned by GitHub's list-secrets endpoints.
type ghSecretEntry struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// listSecretEntries fetches and decodes all secret entries (name + timestamps).
func (p *GitHubSecretsProvider) listSecretEntries(ctx context.Context) ([]ghSecretEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.secretsURL(), nil)
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
		Secrets []ghSecretEntry `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("secrets: github list decode: %w", err)
	}
	return result.Secrets, nil
}

// List returns the names of all GitHub Actions secrets for the repo.
func (p *GitHubSecretsProvider) List(ctx context.Context) ([]string, error) {
	entries, err := p.listSecretEntries(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, s := range entries {
		names[i] = s.Name
	}
	return names, nil
}

// StatAll implements MetadataProvider. It returns presence + timestamp for every
// secret visible to the configured token. UpdatedAt is the updated_at field from
// GitHub, falling back to created_at when updated_at is zero.
func (p *GitHubSecretsProvider) StatAll(ctx context.Context) ([]SecretMeta, error) {
	entries, err := p.listSecretEntries(ctx)
	if err != nil {
		return nil, err
	}
	metas := make([]SecretMeta, len(entries))
	for i, e := range entries {
		ts := e.UpdatedAt
		if ts.IsZero() {
			ts = e.CreatedAt
		}
		metas[i] = SecretMeta{
			Name:      e.Name,
			Exists:    true,
			UpdatedAt: ts,
		}
	}
	return metas, nil
}

// CheckAccess implements AccessChecker. It verifies the configured credentials
// have at least read access by fetching the public key. Errors never contain
// credential material.
func (p *GitHubSecretsProvider) CheckAccess(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.publicKeyURL(), nil)
	if err != nil {
		return fmt.Errorf("github store access: request build: %w", err)
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("github store access: %w (creds redacted)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github store access: HTTP %d (creds redacted)", resp.StatusCode)
	}
	return nil
}

// ListEnvironments returns the GitHub Actions environments defined for the
// configured repository.
func (p *GitHubSecretsProvider) ListEnvironments(ctx context.Context) ([]ProviderEnvironment, error) {
	if err := p.requireRepoEnvironmentTarget(); err != nil {
		return nil, err
	}
	nextURL := p.environmentsURL()
	var envs []ProviderEnvironment
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		p.setHeaders(req)
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("secrets: github list environments: HTTP %d%s", resp.StatusCode, readErrorBody(resp))
			resp.Body.Close()
			return nil, err
		}
		var result struct {
			Environments []struct {
				Name string `json:"name"`
			} `json:"environments"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("secrets: github list environments decode: %w", err)
		}
		resp.Body.Close()
		for _, env := range result.Environments {
			name := strings.TrimSpace(env.Name)
			if name == "" {
				continue
			}
			envs = append(envs, p.githubEnvironment(name, true, "github-api"))
		}
		nextURL = githubNextLink(resp.Header.Get("Link"))
	}
	return envs, nil
}

// ValidateEnvironment verifies that a GitHub Actions environment exists for the
// configured repository.
func (p *GitHubSecretsProvider) ValidateEnvironment(ctx context.Context, name string) (ProviderEnvironment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProviderEnvironment{}, fmt.Errorf("%w: github environment name is required", ErrInvalidKey)
	}
	if err := p.requireRepoEnvironmentTarget(); err != nil {
		return ProviderEnvironment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.environmentURL(name), nil)
	if err != nil {
		return ProviderEnvironment{}, err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return ProviderEnvironment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ProviderEnvironment{}, fmt.Errorf("%w: github environment %s", ErrNotFound, name)
	}
	if resp.StatusCode != http.StatusOK {
		return ProviderEnvironment{}, fmt.Errorf("secrets: github validate environment %q: HTTP %d%s", name, resp.StatusCode, readErrorBody(resp))
	}
	var result struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil && !errors.Is(err, io.EOF) {
		return ProviderEnvironment{}, fmt.Errorf("secrets: github validate environment decode: %w", err)
	}
	if strings.TrimSpace(result.Name) != "" {
		name = strings.TrimSpace(result.Name)
	}
	return p.githubEnvironment(name, true, "github-api"), nil
}

// EnsureEnvironment creates the GitHub Actions environment when it is missing.
func (p *GitHubSecretsProvider) EnsureEnvironment(ctx context.Context, name string) (ProviderEnvironment, error) {
	env, err := p.ValidateEnvironment(ctx, name)
	if err == nil {
		return env, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ProviderEnvironment{}, err
	}
	name = strings.TrimSpace(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.environmentURL(name), bytes.NewReader([]byte("{}")))
	if err != nil {
		return ProviderEnvironment{}, err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return ProviderEnvironment{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return p.githubEnvironment(name, true, "github-api"), nil
	default:
		return ProviderEnvironment{}, fmt.Errorf("secrets: github create environment %q: HTTP %d%s", name, resp.StatusCode, readErrorBody(resp))
	}
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

func (p *GitHubSecretsProvider) secretsURL() string {
	switch p.scope {
	case GitHubScopeOrg:
		return fmt.Sprintf("%s/orgs/%s/actions/secrets", p.base(), p.org)
	case GitHubScopeEnv:
		return fmt.Sprintf("%s/repos/%s/%s/environments/%s/secrets", p.base(), p.owner, p.repo, url.PathEscape(p.env))
	default: // GitHubScopeRepo
		return fmt.Sprintf("%s/repos/%s/%s/actions/secrets", p.base(), p.owner, p.repo)
	}
}

func (p *GitHubSecretsProvider) environmentsURL() string {
	values := url.Values{}
	values.Set("per_page", "100")
	return fmt.Sprintf("%s/repos/%s/%s/environments?%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), values.Encode())
}

func (p *GitHubSecretsProvider) environmentURL(name string) string {
	return fmt.Sprintf("%s/repos/%s/%s/environments/%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), url.PathEscape(name))
}

func (p *GitHubSecretsProvider) secretURL(key string) string {
	return p.secretsURL() + "/" + url.PathEscape(key)
}

func (p *GitHubSecretsProvider) publicKeyURL() string {
	return p.secretsURL() + "/public-key"
}

func (p *GitHubSecretsProvider) requireRepoEnvironmentTarget() error {
	if strings.TrimSpace(p.owner) == "" || strings.TrimSpace(p.repo) == "" {
		return fmt.Errorf("%w: github environments require a repository target", ErrUnsupported)
	}
	return nil
}

func (p *GitHubSecretsProvider) githubEnvironment(name string, exists bool, source string) ProviderEnvironment {
	subject := fmt.Sprintf("%s on %s/%s", name, p.owner, p.repo)
	return ProviderEnvironment{
		Provider: "github",
		Name:     name,
		Label:    "github env " + subject,
		Exists:   exists,
		Source:   source,
	}
}

func githubNextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}

type repoPublicKeyResponse struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

func (p *GitHubSecretsProvider) repoPublicKey(ctx context.Context) (keyID, keyBase64 string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.publicKeyURL(), nil)
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
