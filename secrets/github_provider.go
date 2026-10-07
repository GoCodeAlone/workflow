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
	safeErrors      bool   // direct-token providers omit upstream bodies and errors
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
		visibility = OrgVisibilityPrivate
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

// NewGitHubSecretsProviderWithToken creates a repository provider from an
// in-memory credential. It does not read environment variables. Requests are
// bounded and redirects are refused. Stat, Identity, SetWithReceipt, List, and
// StatAll omit upstream bodies and causes. Legacy Set retains upsert semantics.
func NewGitHubSecretsProviderWithToken(repo, token string) (*GitHubSecretsProvider, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !validGitHubTargetName(parts[0]) || !validGitHubTargetName(parts[1]) {
		return nil, errors.New("secrets: github repo must be owner/repo")
	}
	if !validGitHubToken(token) {
		return nil, errors.New("secrets: github token is empty or invalid")
	}
	return &GitHubSecretsProvider{
		scope: GitHubScopeRepo, owner: parts[0], repo: parts[1], token: token,
		client: githubDeliveryClient(nil), safeErrors: true,
	}, nil
}

// NewGitHubOrgSecretsProviderWithToken creates an organization provider from an
// in-memory credential. Repository IDs are copied and must be unique positive
// IDs used only with selected visibility. Empty visibility defaults to private.
func NewGitHubOrgSecretsProviderWithToken(org, token string, visibility GitHubOrgVisibility, selectedRepoIDs []int64) (*GitHubSecretsProvider, error) {
	if !validGitHubTargetName(org) {
		return nil, errors.New("secrets: github organization name is invalid")
	}
	if !validGitHubToken(token) {
		return nil, errors.New("secrets: github token is empty or invalid")
	}
	if visibility == "" {
		visibility = OrgVisibilityPrivate
	}
	if err := validateGitHubSelection(visibility, selectedRepoIDs); err != nil {
		return nil, err
	}
	return &GitHubSecretsProvider{
		scope: GitHubScopeOrg, org: org, token: token, orgVisibility: visibility,
		selectedRepoIDs: append([]int64(nil), selectedRepoIDs...),
		client:          githubDeliveryClient(nil), safeErrors: true,
	}, nil
}

func validGitHubTargetName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func validGitHubToken(token string) bool {
	if token == "" {
		return false
	}
	for _, c := range token {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func validateGitHubSelection(visibility GitHubOrgVisibility, ids []int64) error {
	switch visibility {
	case OrgVisibilityAll, OrgVisibilityPrivate:
		if len(ids) != 0 {
			return errors.New("secrets: github selected repository IDs require selected visibility")
		}
	case OrgVisibilitySelected:
		if len(ids) == 0 {
			return errors.New("secrets: github selected visibility requires repository IDs")
		}
		seen := make(map[int64]bool, len(ids))
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return errors.New("secrets: github selected repository IDs must be unique positive IDs")
			}
			seen[id] = true
		}
	default:
		return errors.New("secrets: github organization visibility must be all, selected, or private")
	}
	return nil
}

// githubDeliveryClient copies the caller's client so safe methods cannot inherit
// an unbounded timeout or a redirect policy that forwards authorization.
func githubDeliveryClient(client *http.Client) *http.Client {
	bounded := &http.Client{}
	if client != nil {
		*bounded = *client
	}
	if bounded.Timeout <= 0 || bounded.Timeout > 30*time.Second {
		bounded.Timeout = 30 * time.Second
	}
	bounded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return bounded
}

// GitHubSecretIdentity describes immutable IDs for the configured namespace.
// Organization selection describes the intended write policy, not a remote
// secret's current visibility. Names alone do not identify a namespace forever.
type GitHubSecretIdentity struct {
	Scope                 GitHubSecretScope   `json:"scope"`
	RepositoryID          int64               `json:"repository_id,omitempty"`
	OwnerID               int64               `json:"owner_id,omitempty"`
	OrganizationID        int64               `json:"organization_id,omitempty"`
	EnvironmentID         int64               `json:"environment_id,omitempty"`
	Visibility            GitHubOrgVisibility `json:"visibility,omitempty"`
	SelectedRepositoryIDs []int64             `json:"selected_repository_ids,omitempty"`
}

// GitHubSecretWriteReceipt reports what a PUT response establishes. Created
// means GitHub returned 201, not an atomic create-only guarantee. MayHaveWritten
// also covers successful updates and ambiguous transport/server failures.
type GitHubSecretWriteReceipt struct {
	Created        bool `json:"created"`
	MayHaveWritten bool `json:"may_have_written"`
}

func (p *GitHubSecretsProvider) validateDeliveryTarget() error {
	if p.scope == GitHubScopeOrg {
		if !validGitHubTargetName(p.org) {
			return errors.New("secrets: github organization target is invalid")
		}
		return validateGitHubSelection(p.orgVisibility, p.selectedRepoIDs)
	}
	if p.scope != "" && p.scope != GitHubScopeRepo && p.scope != GitHubScopeEnv {
		return errors.New("secrets: github secret scope is invalid")
	}
	if !validGitHubTargetName(p.owner) || !validGitHubTargetName(p.repo) || p.scope == GitHubScopeEnv && p.env == "" {
		return errors.New("secrets: github repository or environment target is invalid")
	}
	return nil
}

func validGitHubSecretKey(key string) bool {
	// The 256-byte limit is this helper's bound, not an asserted GitHub API limit.
	if key == "" || len(key) > 256 || strings.HasPrefix(strings.ToUpper(key), "GITHUB_") {
		return false
	}
	for i, c := range key {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (p *GitHubSecretsProvider) deliveryRequest(ctx context.Context, method, target string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errors.New("secrets: github request is invalid")
	}
	p.setHeaders(req)
	return p.deliveryDo(ctx, req)
}

func (p *GitHubSecretsProvider) deliveryDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := githubDeliveryClient(p.client).Do(req)
	if err != nil {
		// Transport errors can embed request URLs, headers, or body fragments.
		// Context sentinel errors are safe and retain cancellation semantics.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("secrets: github request failed: %w", ctx.Err())
		}
		return nil, errors.New("secrets: github request failed")
	}
	return resp, nil
}

func decodeGitHubDelivery(body io.Reader, out any) error {
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	if err := decoder.Decode(out); err != nil {
		return errors.New("secrets: github metadata response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("secrets: github metadata response is invalid")
	}
	return nil
}

func (p *GitHubSecretsProvider) deliveryMetadata(ctx context.Context, target string, out any) error {
	resp, err := p.deliveryRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("secrets: github metadata request: HTTP %d", resp.StatusCode)
	}
	return decodeGitHubDelivery(resp.Body, out)
}

// Stat fetches metadata for one exact secret name without reading its value.
// HTTP 404 returns ErrNotFound; authorization and response errors remain errors.
func (p *GitHubSecretsProvider) Stat(ctx context.Context, key string) (SecretMeta, error) {
	meta := SecretMeta{Name: key}
	if !validGitHubSecretKey(key) {
		return meta, ErrInvalidKey
	}
	if err := p.validateDeliveryTarget(); err != nil {
		return meta, err
	}
	var entry ghSecretEntry
	if err := p.deliveryMetadata(ctx, p.secretURL(key), &entry); err != nil {
		return meta, err
	}
	if !strings.EqualFold(entry.Name, key) {
		return meta, errors.New("secrets: github secret metadata name does not match")
	}
	meta.Exists = true
	meta.UpdatedAt = entry.UpdatedAt
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = entry.CreatedAt
	}
	return meta, nil
}

// Identity resolves the immutable repository/owner, organization, and optional
// environment IDs. It does not create environments or change permissions.
func (p *GitHubSecretsProvider) Identity(ctx context.Context) (GitHubSecretIdentity, error) {
	identity := GitHubSecretIdentity{Scope: p.scope}
	if err := p.validateDeliveryTarget(); err != nil {
		return identity, err
	}
	if p.scope == GitHubScopeOrg {
		var org struct {
			ID int64 `json:"id"`
		}
		if err := p.deliveryMetadata(ctx, p.base()+"/orgs/"+url.PathEscape(p.org), &org); err != nil {
			return identity, err
		}
		if org.ID <= 0 {
			return identity, errors.New("secrets: github organization ID is missing or invalid")
		}
		identity.OrganizationID = org.ID
		identity.Visibility = p.orgVisibility
		identity.SelectedRepositoryIDs = append([]int64(nil), p.selectedRepoIDs...)
		return identity, nil
	}
	var repo struct {
		ID    int64 `json:"id"`
		Owner struct {
			ID int64 `json:"id"`
		} `json:"owner"`
	}
	target := p.base() + "/repos/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repo)
	if err := p.deliveryMetadata(ctx, target, &repo); err != nil {
		return identity, err
	}
	if repo.ID <= 0 || repo.Owner.ID <= 0 {
		return identity, errors.New("secrets: github repository or owner ID is missing or invalid")
	}
	identity.Scope = GitHubScopeRepo
	identity.RepositoryID, identity.OwnerID = repo.ID, repo.Owner.ID
	if p.scope == GitHubScopeEnv {
		var env struct {
			ID int64 `json:"id"`
		}
		if err := p.deliveryMetadata(ctx, p.environmentURL(p.env), &env); err != nil {
			return identity, err
		}
		if env.ID <= 0 {
			return identity, errors.New("secrets: github environment ID is missing or invalid")
		}
		identity.Scope, identity.EnvironmentID = GitHubScopeEnv, env.ID
	}
	return identity, nil
}

// SetWithReceipt encrypts and upserts a secret, exposing only a metadata receipt
// and safe errors. It never provides an atomic create-only operation. A caller
// requiring a fresh name must check Stat and control concurrent writers itself.
func (p *GitHubSecretsProvider) SetWithReceipt(ctx context.Context, key, value string) (GitHubSecretWriteReceipt, error) {
	receipt := GitHubSecretWriteReceipt{}
	if !validGitHubSecretKey(key) {
		return receipt, ErrInvalidKey
	}
	if err := p.validateDeliveryTarget(); err != nil {
		return receipt, err
	}
	var publicKey repoPublicKeyResponse
	if err := p.deliveryMetadata(ctx, p.publicKeyURL(), &publicKey); err != nil {
		return receipt, err
	}
	if publicKey.KeyID == "" {
		return receipt, errors.New("secrets: github public key ID is missing")
	}
	encrypted, err := encryptSecret(publicKey.Key, value)
	if err != nil {
		return receipt, errors.New("secrets: github secret encryption failed")
	}
	body, err := json.Marshal(p.secretPayload(publicKey.KeyID, encrypted))
	if err != nil {
		return receipt, errors.New("secrets: github secret payload is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.secretURL(key), bytes.NewReader(body))
	if err != nil {
		return receipt, errors.New("secrets: github secret write request is invalid")
	}
	p.setHeaders(req)
	if ctx.Err() != nil {
		return receipt, fmt.Errorf("secrets: github secret write canceled before upload: %w", ctx.Err())
	}
	resp, err := p.deliveryDo(ctx, req)
	if err != nil {
		receipt.MayHaveWritten = true
		return receipt, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		receipt.Created, receipt.MayHaveWritten = true, true
		return receipt, nil
	case http.StatusNoContent:
		receipt.MayHaveWritten = true
		return receipt, nil
	default:
		// Only explicit client rejections establish that the PUT failed. A
		// timeout, server failure, redirect, or unexpected success is uncertain.
		receipt.MayHaveWritten = resp.StatusCode < 400 || resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout
		return receipt, fmt.Errorf("secrets: github secret write: HTTP %d", resp.StatusCode)
	}
}

func (p *GitHubSecretsProvider) secretPayload(keyID, encrypted string) map[string]any {
	payload := map[string]any{"encrypted_value": encrypted, "key_id": keyID}
	if p.scope == GitHubScopeOrg {
		payload["visibility"] = string(p.orgVisibility)
		if p.orgVisibility == OrgVisibilitySelected {
			payload["selected_repository_ids"] = p.selectedRepoIDs
		}
	}
	return payload
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

	body, _ := json.Marshal(p.secretPayload(pubKeyID, encrypted))
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
	if p.safeErrors {
		if err := p.validateDeliveryTarget(); err != nil {
			return nil, err
		}
	}
	initial, err := url.Parse(p.secretsURL())
	if err != nil {
		return nil, errors.New("secrets: github list URL is invalid")
	}
	query := initial.Query()
	query.Set("per_page", "100")
	initial.RawQuery = query.Encode()
	nextURL := initial.String()
	seen := map[string]bool{}
	var entries []ghSecretEntry
	for nextURL != "" {
		next, err := url.Parse(nextURL)
		if err != nil || next.Scheme != initial.Scheme || next.Host != initial.Host || next.User != nil || next.EscapedPath() != initial.EscapedPath() || next.Fragment != "" {
			return nil, errors.New("secrets: github secret pagination escaped the configured namespace")
		}
		// Canonical query ordering also catches cycles with reordered parameters.
		next.RawQuery = next.Query().Encode()
		canonical := next.String()
		if seen[canonical] || len(seen) >= 10000 {
			return nil, errors.New("secrets: github secret pagination repeated or exceeded the page limit")
		}
		seen[canonical] = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, canonical, nil)
		if err != nil {
			return nil, errors.New("secrets: github list request is invalid")
		}
		p.setHeaders(req)
		resp, err := githubDeliveryClient(p.client).Do(req)
		if err != nil {
			if p.safeErrors {
				return nil, errors.New("secrets: github list request failed")
			}
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			detail := ""
			if !p.safeErrors {
				detail = readErrorBody(resp)
			}
			resp.Body.Close()
			return nil, fmt.Errorf("secrets: github list secrets: HTTP %d%s", resp.StatusCode, detail)
		}
		var result struct {
			Secrets *[]ghSecretEntry `json:"secrets"`
		}
		err = decodeGitHubDelivery(resp.Body, &result)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if result.Secrets == nil {
			return nil, errors.New("secrets: github secret list is missing")
		}
		entries = append(entries, (*result.Secrets)...)
		nextURL = githubNextLink(resp.Header.Get("Link"))
	}
	return entries, nil
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

type ghVariableEntry struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (p *GitHubSecretsProvider) listVariableEntries(ctx context.Context) ([]ghVariableEntry, error) {
	nextURL := p.variablesURL()
	var entries []ghVariableEntry
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
			err := fmt.Errorf("secrets: github list variables: HTTP %d%s", resp.StatusCode, readErrorBody(resp))
			resp.Body.Close()
			return nil, err
		}
		var result struct {
			Variables []ghVariableEntry `json:"variables"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("secrets: github list variables decode: %w", err)
		}
		resp.Body.Close()
		entries = append(entries, result.Variables...)
		nextURL = githubNextLink(resp.Header.Get("Link"))
	}
	return entries, nil
}

// ListVariables returns metadata for GitHub Actions variables in the configured
// repo, environment, or organization scope. Returned values are redacted.
func (p *GitHubSecretsProvider) ListVariables(ctx context.Context) ([]VariableMeta, error) {
	entries, err := p.listVariableEntries(ctx)
	if err != nil {
		return nil, err
	}
	metas := make([]VariableMeta, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		ts := entry.UpdatedAt
		if ts.IsZero() {
			ts = entry.CreatedAt
		}
		metas = append(metas, VariableMeta{
			Name:      name,
			Exists:    true,
			UpdatedAt: ts,
		})
	}
	return metas, nil
}

// CheckVariable reports whether a GitHub Actions variable exists without
// returning its value.
func (p *GitHubSecretsProvider) CheckVariable(ctx context.Context, key string) (VariableMeta, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return VariableMeta{}, ErrInvalidKey
	}
	entries, err := p.listVariableEntries(ctx)
	if err != nil {
		return VariableMeta{}, err
	}
	for _, entry := range entries {
		if entry.Name != key {
			continue
		}
		ts := entry.UpdatedAt
		if ts.IsZero() {
			ts = entry.CreatedAt
		}
		return VariableMeta{Name: key, Exists: true, UpdatedAt: ts}, nil
	}
	return VariableMeta{Name: key, Exists: false}, nil
}

// SetVariable creates or updates a GitHub Actions variable in the configured
// repo, environment, or organization scope.
func (p *GitHubSecretsProvider) SetVariable(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrInvalidKey
	}
	meta, err := p.CheckVariable(ctx, key)
	if err != nil {
		return err
	}
	method := http.MethodPost
	targetURL := p.variablesURL()
	payload := map[string]any{
		"name":  key,
		"value": value,
	}
	if meta.Exists {
		method = http.MethodPatch
		targetURL = p.variableURL(key)
		delete(payload, "name")
	}
	if p.scope == GitHubScopeOrg {
		payload["visibility"] = string(p.orgVisibility)
		if p.orgVisibility == OrgVisibilitySelected {
			payload["selected_repository_ids"] = p.selectedRepoIDs
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusOK:
		return nil
	default:
		return fmt.Errorf("secrets: github set variable %q: HTTP %d%s", key, resp.StatusCode, readErrorBody(resp))
	}
}

// DeleteVariable removes a GitHub Actions variable.
func (p *GitHubSecretsProvider) DeleteVariable(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrInvalidKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.variableURL(key), nil)
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
		return fmt.Errorf("secrets: github delete variable %q: HTTP %d%s", key, resp.StatusCode, readErrorBody(resp))
	}
	return nil
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

func (p *GitHubSecretsProvider) variablesURL() string {
	values := url.Values{}
	values.Set("per_page", "100")
	switch p.scope {
	case GitHubScopeOrg:
		return fmt.Sprintf("%s/orgs/%s/actions/variables?%s", p.base(), url.PathEscape(p.org), values.Encode())
	case GitHubScopeEnv:
		return fmt.Sprintf("%s/repos/%s/%s/environments/%s/variables?%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), url.PathEscape(p.env), values.Encode())
	default:
		return fmt.Sprintf("%s/repos/%s/%s/actions/variables?%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), values.Encode())
	}
}

func (p *GitHubSecretsProvider) variableURL(key string) string {
	switch p.scope {
	case GitHubScopeOrg:
		return fmt.Sprintf("%s/orgs/%s/actions/variables/%s", p.base(), url.PathEscape(p.org), url.PathEscape(key))
	case GitHubScopeEnv:
		return fmt.Sprintf("%s/repos/%s/%s/environments/%s/variables/%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), url.PathEscape(p.env), url.PathEscape(key))
	default:
		return fmt.Sprintf("%s/repos/%s/%s/actions/variables/%s", p.base(), url.PathEscape(p.owner), url.PathEscape(p.repo), url.PathEscape(key))
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
