package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGWorkflowStore implements WorkflowStore backed by PostgreSQL.
// Versioning is handled via a workflow_versions table; the main workflows table
// always reflects the latest version.
type PGWorkflowStore struct {
	pool *pgxpool.Pool
}

func (s *PGWorkflowStore) Create(ctx context.Context, w *WorkflowRecord) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	if w.Version == 0 {
		w.Version = 1
	}
	if w.Status == "" {
		w.Status = WorkflowStatusDraft
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Insert into main table.
	_, err = tx.Exec(ctx, `
		INSERT INTO workflows (id, project_id, name, slug, description, config_yaml,
			version, status, created_by, updated_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())`,
		w.ID, w.ProjectID, w.Name, w.Slug, w.Description, w.ConfigYAML,
		w.Version, w.Status, w.CreatedBy, w.UpdatedBy)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: workflow slug %s in project", ErrDuplicate, w.Slug)
		}
		return fmt.Errorf("insert workflow: %w", err)
	}

	// Insert initial version snapshot.
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_versions (workflow_id, version, config_yaml, status, updated_by, created_at)
		VALUES ($1,$2,$3,$4,$5,NOW())`,
		w.ID, w.Version, w.ConfigYAML, w.Status, w.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert workflow version: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PGWorkflowStore) Get(ctx context.Context, id uuid.UUID) (*WorkflowRecord, error) {
	return s.scanOne(ctx, `SELECT * FROM workflows WHERE id = $1`, id)
}

func (s *PGWorkflowStore) GetBySlug(ctx context.Context, projectID uuid.UUID, slug string) (*WorkflowRecord, error) {
	return s.scanOne(ctx, `SELECT * FROM workflows WHERE project_id = $1 AND slug = $2`, projectID, slug)
}

func (s *PGWorkflowStore) Update(ctx context.Context, w *WorkflowRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Bump version.
	var currentVersion int
	err = tx.QueryRow(ctx, `SELECT version FROM workflows WHERE id = $1 FOR UPDATE`, w.ID).Scan(&currentVersion)
	if err != nil {
		return ErrNotFound
	}
	w.Version = currentVersion + 1

	// Update main table.
	tag, err := tx.Exec(ctx, `
		UPDATE workflows SET name=$2, slug=$3, description=$4, config_yaml=$5,
			version=$6, status=$7, updated_by=$8, updated_at=NOW()
		WHERE id=$1`,
		w.ID, w.Name, w.Slug, w.Description, w.ConfigYAML,
		w.Version, w.Status, w.UpdatedBy)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: workflow slug %s in project", ErrDuplicate, w.Slug)
		}
		return fmt.Errorf("update workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	// Insert version snapshot.
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_versions (workflow_id, version, config_yaml, status, updated_by, created_at)
		VALUES ($1,$2,$3,$4,$5,NOW())`,
		w.ID, w.Version, w.ConfigYAML, w.Status, w.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert workflow version: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PGWorkflowStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete versions first.
	_, err = tx.Exec(ctx, `DELETE FROM workflow_versions WHERE workflow_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete workflow versions: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM workflows WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return tx.Commit(ctx)
}

func (s *PGWorkflowStore) List(ctx context.Context, f WorkflowFilter) ([]*WorkflowRecord, error) {
	query := `SELECT * FROM workflows WHERE 1=1`
	args := []any{}
	idx := 1

	if f.ProjectID != nil {
		query += fmt.Sprintf(` AND project_id = $%d`, idx)
		args = append(args, *f.ProjectID)
		idx++
	}
	if f.Status != "" {
		query += fmt.Sprintf(` AND status = $%d`, idx)
		args = append(args, f.Status)
		idx++
	}
	if f.Slug != "" {
		query += fmt.Sprintf(` AND slug = $%d`, idx)
		args = append(args, f.Slug)
		idx++
	}

	query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	limit := f.Pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Pagination.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var workflows []*WorkflowRecord
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, w)
	}
	return workflows, rows.Err()
}

func (s *PGWorkflowStore) GetVersion(ctx context.Context, id uuid.UUID, version int) (*WorkflowRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.project_id, w.name, w.slug, w.description,
			v.config_yaml, v.version, v.status, w.created_by, v.updated_by,
			w.created_at, v.created_at
		FROM workflows w
		JOIN workflow_versions v ON v.workflow_id = w.id
		WHERE w.id = $1 AND v.version = $2`, id, version)
	if err != nil {
		return nil, fmt.Errorf("query workflow version: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query workflow version: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanWorkflow(rows)
}

func (s *PGWorkflowStore) ListVersions(ctx context.Context, id uuid.UUID) ([]*WorkflowRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.project_id, w.name, w.slug, w.description,
			v.config_yaml, v.version, v.status, w.created_by, v.updated_by,
			w.created_at, v.created_at
		FROM workflows w
		JOIN workflow_versions v ON v.workflow_id = w.id
		WHERE w.id = $1
		ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list workflow versions: %w", err)
	}
	defer rows.Close()

	var versions []*WorkflowRecord
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, w)
	}
	return versions, rows.Err()
}

func (s *PGWorkflowStore) scanOne(ctx context.Context, query string, args ...any) (*WorkflowRecord, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query workflow: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query workflow: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanWorkflow(rows)
}

func scanWorkflow(rows pgx.Rows) (*WorkflowRecord, error) {
	var w WorkflowRecord
	err := rows.Scan(
		&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description,
		&w.ConfigYAML, &w.Version, &w.Status, &w.CreatedBy, &w.UpdatedBy,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan workflow: %w", err)
	}
	return &w, nil
}
