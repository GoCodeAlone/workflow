package iam

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- AWS Provider Tests ---

func TestAWSProvider_Type(t *testing.T) {
	p := &AWSIAMProvider{}
	if p.Type() != store.IAMProviderAWS {
		t.Errorf("expected %s, got %s", store.IAMProviderAWS, p.Type())
	}
}

func TestAWSProvider_ValidateConfig_Valid(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"account_id":"123456789012","region":"us-east-1"}`)
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestAWSProvider_ValidateConfig_MissingAccountID(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"region":"us-east-1"}`)
	if err := p.ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for missing account_id")
	}
}

func TestAWSProvider_ValidateConfig_InvalidJSON(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{invalid}`)
	if err := p.ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAWSProvider_ResolveIdentities_ValidARN(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"account_id":"123456789012"}`)
	creds := map[string]string{"arn": "arn:aws:iam::123456789012:role/MyRole"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Provider != string(store.IAMProviderAWS) {
		t.Errorf("expected provider %s, got %s", store.IAMProviderAWS, ids[0].Provider)
	}
	if ids[0].Identifier != "arn:aws:iam::123456789012:role/MyRole" {
		t.Errorf("unexpected identifier: %s", ids[0].Identifier)
	}
}

func TestAWSProvider_ResolveIdentities_MissingARN(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"account_id":"123456789012"}`)

	_, err := p.ResolveIdentities(context.Background(), cfg, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing ARN")
	}
}

func TestAWSProvider_ResolveIdentities_InvalidARN(t *testing.T) {
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"account_id":"123456789012"}`)
	creds := map[string]string{"arn": "not-an-arn"}

	_, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err == nil {
		t.Fatal("expected error for invalid ARN format")
	}
}

func TestAWSProvider_TestConnection(t *testing.T) {
	t.Skip("requires real AWS credentials")
	p := &AWSIAMProvider{}
	cfg := json.RawMessage(`{"account_id":"123456789012","region":"us-east-1"}`)
	if err := p.TestConnection(context.Background(), cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- Kubernetes Provider Tests ---

func TestKubernetesProvider_Type(t *testing.T) {
	p := &KubernetesProvider{}
	if p.Type() != store.IAMProviderKubernetes {
		t.Errorf("expected %s, got %s", store.IAMProviderKubernetes, p.Type())
	}
}

func TestKubernetesProvider_ValidateConfig_Valid(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"cluster_name":"prod","namespace":"default"}`)
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestKubernetesProvider_ValidateConfig_MissingClusterName(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"namespace":"default"}`)
	if err := p.ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for missing cluster_name")
	}
}

func TestKubernetesProvider_ResolveIdentities_ServiceAccount(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"cluster_name":"prod"}`)
	creds := map[string]string{"service_account": "my-svc"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Identifier != "sa:my-svc" {
		t.Errorf("expected identifier 'sa:my-svc', got %s", ids[0].Identifier)
	}
}

func TestKubernetesProvider_ResolveIdentities_Group(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"cluster_name":"prod"}`)
	creds := map[string]string{"group": "devs"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Identifier != "group:devs" {
		t.Errorf("expected identifier 'group:devs', got %s", ids[0].Identifier)
	}
}

func TestKubernetesProvider_ResolveIdentities_Both(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"cluster_name":"prod"}`)
	creds := map[string]string{"service_account": "my-svc", "group": "devs"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 identities, got %d", len(ids))
	}
}

func TestKubernetesProvider_ResolveIdentities_Neither(t *testing.T) {
	p := &KubernetesProvider{}
	cfg := json.RawMessage(`{"cluster_name":"prod"}`)

	_, err := p.ResolveIdentities(context.Background(), cfg, map[string]string{})
	if err == nil {
		t.Fatal("expected error when neither service_account nor group provided")
	}
}

// --- OIDC Provider Tests ---

func TestOIDCProvider_Type(t *testing.T) {
	p := &OIDCProvider{}
	if p.Type() != store.IAMProviderOIDC {
		t.Errorf("expected %s, got %s", store.IAMProviderOIDC, p.Type())
	}
}

func TestOIDCProvider_ValidateConfig_Valid(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":"https://example.com","client_id":"my-client"}`)
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestOIDCProvider_ValidateConfig_MissingIssuer(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"client_id":"my-client"}`)
	if err := p.ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for missing issuer")
	}
}

func TestOIDCProvider_ValidateConfig_MissingClientID(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":"https://example.com"}`)
	if err := p.ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

func TestOIDCProvider_ResolveIdentities_DefaultClaim(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":"https://example.com","client_id":"my-client"}`)
	creds := map[string]string{"sub": "user-123"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Identifier != "user-123" {
		t.Errorf("expected identifier 'user-123', got %s", ids[0].Identifier)
	}
}

func TestOIDCProvider_ResolveIdentities_CustomClaim(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":"https://example.com","client_id":"my-client","claim_key":"email"}`)
	creds := map[string]string{"email": "user@example.com"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ids[0].Identifier != "user@example.com" {
		t.Errorf("expected identifier 'user@example.com', got %s", ids[0].Identifier)
	}
}

func TestOIDCProvider_ResolveIdentities_MissingClaim(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":"https://example.com","client_id":"my-client","claim_key":"email"}`)
	creds := map[string]string{"sub": "user-123"} // no email

	_, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err == nil {
		t.Fatal("expected error for missing claim")
	}
}

// --- IAMResolver Tests ---

// mockIAMStore for resolver tests
type mockIAMStore struct {
	providers []*store.IAMProviderConfig
	roles     map[string]store.Role // key: providerID+"|"+externalID
}

func newMockIAMStore() *mockIAMStore {
	return &mockIAMStore{
		roles: make(map[string]store.Role),
	}
}

func (s *mockIAMStore) CreateProvider(_ context.Context, p *store.IAMProviderConfig) error {
	s.providers = append(s.providers, p)
	return nil
}

func (s *mockIAMStore) GetProvider(_ context.Context, id uuid.UUID) (*store.IAMProviderConfig, error) {
	for _, p := range s.providers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mockIAMStore) UpdateProvider(_ context.Context, _ *store.IAMProviderConfig) error {
	return nil
}

func (s *mockIAMStore) DeleteProvider(_ context.Context, _ uuid.UUID) error { return nil }

func (s *mockIAMStore) ListProviders(_ context.Context, f store.IAMProviderFilter) ([]*store.IAMProviderConfig, error) {
	var out []*store.IAMProviderConfig
	for _, p := range s.providers {
		if f.CompanyID != nil && p.CompanyID != *f.CompanyID {
			continue
		}
		if f.Enabled != nil && p.Enabled != *f.Enabled {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *mockIAMStore) CreateMapping(_ context.Context, _ *store.IAMRoleMapping) error { return nil }

func (s *mockIAMStore) GetMapping(_ context.Context, _ uuid.UUID) (*store.IAMRoleMapping, error) {
	return nil, store.ErrNotFound
}

func (s *mockIAMStore) DeleteMapping(_ context.Context, _ uuid.UUID) error { return nil }

func (s *mockIAMStore) ListMappings(_ context.Context, _ store.IAMRoleMappingFilter) ([]*store.IAMRoleMapping, error) {
	return nil, nil
}

func (s *mockIAMStore) ResolveRole(_ context.Context, providerID uuid.UUID, externalID string, _ string, _ uuid.UUID) (store.Role, error) {
	key := providerID.String() + "|" + externalID
	role, ok := s.roles[key]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func TestIAMResolver_RegisterProvider(t *testing.T) {
	is := newMockIAMStore()
	resolver := NewIAMResolver(is)

	resolver.RegisterProvider(&AWSIAMProvider{})
	p, ok := resolver.GetProvider(store.IAMProviderAWS)
	if !ok {
		t.Fatal("expected provider to be registered")
	}
	if p.Type() != store.IAMProviderAWS {
		t.Errorf("expected %s, got %s", store.IAMProviderAWS, p.Type())
	}
}

func TestIAMResolver_GetProvider_Found(t *testing.T) {
	is := newMockIAMStore()
	resolver := NewIAMResolver(is)
	resolver.RegisterProvider(&KubernetesProvider{})

	p, ok := resolver.GetProvider(store.IAMProviderKubernetes)
	if !ok || p == nil {
		t.Fatal("expected to find kubernetes provider")
	}
}

func TestIAMResolver_GetProvider_NotFound(t *testing.T) {
	is := newMockIAMStore()
	resolver := NewIAMResolver(is)

	_, ok := resolver.GetProvider(store.IAMProviderSAML)
	if ok {
		t.Fatal("expected not to find unregistered provider")
	}
}

func TestIAMResolver_ResolveRole_SingleProvider(t *testing.T) {
	companyID := uuid.New()
	providerID := uuid.New()
	resourceID := uuid.New()

	is := newMockIAMStore()
	is.providers = append(is.providers, &store.IAMProviderConfig{
		ID:           providerID,
		CompanyID:    companyID,
		ProviderType: store.IAMProviderAWS,
		Enabled:      true,
	})
	is.roles[providerID.String()+"|"+"arn:aws:iam::123:role/Admin"] = store.RoleAdmin

	resolver := NewIAMResolver(is)
	identity := ExternalIdentity{
		Provider:   string(store.IAMProviderAWS),
		Identifier: "arn:aws:iam::123:role/Admin",
	}

	role, err := resolver.ResolveRole(context.Background(), companyID, identity, "project", resourceID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if role != store.RoleAdmin {
		t.Errorf("expected admin, got %s", role)
	}
}

func TestIAMResolver_ResolveRole_MultiProvider_HighestWins(t *testing.T) {
	companyID := uuid.New()
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	resourceID := uuid.New()

	is := newMockIAMStore()
	is.providers = append(is.providers,
		&store.IAMProviderConfig{
			ID:           provider1ID,
			CompanyID:    companyID,
			ProviderType: store.IAMProviderAWS,
			Enabled:      true,
		},
		&store.IAMProviderConfig{
			ID:           provider2ID,
			CompanyID:    companyID,
			ProviderType: store.IAMProviderOIDC,
			Enabled:      true,
		},
	)
	is.roles[provider1ID.String()+"|"+"user@example.com"] = store.RoleViewer
	is.roles[provider2ID.String()+"|"+"user@example.com"] = store.RoleOwner

	resolver := NewIAMResolver(is)
	identity := ExternalIdentity{
		Provider:   "multi",
		Identifier: "user@example.com",
	}

	role, err := resolver.ResolveRole(context.Background(), companyID, identity, "project", resourceID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if role != store.RoleOwner {
		t.Errorf("expected owner (highest), got %s", role)
	}
}

func TestIAMResolver_ResolveRole_NoMatch(t *testing.T) {
	companyID := uuid.New()
	providerID := uuid.New()
	resourceID := uuid.New()

	is := newMockIAMStore()
	is.providers = append(is.providers, &store.IAMProviderConfig{
		ID:           providerID,
		CompanyID:    companyID,
		ProviderType: store.IAMProviderAWS,
		Enabled:      true,
	})
	// No role mapping for this identity

	resolver := NewIAMResolver(is)
	identity := ExternalIdentity{
		Identifier: "unknown-identity",
	}

	_, err := resolver.ResolveRole(context.Background(), companyID, identity, "project", resourceID)
	if err == nil {
		t.Fatal("expected error for no role match")
	}
}

func TestIAMResolver_ResolveRole_DisabledProvider(t *testing.T) {
	companyID := uuid.New()
	providerID := uuid.New()
	resourceID := uuid.New()

	is := newMockIAMStore()
	is.providers = append(is.providers, &store.IAMProviderConfig{
		ID:           providerID,
		CompanyID:    companyID,
		ProviderType: store.IAMProviderAWS,
		Enabled:      false, // disabled
	})
	is.roles[providerID.String()+"|"+"arn:aws:iam::123:role/Admin"] = store.RoleAdmin

	resolver := NewIAMResolver(is)
	identity := ExternalIdentity{
		Identifier: "arn:aws:iam::123:role/Admin",
	}

	_, err := resolver.ResolveRole(context.Background(), companyID, identity, "project", resourceID)
	if err == nil {
		t.Fatal("expected error since provider is disabled")
	}
}
