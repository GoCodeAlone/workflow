package tenant

import (
	"fmt"
	"sync"
	"time"
)

// TenantQuota defines resource limits for a single tenant.
type TenantQuota struct {
	TenantID string

	// MaxWorkflowsPerMinute is the rate limit for workflow executions.
	MaxWorkflowsPerMinute int
	// MaxConcurrentWorkflows is the maximum number of workflows running at once.
	MaxConcurrentWorkflows int
	// MaxStorageBytes is the maximum storage allowed in bytes.
	MaxStorageBytes int64
	// MaxAPIRequestsPerMinute is the rate limit for API requests.
	MaxAPIRequestsPerMinute int
}

// DefaultQuota returns a default quota for a new tenant.
func DefaultQuota(tenantID string) TenantQuota {
	return TenantQuota{
		TenantID:                tenantID,
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1 << 30, // 1 GB
		MaxAPIRequestsPerMinute: 1000,
	}
}

// TenantUsage tracks current resource usage for a tenant.
type TenantUsage struct {
	mu                  sync.Mutex
	ConcurrentWorkflows int
	StorageBytes        int64
	workflowTokens      int
	apiTokens           int
	lastRefill          time.Time
	workflowRPM         int
	apiRPM              int
}

// NewTenantUsage creates usage tracking for a tenant with given rate limits.
func NewTenantUsage(workflowRPM, apiRPM int) *TenantUsage {
	return &TenantUsage{
		workflowTokens: workflowRPM,
		apiTokens:      apiRPM,
		lastRefill:     time.Now(),
		workflowRPM:    workflowRPM,
		apiRPM:         apiRPM,
	}
}

// refillTokens adds tokens based on elapsed time.
func (u *TenantUsage) refillTokens() {
	elapsed := time.Since(u.lastRefill).Minutes()
	if elapsed <= 0 {
		return
	}

	u.workflowTokens += int(elapsed * float64(u.workflowRPM))
	if u.workflowTokens > u.workflowRPM {
		u.workflowTokens = u.workflowRPM
	}

	u.apiTokens += int(elapsed * float64(u.apiRPM))
	if u.apiTokens > u.apiRPM {
		u.apiTokens = u.apiRPM
	}

	u.lastRefill = time.Now()
}

// QuotaRegistry manages quotas and usage tracking for all tenants.
type QuotaRegistry struct {
	mu     sync.RWMutex
	quotas map[string]TenantQuota
	usage  map[string]*TenantUsage
}

// NewQuotaRegistry creates a new quota registry.
func NewQuotaRegistry() *QuotaRegistry {
	return &QuotaRegistry{
		quotas: make(map[string]TenantQuota),
		usage:  make(map[string]*TenantUsage),
	}
}

// SetQuota sets the quota for a tenant.
func (r *QuotaRegistry) SetQuota(quota TenantQuota) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.quotas[quota.TenantID] = quota
	if _, ok := r.usage[quota.TenantID]; !ok {
		r.usage[quota.TenantID] = NewTenantUsage(quota.MaxWorkflowsPerMinute, quota.MaxAPIRequestsPerMinute)
	}
}

// GetQuota returns the quota for a tenant.
func (r *QuotaRegistry) GetQuota(tenantID string) (TenantQuota, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q, ok := r.quotas[tenantID]
	return q, ok
}

// RemoveQuota removes a tenant's quota and usage tracking.
func (r *QuotaRegistry) RemoveQuota(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.quotas, tenantID)
	delete(r.usage, tenantID)
}

// CheckWorkflowRate checks whether the tenant can execute another workflow.
// Returns nil if allowed, or an error describing the quota violation.
func (r *QuotaRegistry) CheckWorkflowRate(tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	quota, ok := r.quotas[tenantID]
	if !ok {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage := r.getOrCreateUsageLocked(tenantID, quota)
	usage.mu.Lock()
	defer usage.mu.Unlock()

	usage.refillTokens()

	if usage.workflowTokens <= 0 {
		return fmt.Errorf("tenant %s exceeded workflow rate limit (%d/min)", tenantID, quota.MaxWorkflowsPerMinute)
	}

	usage.workflowTokens--
	return nil
}

// CheckAPIRate checks whether the tenant can make another API request.
func (r *QuotaRegistry) CheckAPIRate(tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	quota, ok := r.quotas[tenantID]
	if !ok {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage := r.getOrCreateUsageLocked(tenantID, quota)
	usage.mu.Lock()
	defer usage.mu.Unlock()

	usage.refillTokens()

	if usage.apiTokens <= 0 {
		return fmt.Errorf("tenant %s exceeded API rate limit (%d/min)", tenantID, quota.MaxAPIRequestsPerMinute)
	}

	usage.apiTokens--
	return nil
}

// CheckConcurrency checks whether the tenant can start another concurrent workflow.
func (r *QuotaRegistry) CheckConcurrency(tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	quota, ok := r.quotas[tenantID]
	if !ok {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage := r.getOrCreateUsageLocked(tenantID, quota)
	usage.mu.Lock()
	defer usage.mu.Unlock()

	if usage.ConcurrentWorkflows >= quota.MaxConcurrentWorkflows {
		return fmt.Errorf("tenant %s exceeded concurrency limit (%d)", tenantID, quota.MaxConcurrentWorkflows)
	}

	return nil
}

// AcquireWorkflowSlot atomically checks rate + concurrency and increments the counter.
func (r *QuotaRegistry) AcquireWorkflowSlot(tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	quota, ok := r.quotas[tenantID]
	if !ok {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage := r.getOrCreateUsageLocked(tenantID, quota)
	usage.mu.Lock()
	defer usage.mu.Unlock()

	usage.refillTokens()

	if usage.workflowTokens <= 0 {
		return fmt.Errorf("tenant %s exceeded workflow rate limit (%d/min)", tenantID, quota.MaxWorkflowsPerMinute)
	}

	if usage.ConcurrentWorkflows >= quota.MaxConcurrentWorkflows {
		return fmt.Errorf("tenant %s exceeded concurrency limit (%d)", tenantID, quota.MaxConcurrentWorkflows)
	}

	usage.workflowTokens--
	usage.ConcurrentWorkflows++
	return nil
}

// ReleaseWorkflowSlot decrements the concurrent workflow counter.
func (r *QuotaRegistry) ReleaseWorkflowSlot(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	usage, ok := r.usage[tenantID]
	if !ok {
		return
	}

	usage.mu.Lock()
	defer usage.mu.Unlock()

	if usage.ConcurrentWorkflows > 0 {
		usage.ConcurrentWorkflows--
	}
}

// CheckStorage checks whether the tenant has storage capacity remaining.
func (r *QuotaRegistry) CheckStorage(tenantID string, additionalBytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	quota, ok := r.quotas[tenantID]
	if !ok {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage := r.getOrCreateUsageLocked(tenantID, quota)
	usage.mu.Lock()
	defer usage.mu.Unlock()

	if usage.StorageBytes+additionalBytes > quota.MaxStorageBytes {
		return fmt.Errorf("tenant %s would exceed storage limit (%d bytes)", tenantID, quota.MaxStorageBytes)
	}

	return nil
}

// UpdateStorage updates the storage usage for a tenant.
func (r *QuotaRegistry) UpdateStorage(tenantID string, bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if usage, ok := r.usage[tenantID]; ok {
		usage.mu.Lock()
		usage.StorageBytes = bytes
		usage.mu.Unlock()
	}
}

// GetUsageSnapshot returns a snapshot of current usage for a tenant.
func (r *QuotaRegistry) GetUsageSnapshot(tenantID string) (UsageSnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	usage, ok := r.usage[tenantID]
	if !ok {
		return UsageSnapshot{}, false
	}

	usage.mu.Lock()
	defer usage.mu.Unlock()

	return UsageSnapshot{
		ConcurrentWorkflows: usage.ConcurrentWorkflows,
		StorageBytes:        usage.StorageBytes,
		WorkflowTokens:      usage.workflowTokens,
		APITokens:           usage.apiTokens,
	}, true
}

// UsageSnapshot is a point-in-time snapshot of tenant usage.
type UsageSnapshot struct {
	ConcurrentWorkflows int
	StorageBytes        int64
	WorkflowTokens      int
	APITokens           int
}

func (r *QuotaRegistry) getOrCreateUsageLocked(tenantID string, quota TenantQuota) *TenantUsage {
	usage, ok := r.usage[tenantID]
	if !ok {
		usage = NewTenantUsage(quota.MaxWorkflowsPerMinute, quota.MaxAPIRequestsPerMinute)
		r.usage[tenantID] = usage
	}
	return usage
}
