package compliance

import (
	"log/slog"
	"sync"
	"time"
)

// DataRetentionPolicy defines how long data is kept.
type DataRetentionPolicy struct {
	Name           string `json:"name"`
	DataType       string `json:"data_type"` // "audit_logs", "executions", "events", "dlq_entries"
	RetentionDays  int    `json:"retention_days"`
	ArchiveEnabled bool   `json:"archive_enabled"`
	ArchiveFormat  string `json:"archive_format"` // "json", "parquet"
}

// RetentionManager enforces data retention policies.
type RetentionManager struct {
	mu       sync.RWMutex
	policies map[string]*DataRetentionPolicy
	logger   *slog.Logger
}

// NewRetentionManager creates a new RetentionManager. If logger is nil, a default
// logger is used.
func NewRetentionManager(logger *slog.Logger) *RetentionManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &RetentionManager{
		policies: make(map[string]*DataRetentionPolicy),
		logger:   logger,
	}
}

// AddPolicy registers a retention policy for a specific data type. If a policy
// for the data type already exists, it is replaced.
func (m *RetentionManager) AddPolicy(policy *DataRetentionPolicy) {
	if policy == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[policy.DataType] = policy
	m.logger.Info("retention policy added",
		"data_type", policy.DataType,
		"retention_days", policy.RetentionDays,
		"archive_enabled", policy.ArchiveEnabled,
	)
}

// GetPolicy retrieves the policy for a given data type.
func (m *RetentionManager) GetPolicy(dataType string) (*DataRetentionPolicy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[dataType]
	return p, ok
}

// ListPolicies returns all registered retention policies.
func (m *RetentionManager) ListPolicies() []*DataRetentionPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*DataRetentionPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		result = append(result, p)
	}
	return result
}

// ShouldRetain returns true if data of the given type created at createdAt should
// still be retained according to the policy. If no policy exists for the data type,
// the data is retained (conservative default).
func (m *RetentionManager) ShouldRetain(dataType string, createdAt time.Time) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.policies[dataType]
	if !ok {
		// No policy means retain indefinitely.
		return true
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -p.RetentionDays)
	return !createdAt.Before(cutoff)
}

// DefaultPolicies returns sensible default retention policies for common data types.
func DefaultPolicies() []*DataRetentionPolicy {
	return []*DataRetentionPolicy{
		{
			Name:           "Audit Logs",
			DataType:       "audit_logs",
			RetentionDays:  2555, // ~7 years (SOC2 recommends 7 years)
			ArchiveEnabled: true,
			ArchiveFormat:  "json",
		},
		{
			Name:           "Workflow Executions",
			DataType:       "executions",
			RetentionDays:  365, // 1 year
			ArchiveEnabled: true,
			ArchiveFormat:  "json",
		},
		{
			Name:           "Event Bus Events",
			DataType:       "events",
			RetentionDays:  90, // 90 days
			ArchiveEnabled: false,
			ArchiveFormat:  "",
		},
		{
			Name:           "Dead Letter Queue Entries",
			DataType:       "dlq_entries",
			RetentionDays:  180, // 6 months
			ArchiveEnabled: true,
			ArchiveFormat:  "json",
		},
		{
			Name:           "API Access Logs",
			DataType:       "api_access_logs",
			RetentionDays:  365, // 1 year
			ArchiveEnabled: true,
			ArchiveFormat:  "json",
		},
		{
			Name:           "Security Events",
			DataType:       "security_events",
			RetentionDays:  2555, // ~7 years
			ArchiveEnabled: true,
			ArchiveFormat:  "json",
		},
	}
}
