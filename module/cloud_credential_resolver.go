package module

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CloudCredentialResolver resolves credentials for a specific cloud provider and credential type.
type CloudCredentialResolver interface {
	// Provider returns the cloud provider name (for example, "aws", "gcp", "azure", or core-local "kubernetes").
	Provider() string
	// CredentialType returns the credential type this resolver handles (e.g., "static", "env", "profile", "role_arn").
	CredentialType() string
	// Resolve resolves credentials from the given config and stores them in the CloudAccount.
	Resolve(m *CloudAccount) error
}

type contextCloudCredentialResolver interface {
	CloudCredentialResolver
	ResolveContext(context.Context, *CloudAccount) error
}

type credentialResolverRegistry struct {
	sync.RWMutex
	builtins map[string]map[string]CloudCredentialResolver
	external map[string]map[string]map[uint64]CloudCredentialResolver
	nextID   uint64
}

var credentialResolvers = credentialResolverRegistry{
	builtins: make(map[string]map[string]CloudCredentialResolver),
	external: make(map[string]map[string]map[uint64]CloudCredentialResolver),
}

var externalCredentialResolverTypes = map[string]map[string]struct{}{
	"aws":   makeCredentialResolverTypeSet("static", "env", "profile", "role_arn"),
	"gcp":   makeCredentialResolverTypeSet("static", "env", "service_account_json", "service_account_key", "workload_identity", "application_default"),
	"azure": makeCredentialResolverTypeSet("static", "env", "client_credentials", "managed_identity", "cli"),
}

func makeCredentialResolverTypeSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// RegisterCredentialResolver registers a core-local CloudCredentialResolver.
// It is safe to call from init functions and concurrently with resolution.
func RegisterCredentialResolver(resolver CloudCredentialResolver) {
	if resolver == nil {
		return
	}
	provider := resolver.Provider()
	credentialType := resolver.CredentialType()
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	if credentialResolvers.builtins[provider] == nil {
		credentialResolvers.builtins[provider] = make(map[string]CloudCredentialResolver)
	}
	credentialResolvers.builtins[provider][credentialType] = resolver
}

// RegisterExternalCredentialResolver discovers and registers every exact
// provider/type pair advertised by client. The returned cleanup removes only
// this registration, making the process-global registry safe for test-scoped
// and plugin-lifecycle use. Multiple plugins may advertise the same pair, but
// dispatch fails closed before invoking either one.
func RegisterExternalCredentialResolver(ctx context.Context, client pb.CredentialResolverClient) (func(), error) {
	if client == nil {
		return nil, fmt.Errorf("register external credential resolver: client is nil")
	}
	response, err := client.DescribeResolvers(ctx, &pb.CredentialResolverDeclarationsRequest{})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("register external credential resolver: plugin does not serve the optional CredentialResolver contract")
		}
		return nil, fmt.Errorf("register external credential resolver: runtime discovery failed")
	}
	if response.GetError() != nil {
		return nil, fmt.Errorf("register external credential resolver: runtime declarations were rejected (%s)", safeCredentialResolutionCode(response.GetError().GetCode()))
	}
	pairs, err := validateExternalCredentialResolverDeclarations(response.GetResolvers())
	if err != nil {
		return nil, err
	}

	credentialResolvers.Lock()
	credentialResolvers.nextID++
	id := credentialResolvers.nextID
	for _, pair := range pairs {
		if credentialResolvers.external[pair.provider] == nil {
			credentialResolvers.external[pair.provider] = make(map[string]map[uint64]CloudCredentialResolver)
		}
		if credentialResolvers.external[pair.provider][pair.credentialType] == nil {
			credentialResolvers.external[pair.provider][pair.credentialType] = make(map[uint64]CloudCredentialResolver)
		}
		credentialResolvers.external[pair.provider][pair.credentialType][id] = &externalCloudCredentialResolver{
			provider: pair.provider, credentialType: pair.credentialType, client: client,
		}
	}
	credentialResolvers.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			credentialResolvers.Lock()
			defer credentialResolvers.Unlock()
			for _, pair := range pairs {
				byType := credentialResolvers.external[pair.provider]
				registrations := byType[pair.credentialType]
				delete(registrations, id)
				if len(registrations) == 0 {
					delete(byType, pair.credentialType)
				}
				if len(byType) == 0 {
					delete(credentialResolvers.external, pair.provider)
				}
			}
		})
	}, nil
}

type credentialResolverPair struct {
	provider       string
	credentialType string
}

func validateExternalCredentialResolverDeclarations(declarations []*pb.CredentialResolverDeclaration) ([]credentialResolverPair, error) {
	if len(declarations) == 0 {
		return nil, fmt.Errorf("register external credential resolver: plugin declared no resolvers")
	}
	seenProviders := make(map[string]struct{}, len(declarations))
	seenPairs := make(map[credentialResolverPair]struct{})
	var pairs []credentialResolverPair
	for _, declaration := range declarations {
		if declaration == nil {
			return nil, fmt.Errorf("register external credential resolver: plugin returned an empty declaration")
		}
		provider := strings.TrimSpace(declaration.GetProvider())
		if provider == "" {
			return nil, fmt.Errorf("register external credential resolver: plugin returned an empty provider")
		}
		if _, exists := seenProviders[provider]; exists {
			return nil, fmt.Errorf("register external credential resolver: plugin duplicated provider %q", provider)
		}
		allowedTypes, supported := externalCredentialResolverTypes[provider]
		if !supported {
			return nil, fmt.Errorf("register external credential resolver: provider %q is unsupported; mock and kubernetes remain core-local", provider)
		}
		seenProviders[provider] = struct{}{}
		if len(declaration.GetCredentialTypes()) == 0 {
			return nil, fmt.Errorf("register external credential resolver: provider %q declared no credential types", provider)
		}
		for _, rawType := range declaration.GetCredentialTypes() {
			credentialType := strings.TrimSpace(rawType)
			if credentialType == "" {
				return nil, fmt.Errorf("register external credential resolver: provider %q declared an empty credential type", provider)
			}
			if _, supported := allowedTypes[credentialType]; !supported {
				return nil, fmt.Errorf("register external credential resolver: provider %q has unsupported credential type %q", provider, credentialType)
			}
			pair := credentialResolverPair{provider: provider, credentialType: credentialType}
			if _, exists := seenPairs[pair]; exists {
				return nil, fmt.Errorf("register external credential resolver: provider %q duplicated credential type %q", provider, credentialType)
			}
			seenPairs[pair] = struct{}{}
			pairs = append(pairs, pair)
		}
	}
	return pairs, nil
}

func selectCredentialResolver(provider, credentialType string, externalOnly bool) (CloudCredentialResolver, error) {
	credentialResolvers.RLock()
	external := credentialResolvers.external[provider][credentialType]
	var externalResolver CloudCredentialResolver
	for _, resolver := range external {
		externalResolver = resolver
	}
	externalCount := len(external)
	var builtin CloudCredentialResolver
	if byType := credentialResolvers.builtins[provider]; byType != nil {
		builtin = byType[credentialType]
	}
	credentialResolvers.RUnlock()

	if externalCount > 1 {
		return nil, fmt.Errorf("multiple external credential resolvers match provider %q and credential type %q; remove the duplicate plugin before resolving", provider, credentialType)
	}
	if externalCount == 1 {
		return externalResolver, nil
	}
	if !externalOnly && builtin != nil {
		return builtin, nil
	}
	return nil, fmt.Errorf("no external credential resolver matches provider %q and credential type %q; install a plugin that declares this credentialResolvers capability", provider, credentialType)
}

type externalCloudCredentialResolver struct {
	provider       string
	credentialType string
	client         pb.CredentialResolverClient
}

func (r *externalCloudCredentialResolver) Provider() string       { return r.provider }
func (r *externalCloudCredentialResolver) CredentialType() string { return r.credentialType }
func (r *externalCloudCredentialResolver) Resolve(account *CloudAccount) error {
	return r.ResolveContext(context.Background(), account)
}

func (r *externalCloudCredentialResolver) ResolveContext(ctx context.Context, account *CloudAccount) error {
	configJSON, err := json.Marshal(account.config)
	if err != nil {
		return fmt.Errorf("external credential resolver config is not JSON-compatible")
	}
	response, err := r.client.Resolve(ctx, &pb.CredentialResolveRequest{
		Provider:       r.provider,
		CredentialType: r.credentialType,
		ConfigJson:     configJSON,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("external credential resolver failed (transport_%s)", status.Code(err).String())
	}
	if response.GetError() != nil {
		return fmt.Errorf("external credential resolver failed (%s)", safeCredentialResolutionCode(response.GetError().GetCode()))
	}
	resolved := response.GetCredentials()
	if resolved == nil {
		return fmt.Errorf("external credential resolver failed (empty_response)")
	}
	if resolved.GetProvider() != r.provider {
		return fmt.Errorf("external credential resolver failed (invalid_response)")
	}
	account.creds = cloudCredentialsFromProto(resolved)
	if account.creds.Region == "" {
		account.creds.Region = account.region
	}
	return nil
}

var credentialResolutionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func safeCredentialResolutionCode(code string) string {
	if !credentialResolutionCodePattern.MatchString(code) {
		return "provider_error"
	}
	return code
}

func cloudCredentialsFromProto(credentials *pb.ResolvedCloudCredentials) *CloudCredentials {
	if credentials == nil {
		return nil
	}
	return &CloudCredentials{
		Provider:           credentials.GetProvider(),
		Region:             credentials.GetRegion(),
		AccessKey:          credentials.GetAccessKey(),
		SecretKey:          credentials.GetSecretKey(),
		SessionToken:       credentials.GetSessionToken(),
		RoleARN:            credentials.GetRoleArn(),
		ProjectID:          credentials.GetProjectId(),
		ServiceAccountJSON: append([]byte(nil), credentials.GetServiceAccountJson()...),
		TenantID:           credentials.GetTenantId(),
		ClientID:           credentials.GetClientId(),
		ClientSecret:       credentials.GetClientSecret(),
		SubscriptionID:     credentials.GetSubscriptionId(),
		Kubeconfig:         append([]byte(nil), credentials.GetKubeconfig()...),
		Context:            credentials.GetContext(),
		Token:              credentials.GetToken(),
		Extra:              cloneCredentialExtra(credentials.GetExtra()),
	}
}

func cloneCredentialExtra(extra map[string]string) map[string]string {
	if extra == nil {
		return nil
	}
	cloned := make(map[string]string, len(extra))
	for key, value := range extra {
		cloned[key] = value
	}
	return cloned
}
