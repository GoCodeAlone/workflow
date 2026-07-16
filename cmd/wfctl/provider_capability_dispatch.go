package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// providerCapabilityOwner is one installed plugin's provider-neutral optional
// capability declaration. Runtime clients are loaded only after exact
// selection succeeds, so collisions fail before an operation can be sent.
type providerCapabilityOwner struct {
	Name         string
	InstallName  string
	Version      string
	Declarations config.ProviderDeclarations
}

type providerCapabilityRoute struct {
	PluginName        string
	PluginInstallName string
	PluginVersion     string
	Declarations      config.ProviderDeclarations

	CredentialSource   *config.CredentialSourceDecl
	CredentialResolver *config.CredentialResolverDecl
	KubernetesBackend  *config.KubernetesBackendDecl
	ContainerRegistry  *config.ContainerRegistryDecl
	SecretStore        *config.SecretStoreDecl
}

type providerCapabilityIndex struct {
	owners []providerCapabilityOwner
}

type providerCapabilityDiagnosticsContextKey struct{}

func withProviderCapabilityDiagnosticsSuppressed(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, providerCapabilityDiagnosticsContextKey{}, true)
}

func providerCapabilityDiagnosticsSuppressed(ctx context.Context) bool {
	return ctx != nil && ctx.Value(providerCapabilityDiagnosticsContextKey{}) == true
}

func newProviderCapabilityIndex(owners []providerCapabilityOwner) (*providerCapabilityIndex, error) {
	cloned := append([]providerCapabilityOwner(nil), owners...)
	for index := range cloned {
		owner := &cloned[index]
		if strings.TrimSpace(owner.Name) == "" {
			return nil, fmt.Errorf("provider capability owner name is required")
		}
		if strings.TrimSpace(owner.InstallName) == "" {
			owner.InstallName = normalizePluginName(owner.Name)
		}
		if owner.InstallName == "." || owner.InstallName == ".." || filepath.Base(owner.InstallName) != owner.InstallName {
			return nil, fmt.Errorf("plugin %q install name %q is invalid", owner.Name, owner.InstallName)
		}
		if err := owner.Declarations.Validate(); err != nil {
			return nil, fmt.Errorf("plugin %q provider declarations: %w", owner.Name, err)
		}
	}
	sort.Slice(cloned, func(i, j int) bool {
		if cloned[i].Name != cloned[j].Name {
			return cloned[i].Name < cloned[j].Name
		}
		return cloned[i].Version < cloned[j].Version
	})
	return &providerCapabilityIndex{owners: cloned}, nil
}

func loadProviderCapabilityIndex(pluginDir string) (*providerCapabilityIndex, error) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return newProviderCapabilityIndex(nil)
		}
		return nil, fmt.Errorf("scan plugin directory %q: %w", pluginDir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	owners := make([]providerCapabilityOwner, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(pluginDir, entry.Name(), "plugin.json")
		manifest, loadErr := plugin.LoadManifest(path)
		if errors.Is(loadErr, os.ErrNotExist) {
			continue
		}
		if loadErr != nil {
			return nil, fmt.Errorf("load plugin %q manifest: %w", entry.Name(), loadErr)
		}
		if validateErr := manifest.Validate(); validateErr != nil {
			return nil, fmt.Errorf("validate plugin %q manifest: %w", entry.Name(), validateErr)
		}
		if normalizePluginName(manifest.Name) != normalizePluginName(entry.Name()) {
			return nil, fmt.Errorf("plugin directory %q contains manifest for %q", entry.Name(), manifest.Name)
		}
		owners = append(owners, providerCapabilityOwner{
			Name:        manifest.Name,
			InstallName: entry.Name(),
			Version:     manifest.Version,
			Declarations: config.ProviderDeclarations{
				CredentialSources:   manifest.CredentialSources,
				CredentialResolvers: manifest.CredentialResolvers,
				KubernetesBackends:  manifest.KubernetesBackends,
				ContainerRegistries: manifest.ContainerRegistries,
				SecretStores:        manifest.SecretStores,
			},
		})
	}
	return newProviderCapabilityIndex(owners)
}

func (i *providerCapabilityIndex) selectCredentialSource(source string) (providerCapabilityRoute, error) {
	var matches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.CredentialSources {
			declaration := &owner.Declarations.CredentialSources[j]
			if declaration.Source == source {
				matches = append(matches, routeFor(owner, func(route *providerCapabilityRoute) { route.CredentialSource = declaration }))
			}
		}
	}
	return selectProviderCapabilityRoute("credential source", source, matches)
}

func (i *providerCapabilityIndex) selectCredentialResolver(provider, credentialType string) (providerCapabilityRoute, error) {
	var matches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.CredentialResolvers {
			declaration := &owner.Declarations.CredentialResolvers[j]
			if declaration.Provider == provider {
				matches = append(matches, routeFor(owner, func(route *providerCapabilityRoute) { route.CredentialResolver = declaration }))
			}
		}
	}
	route, err := selectProviderCapabilityRoute("credential resolver provider", provider, matches)
	if err != nil {
		return providerCapabilityRoute{}, err
	}
	if !containsExact(route.CredentialResolver.CredentialTypes, credentialType) {
		return providerCapabilityRoute{}, providerCapabilityNotFoundError{family: "credential resolver", key: provider + "/" + credentialType}
	}
	return route, nil
}

func (i *providerCapabilityIndex) selectKubernetesBackend(name string) (providerCapabilityRoute, error) {
	var nameMatches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.KubernetesBackends {
			declaration := &owner.Declarations.KubernetesBackends[j]
			if declaration.Name == name {
				nameMatches = append(nameMatches, routeFor(owner, func(route *providerCapabilityRoute) { route.KubernetesBackend = declaration }))
			}
		}
	}
	route, err := selectProviderCapabilityRoute("kubernetes backend", name, nameMatches)
	if err != nil {
		return providerCapabilityRoute{}, err
	}
	var resourceMatches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.KubernetesBackends {
			declaration := &owner.Declarations.KubernetesBackends[j]
			if declaration.ResourceType == route.KubernetesBackend.ResourceType {
				resourceMatches = append(resourceMatches, routeFor(owner, func(candidate *providerCapabilityRoute) { candidate.KubernetesBackend = declaration }))
			}
		}
	}
	return selectProviderCapabilityRoute("kubernetes backend resource type", route.KubernetesBackend.ResourceType, resourceMatches)
}

func (i *providerCapabilityIndex) selectContainerRegistry(registryType, operation string) (providerCapabilityRoute, error) {
	var matches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.ContainerRegistries {
			declaration := &owner.Declarations.ContainerRegistries[j]
			if declaration.Type == registryType {
				matches = append(matches, routeFor(owner, func(route *providerCapabilityRoute) { route.ContainerRegistry = declaration }))
			}
		}
	}
	route, err := selectProviderCapabilityRoute("container registry type", registryType, matches)
	if err != nil {
		return providerCapabilityRoute{}, err
	}
	if !containsExact(route.ContainerRegistry.Operations, operation) {
		return providerCapabilityRoute{}, providerCapabilityNotFoundError{family: "container registry", key: registryType + "/" + operation}
	}
	return route, nil
}

func (i *providerCapabilityIndex) selectSecretStore(storeType, operation, scope string) (providerCapabilityRoute, error) {
	var matches []providerCapabilityRoute
	for _, owner := range i.owners {
		for j := range owner.Declarations.SecretStores {
			declaration := &owner.Declarations.SecretStores[j]
			if declaration.Type == storeType {
				matches = append(matches, routeFor(owner, func(route *providerCapabilityRoute) { route.SecretStore = declaration }))
			}
		}
	}
	route, err := selectProviderCapabilityRoute("secret store type", storeType, matches)
	if err != nil {
		return providerCapabilityRoute{}, err
	}
	if !containsExact(route.SecretStore.Operations, operation) || !containsExact(route.SecretStore.Scopes, scope) {
		return providerCapabilityRoute{}, providerCapabilityNotFoundError{family: "secret store", key: storeType + "/" + operation + "/" + scope}
	}
	return route, nil
}

func routeFor(owner providerCapabilityOwner, set func(*providerCapabilityRoute)) providerCapabilityRoute {
	route := providerCapabilityRoute{
		PluginName: owner.Name, PluginInstallName: owner.InstallName,
		PluginVersion: owner.Version, Declarations: owner.Declarations,
	}
	set(&route)
	return route
}

func selectProviderCapabilityRoute(family, key string, matches []providerCapabilityRoute) (providerCapabilityRoute, error) {
	switch len(matches) {
	case 0:
		return providerCapabilityRoute{}, providerCapabilityNotFoundError{family: family, key: key}
	case 1:
		return matches[0], nil
	default:
		owners := make([]string, 0, len(matches))
		for _, match := range matches {
			owners = append(owners, match.PluginName+"@"+match.PluginVersion)
		}
		sort.Strings(owners)
		return providerCapabilityRoute{}, fmt.Errorf("provider capability collision for %s %q: %s", family, key, strings.Join(owners, ", "))
	}
}

type providerCapabilityNotFoundError struct {
	family string
	key    string
}

func (e providerCapabilityNotFoundError) Error() string {
	return fmt.Sprintf("no installed plugin declares %s %q; run wfctl plugin install <plugin-name> or upgrade the installed plugin to a version that declares it", e.family, e.key)
}

func providerPluginDir(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue
	}
	return defaultPluginCommandDir()
}

var resolveContainerRegistryCapability = discoverContainerRegistryCapability

var containerRegistryOperationTimeout = 30 * time.Minute

type containerRegistryOperationRequest struct {
	Registry       config.CIRegistry
	ImageReference string
}

type preparedContainerRegistryCapability struct {
	client         pb.ContainerRegistryClient
	closePlugin    func()
	handled        bool
	operation      string
	registry       config.CIRegistry
	imageReference string
}

func (p *preparedContainerRegistryCapability) Close() {
	if p != nil && p.closePlugin != nil {
		p.closePlugin()
		p.closePlugin = nil
	}
}

func closePreparedContainerRegistryCapabilities(prepared []*preparedContainerRegistryCapability) {
	for _, capability := range prepared {
		capability.Close()
	}
}

func prepareContainerRegistryCapabilities(ctx context.Context, pluginDir, operation string, requests []containerRegistryOperationRequest) ([]*preparedContainerRegistryCapability, error) {
	prepared := make([]*preparedContainerRegistryCapability, 0, len(requests))
	for _, request := range requests {
		client, closePlugin, found, err := resolveContainerRegistryCapability(ctx, providerPluginDir(pluginDir), request.Registry.Type, operation)
		if err != nil {
			closePreparedContainerRegistryCapabilities(prepared)
			return nil, fmt.Errorf("%s %s preflight: %w", operation, request.Registry.Name, err)
		}
		prepared = append(prepared, &preparedContainerRegistryCapability{
			client: client, closePlugin: closePlugin, handled: found, operation: operation,
			registry: request.Registry, imageReference: request.ImageReference,
		})
	}
	return prepared, nil
}

func discoverContainerRegistryCapability(ctx context.Context, pluginDir, registryType, operation string) (pb.ContainerRegistryClient, func(), bool, error) {
	pluginDir = providerPluginDir(pluginDir)
	index, err := loadProviderCapabilityIndex(pluginDir)
	if err != nil {
		return nil, nil, false, err
	}
	route, err := index.selectContainerRegistry(registryType, operation)
	if err != nil {
		var notFound providerCapabilityNotFoundError
		if errors.As(err, &notFound) && notFound.family == "container registry type" {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}

	manager := external.NewExternalPluginManager(pluginDir, log.New(io.Discard, "", 0))
	if err := manager.StageCredentialResolvers(); err != nil {
		manager.Shutdown()
		return nil, nil, false, fmt.Errorf("stage unrelated credential resolvers before container registry discovery: %w", err)
	}
	adapter, err := manager.LoadPluginContext(ctx, route.PluginInstallName)
	if err != nil {
		manager.Shutdown()
		return nil, nil, false, fmt.Errorf("load plugin %q for container registry %q failed; provider error text suppressed", route.PluginName, registryType)
	}
	closePlugin := func() { _ = manager.ShutdownContext(ctx) }
	if adapter.Conn() == nil {
		closePlugin()
		return nil, nil, false, fmt.Errorf("plugin %q has no gRPC connection", route.PluginName)
	}
	if !contractRegistryAdvertises(adapter.ContractRegistry(), pb.ContainerRegistry_ServiceDesc.ServiceName) {
		closePlugin()
		return nil, nil, false, fmt.Errorf("plugin %q declares container registry %q but does not advertise ContainerRegistry", route.PluginName, registryType)
	}

	client := pb.NewContainerRegistryClient(adapter.Conn())
	describeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	declarations, err := client.DescribeRegistries(describeCtx, &pb.ContainerRegistryDeclarationsRequest{})
	cancel()
	if err != nil {
		closePlugin()
		return nil, nil, false, fmt.Errorf("plugin %q: %w", route.PluginName, providerCapabilityTransportError("ContainerRegistry.DescribeRegistries", err))
	}
	if declarations.GetError() != nil {
		closePlugin()
		return nil, nil, false, fmt.Errorf("plugin %q ContainerRegistry.DescribeRegistries: %s; provider error text suppressed", route.PluginName, declarations.GetError().GetCode())
	}
	failures := compareProviderDeclarationsWithRuntime(
		config.ProviderDeclarations{ContainerRegistries: route.Declarations.ContainerRegistries},
		providerRuntimeDeclarations{
			AdvertisedServices:  map[string]bool{pb.ContainerRegistry_ServiceDesc.ServiceName: true},
			ContainerRegistries: declarations.GetRegistries(),
		})
	if len(failures) > 0 {
		closePlugin()
		return nil, nil, false, fmt.Errorf("plugin %q container registry manifest/runtime mismatch: %s", route.PluginName, strings.Join(failures, "; "))
	}
	return client, closePlugin, true, nil
}

func contractRegistryAdvertises(registry *pb.ContractRegistry, serviceName string) bool {
	for _, contract := range registry.GetContracts() {
		if contract.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE && contract.GetServiceName() == serviceName {
			return true
		}
	}
	return false
}

func runContainerRegistryCapability(ctx context.Context, pluginDir, operation string, registry config.CIRegistry, imageReference string, dryRun bool, outputWriter io.Writer) (bool, error) {
	operationCtx, cancel := context.WithTimeout(ctx, containerRegistryOperationTimeout)
	defer cancel()
	prepared, err := prepareContainerRegistryCapabilities(operationCtx, pluginDir, operation, []containerRegistryOperationRequest{{
		Registry: registry, ImageReference: imageReference,
	}})
	if err != nil {
		return false, err
	}
	defer closePreparedContainerRegistryCapabilities(prepared)
	return executeContainerRegistryCapability(operationCtx, prepared[0], dryRun, outputWriter)
}

func executeContainerRegistryCapability(ctx context.Context, prepared *preparedContainerRegistryCapability, dryRun bool, outputWriter io.Writer) (bool, error) {
	if prepared == nil || !prepared.handled {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	registry := prepared.registry
	operation := prepared.operation
	typedConfig := containerRegistryConfigToProto(registry)
	var response *pb.ContainerRegistryOperationResponse
	var err error
	var transportOperation string
	switch operation {
	case "login":
		transportOperation = "ContainerRegistry.Login"
		response, err = prepared.client.Login(ctx, &pb.ContainerRegistryLoginRequest{Registry: typedConfig, DryRun: dryRun})
	case "logout":
		transportOperation = "ContainerRegistry.Logout"
		response, err = prepared.client.Logout(ctx, &pb.ContainerRegistryLogoutRequest{Registry: typedConfig, DryRun: dryRun})
	case "push":
		transportOperation = "ContainerRegistry.Push"
		response, err = prepared.client.Push(ctx, &pb.ContainerRegistryPushRequest{Registry: typedConfig, DryRun: dryRun, ImageReference: prepared.imageReference})
	case "prune":
		transportOperation = "ContainerRegistry.Prune"
		response, err = prepared.client.Prune(ctx, &pb.ContainerRegistryPruneRequest{Registry: typedConfig, DryRun: dryRun})
	default:
		return true, fmt.Errorf("unsupported container registry operation %q", operation)
	}
	if err != nil {
		return true, fmt.Errorf("container registry %s: %w", registry.Name, providerCapabilityTransportError(transportOperation, err))
	}
	if response.GetError() != nil {
		return true, fmt.Errorf("container registry %s %s: %s; provider error text suppressed", registry.Name, operation, response.GetError().GetCode())
	}
	if response == nil || response.GetResult() == nil {
		return true, fmt.Errorf("container registry %s %s: empty_response; provider error text suppressed", registry.Name, operation)
	}
	if output := response.GetResult().GetOutput(); len(output) > 0 {
		if outputWriter == nil {
			outputWriter = io.Discard
		}
		written, err := outputWriter.Write(output)
		if err != nil {
			return true, fmt.Errorf("container registry %s %s write output: %w", registry.Name, operation, err)
		}
		if written != len(output) {
			return true, fmt.Errorf("container registry %s %s write output: %w", registry.Name, operation, io.ErrShortWrite)
		}
	}
	return true, nil
}

func containerRegistryConfigToProto(registry config.CIRegistry) *pb.ContainerRegistryConfig {
	typed := &pb.ContainerRegistryConfig{
		Name: registry.Name, Type: registry.Type, Path: registry.Path, ApiBaseUrl: registry.APIBaseURL,
	}
	if registry.Auth != nil {
		typed.Auth = &pb.ContainerRegistryAuth{Env: registry.Auth.Env, File: registry.Auth.File, AwsProfile: registry.Auth.AWSProfile}
		if registry.Auth.Vault != nil {
			typed.Auth.Vault = &pb.ContainerRegistryVaultAuth{Address: registry.Auth.Vault.Address, Path: registry.Auth.Vault.Path}
		}
	}
	if registry.Retention != nil {
		typed.Retention = &pb.ContainerRegistryRetention{
			KeepLatest: int64(registry.Retention.KeepLatest), UntaggedTtl: registry.Retention.UntaggedTTL, Schedule: registry.Retention.Schedule,
		}
	}
	return typed
}

type credentialIssuerRequest struct {
	PluginDir       string
	StateDir        string
	LockDir         string
	Source          string
	LogicalName     string
	ConfigJSON      []byte
	PreparationKey  string
	AckSingleWriter bool
	NonInteractive  bool
	BeforeIssue     func(context.Context, bool) error
}

type credentialIssueResult struct {
	OperationID         string
	Identifier          string
	IdentifierSensitive bool
	Outputs             map[string][]byte
	DeletePrevious      func(identifier string) error
	Finalize            func(storeError error) error
}

type credentialOperationStatus string

const (
	credentialOperationPreparing       credentialOperationStatus = "preparing"
	credentialOperationStarted         credentialOperationStatus = "started"
	credentialOperationIssued          credentialOperationStatus = "issued"
	credentialOperationStored          credentialOperationStatus = "stored"
	credentialOperationStoreFailed     credentialOperationStatus = "store_failed"
	credentialOperationUnknown         credentialOperationStatus = "unknown"
	credentialOperationUnknownCreated  credentialOperationStatus = "unknown_created"
	credentialOperationAmbiguous       credentialOperationStatus = "ambiguous"
	credentialOperationRollbackUnknown credentialOperationStatus = "rollback_unknown"
	credentialOperationRolledBack      credentialOperationStatus = "rolled_back"
)

type credentialOperationAudit struct {
	OperationID     string                           `json:"operationId"`
	PluginName      string                           `json:"pluginName"`
	PluginVersion   string                           `json:"pluginVersion"`
	ConcurrencyMode config.CredentialConcurrencyMode `json:"concurrencyMode"`
	Acknowledgement string                           `json:"acknowledgement"`
	Status          credentialOperationStatus        `json:"status"`
	At              time.Time                        `json:"at"`
}

type credentialOperationState struct {
	OperationID         string                           `json:"operationId"`
	Source              string                           `json:"source"`
	LogicalName         string                           `json:"logicalName"`
	PluginName          string                           `json:"pluginName"`
	PluginVersion       string                           `json:"pluginVersion"`
	ConcurrencyMode     config.CredentialConcurrencyMode `json:"concurrencyMode"`
	Acknowledgement     string                           `json:"acknowledgement"`
	Status              credentialOperationStatus        `json:"status"`
	Identifier          string                           `json:"identifier,omitempty"`
	IdentifierSensitive bool                             `json:"identifierSensitive,omitempty"`
	RollbackOperationID string                           `json:"rollbackOperationId,omitempty"`
	RequestDigest       string                           `json:"requestDigest,omitempty"`
	Audit               []credentialOperationAudit       `json:"audit"`
}

var resolveCredentialIssuerCapability = discoverCredentialIssuerCapability

var credentialSingleWriterConfirm = func(source string) (bool, error) {
	return confirmAction(fmt.Sprintf("Credential source %q requires a single writer. Continue?", source), false, os.Stderr, nil)
}

var credentialReconcileSleep = func(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var (
	credentialReconcileAttempts          = 3
	credentialReconcilePageLimit         = 10
	credentialReconcileDelay             = 250 * time.Millisecond
	persistCredentialReconciliationState = persistCredentialOperationState
	credentialIssueTimeout               = 2 * time.Minute
	providerCommandOperationTimeout      = 30 * time.Minute
	providerCommandContext               = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
)

func boundedProviderCommandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	baseCtx, stop := providerCommandContext()
	ctx, cancel := context.WithTimeout(baseCtx, timeout)
	return ctx, func() {
		cancel()
		stop()
	}
}

func discoverCredentialIssuerCapability(ctx context.Context, pluginDir, source string) (pb.CredentialIssuerClient, func(), config.CredentialSourceDecl, string, string, bool, error) {
	pluginDir = providerPluginDir(pluginDir)
	index, err := loadProviderCapabilityIndex(pluginDir)
	if err != nil {
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, err
	}
	route, err := index.selectCredentialSource(source)
	if err != nil {
		var notFound providerCapabilityNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil, config.CredentialSourceDecl{}, "", "", false, nil
		}
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, err
	}

	manager := external.NewExternalPluginManager(pluginDir, log.New(io.Discard, "", 0))
	if err := manager.StageCredentialResolvers(); err != nil {
		manager.Shutdown()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("stage unrelated credential resolvers before credential issuer discovery: %w", err)
	}
	adapter, err := manager.LoadPluginContext(ctx, route.PluginInstallName)
	if err != nil {
		manager.Shutdown()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("load plugin %q for credential source %q failed; provider error text suppressed", route.PluginName, source)
	}
	closePlugin := func() { _ = manager.ShutdownContext(ctx) }
	if adapter.Conn() == nil {
		closePlugin()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("plugin %q has no gRPC connection", route.PluginName)
	}
	if !contractRegistryAdvertises(adapter.ContractRegistry(), pb.CredentialIssuer_ServiceDesc.ServiceName) {
		closePlugin()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("plugin %q declares credential source %q but does not advertise CredentialIssuer", route.PluginName, source)
	}

	client := pb.NewCredentialIssuerClient(adapter.Conn())
	describeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	declarations, err := client.DescribeSources(describeCtx, &pb.CredentialSourceDeclarationsRequest{})
	cancel()
	if err != nil {
		closePlugin()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("plugin %q: %w", route.PluginName, providerCapabilityTransportError("CredentialIssuer.DescribeSources", err))
	}
	if declarations.GetError() != nil {
		closePlugin()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("plugin %q CredentialIssuer.DescribeSources: %s; provider error text suppressed", route.PluginName, declarations.GetError().GetCode())
	}
	failures := compareProviderDeclarationsWithRuntime(
		config.ProviderDeclarations{CredentialSources: route.Declarations.CredentialSources},
		providerRuntimeDeclarations{
			AdvertisedServices: map[string]bool{pb.CredentialIssuer_ServiceDesc.ServiceName: true},
			CredentialSources:  declarations.GetSources(),
		})
	if len(failures) > 0 {
		closePlugin()
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, fmt.Errorf("plugin %q credential source manifest/runtime mismatch: %s", route.PluginName, strings.Join(failures, "; "))
	}
	return client, closePlugin, *route.CredentialSource, route.PluginName, route.PluginVersion, true, nil
}

func runCredentialIssuerCapability(ctx context.Context, request credentialIssuerRequest) (*credentialIssueResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	client, closePlugin, declaration, pluginName, pluginVersion, found, err := resolveCredentialIssuerCapability(ctx, request.PluginDir, request.Source)
	if err != nil || !found {
		return nil, found, err
	}
	var closeOnce sync.Once
	closeSelectedPlugin := func() {
		closeOnce.Do(func() {
			if closePlugin != nil {
				closePlugin()
			}
		})
	}
	handedOff := false
	defer func() {
		if !handedOff {
			closeSelectedPlugin()
		}
	}()
	acknowledgement := "not_required"
	if declaration.ConcurrencyMode == config.CredentialConcurrencySingleWriter && !request.AckSingleWriter {
		if request.NonInteractive {
			return nil, true, fmt.Errorf("credential source %q requires single-writer operation serialization; non-interactive use requires --ack-single-writer", request.Source)
		}
		confirmed, confirmErr := credentialSingleWriterConfirm(request.Source)
		if confirmErr != nil {
			return nil, true, fmt.Errorf("confirm single-writer credential issuance: %w", confirmErr)
		}
		if !confirmed {
			return nil, true, fmt.Errorf("single-writer credential issuance cancelled")
		}
		acknowledgement = "interactive_confirmed"
	} else if declaration.ConcurrencyMode == config.CredentialConcurrencySingleWriter {
		acknowledgement = "flag_acknowledged"
	}
	if request.StateDir == "" {
		return nil, true, fmt.Errorf("credential operation state directory is required")
	}
	if strings.TrimSpace(request.LogicalName) == "" {
		return nil, true, fmt.Errorf("credential logical name is required")
	}

	lockDir := request.LockDir
	if lockDir == "" {
		lockDir = request.StateDir
	}
	release, err := acquireCredentialOperationLock(lockDir, request.Source, request.LogicalName)
	if err != nil {
		return nil, true, err
	}
	lockHandedOff := false
	defer func() {
		if !lockHandedOff {
			release()
		}
	}()

	state, err := loadCredentialOperationState(request.StateDir, request.Source, request.LogicalName)
	requestDigest := credentialIssuerRequestDigest(request)
	var preservedAudit []credentialOperationAudit
	resumingPreparation := false
	switch {
	case err == nil:
		switch state.Status {
		case credentialOperationPreparing:
			if request.BeforeIssue == nil {
				return nil, true, fmt.Errorf("credential operation %s requires its pre-Issue preparation before it can resume", state.OperationID)
			}
			if state.PluginName != pluginName || state.PluginVersion != pluginVersion || state.ConcurrencyMode != declaration.ConcurrencyMode {
				return nil, true, fmt.Errorf("credential operation %s preparation owner changed; inspect durable state before retrying", state.OperationID)
			}
			if state.RequestDigest == "" || state.RequestDigest != requestDigest {
				return nil, true, fmt.Errorf("credential operation %s preparation inputs changed; inspect durable state before retrying", state.OperationID)
			}
			resumingPreparation = true
		case credentialOperationStarted:
			return nil, true, reconcileCredentialOperation(ctx, client, request, state, credentialIdentifierSensitivity(declaration))
		case credentialOperationStored, credentialOperationRolledBack:
			preservedAudit = append(preservedAudit, state.Audit...)
		default:
			return nil, true, uncertainCredentialOperationError(state)
		}
	case !os.IsNotExist(err):
		return nil, true, err
	}

	if !resumingPreparation {
		operationID, operationIDErr := newCredentialOperationID()
		if operationIDErr != nil {
			return nil, true, operationIDErr
		}
		state = &credentialOperationState{
			OperationID: operationID, Source: request.Source, LogicalName: request.LogicalName,
			PluginName: pluginName, PluginVersion: pluginVersion, ConcurrencyMode: declaration.ConcurrencyMode,
			Acknowledgement: acknowledgement, RequestDigest: requestDigest, Audit: preservedAudit,
		}
		initialStatus := credentialOperationStarted
		if request.BeforeIssue != nil {
			initialStatus = credentialOperationPreparing
		}
		if err := persistCredentialOperationState(request.StateDir, state, initialStatus); err != nil {
			return nil, true, fmt.Errorf("persist credential operation before Issue: %w", err)
		}
	}

	if request.BeforeIssue != nil {
		if err := ctx.Err(); err != nil {
			return nil, true, err
		}
		if err := request.BeforeIssue(ctx, credentialIdentifierSensitivity(declaration)); err != nil {
			return nil, true, fmt.Errorf("credential pre-Issue preparation: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, true, err
		}
		if err := persistCredentialOperationState(request.StateDir, state, credentialOperationStarted); err != nil {
			return nil, true, fmt.Errorf("persist credential operation after pre-Issue preparation: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	issueCtx, issueCancel := context.WithTimeout(ctx, credentialIssueTimeout)
	response, issueErr := client.Issue(issueCtx, &pb.CredentialIssueRequest{
		OperationId: state.OperationID,
		Source:      request.Source,
		Selector:    &pb.CredentialSelector{LogicalName: request.LogicalName, OperationId: state.OperationID},
		ConfigJson:  append([]byte(nil), request.ConfigJSON...),
	})
	issueCancel()
	if issueErr != nil {
		reconcileErr := reconcileCredentialOperation(ctx, client, request, state, credentialIdentifierSensitivity(declaration))
		return nil, true, errors.Join(
			fmt.Errorf("credential Issue response uncertain: %w", providerCapabilityTransportError("CredentialIssuer.Issue", issueErr)),
			reconcileErr,
		)
	}
	if response.GetError() != nil {
		status := credentialStatusFromReconciliation(response.GetError().GetReconciliationState())
		identifier, sensitive := firstCredentialErrorIdentifier(response.GetError())
		setCredentialStateIdentifier(state, identifier, sensitive || credentialIdentifierSensitivity(declaration))
		if err := persistCredentialOperationState(request.StateDir, state, status); err != nil {
			return nil, true, err
		}
		return nil, true, fmt.Errorf("credential Issue %s; provider error text suppressed", response.GetError().GetCode())
	}
	if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED {
		status := credentialStatusFromReconciliation(response.GetReconciliationState())
		if err := persistCredentialOperationState(request.StateDir, state, status); err != nil {
			return nil, true, err
		}
		return nil, true, uncertainCredentialOperationError(state)
	}

	outputs, err := validateCredentialIssueOutputs(declaration, response)
	if err != nil {
		setCredentialStateIdentifier(state, response.GetIdentifier(), response.GetIdentifierSensitive() || credentialIdentifierSensitivity(declaration))
		if persistErr := persistCredentialOperationState(request.StateDir, state, credentialOperationUnknownCreated); persistErr != nil {
			return nil, true, errors.Join(err, persistErr)
		}
		return nil, true, err
	}
	setCredentialStateIdentifier(state, response.GetIdentifier(), response.GetIdentifierSensitive())
	if err := persistCredentialOperationState(request.StateDir, state, credentialOperationIssued); err != nil {
		return nil, true, err
	}

	result := &credentialIssueResult{
		OperationID:         state.OperationID,
		Identifier:          response.GetIdentifier(),
		IdentifierSensitive: credentialIdentifierSensitivity(declaration),
		Outputs:             outputs,
	}
	var deletePreviousOnce sync.Once
	var deletePreviousErr error
	result.DeletePrevious = func(identifier string) error {
		deletePreviousOnce.Do(func() {
			if err := ctx.Err(); err != nil {
				deletePreviousErr = err
				return
			}
			if strings.TrimSpace(identifier) == "" {
				deletePreviousErr = fmt.Errorf("previous credential identifier is required")
				return
			}
			deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			deleteResponse, deleteErr := client.Delete(deleteCtx, &pb.CredentialDeleteRequest{
				OperationId: state.OperationID + "-delete-previous",
				Source:      request.Source,
				Identifier:  identifier,
			})
			if deleteErr != nil {
				deletePreviousErr = providerCapabilityTransportError("CredentialIssuer.DeletePrevious", deleteErr)
				return
			}
			if deleteResponse == nil {
				deletePreviousErr = fmt.Errorf("credential previous Delete empty_response; provider error text suppressed")
				return
			}
			if deleteResponse.GetError() != nil {
				deletePreviousErr = fmt.Errorf("credential previous Delete %s; provider error text suppressed", deleteResponse.GetError().GetCode())
				return
			}
			if deleteResponse.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED || deleteResponse.GetIdentifier() != identifier {
				deletePreviousErr = fmt.Errorf("credential previous Delete returned an uncertain or mismatched acknowledgement; automatic retry blocked")
			}
		})
		return deletePreviousErr
	}
	var finalizeOnce sync.Once
	var finalizeErr error
	result.Finalize = func(storeError error) error {
		finalizeOnce.Do(func() {
			defer release()
			defer closeSelectedPlugin()
			finalState, err := loadCredentialOperationState(request.StateDir, request.Source, request.LogicalName)
			if err != nil {
				finalizeErr = err
				return
			}
			if storeError == nil {
				finalizeErr = persistCredentialOperationState(request.StateDir, finalState, credentialOperationStored)
				return
			}
			finalState.RollbackOperationID = state.OperationID + "-rollback"
			if err := persistCredentialOperationState(request.StateDir, finalState, credentialOperationStoreFailed); err != nil {
				finalizeErr = fmt.Errorf("persist credential store failure: %w", err)
				return
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				persistErr := persistCredentialOperationState(request.StateDir, finalState, credentialOperationRollbackUnknown)
				finalizeErr = errors.Join(ctxErr, persistErr)
				return
			}

			rollbackCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			rollbackResponse, rollbackErr := client.Delete(rollbackCtx, &pb.CredentialDeleteRequest{
				OperationId: finalState.RollbackOperationID,
				Source:      request.Source,
				Identifier:  response.GetIdentifier(),
			})
			if rollbackErr != nil {
				persistErr := persistCredentialOperationState(request.StateDir, finalState, credentialOperationRollbackUnknown)
				finalizeErr = errors.Join(providerCapabilityTransportError("CredentialIssuer.RollbackDelete", rollbackErr), persistErr)
				return
			}
			if rollbackResponse.GetError() != nil {
				persistErr := persistCredentialOperationState(request.StateDir, finalState, credentialOperationRollbackUnknown)
				finalizeErr = errors.Join(fmt.Errorf("credential rollback Delete %s; provider error text suppressed", rollbackResponse.GetError().GetCode()), persistErr)
				return
			}
			if rollbackResponse.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED || rollbackResponse.GetIdentifier() != response.GetIdentifier() {
				persistErr := persistCredentialOperationState(request.StateDir, finalState, credentialOperationRollbackUnknown)
				finalizeErr = errors.Join(fmt.Errorf("credential rollback Delete returned an uncertain or mismatched acknowledgement; automatic retry blocked"), persistErr)
				return
			}
			finalizeErr = persistCredentialOperationState(request.StateDir, finalState, credentialOperationRolledBack)
		})
		return finalizeErr
	}
	handedOff = true
	lockHandedOff = true
	return result, true, nil
}

func credentialIssuerRequestDigest(request credentialIssuerRequest) string {
	digest := sha256.Sum256([]byte("wfctl-credential-issue-v1\x00" + request.Source + "\x00" + request.LogicalName + "\x00" + request.PreparationKey + "\x00" + string(request.ConfigJSON)))
	return fmt.Sprintf("%x", digest[:])
}

func reconcileCredentialOperation(parent context.Context, client pb.CredentialIssuerClient, request credentialIssuerRequest, state *credentialOperationState, identifierSensitive bool) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var records []*pb.CredentialRecord
	for attempt := 0; attempt < credentialReconcileAttempts; attempt++ {
		records = nil
		identifiers := make(map[string]struct{})
		pageToken := ""
		complete := false
		for page := 0; page < credentialReconcilePageLimit; page++ {
			response, err := client.List(ctx, &pb.CredentialListRequest{
				Source:    request.Source,
				Selector:  &pb.CredentialSelector{LogicalName: request.LogicalName, OperationId: state.OperationID},
				PageToken: pageToken,
				PageSize:  100,
			})
			if err != nil {
				return persistCredentialReconciliationFailure(request, state, providerCapabilityTransportError("CredentialIssuer.ReconcileList", err))
			}
			if response.GetError() != nil {
				return persistCredentialReconciliationFailure(request, state, fmt.Errorf("credential reconciliation List %s; provider error text suppressed", response.GetError().GetCode()))
			}
			for _, record := range response.GetCredentials() {
				if record == nil || record.GetIdentifier() == "" || record.GetLogicalName() != request.LogicalName || record.GetOperationId() != state.OperationID {
					return persistCredentialReconciliationFailure(request, state, fmt.Errorf("credential reconciliation inventory selector mismatch; automatic retry blocked"))
				}
				if _, duplicate := identifiers[record.GetIdentifier()]; duplicate {
					return persistCredentialReconciliationFailure(request, state, fmt.Errorf("credential reconciliation inventory contains duplicate identifiers; automatic retry blocked"))
				}
				identifiers[record.GetIdentifier()] = struct{}{}
				records = append(records, record)
			}
			pageToken = response.GetNextPageToken()
			if pageToken == "" {
				complete = true
				break
			}
		}
		if !complete {
			return persistCredentialReconciliationFailure(request, state, fmt.Errorf("credential reconciliation inventory exceeded the bounded page limit; automatic retry blocked"))
		}
		if len(records) > 0 || attempt == credentialReconcileAttempts-1 {
			break
		}
		if err := credentialReconcileSleep(ctx, credentialReconcileDelay); err != nil {
			return persistCredentialReconciliationFailure(request, state, fmt.Errorf("credential reconciliation window ended; automatic retry blocked"))
		}
	}
	status := credentialOperationUnknown
	switch len(records) {
	case 0:
		status = credentialOperationUnknown
	case 1:
		status = credentialOperationUnknownCreated
		setCredentialStateIdentifier(state, records[0].GetIdentifier(), identifierSensitive || records[0].GetIdentifierSensitive())
	default:
		status = credentialOperationAmbiguous
	}
	state.Status = status
	diagnosis := uncertainCredentialOperationError(state)
	if err := persistCredentialReconciliationState(request.StateDir, state, status); err != nil {
		return errors.Join(diagnosis, err)
	}
	return diagnosis
}

func persistCredentialReconciliationFailure(request credentialIssuerRequest, state *credentialOperationState, diagnosis error) error {
	persistErr := persistCredentialReconciliationState(request.StateDir, state, credentialOperationUnknown)
	return errors.Join(diagnosis, persistErr)
}

func credentialStatusFromReconciliation(state pb.CredentialReconciliationState) credentialOperationStatus {
	switch state {
	case pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED:
		return credentialOperationUnknownCreated
	case pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_AMBIGUOUS:
		return credentialOperationAmbiguous
	default:
		return credentialOperationUnknown
	}
}

func firstCredentialErrorIdentifier(operationError *pb.CredentialOperationError) (string, bool) {
	if len(operationError.GetIdentifiers()) != 1 {
		return "", false
	}
	return operationError.GetIdentifiers()[0].GetValue(), operationError.GetIdentifiers()[0].GetSensitive()
}

func setCredentialStateIdentifier(state *credentialOperationState, identifier string, sensitive bool) {
	state.Identifier = identifier
	state.IdentifierSensitive = sensitive
	if sensitive {
		state.Identifier = ""
	}
}

func credentialIdentifierSensitivity(declaration config.CredentialSourceDecl) bool {
	for _, output := range declaration.Outputs {
		if output.Key == declaration.IdentifierKey {
			return output.IsSensitive()
		}
	}
	return true
}

func uncertainCredentialOperationError(state *credentialOperationState) error {
	guidance := "inspect the provider inventory and operation state before retrying"
	if state.Identifier != "" && !state.IdentifierSensitive {
		guidance = fmt.Sprintf("credential identifier %q requires recovery or cleanup before retrying", state.Identifier)
	}
	return fmt.Errorf("credential operation %s is %s; %s (automatic retry blocked)", state.OperationID, state.Status, guidance)
}

func validateCredentialIssueOutputs(declaration config.CredentialSourceDecl, response *pb.CredentialIssueResponse) (map[string][]byte, error) {
	declared := make(map[string]config.CredentialOutputDecl, len(declaration.Outputs))
	for _, output := range declaration.Outputs {
		declared[output.Key] = output
	}
	outputs := make(map[string][]byte, len(response.GetOutputs()))
	for _, output := range response.GetOutputs() {
		manifestOutput, ok := declared[output.GetKey()]
		if !ok {
			return nil, fmt.Errorf("credential Issue returned undeclared output %q", output.GetKey())
		}
		if _, duplicate := outputs[output.GetKey()]; duplicate {
			return nil, fmt.Errorf("credential Issue returned duplicate output %q", output.GetKey())
		}
		if output.GetSensitive() != manifestOutput.IsSensitive() {
			return nil, fmt.Errorf("credential Issue output %q sensitivity differs from manifest", output.GetKey())
		}
		outputs[output.GetKey()] = append([]byte(nil), output.GetValue()...)
	}
	for key := range declared {
		if _, ok := outputs[key]; !ok {
			return nil, fmt.Errorf("credential Issue omitted declared output %q", key)
		}
	}
	identifierValue, ok := outputs[declaration.IdentifierKey]
	if !ok || !utf8.Valid(identifierValue) || strings.TrimSpace(string(identifierValue)) == "" || string(identifierValue) != response.GetIdentifier() {
		return nil, fmt.Errorf("credential Issue identifier does not match declared identifier output %q", declaration.IdentifierKey)
	}
	if response.GetIdentifierSensitive() != declared[declaration.IdentifierKey].IsSensitive() {
		return nil, fmt.Errorf("credential Issue identifier sensitivity differs from declared identifier output %q", declaration.IdentifierKey)
	}
	return outputs, nil
}

func credentialOperationFile(stateDir, source, logicalName string) string {
	digest := sha256.Sum256([]byte(source + "\x00" + logicalName))
	return filepath.Join(stateDir, fmt.Sprintf("%x.json", digest[:16]))
}

func credentialOperationStateDirForConfig(configFile string) (string, error) {
	base, err := defaultStateDir()
	if err != nil {
		return "", err
	}
	absoluteConfig, err := filepath.Abs(configFile)
	if err != nil {
		return "", fmt.Errorf("resolve absolute config path: %w", err)
	}
	canonicalConfig := filepath.Clean(absoluteConfig)
	if resolvedConfig, resolveErr := filepath.EvalSymlinks(canonicalConfig); resolveErr == nil {
		canonicalConfig = resolvedConfig
	} else if resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(canonicalConfig)); parentErr == nil {
		canonicalConfig = filepath.Join(resolvedParent, filepath.Base(canonicalConfig))
	}
	digest := sha256.Sum256([]byte(filepath.Dir(canonicalConfig)))
	return filepath.Join(base, "provider-operations", fmt.Sprintf("%x", digest[:16])), nil
}

func credentialOperationLockDir() (string, error) {
	base, err := defaultStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "provider-operation-locks"), nil
}

var errCredentialOperationLocked = errors.New("credential operation is already locked")

func acquireCredentialOperationLock(lockDir, source, logicalName string) (func(), error) {
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return nil, err
	}
	lockPath := strings.TrimSuffix(credentialOperationFile(lockDir, source, logicalName), ".json") + ".lock"
	file, err := lockCredentialOperationFile(lockPath)
	if err != nil {
		if errors.Is(err, errCredentialOperationLocked) {
			return nil, fmt.Errorf("credential operation for source %q logical name %q is already locked by a local writer", source, logicalName)
		}
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unlockCredentialOperationFile(file) }) }, nil
}

func loadCredentialOperationState(stateDir, source, logicalName string) (*credentialOperationState, error) {
	data, err := os.ReadFile(credentialOperationFile(stateDir, source, logicalName))
	if err != nil {
		return nil, err
	}
	var state credentialOperationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse credential operation state: %w", err)
	}
	if state.Source != source || state.LogicalName != logicalName || state.OperationID == "" {
		return nil, fmt.Errorf("credential operation state identity mismatch")
	}
	return &state, nil
}

func persistCredentialOperationState(stateDir string, state *credentialOperationState, status credentialOperationStatus) error {
	state.Status = status
	auditOperationID := state.OperationID
	if (status == credentialOperationRollbackUnknown || status == credentialOperationRolledBack) && state.RollbackOperationID != "" {
		auditOperationID = state.RollbackOperationID
	}
	state.Audit = append(state.Audit, credentialOperationAudit{
		OperationID: auditOperationID, PluginName: state.PluginName, PluginVersion: state.PluginVersion,
		ConcurrencyMode: state.ConcurrencyMode, Acknowledgement: state.Acknowledgement,
		Status: status, At: time.Now().UTC(),
	})
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := credentialOperationFile(stateDir, state.Source, state.LogicalName)
	return writeDurablePrivateFile(stateDir, path, ".credential-operation-*", data)
}

func writeDurablePrivateFile(dir, path, tempPattern string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("durable private state path %q must be a real directory", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return durableReplaceCredentialOperationState(tmpPath, path)
}

func newCredentialOperationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate credential operation ID: %w", err)
	}
	return fmt.Sprintf("%x", id[:]), nil
}

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// providerRuntimeDeclarations is the verifier's typed view of a plugin's
// authoritative runtime declarations. The service registry remains separate:
// serving a family without declaring it on disk, or declaring it without
// serving the typed RPC, are both hard failures.
type providerRuntimeDeclarations struct {
	AdvertisedServices      map[string]bool
	CredentialSources       []*pb.CredentialSourceDeclaration
	CredentialResolvers     []*pb.CredentialResolverDeclaration
	KubernetesResourceTypes []string
	ContainerRegistries     []*pb.ContainerRegistryDeclaration
	SecretStores            []*pb.SecretStoreDeclaration
}

func compareProviderDeclarationsWithRuntime(declared config.ProviderDeclarations, runtime providerRuntimeDeclarations) []string {
	var failures []string
	failures = append(failures, compareProviderFamily(
		"credentialSources", len(declared.CredentialSources) > 0,
		runtime.AdvertisedServices[pb.CredentialIssuer_ServiceDesc.ServiceName],
		canonicalCredentialSources(declared.CredentialSources),
		canonicalRuntimeCredentialSources(runtime.CredentialSources))...)
	failures = append(failures, compareProviderFamily(
		"credentialResolvers", len(declared.CredentialResolvers) > 0,
		runtime.AdvertisedServices[pb.CredentialResolver_ServiceDesc.ServiceName],
		canonicalCredentialResolvers(declared.CredentialResolvers),
		canonicalRuntimeCredentialResolvers(runtime.CredentialResolvers))...)

	declaredResourceTypes := make([]string, 0, len(declared.KubernetesBackends))
	for _, backend := range declared.KubernetesBackends {
		declaredResourceTypes = append(declaredResourceTypes, backend.ResourceType)
	}
	sort.Strings(declaredResourceTypes)
	runtimeResourceTypes := append([]string(nil), runtime.KubernetesResourceTypes...)
	sort.Strings(runtimeResourceTypes)
	if len(declared.KubernetesBackends) > 0 {
		if !runtime.AdvertisedServices[pb.ResourceDriver_ServiceDesc.ServiceName] {
			failures = append(failures, "kubernetesBackends: plugin.json declares capabilities but binary does not advertise ResourceDriver")
		} else {
			served := make(map[string]struct{}, len(runtimeResourceTypes))
			for _, resourceType := range runtimeResourceTypes {
				served[resourceType] = struct{}{}
			}
			for _, resourceType := range declaredResourceTypes {
				if _, ok := served[resourceType]; !ok {
					failures = append(failures, fmt.Sprintf("kubernetesBackends: runtime ResourceDriver does not serve declared resourceType %q", resourceType))
				}
			}
		}
	}

	failures = append(failures, compareProviderFamily(
		"containerRegistries", len(declared.ContainerRegistries) > 0,
		runtime.AdvertisedServices[pb.ContainerRegistry_ServiceDesc.ServiceName],
		canonicalContainerRegistries(declared.ContainerRegistries),
		canonicalRuntimeContainerRegistries(runtime.ContainerRegistries))...)
	failures = append(failures, compareProviderFamily(
		"secretStores", len(declared.SecretStores) > 0,
		runtime.AdvertisedServices[pb.SecretStore_ServiceDesc.ServiceName],
		canonicalSecretStores(declared.SecretStores),
		canonicalRuntimeSecretStores(runtime.SecretStores))...)
	return failures
}

func compareProviderFamily(family string, declared, advertised bool, disk, runtime any) []string {
	var failures []string
	if declared && !advertised {
		failures = append(failures, fmt.Sprintf("%s: plugin.json declares capabilities but binary does not advertise the typed service", family))
	}
	if !declared && advertised {
		failures = append(failures, fmt.Sprintf("%s: binary serves an undeclared typed capability", family))
	}
	if !reflect.DeepEqual(disk, runtime) {
		failures = append(failures, fmt.Sprintf("%s: runtime declarations are undeclared or differ from plugin.json (disk=%v runtime=%v)", family, disk, runtime))
	}
	return failures
}

func canonicalCredentialSources(values []config.CredentialSourceDecl) []config.CredentialSourceDecl {
	if len(values) == 0 {
		return nil
	}
	result := append([]config.CredentialSourceDecl(nil), values...)
	for i := range result {
		result[i].Outputs = append([]config.CredentialOutputDecl(nil), result[i].Outputs...)
		for j := range result[i].Outputs {
			sensitive := result[i].Outputs[j].IsSensitive()
			result[i].Outputs[j].Sensitive = &sensitive
		}
		sort.Slice(result[i].Outputs, func(a, b int) bool { return result[i].Outputs[a].Key < result[i].Outputs[b].Key })
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Source < result[j].Source })
	return result
}

func canonicalRuntimeCredentialSources(values []*pb.CredentialSourceDeclaration) []config.CredentialSourceDecl {
	if len(values) == 0 {
		return nil
	}
	result := make([]config.CredentialSourceDecl, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		mode := config.CredentialConcurrencyMode("unspecified")
		switch value.GetConcurrencyMode() {
		case pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT:
			mode = config.CredentialConcurrencyProviderIdempotent
		case pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_SINGLE_WRITER_REQUIRED:
			mode = config.CredentialConcurrencySingleWriter
		}
		outputs := make([]config.CredentialOutputDecl, 0, len(value.GetOutputs()))
		for _, output := range value.GetOutputs() {
			if output == nil {
				continue
			}
			sensitive := output.GetSensitive()
			outputs = append(outputs, config.CredentialOutputDecl{Key: output.GetKey(), Sensitive: &sensitive})
		}
		result = append(result, config.CredentialSourceDecl{Source: value.GetSource(), ConcurrencyMode: mode, Outputs: outputs, IdentifierKey: value.GetIdentifierKey()})
	}
	return canonicalCredentialSources(result)
}

func canonicalCredentialResolvers(values []config.CredentialResolverDecl) []config.CredentialResolverDecl {
	if len(values) == 0 {
		return nil
	}
	result := append([]config.CredentialResolverDecl(nil), values...)
	for i := range result {
		result[i].CredentialTypes = append([]string(nil), result[i].CredentialTypes...)
		sort.Strings(result[i].CredentialTypes)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Provider < result[j].Provider })
	return result
}

func canonicalRuntimeCredentialResolvers(values []*pb.CredentialResolverDeclaration) []config.CredentialResolverDecl {
	if len(values) == 0 {
		return nil
	}
	result := make([]config.CredentialResolverDecl, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, config.CredentialResolverDecl{Provider: value.GetProvider(), CredentialTypes: append([]string(nil), value.GetCredentialTypes()...)})
		}
	}
	return canonicalCredentialResolvers(result)
}

func canonicalContainerRegistries(values []config.ContainerRegistryDecl) []config.ContainerRegistryDecl {
	if len(values) == 0 {
		return nil
	}
	result := append([]config.ContainerRegistryDecl(nil), values...)
	for i := range result {
		result[i].Operations = append([]string(nil), result[i].Operations...)
		sort.Strings(result[i].Operations)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result
}

func canonicalRuntimeContainerRegistries(values []*pb.ContainerRegistryDeclaration) []config.ContainerRegistryDecl {
	if len(values) == 0 {
		return nil
	}
	result := make([]config.ContainerRegistryDecl, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, config.ContainerRegistryDecl{Type: value.GetType(), Operations: append([]string(nil), value.GetOperations()...)})
		}
	}
	return canonicalContainerRegistries(result)
}

func canonicalSecretStores(values []config.SecretStoreDecl) []config.SecretStoreDecl {
	if len(values) == 0 {
		return nil
	}
	result := append([]config.SecretStoreDecl(nil), values...)
	for i := range result {
		result[i].Operations = append([]string(nil), result[i].Operations...)
		result[i].Scopes = append([]string(nil), result[i].Scopes...)
		sort.Strings(result[i].Operations)
		sort.Strings(result[i].Scopes)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result
}

func canonicalRuntimeSecretStores(values []*pb.SecretStoreDeclaration) []config.SecretStoreDecl {
	if len(values) == 0 {
		return nil
	}
	result := make([]config.SecretStoreDecl, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, config.SecretStoreDecl{Type: value.GetType(), Operations: append([]string(nil), value.GetOperations()...), Scopes: append([]string(nil), value.GetScopes()...)})
		}
	}
	return canonicalSecretStores(result)
}
