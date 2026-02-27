package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGConfig holds PostgreSQL connection configuration.
type PGConfig struct {
	URL             string `yaml:"url" json:"url"`
	MaxConns        int32  `yaml:"max_conns" json:"max_conns"`
	MinConns        int32  `yaml:"min_conns" json:"min_conns"`
	MaxConnIdleTime string `yaml:"max_conn_idle_time" json:"max_conn_idle_time"`
}

// PGStore wraps a pgxpool.Pool and provides access to all domain stores.
type PGStore struct {
	pool *pgxpool.Pool

	users              *PGUserStore
	companies          *PGCompanyStore
	projects           *PGProjectStore
	workflows          *PGWorkflowStore
	memberships        *PGMembershipStore
	crossWorkflowLinks *PGCrossWorkflowLinkStore
	sessions           *PGSessionStore
	executions         *PGExecutionStore
	logs               *PGLogStore
	audit              *PGAuditStore
	iam                *PGIAMStore
	configDocs         *PGConfigStore
}

// NewPGStore connects to PostgreSQL and returns a PGStore with all sub-stores.
func NewPGStore(ctx context.Context, cfg PGConfig) (*PGStore, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pg config: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pg pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pg: %w", err)
	}

	s := &PGStore{pool: pool}
	s.users = &PGUserStore{pool: pool}
	s.companies = &PGCompanyStore{pool: pool}
	s.projects = &PGProjectStore{pool: pool}
	s.workflows = &PGWorkflowStore{pool: pool}
	s.memberships = &PGMembershipStore{pool: pool}
	s.crossWorkflowLinks = &PGCrossWorkflowLinkStore{pool: pool}
	s.sessions = &PGSessionStore{pool: pool}
	s.executions = &PGExecutionStore{pool: pool}
	s.logs = &PGLogStore{pool: pool}
	s.audit = &PGAuditStore{pool: pool}
	s.iam = &PGIAMStore{pool: pool}
	s.configDocs = NewPGConfigStore(pool)

	return s, nil
}

// Pool returns the underlying pgxpool.Pool.
func (s *PGStore) Pool() *pgxpool.Pool { return s.pool }

// Close closes the connection pool.
func (s *PGStore) Close() { s.pool.Close() }

// Users returns the UserStore.
func (s *PGStore) Users() UserStore { return s.users }

// Companies returns the CompanyStore.
func (s *PGStore) Companies() CompanyStore { return s.companies }

// Projects returns the ProjectStore.
func (s *PGStore) Projects() ProjectStore { return s.projects }

// Workflows returns the WorkflowStore.
func (s *PGStore) Workflows() WorkflowStore { return s.workflows }

// Memberships returns the MembershipStore.
func (s *PGStore) Memberships() MembershipStore { return s.memberships }

// CrossWorkflowLinks returns the CrossWorkflowLinkStore.
func (s *PGStore) CrossWorkflowLinks() CrossWorkflowLinkStore { return s.crossWorkflowLinks }

// Sessions returns the SessionStore.
func (s *PGStore) Sessions() SessionStore { return s.sessions }

// Executions returns the ExecutionStore.
func (s *PGStore) Executions() ExecutionStore { return s.executions }

// Logs returns the LogStore.
func (s *PGStore) Logs() LogStore { return s.logs }

// Audit returns the AuditStore.
func (s *PGStore) Audit() AuditStore { return s.audit }

// IAM returns the IAMStore.
func (s *PGStore) IAM() IAMStore { return s.iam }

// ConfigDocs returns the PGConfigStore.
func (s *PGStore) ConfigDocs() *PGConfigStore { return s.configDocs }
