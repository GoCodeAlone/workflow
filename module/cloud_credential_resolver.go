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
	builtins       map[string]map[string]CloudCredentialResolver
	external       map[string]map[string]map[uint64]externalCredentialResolverEntry
	owners         map[string]map[uint64]*ExternalCredentialResolverRegistration
	nextID         uint64
	nextGeneration uint64
}

type externalCredentialResolverEntry struct {
	registration *ExternalCredentialResolverRegistration
	resolver     CloudCredentialResolver
}

var credentialResolvers = credentialResolverRegistry{
	builtins: make(map[string]map[string]CloudCredentialResolver),
	external: make(map[string]map[string]map[uint64]externalCredentialResolverEntry),
	owners:   make(map[string]map[uint64]*ExternalCredentialResolverRegistration),
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
	pairs      []credentialResolverPair
	resolvers  map[credentialResolverPair]CloudCredentialResolver
	owner      string
	generation uint64
	id         uint64
	active     bool
	closed     bool
	inFlight   int
	drained    chan struct{}
}

// ExternalCredentialResolverRegistrationProviderServiceName is the service
// name applications use to scope credential resolution to their own external
// plugin manager during staged engine builds.
const ExternalCredentialResolverRegistrationProviderServiceName = "workflow.external-credential-resolver-registrations" //nolint:gosec // service registry key, not credential material

// ExternalCredentialResolverRegistrationProvider exposes the resolver
// registrations owned by one application-scoped external plugin manager.
type ExternalCredentialResolverRegistrationProvider interface {
	CredentialResolverRegistrations() []*ExternalCredentialResolverRegistration
}

// PrepareExternalCredentialResolver discovers and validates every exact
// provider/type pair advertised by client without changing the live registry.
func PrepareExternalCredentialResolver(ctx context.Context, client pb.CredentialResolverClient) (*ExternalCredentialResolverRegistration, error) {
	return prepareExternalCredentialResolver(ctx, "", client)
}

// PrepareOwnedExternalCredentialResolver discovers and validates a resolver
// registration associated with a stable process-wide plugin identity. Active
// registrations with the same non-empty owner are treated as generations of
// one logical plugin; the latest generation is selected deterministically.
func PrepareOwnedExternalCredentialResolver(ctx context.Context, owner string, client pb.CredentialResolverClient) (*ExternalCredentialResolverRegistration, error) {
	if err := validateExternalCredentialResolverOwner(owner); err != nil {
		return nil, err
	}
	return prepareExternalCredentialResolver(ctx, owner, client)
}

// PrepareExternalCredentialResolverOwner creates a zero-resolver generation
// for an owned plugin that no longer advertises the optional resolver contract.
// Activating it shadows older generations from the same logical plugin.
func PrepareExternalCredentialResolverOwner(owner string) (*ExternalCredentialResolverRegistration, error) {
	if err := validateExternalCredentialResolverOwner(owner); err != nil {
		return nil, err
	}
	registration := &ExternalCredentialResolverRegistration{
		owner:     owner,
		resolvers: make(map[credentialResolverPair]CloudCredentialResolver),
	}
	assignExternalCredentialResolverGeneration(registration)
	return registration, nil
}

func validateExternalCredentialResolverOwner(owner string) error {
	if strings.TrimSpace(owner) == "" || owner != strings.TrimSpace(owner) || strings.ContainsRune(owner, '\x00') {
		return fmt.Errorf("register external credential resolver: owner is invalid")
	}
	return nil
}

func prepareExternalCredentialResolver(ctx context.Context, owner string, client pb.CredentialResolverClient) (*ExternalCredentialResolverRegistration, error) {
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
		owner:     owner,
	}
	assignExternalCredentialResolverGeneration(registration)
	for _, pair := range pairs {
		registration.resolvers[pair] = &externalCloudCredentialResolver{
			provider: pair.provider, credentialType: pair.credentialType, client: client,
		}
	}
	return registration, nil
}

func assignExternalCredentialResolverGeneration(registration *ExternalCredentialResolverRegistration) {
	credentialResolvers.Lock()
	credentialResolvers.nextGeneration++
	registration.generation = credentialResolvers.nextGeneration
	credentialResolvers.Unlock()
}

// Activate publishes a prepared registration. It may be called once.
func (registration *ExternalCredentialResolverRegistration) Activate() error {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	return activateExternalCredentialResolverRegistrationLocked(registration)
}

// ActivateExternalCredentialResolverRegistrations atomically publishes a set
// of prepared registrations. Validation completes before the registry changes.
func ActivateExternalCredentialResolverRegistrations(registrations []*ExternalCredentialResolverRegistration) error {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	seen := make(map[*ExternalCredentialResolverRegistration]struct{}, len(registrations))
	for _, registration := range registrations {
		if registration == nil {
			return fmt.Errorf("activate external credential resolvers: registration is nil")
		}
		if _, exists := seen[registration]; exists {
			return fmt.Errorf("activate external credential resolvers: registration is duplicated")
		}
		seen[registration] = struct{}{}
		if registration.closed {
			return fmt.Errorf("activate external credential resolvers: registration is closed")
		}
		if registration.active {
			return fmt.Errorf("activate external credential resolvers: registration is already active")
		}
	}
	for _, registration := range registrations {
		if err := activateExternalCredentialResolverRegistrationLocked(registration); err != nil {
			return err
		}
	}
	return nil
}

// Close removes an active registration and permanently closes the handle. It
// is idempotent and also safely closes a prepared-but-never-activated handle.
func (registration *ExternalCredentialResolverRegistration) Close() {
	if registration == nil {
		return
	}
	credentialResolvers.Lock()
	drained := deactivateExternalCredentialResolverRegistrationLocked(registration, true)
	credentialResolvers.Unlock()
	if drained != nil {
		<-drained
	}
}

// ReplaceExternalCredentialResolverRegistration atomically swaps the old live
// registration for the prepared replacement. Either argument may be nil for
// advertised-to-unadvertised and unadvertised-to-advertised transitions.
func ReplaceExternalCredentialResolverRegistration(oldRegistration, newRegistration *ExternalCredentialResolverRegistration) error {
	return replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration, nil)
}

func replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration *ExternalCredentialResolverRegistration, whileLocked func()) error {
	credentialResolvers.Lock()
	if oldRegistration != nil && (oldRegistration.closed || !oldRegistration.active) {
		credentialResolvers.Unlock()
		return fmt.Errorf("replace external credential resolver: old registration is not active")
	}
	if newRegistration != nil && (newRegistration.closed || newRegistration.active) {
		credentialResolvers.Unlock()
		return fmt.Errorf("replace external credential resolver: new registration is not prepared")
	}
	drained := deactivateExternalCredentialResolverRegistrationLocked(oldRegistration, true)
	if newRegistration != nil {
		if err := activateExternalCredentialResolverRegistrationLocked(newRegistration); err != nil {
			credentialResolvers.Unlock()
			return err
		}
	}
	if whileLocked != nil {
		whileLocked()
	}
	credentialResolvers.Unlock()
	if drained != nil {
		<-drained
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
	if registration.owner != "" {
		if credentialResolvers.owners[registration.owner] == nil {
			credentialResolvers.owners[registration.owner] = make(map[uint64]*ExternalCredentialResolverRegistration)
		}
		credentialResolvers.owners[registration.owner][registration.id] = registration
	}
	for _, pair := range registration.pairs {
		if credentialResolvers.external[pair.provider] == nil {
			credentialResolvers.external[pair.provider] = make(map[string]map[uint64]externalCredentialResolverEntry)
		}
		if credentialResolvers.external[pair.provider][pair.credentialType] == nil {
			credentialResolvers.external[pair.provider][pair.credentialType] = make(map[uint64]externalCredentialResolverEntry)
		}
		credentialResolvers.external[pair.provider][pair.credentialType][registration.id] = externalCredentialResolverEntry{
			registration: registration,
			resolver:     registration.resolvers[pair],
		}
	}
	registration.active = true
	return nil
}

func deactivateExternalCredentialResolverRegistrationLocked(registration *ExternalCredentialResolverRegistration, closeRegistration bool) <-chan struct{} {
	if registration == nil {
		return nil
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
		if registration.owner != "" {
			delete(credentialResolvers.owners[registration.owner], registration.id)
			if len(credentialResolvers.owners[registration.owner]) == 0 {
				delete(credentialResolvers.owners, registration.owner)
			}
		}
		registration.active = false
		registration.id = 0
	}
	if closeRegistration {
		registration.closed = true
	}
	if registration.inFlight == 0 {
		return nil
	}
	if registration.drained == nil {
		registration.drained = make(chan struct{})
	}
	return registration.drained
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

type credentialResolverLease struct {
	resolver     CloudCredentialResolver
	registration *ExternalCredentialResolverRegistration
	once         sync.Once
}

func (lease *credentialResolverLease) Release() {
	if lease == nil || lease.registration == nil {
		return
	}
	lease.once.Do(func() {
		credentialResolvers.Lock()
		registration := lease.registration
		registration.inFlight--
		if registration.inFlight == 0 && registration.drained != nil {
			close(registration.drained)
			registration.drained = nil
		}
		credentialResolvers.Unlock()
	})
}

func selectCredentialResolver(provider, credentialType string, externalOnly bool) (*credentialResolverLease, error) {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()
	external := credentialResolvers.external[provider][credentialType]
	type ownerKey struct {
		name        string
		anonymousID uint64
	}
	type ownedResolver struct {
		id           uint64
		resolver     CloudCredentialResolver
		registration *ExternalCredentialResolverRegistration
	}
	selectedByOwner := make(map[ownerKey]ownedResolver, len(external))
	for id, entry := range external {
		owner := entry.registration.owner
		if owner != "" {
			latestID := uint64(0)
			for generationID := range credentialResolvers.owners[owner] {
				if generationID > latestID {
					latestID = generationID
				}
			}
			if id != latestID {
				continue
			}
		}
		key := ownerKey{name: owner}
		if owner == "" {
			key.anonymousID = id
		}
		selected, exists := selectedByOwner[key]
		if !exists || id > selected.id {
			selectedByOwner[key] = ownedResolver{id: id, resolver: entry.resolver, registration: entry.registration}
		}
	}
	var selectedExternal ownedResolver
	for _, selected := range selectedByOwner {
		selectedExternal = selected
	}
	externalCount := len(selectedByOwner)
	var builtin CloudCredentialResolver
	if byType := credentialResolvers.builtins[provider]; byType != nil {
		builtin = byType[credentialType]
	}
	if externalCount > 1 {
		return nil, fmt.Errorf("multiple external credential resolvers match provider %q and credential type %q; remove the duplicate plugin before resolving", provider, credentialType)
	}
	if externalCount == 1 {
		selectedExternal.registration.inFlight++
		return &credentialResolverLease{resolver: selectedExternal.resolver, registration: selectedExternal.registration}, nil
	}
	if !externalOnly && builtin != nil {
		return &credentialResolverLease{resolver: builtin}, nil
	}
	return nil, fmt.Errorf("no external credential resolver matches provider %q and credential type %q; install a plugin that declares this credentialResolvers capability", provider, credentialType)
}

func selectCredentialResolverFromRegistrations(registrations []*ExternalCredentialResolverRegistration, provider, credentialType string, externalOnly bool) (*credentialResolverLease, error) {
	credentialResolvers.Lock()
	defer credentialResolvers.Unlock()

	latestByOwner := make(map[string]*ExternalCredentialResolverRegistration)
	var anonymous []*ExternalCredentialResolverRegistration
	for _, registration := range registrations {
		if registration == nil || registration.closed {
			continue
		}
		if registration.owner == "" {
			anonymous = append(anonymous, registration)
			continue
		}
		latest := latestByOwner[registration.owner]
		if latest == nil || registration.generation > latest.generation {
			latestByOwner[registration.owner] = registration
		}
	}

	pair := credentialResolverPair{provider: provider, credentialType: credentialType}
	matching := make([]*ExternalCredentialResolverRegistration, 0, len(latestByOwner)+len(anonymous))
	for _, registration := range latestByOwner {
		if registration.resolvers[pair] != nil {
			matching = append(matching, registration)
		}
	}
	for _, registration := range anonymous {
		if registration.resolvers[pair] != nil {
			matching = append(matching, registration)
		}
	}
	if len(matching) > 1 {
		return nil, fmt.Errorf("multiple external credential resolvers match provider %q and credential type %q; remove the duplicate plugin before resolving", provider, credentialType)
	}
	if len(matching) == 1 {
		registration := matching[0]
		registration.inFlight++
		return &credentialResolverLease{resolver: registration.resolvers[pair], registration: registration}, nil
	}
	if !externalOnly {
		if builtin := credentialResolvers.builtins[provider][credentialType]; builtin != nil {
			return &credentialResolverLease{resolver: builtin}, nil
		}
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
