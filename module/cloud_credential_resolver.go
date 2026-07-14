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

// ExternalCredentialResolverRegistration is a validated provider/type set that
// can be activated independently from discovery. Separating preparation from
// activation lets plugin managers atomically replace live registrations.
type ExternalCredentialResolverRegistration struct {
	pairs     []credentialResolverPair
	resolvers map[credentialResolverPair]CloudCredentialResolver
	id        uint64
	active    bool
	closed    bool
}

// PrepareExternalCredentialResolver discovers and validates every exact
// provider/type pair advertised by client without changing the live registry.
func PrepareExternalCredentialResolver(ctx context.Context, client pb.CredentialResolverClient) (*ExternalCredentialResolverRegistration, error) {
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
	registration := &ExternalCredentialResolverRegistration{
		pairs:     pairs,
		resolvers: make(map[credentialResolverPair]CloudCredentialResolver, len(pairs)),
	}
	for _, pair := range pairs {
		registration.resolvers[pair] = &externalCloudCredentialResolver{
			provider: pair.provider, credentialType: pair.credentialType, client: client,
		}
	}
	return registration, nil
}

// Activate publishes a prepared registration. It may be called once.
func (registration *ExternalCredentialResolverRegistration) Activate() error {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	return activateExternalCredentialResolverRegistrationLocked(registration)
}

// Close removes an active registration and permanently closes the handle. It
// is idempotent and also safely closes a prepared-but-never-activated handle.
func (registration *ExternalCredentialResolverRegistration) Close() {
	if registration == nil {
		return
	}
	credentialResolvers.Lock()
	deactivateExternalCredentialResolverRegistrationLocked(registration, true)
	credentialResolvers.Unlock()
}

// ReplaceExternalCredentialResolverRegistration atomically swaps the old live
// registration for the prepared replacement. Either argument may be nil for
// advertised-to-unadvertised and unadvertised-to-advertised transitions.
func ReplaceExternalCredentialResolverRegistration(oldRegistration, newRegistration *ExternalCredentialResolverRegistration) error {
	return replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration, nil)
}

func replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration *ExternalCredentialResolverRegistration, whileLocked func()) error {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	if oldRegistration != nil && (oldRegistration.closed || !oldRegistration.active) {
		return fmt.Errorf("replace external credential resolver: old registration is not active")
	}
	if newRegistration != nil && (newRegistration.closed || newRegistration.active) {
		return fmt.Errorf("replace external credential resolver: new registration is not prepared")
	}
	deactivateExternalCredentialResolverRegistrationLocked(oldRegistration, true)
	if whileLocked != nil {
		whileLocked()
	}
	if newRegistration != nil {
		return activateExternalCredentialResolverRegistrationLocked(newRegistration)
	}
	return nil
}

func activateExternalCredentialResolverRegistrationLocked(registration *ExternalCredentialResolverRegistration) error {
	if registration == nil {
		return fmt.Errorf("activate external credential resolver: registration is nil")
	}
	if registration.closed {
		return fmt.Errorf("activate external credential resolver: registration is closed")
	}
	if registration.active {
		return fmt.Errorf("activate external credential resolver: registration is already active")
	}
	credentialResolvers.nextID++
	registration.id = credentialResolvers.nextID
	for _, pair := range registration.pairs {
		if credentialResolvers.external[pair.provider] == nil {
			credentialResolvers.external[pair.provider] = make(map[string]map[uint64]CloudCredentialResolver)
		}
		if credentialResolvers.external[pair.provider][pair.credentialType] == nil {
			credentialResolvers.external[pair.provider][pair.credentialType] = make(map[uint64]CloudCredentialResolver)
		}
		credentialResolvers.external[pair.provider][pair.credentialType][registration.id] = registration.resolvers[pair]
	}
	registration.active = true
	return nil
}

func deactivateExternalCredentialResolverRegistrationLocked(registration *ExternalCredentialResolverRegistration, closeRegistration bool) {
	if registration == nil {
		return
	}
	if registration.active {
		for _, pair := range registration.pairs {
			byType := credentialResolvers.external[pair.provider]
			registrations := byType[pair.credentialType]
			delete(registrations, registration.id)
			if len(registrations) == 0 {
				delete(byType, pair.credentialType)
			}
			if len(byType) == 0 {
				delete(credentialResolvers.external, pair.provider)
			}
		}
		registration.active = false
		registration.id = 0
	}
	if closeRegistration {
		registration.closed = true
	}
}

// RegisterExternalCredentialResolver preserves the original one-step API as a
// compatibility wrapper around preparation and activation.
func RegisterExternalCredentialResolver(ctx context.Context, client pb.CredentialResolverClient) (func(), error) {
	registration, err := PrepareExternalCredentialResolver(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := registration.Activate(); err != nil {
		registration.Close()
		return nil, err
	}
	return registration.Close, nil
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

// ExternalCredentialResolutionError is a sanitized provider-reported
// resolution failure. Code is a stable machine-readable value and Retryable
// is safe orchestration metadata; provider messages and credential payloads
// are intentionally excluded.
type ExternalCredentialResolutionError struct {
	Code      string
	Retryable bool
}

func (e *ExternalCredentialResolutionError) Error() string {
	if e == nil {
		return "external credential resolver failed"
	}
	return fmt.Sprintf("external credential resolver failed (%s)", safeCredentialResolutionCode(e.Code))
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
		return &ExternalCredentialResolutionError{
			Code:      safeCredentialResolutionCode(response.GetError().GetCode()),
			Retryable: response.GetError().GetRetryable(),
		}
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
