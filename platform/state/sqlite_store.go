// Package state provides persistent StateStore implementations for the
// platform abstraction layer. It includes SQLite for single-node/local
// development and PostgreSQL for production multi-node deployments.
package state

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var sqliteMigration string

// SQLiteStore implements platform.StateStore using an SQLite database.
// It is suitable for single-node deployments and local development.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // process-level lock for advisory lock emulation
}

// NewSQLiteStore creates a new SQLite-backed state store. The dsn parameter
// is the path to the SQLite database file. Use ":memory:" for an in-memory
// database (useful for testing).
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	// Append pragmas to the DSN so they apply to every connection in the pool.
	if dsn != ":memory:" {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Limit to one open connection to serialize writes and avoid SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	// For :memory: databases, set pragmas after opening since DSN params
	// are not supported.
	if dsn == ":memory:" {
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable foreign keys: %w", err)
		}
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// migrate runs the initial schema migration.
func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(sqliteMigration)
	return err
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// SaveResource persists the state of a resource within a context path.
func (s *SQLiteStore) SaveResource(ctx context.Context, contextPath string, output *platform.ResourceOutput) error {
	props, err := json.Marshal(output.Properties)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO platform_resources (context_path, name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT (context_path, name) DO UPDATE SET
			type = excluded.type,
			provider_type = excluded.provider_type,
			endpoint = excluded.endpoint,
			connection_str = excluded.connection_str,
			credential_ref = excluded.credential_ref,
			properties = excluded.properties,
			status = excluded.status,
			last_synced = excluded.last_synced,
			updated_at = datetime('now')
	`, contextPath, output.Name, output.Type, output.ProviderType,
		output.Endpoint, output.ConnectionStr, output.CredentialRef,
		string(props), string(output.Status), output.LastSynced.Format(time.RFC3339))

	return err
}

// GetResource retrieves a resource's state by context path and resource name.
func (s *SQLiteStore) GetResource(ctx context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced
		FROM platform_resources
		WHERE context_path = ? AND name = ?
	`, contextPath, resourceName)

	return scanResource(row)
}

// ListResources returns all resources in a context path.
func (s *SQLiteStore) ListResources(ctx context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced
		FROM platform_resources
		WHERE context_path = ?
		ORDER BY name
	`, contextPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []*platform.ResourceOutput
	for rows.Next() {
		r, err := scanResourceRows(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

// DeleteResource removes a resource from state.
func (s *SQLiteStore) DeleteResource(ctx context.Context, contextPath, resourceName string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM platform_resources WHERE context_path = ? AND name = ?
	`, contextPath, resourceName)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return &platform.ResourceNotFoundError{Name: resourceName}
	}
	return nil
}

// SavePlan persists an execution plan.
func (s *SQLiteStore) SavePlan(ctx context.Context, plan *platform.Plan) error {
	actions, err := json.Marshal(plan.Actions)
	if err != nil {
		return fmt.Errorf("marshal actions: %w", err)
	}
	fidelity, err := json.Marshal(plan.FidelityReports)
	if err != nil {
		return fmt.Errorf("marshal fidelity reports: %w", err)
	}

	var approvedAt *string
	if plan.ApprovedAt != nil {
		s := plan.ApprovedAt.Format(time.RFC3339)
		approvedAt = &s
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO platform_plans (id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			tier = excluded.tier,
			context_path = excluded.context_path,
			actions = excluded.actions,
			approved_at = excluded.approved_at,
			approved_by = excluded.approved_by,
			status = excluded.status,
			provider = excluded.provider,
			dry_run = excluded.dry_run,
			fidelity_reports = excluded.fidelity_reports
	`, plan.ID, int(plan.Tier), plan.Context, string(actions),
		plan.CreatedAt.Format(time.RFC3339), approvedAt, plan.ApprovedBy,
		plan.Status, plan.Provider, plan.DryRun, string(fidelity))

	return err
}

// GetPlan retrieves an execution plan by its ID.
func (s *SQLiteStore) GetPlan(ctx context.Context, planID string) (*platform.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports
		FROM platform_plans WHERE id = ?
	`, planID)

	return scanPlan(row)
}

// ListPlans lists plans for a context path, ordered by creation time descending.
func (s *SQLiteStore) ListPlans(ctx context.Context, contextPath string, limit int) ([]*platform.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports
		FROM platform_plans
		WHERE context_path = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, contextPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*platform.Plan
	for rows.Next() {
		p, err := scanPlanRows(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// Lock acquires an advisory lock for a context path. SQLite advisory locks
// are emulated using a database row and a process-level mutex.
func (s *SQLiteStore) Lock(ctx context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()

	// Clean up expired locks first.
	now := time.Now().UTC()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM platform_locks WHERE expires_at < ?`, now.Format(time.RFC3339))

	// Try to insert a lock row.
	holderID := uuid.New().String()
	expiresAt := now.Add(ttl)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO platform_locks (context_path, holder, acquired_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, contextPath, holderID, now.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	if err != nil {
		s.mu.Unlock()
		return nil, &platform.LockConflictError{ContextPath: contextPath}
	}

	return &sqliteLockHandle{
		store:       s,
		contextPath: contextPath,
		holderID:    holderID,
	}, nil
}

// Dependencies returns dependency references for resources that depend on
// the given resource.
func (s *SQLiteStore) Dependencies(ctx context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_context, source_resource, target_context, target_resource, dep_type
		FROM platform_dependencies
		WHERE source_context = ? AND source_resource = ?
	`, contextPath, resourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []platform.DependencyRef
	for rows.Next() {
		var d platform.DependencyRef
		if err := rows.Scan(&d.SourceContext, &d.SourceResource, &d.TargetContext, &d.TargetResource, &d.Type); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}

// AddDependency records a cross-resource or cross-tier dependency.
func (s *SQLiteStore) AddDependency(ctx context.Context, dep platform.DependencyRef) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO platform_dependencies (source_context, source_resource, target_context, target_resource, dep_type)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (source_context, source_resource, target_context, target_resource) DO UPDATE SET
			dep_type = excluded.dep_type
	`, dep.SourceContext, dep.SourceResource, dep.TargetContext, dep.TargetResource, dep.Type)
	return err
}

// SaveDriftReport persists a drift detection report.
func (s *SQLiteStore) SaveDriftReport(ctx context.Context, report *DriftReport) error {
	expected, err := json.Marshal(report.Expected)
	if err != nil {
		return fmt.Errorf("marshal expected: %w", err)
	}
	actual, err := json.Marshal(report.Actual)
	if err != nil {
		return fmt.Errorf("marshal actual: %w", err)
	}
	diffs, err := json.Marshal(report.Diffs)
	if err != nil {
		return fmt.Errorf("marshal diffs: %w", err)
	}

	var resolvedAt *string
	if report.ResolvedAt != nil {
		s := report.ResolvedAt.Format(time.RFC3339)
		resolvedAt = &s
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO platform_drift_reports (context_path, resource_name, resource_type, tier, drift_type, expected, actual, diffs, detected_at, resolved_at, resolved_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, report.ContextPath, report.ResourceName, report.ResourceType,
		int(report.Tier), report.DriftType, string(expected), string(actual),
		string(diffs), report.DetectedAt.Format(time.RFC3339), resolvedAt, report.ResolvedBy)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err == nil {
		report.ID = id
	}
	return nil
}

// ListDriftReports returns drift reports for a context path, ordered by
// detection time descending.
func (s *SQLiteStore) ListDriftReports(ctx context.Context, contextPath string, limit int) ([]*DriftReport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, context_path, resource_name, resource_type, tier, drift_type, expected, actual, diffs, detected_at, resolved_at, resolved_by
		FROM platform_drift_reports
		WHERE context_path = ?
		ORDER BY detected_at DESC
		LIMIT ?
	`, contextPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*DriftReport
	for rows.Next() {
		r, err := scanDriftReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// -- Internal helpers --

// sqliteLockHandle implements platform.LockHandle for SQLite.
type sqliteLockHandle struct {
	store       *SQLiteStore
	contextPath string
	holderID    string
}

// Unlock releases the advisory lock.
func (h *sqliteLockHandle) Unlock(ctx context.Context) error {
	_, err := h.store.db.ExecContext(ctx, `
		DELETE FROM platform_locks WHERE context_path = ? AND holder = ?
	`, h.contextPath, h.holderID)
	h.store.mu.Unlock()
	return err
}

// Refresh extends the lock TTL.
func (h *sqliteLockHandle) Refresh(ctx context.Context, ttl time.Duration) error {
	expiresAt := time.Now().UTC().Add(ttl)
	_, err := h.store.db.ExecContext(ctx, `
		UPDATE platform_locks SET expires_at = ? WHERE context_path = ? AND holder = ?
	`, expiresAt.Format(time.RFC3339), h.contextPath, h.holderID)
	return err
}

// scanner is an interface satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanResource(s scanner) (*platform.ResourceOutput, error) {
	var r platform.ResourceOutput
	var propsJSON, statusStr, lastSyncedStr string

	if err := s.Scan(&r.Name, &r.Type, &r.ProviderType, &r.Endpoint,
		&r.ConnectionStr, &r.CredentialRef, &propsJSON, &statusStr, &lastSyncedStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, &platform.ResourceNotFoundError{}
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(propsJSON), &r.Properties); err != nil {
		return nil, fmt.Errorf("unmarshal properties: %w", err)
	}
	r.Status = platform.ResourceStatus(statusStr)
	if t, err := time.Parse(time.RFC3339, lastSyncedStr); err == nil {
		r.LastSynced = t
	}

	return &r, nil
}

func scanResourceRows(rows *sql.Rows) (*platform.ResourceOutput, error) {
	return scanResource(rows)
}

func scanPlan(s scanner) (*platform.Plan, error) {
	var p platform.Plan
	var actionsJSON, createdAtStr, approvedBy, statusStr, provider, fidelityJSON string
	var approvedAtStr *string
	var tier int
	var dryRun bool

	if err := s.Scan(&p.ID, &tier, &p.Context, &actionsJSON, &createdAtStr,
		&approvedAtStr, &approvedBy, &statusStr, &provider, &dryRun, &fidelityJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, &platform.ResourceNotFoundError{}
		}
		return nil, err
	}

	p.Tier = platform.Tier(tier)
	p.ApprovedBy = approvedBy
	p.Status = statusStr
	p.Provider = provider
	p.DryRun = dryRun

	if err := json.Unmarshal([]byte(actionsJSON), &p.Actions); err != nil {
		return nil, fmt.Errorf("unmarshal actions: %w", err)
	}
	if err := json.Unmarshal([]byte(fidelityJSON), &p.FidelityReports); err != nil {
		return nil, fmt.Errorf("unmarshal fidelity reports: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		p.CreatedAt = t
	}
	if approvedAtStr != nil && *approvedAtStr != "" {
		if t, err := time.Parse(time.RFC3339, *approvedAtStr); err == nil {
			p.ApprovedAt = &t
		}
	}

	return &p, nil
}

func scanPlanRows(rows *sql.Rows) (*platform.Plan, error) {
	return scanPlan(rows)
}

func scanDriftReport(rows *sql.Rows) (*DriftReport, error) {
	var r DriftReport
	var expectedJSON, actualJSON, diffsJSON, detectedAtStr, resolvedBy string
	var resolvedAtStr *string
	var tier int

	if err := rows.Scan(&r.ID, &r.ContextPath, &r.ResourceName, &r.ResourceType,
		&tier, &r.DriftType, &expectedJSON, &actualJSON, &diffsJSON,
		&detectedAtStr, &resolvedAtStr, &resolvedBy); err != nil {
		return nil, err
	}

	r.Tier = platform.Tier(tier)
	r.ResolvedBy = resolvedBy

	if err := json.Unmarshal([]byte(expectedJSON), &r.Expected); err != nil {
		return nil, fmt.Errorf("unmarshal expected: %w", err)
	}
	if err := json.Unmarshal([]byte(actualJSON), &r.Actual); err != nil {
		return nil, fmt.Errorf("unmarshal actual: %w", err)
	}
	if err := json.Unmarshal([]byte(diffsJSON), &r.Diffs); err != nil {
		return nil, fmt.Errorf("unmarshal diffs: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, detectedAtStr); err == nil {
		r.DetectedAt = t
	}
	if resolvedAtStr != nil && *resolvedAtStr != "" {
		if t, err := time.Parse(time.RFC3339, *resolvedAtStr); err == nil {
			r.ResolvedAt = &t
		}
	}

	return &r, nil
}

// DriftReport represents the result of a drift detection check for a
// single resource.
type DriftReport struct {
	// ID is the database identifier (set after save).
	ID int64 `json:"id"`

	// ContextPath is the hierarchical context path of the resource.
	ContextPath string `json:"contextPath"`

	// ResourceName is the name of the resource that drifted.
	ResourceName string `json:"resourceName"`

	// ResourceType is the provider-specific resource type.
	ResourceType string `json:"resourceType"`

	// Tier is the infrastructure tier the resource belongs to.
	Tier platform.Tier `json:"tier"`

	// DriftType classifies the drift: "changed", "added", "removed".
	DriftType string `json:"driftType"`

	// Expected is the desired state properties from the state store.
	Expected map[string]any `json:"expected"`

	// Actual is the live state properties read from the provider.
	Actual map[string]any `json:"actual"`

	// Diffs contains the individual field differences.
	Diffs []platform.DiffEntry `json:"diffs"`

	// DetectedAt is when the drift was detected.
	DetectedAt time.Time `json:"detectedAt"`

	// ResolvedAt is when the drift was remediated (nil if unresolved).
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`

	// ResolvedBy identifies who or what resolved the drift.
	ResolvedBy string `json:"resolvedBy,omitempty"`
}
