//go:build postgres_platform

package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgreSQL schema adapted from the SQLite migration.
const postgresMigration = `
CREATE TABLE IF NOT EXISTS platform_resources (
    context_path TEXT    NOT NULL,
    name         TEXT    NOT NULL,
    type         TEXT    NOT NULL DEFAULT '',
    provider_type TEXT   NOT NULL DEFAULT '',
    endpoint     TEXT    NOT NULL DEFAULT '',
    connection_str TEXT  NOT NULL DEFAULT '',
    credential_ref TEXT  NOT NULL DEFAULT '',
    properties   JSONB  NOT NULL DEFAULT '{}',
    status       TEXT    NOT NULL DEFAULT 'pending',
    last_synced  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (context_path, name)
);

CREATE TABLE IF NOT EXISTS platform_plans (
    id           TEXT    PRIMARY KEY,
    tier         INTEGER NOT NULL DEFAULT 0,
    context_path TEXT    NOT NULL DEFAULT '',
    actions      JSONB   NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    approved_at  TIMESTAMPTZ,
    approved_by  TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending',
    provider     TEXT    NOT NULL DEFAULT '',
    dry_run      BOOLEAN NOT NULL DEFAULT FALSE,
    fidelity_reports JSONB NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS platform_dependencies (
    id              BIGSERIAL PRIMARY KEY,
    source_context  TEXT NOT NULL,
    source_resource TEXT NOT NULL,
    target_context  TEXT NOT NULL,
    target_resource TEXT NOT NULL,
    dep_type        TEXT NOT NULL DEFAULT 'hard',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_context, source_resource, target_context, target_resource)
);

CREATE TABLE IF NOT EXISTS platform_drift_reports (
    id              BIGSERIAL PRIMARY KEY,
    context_path    TEXT    NOT NULL,
    resource_name   TEXT    NOT NULL,
    resource_type   TEXT    NOT NULL DEFAULT '',
    tier            INTEGER NOT NULL DEFAULT 0,
    drift_type      TEXT    NOT NULL DEFAULT '',
    expected        JSONB   NOT NULL DEFAULT '{}',
    actual          JSONB   NOT NULL DEFAULT '{}',
    diffs           JSONB   NOT NULL DEFAULT '[]',
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ,
    resolved_by     TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS platform_locks (
    context_path TEXT    PRIMARY KEY,
    holder       TEXT    NOT NULL DEFAULT '',
    acquired_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pg_resources_context ON platform_resources (context_path);
CREATE INDEX IF NOT EXISTS idx_pg_resources_status ON platform_resources (status);
CREATE INDEX IF NOT EXISTS idx_pg_plans_context ON platform_plans (context_path);
CREATE INDEX IF NOT EXISTS idx_pg_plans_status ON platform_plans (status);
CREATE INDEX IF NOT EXISTS idx_pg_deps_source ON platform_dependencies (source_context, source_resource);
CREATE INDEX IF NOT EXISTS idx_pg_deps_target ON platform_dependencies (target_context, target_resource);
CREATE INDEX IF NOT EXISTS idx_pg_drift_context ON platform_drift_reports (context_path);
CREATE INDEX IF NOT EXISTS idx_pg_drift_resource ON platform_drift_reports (context_path, resource_name);
CREATE INDEX IF NOT EXISTS idx_pg_drift_detected ON platform_drift_reports (detected_at);
CREATE INDEX IF NOT EXISTS idx_pg_locks_expires ON platform_locks (expires_at);
`

// PostgresStore implements platform.StateStore using a PostgreSQL database.
// It is suitable for production multi-node deployments and uses PostgreSQL
// advisory locks for concurrent access control.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a new PostgreSQL-backed state store. The dsn
// parameter is a PostgreSQL connection string.
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &PostgresStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

func (s *PostgresStore) migrate() error {
	_, err := s.db.Exec(postgresMigration)
	return err
}

// Close closes the underlying database connection.
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// SaveResource persists the state of a resource within a context path.
func (s *PostgresStore) SaveResource(ctx context.Context, contextPath string, output *platform.ResourceOutput) error {
	props, err := json.Marshal(output.Properties)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO platform_resources (context_path, name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (context_path, name) DO UPDATE SET
			type = EXCLUDED.type,
			provider_type = EXCLUDED.provider_type,
			endpoint = EXCLUDED.endpoint,
			connection_str = EXCLUDED.connection_str,
			credential_ref = EXCLUDED.credential_ref,
			properties = EXCLUDED.properties,
			status = EXCLUDED.status,
			last_synced = EXCLUDED.last_synced,
			updated_at = NOW()
	`, contextPath, output.Name, output.Type, output.ProviderType,
		output.Endpoint, output.ConnectionStr, output.CredentialRef,
		string(props), string(output.Status), output.LastSynced)

	return err
}

// GetResource retrieves a resource's state by context path and resource name.
func (s *PostgresStore) GetResource(ctx context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced
		FROM platform_resources
		WHERE context_path = $1 AND name = $2
	`, contextPath, resourceName)

	return scanPostgresResource(row)
}

// ListResources returns all resources in a context path.
func (s *PostgresStore) ListResources(ctx context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, type, provider_type, endpoint, connection_str, credential_ref, properties, status, last_synced
		FROM platform_resources
		WHERE context_path = $1
		ORDER BY name
	`, contextPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []*platform.ResourceOutput
	for rows.Next() {
		r, err := scanPostgresResourceRows(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

// DeleteResource removes a resource from state.
func (s *PostgresStore) DeleteResource(ctx context.Context, contextPath, resourceName string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM platform_resources WHERE context_path = $1 AND name = $2
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
func (s *PostgresStore) SavePlan(ctx context.Context, plan *platform.Plan) error {
	actions, err := json.Marshal(plan.Actions)
	if err != nil {
		return fmt.Errorf("marshal actions: %w", err)
	}
	fidelity, err := json.Marshal(plan.FidelityReports)
	if err != nil {
		return fmt.Errorf("marshal fidelity reports: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO platform_plans (id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			tier = EXCLUDED.tier,
			context_path = EXCLUDED.context_path,
			actions = EXCLUDED.actions,
			approved_at = EXCLUDED.approved_at,
			approved_by = EXCLUDED.approved_by,
			status = EXCLUDED.status,
			provider = EXCLUDED.provider,
			dry_run = EXCLUDED.dry_run,
			fidelity_reports = EXCLUDED.fidelity_reports
	`, plan.ID, int(plan.Tier), plan.Context, string(actions),
		plan.CreatedAt, plan.ApprovedAt, plan.ApprovedBy,
		plan.Status, plan.Provider, plan.DryRun, string(fidelity))

	return err
}

// GetPlan retrieves an execution plan by its ID.
func (s *PostgresStore) GetPlan(ctx context.Context, planID string) (*platform.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports
		FROM platform_plans WHERE id = $1
	`, planID)

	return scanPostgresPlan(row)
}

// ListPlans lists plans for a context path, ordered by creation time descending.
func (s *PostgresStore) ListPlans(ctx context.Context, contextPath string, limit int) ([]*platform.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tier, context_path, actions, created_at, approved_at, approved_by, status, provider, dry_run, fidelity_reports
		FROM platform_plans
		WHERE context_path = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, contextPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*platform.Plan
	for rows.Next() {
		p, err := scanPostgresPlanRows(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// Lock acquires an advisory lock for a context path using PostgreSQL
// advisory locks for cross-process safety.
func (s *PostgresStore) Lock(ctx context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error) {
	// Use pg_try_advisory_lock with a hash of the context path.
	lockID := hashContextPath(contextPath)

	var acquired bool
	err := s.db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&acquired)
	if err != nil {
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	if !acquired {
		return nil, &platform.LockConflictError{ContextPath: contextPath}
	}

	// Also record in the locks table for visibility.
	holderID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(ttl)
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO platform_locks (context_path, holder, acquired_at, expires_at)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (context_path) DO UPDATE SET holder = EXCLUDED.holder, acquired_at = NOW(), expires_at = EXCLUDED.expires_at
	`, contextPath, holderID, expiresAt)

	return &postgresLockHandle{
		db:          s.db,
		contextPath: contextPath,
		lockID:      lockID,
	}, nil
}

// Dependencies returns dependency references for resources that depend on
// the given resource.
func (s *PostgresStore) Dependencies(ctx context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_context, source_resource, target_context, target_resource, dep_type
		FROM platform_dependencies
		WHERE source_context = $1 AND source_resource = $2
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
func (s *PostgresStore) AddDependency(ctx context.Context, dep platform.DependencyRef) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO platform_dependencies (source_context, source_resource, target_context, target_resource, dep_type)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_context, source_resource, target_context, target_resource) DO UPDATE SET
			dep_type = EXCLUDED.dep_type
	`, dep.SourceContext, dep.SourceResource, dep.TargetContext, dep.TargetResource, dep.Type)
	return err
}

// SaveDriftReport persists a drift detection report.
func (s *PostgresStore) SaveDriftReport(ctx context.Context, report *DriftReport) error {
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

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO platform_drift_reports (context_path, resource_name, resource_type, tier, drift_type, expected, actual, diffs, detected_at, resolved_at, resolved_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, report.ContextPath, report.ResourceName, report.ResourceType,
		int(report.Tier), report.DriftType, string(expected), string(actual),
		string(diffs), report.DetectedAt, report.ResolvedAt, report.ResolvedBy).Scan(&report.ID)

	return err
}

// ListDriftReports returns drift reports for a context path, ordered by
// detection time descending.
func (s *PostgresStore) ListDriftReports(ctx context.Context, contextPath string, limit int) ([]*DriftReport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, context_path, resource_name, resource_type, tier, drift_type, expected, actual, diffs, detected_at, resolved_at, resolved_by
		FROM platform_drift_reports
		WHERE context_path = $1
		ORDER BY detected_at DESC
		LIMIT $2
	`, contextPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*DriftReport
	for rows.Next() {
		r, err := scanPostgresDriftReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// -- Internal helpers --

// postgresLockHandle implements platform.LockHandle for PostgreSQL.
type postgresLockHandle struct {
	db          *sql.DB
	contextPath string
	lockID      int64
}

// Unlock releases the PostgreSQL advisory lock.
func (h *postgresLockHandle) Unlock(ctx context.Context) error {
	_, err := h.db.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, h.lockID)
	if err != nil {
		return err
	}
	_, _ = h.db.ExecContext(ctx, `DELETE FROM platform_locks WHERE context_path = $1`, h.contextPath)
	return nil
}

// Refresh extends the lock TTL by updating the expires_at in the locks table.
func (h *postgresLockHandle) Refresh(ctx context.Context, ttl time.Duration) error {
	expiresAt := time.Now().UTC().Add(ttl)
	_, err := h.db.ExecContext(ctx, `
		UPDATE platform_locks SET expires_at = $1 WHERE context_path = $2
	`, expiresAt, h.contextPath)
	return err
}

// hashContextPath produces a stable int64 hash for advisory lock use.
func hashContextPath(path string) int64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	for i := 0; i < len(path); i++ {
		h ^= uint64(path[i])
		h *= 1099511628211 // FNV prime
	}
	return int64(h & 0x7FFFFFFFFFFFFFFF) //nolint:gosec // intentional truncation for advisory lock key
}

// scanPostgresResource scans a resource row from PostgreSQL. PostgreSQL
// returns TIMESTAMPTZ as time.Time natively via pgx.
func scanPostgresResource(s scanner) (*platform.ResourceOutput, error) {
	var r platform.ResourceOutput
	var propsJSON []byte
	var statusStr string
	var lastSynced time.Time

	if err := s.Scan(&r.Name, &r.Type, &r.ProviderType, &r.Endpoint,
		&r.ConnectionStr, &r.CredentialRef, &propsJSON, &statusStr, &lastSynced); err != nil {
		if err == sql.ErrNoRows {
			return nil, &platform.ResourceNotFoundError{}
		}
		return nil, err
	}

	if err := json.Unmarshal(propsJSON, &r.Properties); err != nil {
		return nil, fmt.Errorf("unmarshal properties: %w", err)
	}
	r.Status = platform.ResourceStatus(statusStr)
	r.LastSynced = lastSynced

	return &r, nil
}

func scanPostgresResourceRows(rows *sql.Rows) (*platform.ResourceOutput, error) {
	return scanPostgresResource(rows)
}

func scanPostgresPlan(s scanner) (*platform.Plan, error) {
	var p platform.Plan
	var actionsJSON, fidelityJSON []byte
	var tier int

	if err := s.Scan(&p.ID, &tier, &p.Context, &actionsJSON, &p.CreatedAt,
		&p.ApprovedAt, &p.ApprovedBy, &p.Status, &p.Provider, &p.DryRun, &fidelityJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, &platform.ResourceNotFoundError{}
		}
		return nil, err
	}

	p.Tier = platform.Tier(tier)

	if err := json.Unmarshal(actionsJSON, &p.Actions); err != nil {
		return nil, fmt.Errorf("unmarshal actions: %w", err)
	}
	if err := json.Unmarshal(fidelityJSON, &p.FidelityReports); err != nil {
		return nil, fmt.Errorf("unmarshal fidelity reports: %w", err)
	}

	return &p, nil
}

func scanPostgresPlanRows(rows *sql.Rows) (*platform.Plan, error) {
	return scanPostgresPlan(rows)
}

func scanPostgresDriftReport(rows *sql.Rows) (*DriftReport, error) {
	var r DriftReport
	var expectedJSON, actualJSON, diffsJSON []byte
	var tier int

	if err := rows.Scan(&r.ID, &r.ContextPath, &r.ResourceName, &r.ResourceType,
		&tier, &r.DriftType, &expectedJSON, &actualJSON, &diffsJSON,
		&r.DetectedAt, &r.ResolvedAt, &r.ResolvedBy); err != nil {
		return nil, err
	}

	r.Tier = platform.Tier(tier)

	if err := json.Unmarshal(expectedJSON, &r.Expected); err != nil {
		return nil, fmt.Errorf("unmarshal expected: %w", err)
	}
	if err := json.Unmarshal(actualJSON, &r.Actual); err != nil {
		return nil, fmt.Errorf("unmarshal actual: %w", err)
	}
	if err := json.Unmarshal(diffsJSON, &r.Diffs); err != nil {
		return nil, fmt.Errorf("unmarshal diffs: %w", err)
	}

	return &r, nil
}
