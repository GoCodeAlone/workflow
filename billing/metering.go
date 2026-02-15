package billing

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// UsageReport summarizes resource usage for a tenant during a billing period.
type UsageReport struct {
	TenantID       string    `json:"tenant_id"`
	Period         time.Time `json:"period"` // first day of billing period
	ExecutionCount int64     `json:"execution_count"`
	PipelineCount  int       `json:"pipeline_count"`
	StepCount      int       `json:"step_count"`
	WorkerPeak     int       `json:"worker_peak"`
}

// UsageMeter tracks and queries resource consumption per tenant.
type UsageMeter interface {
	// RecordExecution records a single pipeline execution for the tenant.
	RecordExecution(ctx context.Context, tenantID, pipelineName string) error
	// GetUsage returns the usage report for the given billing period.
	GetUsage(ctx context.Context, tenantID string, period time.Time) (*UsageReport, error)
	// CheckLimit checks whether the tenant is allowed to run another execution
	// and returns the remaining executions in the current period.
	CheckLimit(ctx context.Context, tenantID string) (allowed bool, remaining int64, err error)
}

// ---------- In-memory implementation (testing / development) ----------

// tenantUsage holds per-period counters for a single tenant.
type tenantUsage struct {
	executions map[string]int64 // period key -> count
	pipelines  map[string]bool  // unique pipeline names
}

// InMemoryMeter is a thread-safe in-memory UsageMeter suitable for tests.
type InMemoryMeter struct {
	mu      sync.RWMutex
	tenants map[string]*tenantUsage
	plans   map[string]string // tenantID -> planID
}

// NewInMemoryMeter creates an InMemoryMeter.
func NewInMemoryMeter() *InMemoryMeter {
	return &InMemoryMeter{
		tenants: make(map[string]*tenantUsage),
		plans:   make(map[string]string),
	}
}

// SetPlan associates a tenant with a billing plan.
func (m *InMemoryMeter) SetPlan(tenantID, planID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[tenantID] = planID
}

func periodKey(t time.Time) string {
	return t.UTC().Format("2006-01")
}

func currentPeriodKey() string {
	return periodKey(time.Now())
}

func (m *InMemoryMeter) getOrCreate(tenantID string) *tenantUsage {
	tu, ok := m.tenants[tenantID]
	if !ok {
		tu = &tenantUsage{
			executions: make(map[string]int64),
			pipelines:  make(map[string]bool),
		}
		m.tenants[tenantID] = tu
	}
	return tu
}

// RecordExecution records a single execution for the tenant.
func (m *InMemoryMeter) RecordExecution(_ context.Context, tenantID, pipelineName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tu := m.getOrCreate(tenantID)
	pk := currentPeriodKey()
	tu.executions[pk]++
	tu.pipelines[pipelineName] = true
	return nil
}

// GetUsage returns the usage report for the given period.
func (m *InMemoryMeter) GetUsage(_ context.Context, tenantID string, period time.Time) (*UsageReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := &UsageReport{
		TenantID: tenantID,
		Period:   time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC),
	}

	tu, ok := m.tenants[tenantID]
	if !ok {
		return report, nil
	}

	pk := periodKey(period)
	report.ExecutionCount = tu.executions[pk]
	report.PipelineCount = len(tu.pipelines)
	return report, nil
}

// CheckLimit checks whether the tenant may run another execution.
func (m *InMemoryMeter) CheckLimit(_ context.Context, tenantID string) (bool, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	planID, ok := m.plans[tenantID]
	if !ok {
		planID = "free" // default
	}
	plan := PlanByID(planID)
	if plan == nil {
		return false, 0, fmt.Errorf("billing: unknown plan %q for tenant %s", planID, tenantID)
	}

	// Unlimited plan.
	if plan.IsUnlimited() {
		return true, -1, nil // -1 signals unlimited
	}

	var count int64
	if tu, ok := m.tenants[tenantID]; ok {
		count = tu.executions[currentPeriodKey()]
	}

	remaining := plan.ExecutionsPerMonth - count
	if remaining < 0 {
		remaining = 0
	}
	return count < plan.ExecutionsPerMonth, remaining, nil
}

// ---------- SQLite implementation ----------

// SQLiteMeter is a UsageMeter backed by a SQLite database.
type SQLiteMeter struct {
	db    *sql.DB
	plans map[string]string // tenantID -> planID (in-memory for simplicity)
	mu    sync.RWMutex
}

// NewSQLiteMeter creates a new SQLiteMeter and initialises the schema.
func NewSQLiteMeter(db *sql.DB) (*SQLiteMeter, error) {
	m := &SQLiteMeter{
		db:    db,
		plans: make(map[string]string),
	}
	if err := m.migrate(); err != nil {
		return nil, fmt.Errorf("billing: migrate: %w", err)
	}
	return m, nil
}

func (m *SQLiteMeter) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS billing_executions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id     TEXT    NOT NULL,
    pipeline_name TEXT    NOT NULL,
    period        TEXT    NOT NULL,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_billing_exec_tenant_period
    ON billing_executions(tenant_id, period);
`
	_, err := m.db.Exec(ddl)
	return err
}

// SetPlan associates a tenant with a billing plan.
func (m *SQLiteMeter) SetPlan(tenantID, planID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[tenantID] = planID
}

// RecordExecution records an execution in SQLite.
func (m *SQLiteMeter) RecordExecution(ctx context.Context, tenantID, pipelineName string) error {
	pk := currentPeriodKey()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO billing_executions (tenant_id, pipeline_name, period) VALUES (?, ?, ?)`,
		tenantID, pipelineName, pk,
	)
	if err != nil {
		return fmt.Errorf("billing: record execution: %w", err)
	}
	return nil
}

// GetUsage returns the usage report for the given period from SQLite.
func (m *SQLiteMeter) GetUsage(ctx context.Context, tenantID string, period time.Time) (*UsageReport, error) {
	pk := periodKey(period)
	report := &UsageReport{
		TenantID: tenantID,
		Period:   time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC),
	}

	row := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT pipeline_name) FROM billing_executions WHERE tenant_id = ? AND period = ?`,
		tenantID, pk,
	)
	if err := row.Scan(&report.ExecutionCount, &report.PipelineCount); err != nil {
		return nil, fmt.Errorf("billing: get usage: %w", err)
	}
	return report, nil
}

// CheckLimit checks whether the tenant may run another execution.
func (m *SQLiteMeter) CheckLimit(ctx context.Context, tenantID string) (bool, int64, error) {
	m.mu.RLock()
	planID, ok := m.plans[tenantID]
	m.mu.RUnlock()
	if !ok {
		planID = "free"
	}
	plan := PlanByID(planID)
	if plan == nil {
		return false, 0, fmt.Errorf("billing: unknown plan %q for tenant %s", planID, tenantID)
	}

	if plan.IsUnlimited() {
		return true, -1, nil
	}

	pk := currentPeriodKey()
	var count int64
	row := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM billing_executions WHERE tenant_id = ? AND period = ?`,
		tenantID, pk,
	)
	if err := row.Scan(&count); err != nil {
		return false, 0, fmt.Errorf("billing: check limit: %w", err)
	}

	remaining := plan.ExecutionsPerMonth - count
	if remaining < 0 {
		remaining = 0
	}
	return count < plan.ExecutionsPerMonth, remaining, nil
}
