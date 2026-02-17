// Package mock provides configurable test doubles for all platform interfaces.
// Each mock struct uses function pointers for customizable behavior and tracks
// all method calls for assertion in tests.
package mock

import (
	"context"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// MockCall records a single method invocation for assertion purposes.
type MockCall struct {
	Method string
	Args   []any
}

// --- MockProvider ---

// MockProvider implements platform.Provider with configurable function pointers.
type MockProvider struct {
	mu sync.RWMutex

	NameFn             func() string
	VersionFn          func() string
	InitializeFn       func(ctx context.Context, config map[string]any) error
	CapabilitiesFn     func() []platform.CapabilityType
	MapCapabilityFn    func(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error)
	ResourceDriverFn   func(resourceType string) (platform.ResourceDriver, error)
	CredentialBrokerFn func() platform.CredentialBroker
	StateStoreFn       func() platform.StateStore
	HealthyFn          func(ctx context.Context) error
	CloseFn            func() error

	Calls []MockCall
}

// NewMockProvider returns a MockProvider with sensible defaults.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// GetCalls returns a copy of the recorded calls.
func (m *MockProvider) GetCalls() []MockCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockProvider) Name() string {
	m.record("Name")
	if m.NameFn != nil {
		return m.NameFn()
	}
	return "mock"
}

func (m *MockProvider) Version() string {
	m.record("Version")
	if m.VersionFn != nil {
		return m.VersionFn()
	}
	return "0.0.0-mock"
}

func (m *MockProvider) Initialize(ctx context.Context, config map[string]any) error {
	m.record("Initialize", config)
	if m.InitializeFn != nil {
		return m.InitializeFn(ctx, config)
	}
	return nil
}

func (m *MockProvider) Capabilities() []platform.CapabilityType {
	m.record("Capabilities")
	if m.CapabilitiesFn != nil {
		return m.CapabilitiesFn()
	}
	return nil
}

func (m *MockProvider) MapCapability(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	m.record("MapCapability", decl, pctx)
	if m.MapCapabilityFn != nil {
		return m.MapCapabilityFn(ctx, decl, pctx)
	}
	return nil, nil
}

func (m *MockProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	m.record("ResourceDriver", resourceType)
	if m.ResourceDriverFn != nil {
		return m.ResourceDriverFn(resourceType)
	}
	return nil, &platform.ResourceDriverNotFoundError{ResourceType: resourceType, Provider: "mock"}
}

func (m *MockProvider) CredentialBroker() platform.CredentialBroker {
	m.record("CredentialBroker")
	if m.CredentialBrokerFn != nil {
		return m.CredentialBrokerFn()
	}
	return nil
}

func (m *MockProvider) StateStore() platform.StateStore {
	m.record("StateStore")
	if m.StateStoreFn != nil {
		return m.StateStoreFn()
	}
	return nil
}

func (m *MockProvider) Healthy(ctx context.Context) error {
	m.record("Healthy")
	if m.HealthyFn != nil {
		return m.HealthyFn(ctx)
	}
	return nil
}

func (m *MockProvider) Close() error {
	m.record("Close")
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// --- MockResourceDriver ---

// MockResourceDriver implements platform.ResourceDriver with configurable function pointers.
type MockResourceDriver struct {
	mu sync.RWMutex

	resourceType string

	CreateFn      func(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error)
	ReadFn        func(ctx context.Context, name string) (*platform.ResourceOutput, error)
	UpdateFn      func(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error)
	DeleteFn      func(ctx context.Context, name string) error
	HealthCheckFn func(ctx context.Context, name string) (*platform.HealthStatus, error)
	ScaleFn       func(ctx context.Context, name string, scaleParams map[string]any) (*platform.ResourceOutput, error)
	DiffFn        func(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error)

	Calls []MockCall
}

// NewMockResourceDriver returns a MockResourceDriver for the given resource type.
func NewMockResourceDriver(resourceType string) *MockResourceDriver {
	return &MockResourceDriver{resourceType: resourceType}
}

func (m *MockResourceDriver) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// GetCalls returns a copy of the recorded calls.
func (m *MockResourceDriver) GetCalls() []MockCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockResourceDriver) ResourceType() string {
	m.record("ResourceType")
	return m.resourceType
}

func (m *MockResourceDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	m.record("Create", name, properties)
	if m.CreateFn != nil {
		return m.CreateFn(ctx, name, properties)
	}
	return &platform.ResourceOutput{
		Name:       name,
		Type:       m.resourceType,
		Status:     platform.ResourceStatusActive,
		Properties: properties,
		LastSynced: time.Now(),
	}, nil
}

func (m *MockResourceDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	m.record("Read", name)
	if m.ReadFn != nil {
		return m.ReadFn(ctx, name)
	}
	return nil, &platform.ResourceNotFoundError{Name: name}
}

func (m *MockResourceDriver) Update(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error) {
	m.record("Update", name, current, desired)
	if m.UpdateFn != nil {
		return m.UpdateFn(ctx, name, current, desired)
	}
	return &platform.ResourceOutput{
		Name:       name,
		Type:       m.resourceType,
		Status:     platform.ResourceStatusActive,
		Properties: desired,
		LastSynced: time.Now(),
	}, nil
}

func (m *MockResourceDriver) Delete(ctx context.Context, name string) error {
	m.record("Delete", name)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, name)
	}
	return nil
}

func (m *MockResourceDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	m.record("HealthCheck", name)
	if m.HealthCheckFn != nil {
		return m.HealthCheckFn(ctx, name)
	}
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   "mock resource is healthy",
		CheckedAt: time.Now(),
	}, nil
}

func (m *MockResourceDriver) Scale(ctx context.Context, name string, scaleParams map[string]any) (*platform.ResourceOutput, error) {
	m.record("Scale", name, scaleParams)
	if m.ScaleFn != nil {
		return m.ScaleFn(ctx, name, scaleParams)
	}
	return nil, &platform.NotScalableError{ResourceType: m.resourceType}
}

func (m *MockResourceDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	m.record("Diff", name, desired)
	if m.DiffFn != nil {
		return m.DiffFn(ctx, name, desired)
	}
	return nil, nil
}

// --- MockCapabilityMapper ---

// MockCapabilityMapper implements platform.CapabilityMapper with configurable function pointers.
type MockCapabilityMapper struct {
	mu sync.RWMutex

	CanMapFn              func(capabilityType string) bool
	MapFn                 func(decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error)
	ValidateConstraintsFn func(decl platform.CapabilityDeclaration, constraints []platform.Constraint) []platform.ConstraintViolation

	Calls []MockCall
}

// NewMockCapabilityMapper returns a MockCapabilityMapper with sensible defaults.
func NewMockCapabilityMapper() *MockCapabilityMapper {
	return &MockCapabilityMapper{}
}

func (m *MockCapabilityMapper) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// GetCalls returns a copy of the recorded calls.
func (m *MockCapabilityMapper) GetCalls() []MockCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockCapabilityMapper) CanMap(capabilityType string) bool {
	m.record("CanMap", capabilityType)
	if m.CanMapFn != nil {
		return m.CanMapFn(capabilityType)
	}
	return false
}

func (m *MockCapabilityMapper) Map(decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	m.record("Map", decl, pctx)
	if m.MapFn != nil {
		return m.MapFn(decl, pctx)
	}
	return nil, nil
}

func (m *MockCapabilityMapper) ValidateConstraints(decl platform.CapabilityDeclaration, constraints []platform.Constraint) []platform.ConstraintViolation {
	m.record("ValidateConstraints", decl, constraints)
	if m.ValidateConstraintsFn != nil {
		return m.ValidateConstraintsFn(decl, constraints)
	}
	return nil
}

// --- MockCredentialBroker ---

// MockCredentialBroker implements platform.CredentialBroker with configurable function pointers.
type MockCredentialBroker struct {
	mu sync.RWMutex

	IssueCredentialFn   func(ctx context.Context, pctx *platform.PlatformContext, request platform.CredentialRequest) (*platform.CredentialRef, error)
	RevokeCredentialFn  func(ctx context.Context, ref *platform.CredentialRef) error
	ResolveCredentialFn func(ctx context.Context, ref *platform.CredentialRef) (string, error)
	RotateCredentialFn  func(ctx context.Context, ref *platform.CredentialRef) (*platform.CredentialRef, error)
	ListCredentialsFn   func(ctx context.Context, pctx *platform.PlatformContext) ([]*platform.CredentialRef, error)

	Calls []MockCall
}

// NewMockCredentialBroker returns a MockCredentialBroker with sensible defaults.
func NewMockCredentialBroker() *MockCredentialBroker {
	return &MockCredentialBroker{}
}

func (m *MockCredentialBroker) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// GetCalls returns a copy of the recorded calls.
func (m *MockCredentialBroker) GetCalls() []MockCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockCredentialBroker) IssueCredential(ctx context.Context, pctx *platform.PlatformContext, request platform.CredentialRequest) (*platform.CredentialRef, error) {
	m.record("IssueCredential", pctx, request)
	if m.IssueCredentialFn != nil {
		return m.IssueCredentialFn(ctx, pctx, request)
	}
	return &platform.CredentialRef{
		ID:          "mock-cred-" + request.Name,
		Name:        request.Name,
		SecretPath:  "mock/secrets/" + request.Name,
		Provider:    "mock",
		Tier:        pctx.Tier,
		ContextPath: pctx.ContextPath(),
	}, nil
}

func (m *MockCredentialBroker) RevokeCredential(ctx context.Context, ref *platform.CredentialRef) error {
	m.record("RevokeCredential", ref)
	if m.RevokeCredentialFn != nil {
		return m.RevokeCredentialFn(ctx, ref)
	}
	return nil
}

func (m *MockCredentialBroker) ResolveCredential(ctx context.Context, ref *platform.CredentialRef) (string, error) {
	m.record("ResolveCredential", ref)
	if m.ResolveCredentialFn != nil {
		return m.ResolveCredentialFn(ctx, ref)
	}
	return "mock-secret-value", nil
}

func (m *MockCredentialBroker) RotateCredential(ctx context.Context, ref *platform.CredentialRef) (*platform.CredentialRef, error) {
	m.record("RotateCredential", ref)
	if m.RotateCredentialFn != nil {
		return m.RotateCredentialFn(ctx, ref)
	}
	return &platform.CredentialRef{
		ID:          ref.ID + "-rotated",
		Name:        ref.Name,
		SecretPath:  ref.SecretPath,
		Provider:    ref.Provider,
		Tier:        ref.Tier,
		ContextPath: ref.ContextPath,
	}, nil
}

func (m *MockCredentialBroker) ListCredentials(ctx context.Context, pctx *platform.PlatformContext) ([]*platform.CredentialRef, error) {
	m.record("ListCredentials", pctx)
	if m.ListCredentialsFn != nil {
		return m.ListCredentialsFn(ctx, pctx)
	}
	return nil, nil
}

// --- MockStateStore ---

// MockStateStore implements platform.StateStore with configurable function pointers
// and in-memory storage that works out of the box.
type MockStateStore struct {
	mu sync.RWMutex

	// In-memory storage maps (used as defaults when function pointers are nil).
	resources    map[string]map[string]*platform.ResourceOutput // contextPath -> resourceName -> output
	plans        map[string]*platform.Plan                      // planID -> plan
	plansByCtx   map[string][]string                            // contextPath -> []planID (ordered by creation)
	dependencies []platform.DependencyRef
	locks        map[string]bool // contextPath -> locked

	SaveResourceFn   func(ctx context.Context, contextPath string, output *platform.ResourceOutput) error
	GetResourceFn    func(ctx context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error)
	ListResourcesFn  func(ctx context.Context, contextPath string) ([]*platform.ResourceOutput, error)
	DeleteResourceFn func(ctx context.Context, contextPath, resourceName string) error
	SavePlanFn       func(ctx context.Context, plan *platform.Plan) error
	GetPlanFn        func(ctx context.Context, planID string) (*platform.Plan, error)
	ListPlansFn      func(ctx context.Context, contextPath string, limit int) ([]*platform.Plan, error)
	LockFn           func(ctx context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error)
	DependenciesFn   func(ctx context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error)
	AddDependencyFn  func(ctx context.Context, dep platform.DependencyRef) error

	Calls []MockCall
}

// NewMockStateStore returns a MockStateStore with initialized in-memory maps.
func NewMockStateStore() *MockStateStore {
	return &MockStateStore{
		resources:    make(map[string]map[string]*platform.ResourceOutput),
		plans:        make(map[string]*platform.Plan),
		plansByCtx:   make(map[string][]string),
		dependencies: nil,
		locks:        make(map[string]bool),
	}
}

func (m *MockStateStore) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// GetCalls returns a copy of the recorded calls.
func (m *MockStateStore) GetCalls() []MockCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockStateStore) SaveResource(ctx context.Context, contextPath string, output *platform.ResourceOutput) error {
	m.record("SaveResource", contextPath, output)
	if m.SaveResourceFn != nil {
		return m.SaveResourceFn(ctx, contextPath, output)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resources[contextPath] == nil {
		m.resources[contextPath] = make(map[string]*platform.ResourceOutput)
	}
	m.resources[contextPath][output.Name] = output
	return nil
}

func (m *MockStateStore) GetResource(ctx context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	m.record("GetResource", contextPath, resourceName)
	if m.GetResourceFn != nil {
		return m.GetResourceFn(ctx, contextPath, resourceName)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if res, ok := m.resources[contextPath][resourceName]; ok {
		return res, nil
	}
	return nil, &platform.ResourceNotFoundError{Name: resourceName}
}

func (m *MockStateStore) ListResources(ctx context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	m.record("ListResources", contextPath)
	if m.ListResourcesFn != nil {
		return m.ListResourcesFn(ctx, contextPath)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*platform.ResourceOutput
	for _, v := range m.resources[contextPath] {
		result = append(result, v)
	}
	return result, nil
}

func (m *MockStateStore) DeleteResource(ctx context.Context, contextPath, resourceName string) error {
	m.record("DeleteResource", contextPath, resourceName)
	if m.DeleteResourceFn != nil {
		return m.DeleteResourceFn(ctx, contextPath, resourceName)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctxMap, ok := m.resources[contextPath]; ok {
		delete(ctxMap, resourceName)
	}
	return nil
}

func (m *MockStateStore) SavePlan(ctx context.Context, plan *platform.Plan) error {
	m.record("SavePlan", plan)
	if m.SavePlanFn != nil {
		return m.SavePlanFn(ctx, plan)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[plan.ID] = plan
	m.plansByCtx[plan.Context] = append(m.plansByCtx[plan.Context], plan.ID)
	return nil
}

func (m *MockStateStore) GetPlan(ctx context.Context, planID string) (*platform.Plan, error) {
	m.record("GetPlan", planID)
	if m.GetPlanFn != nil {
		return m.GetPlanFn(ctx, planID)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.plans[planID]; ok {
		return p, nil
	}
	return nil, &platform.ResourceNotFoundError{Name: planID}
}

func (m *MockStateStore) ListPlans(ctx context.Context, contextPath string, limit int) ([]*platform.Plan, error) {
	m.record("ListPlans", contextPath, limit)
	if m.ListPlansFn != nil {
		return m.ListPlansFn(ctx, contextPath, limit)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.plansByCtx[contextPath]
	var result []*platform.Plan
	// Reverse order (newest first).
	for i := len(ids) - 1; i >= 0 && len(result) < limit; i-- {
		if p, ok := m.plans[ids[i]]; ok {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *MockStateStore) Lock(ctx context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error) {
	m.record("Lock", contextPath, ttl)
	if m.LockFn != nil {
		return m.LockFn(ctx, contextPath, ttl)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[contextPath] {
		return nil, &platform.LockConflictError{ContextPath: contextPath}
	}
	m.locks[contextPath] = true
	return &MockLockHandle{
		store:       m,
		contextPath: contextPath,
	}, nil
}

func (m *MockStateStore) Dependencies(ctx context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error) {
	m.record("Dependencies", contextPath, resourceName)
	if m.DependenciesFn != nil {
		return m.DependenciesFn(ctx, contextPath, resourceName)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []platform.DependencyRef
	for _, dep := range m.dependencies {
		if dep.SourceContext == contextPath && dep.SourceResource == resourceName {
			result = append(result, dep)
		}
	}
	return result, nil
}

func (m *MockStateStore) AddDependency(ctx context.Context, dep platform.DependencyRef) error {
	m.record("AddDependency", dep)
	if m.AddDependencyFn != nil {
		return m.AddDependencyFn(ctx, dep)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dependencies = append(m.dependencies, dep)
	return nil
}

// unlock is called by MockLockHandle to release a lock.
func (m *MockStateStore) unlock(contextPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, contextPath)
}

// --- MockLockHandle ---

// MockLockHandle implements platform.LockHandle for use with MockStateStore.
type MockLockHandle struct {
	store       *MockStateStore
	contextPath string

	UnlockFn  func(ctx context.Context) error
	RefreshFn func(ctx context.Context, ttl time.Duration) error
}

func (h *MockLockHandle) Unlock(ctx context.Context) error {
	if h.UnlockFn != nil {
		return h.UnlockFn(ctx)
	}
	h.store.unlock(h.contextPath)
	return nil
}

func (h *MockLockHandle) Refresh(ctx context.Context, ttl time.Duration) error {
	if h.RefreshFn != nil {
		return h.RefreshFn(ctx, ttl)
	}
	return nil
}
