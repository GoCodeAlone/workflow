package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGIAMStore implements IAMStore backed by PostgreSQL.
type PGIAMStore struct {
	pool *pgxpool.Pool
}

func (s *PGIAMStore) CreateProvider(ctx context.Context, p *IAMProviderConfig) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO iam_provider_configs (id, company_id, provider_type, name, config, enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW())`,
		p.ID, p.CompanyID, p.ProviderType, p.Name, p.Config, p.Enabled)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: iam provider %s in company", ErrDuplicate, p.Name)
		}
		return fmt.Errorf("insert iam provider: %w", err)
	}
	return nil
}

func (s *PGIAMStore) GetProvider(ctx context.Context, id uuid.UUID) (*IAMProviderConfig, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, company_id, provider_type, name, config, enabled, created_at, updated_at
		FROM iam_provider_configs WHERE id = $1`, id)

	var p IAMProviderConfig
	err := row.Scan(&p.ID, &p.CompanyID, &p.ProviderType, &p.Name, &p.Config,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get iam provider: %w", err)
	}
	return &p, nil
}

func (s *PGIAMStore) UpdateProvider(ctx context.Context, p *IAMProviderConfig) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE iam_provider_configs SET provider_type=$2, name=$3, config=$4, enabled=$5, updated_at=NOW()
		WHERE id=$1`,
		p.ID, p.ProviderType, p.Name, p.Config, p.Enabled)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: iam provider %s in company", ErrDuplicate, p.Name)
		}
		return fmt.Errorf("update iam provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGIAMStore) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM iam_provider_configs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete iam provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGIAMStore) ListProviders(ctx context.Context, f IAMProviderFilter) ([]*IAMProviderConfig, error) {
	query := `SELECT id, company_id, provider_type, name, config, enabled, created_at, updated_at
		FROM iam_provider_configs WHERE 1=1`
	args := []any{}
	idx := 1

	if f.CompanyID != nil {
		query += fmt.Sprintf(` AND company_id = $%d`, idx)
		args = append(args, *f.CompanyID)
		idx++
	}
	if f.ProviderType != "" {
		query += fmt.Sprintf(` AND provider_type = $%d`, idx)
		args = append(args, f.ProviderType)
		idx++
	}
	if f.Enabled != nil {
		query += fmt.Sprintf(` AND enabled = $%d`, idx)
		args = append(args, *f.Enabled)
		idx++
	}

	query += fmt.Sprintf(` ORDER BY name ASC LIMIT $%d OFFSET $%d`, idx, idx+1)
	limit := f.Pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Pagination.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list iam providers: %w", err)
	}
	defer rows.Close()

	var providers []*IAMProviderConfig
	for rows.Next() {
		var p IAMProviderConfig
		err := rows.Scan(&p.ID, &p.CompanyID, &p.ProviderType, &p.Name, &p.Config,
			&p.Enabled, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan iam provider: %w", err)
		}
		providers = append(providers, &p)
	}
	return providers, rows.Err()
}

func (s *PGIAMStore) CreateMapping(ctx context.Context, m *IAMRoleMapping) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO iam_role_mappings (id, provider_id, external_identifier, resource_type, resource_id, role, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())`,
		m.ID, m.ProviderID, m.ExternalIdentifier, m.ResourceType, m.ResourceID, m.Role)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: iam role mapping", ErrDuplicate)
		}
		return fmt.Errorf("insert iam mapping: %w", err)
	}
	return nil
}

func (s *PGIAMStore) GetMapping(ctx context.Context, id uuid.UUID) (*IAMRoleMapping, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, provider_id, external_identifier, resource_type, resource_id, role, created_at
		FROM iam_role_mappings WHERE id = $1`, id)

	var m IAMRoleMapping
	err := row.Scan(&m.ID, &m.ProviderID, &m.ExternalIdentifier, &m.ResourceType,
		&m.ResourceID, &m.Role, &m.CreatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get iam mapping: %w", err)
	}
	return &m, nil
}

func (s *PGIAMStore) DeleteMapping(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM iam_role_mappings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete iam mapping: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGIAMStore) ListMappings(ctx context.Context, f IAMRoleMappingFilter) ([]*IAMRoleMapping, error) {
	query := `SELECT id, provider_id, external_identifier, resource_type, resource_id, role, created_at
		FROM iam_role_mappings WHERE 1=1`
	args := []any{}
	idx := 1

	if f.ProviderID != nil {
		query += fmt.Sprintf(` AND provider_id = $%d`, idx)
		args = append(args, *f.ProviderID)
		idx++
	}
	if f.ExternalIdentifier != "" {
		query += fmt.Sprintf(` AND external_identifier = $%d`, idx)
		args = append(args, f.ExternalIdentifier)
		idx++
	}
	if f.ResourceType != "" {
		query += fmt.Sprintf(` AND resource_type = $%d`, idx)
		args = append(args, f.ResourceType)
		idx++
	}
	if f.ResourceID != nil {
		query += fmt.Sprintf(` AND resource_id = $%d`, idx)
		args = append(args, *f.ResourceID)
		idx++
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	limit := f.Pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Pagination.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list iam mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*IAMRoleMapping
	for rows.Next() {
		var m IAMRoleMapping
		err := rows.Scan(&m.ID, &m.ProviderID, &m.ExternalIdentifier, &m.ResourceType,
			&m.ResourceID, &m.Role, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan iam mapping: %w", err)
		}
		mappings = append(mappings, &m)
	}
	return mappings, rows.Err()
}

func (s *PGIAMStore) ResolveRole(ctx context.Context, providerID uuid.UUID, externalID string, resourceType string, resourceID uuid.UUID) (Role, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT role FROM iam_role_mappings
		WHERE provider_id = $1 AND external_identifier = $2
			AND resource_type = $3 AND resource_id = $4`,
		providerID, externalID, resourceType, resourceID)

	var role Role
	err := row.Scan(&role)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("resolve role: %w", err)
	}
	return role, nil
}
