package store

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// MockUserStore
// ---------------------------------------------------------------------------

// MockUserStore is an in-memory implementation of UserStore for testing.
type MockUserStore struct {
	mu    sync.Mutex
	users map[uuid.UUID]*User
}

// NewMockUserStore creates a new MockUserStore.
func NewMockUserStore() *MockUserStore {
	return &MockUserStore{users: make(map[uuid.UUID]*User)}
}

func (s *MockUserStore) Create(_ context.Context, u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	for _, existing := range s.users {
		if existing.Email == u.Email {
			return ErrDuplicate
		}
	}
	now := time.Now()
	u.CreatedAt = now
	u.UpdatedAt = now
	cp := *u
	s.users[u.ID] = &cp
	return nil
}

func (s *MockUserStore) Get(_ context.Context, id uuid.UUID) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (s *MockUserStore) GetByEmail(_ context.Context, email string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockUserStore) GetByOAuth(_ context.Context, provider OAuthProvider, oauthID string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.OAuthProvider == provider && u.OAuthID == oauthID {
			cp := *u
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockUserStore) Update(_ context.Context, u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[u.ID]; !ok {
		return ErrNotFound
	}
	// Check email uniqueness against other users.
	for id, existing := range s.users {
		if id != u.ID && existing.Email == u.Email {
			return ErrDuplicate
		}
	}
	u.UpdatedAt = time.Now()
	cp := *u
	s.users[u.ID] = &cp
	return nil
}

func (s *MockUserStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return ErrNotFound
	}
	delete(s.users, id)
	return nil
}

func (s *MockUserStore) List(_ context.Context, f UserFilter) ([]*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*User
	for _, u := range s.users {
		if f.Email != "" && u.Email != f.Email {
			continue
		}
		if f.Active != nil && u.Active != *f.Active {
			continue
		}
		if f.OAuthProvider != "" && u.OAuthProvider != f.OAuthProvider {
			continue
		}
		cp := *u
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

// ---------------------------------------------------------------------------
// MockCompanyStore
// ---------------------------------------------------------------------------

// MockCompanyStore is an in-memory implementation of CompanyStore for testing.
type MockCompanyStore struct {
	mu              sync.Mutex
	companies       map[uuid.UUID]*Company
	membershipStore *MockMembershipStore
}

// NewMockCompanyStore creates a new MockCompanyStore.
func NewMockCompanyStore() *MockCompanyStore {
	return &MockCompanyStore{companies: make(map[uuid.UUID]*Company)}
}

// SetMembershipStore links a MockMembershipStore for ListForUser support.
func (s *MockCompanyStore) SetMembershipStore(ms *MockMembershipStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.membershipStore = ms
}

func (s *MockCompanyStore) Create(_ context.Context, c *Company) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	for _, existing := range s.companies {
		if existing.Slug == c.Slug {
			return ErrDuplicate
		}
	}
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	cp := *c
	s.companies[c.ID] = &cp
	return nil
}

func (s *MockCompanyStore) Get(_ context.Context, id uuid.UUID) (*Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.companies[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *MockCompanyStore) GetBySlug(_ context.Context, slug string) (*Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.companies {
		if c.Slug == slug {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockCompanyStore) Update(_ context.Context, c *Company) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.companies[c.ID]; !ok {
		return ErrNotFound
	}
	for id, existing := range s.companies {
		if id != c.ID && existing.Slug == c.Slug {
			return ErrDuplicate
		}
	}
	c.UpdatedAt = time.Now()
	cp := *c
	s.companies[c.ID] = &cp
	return nil
}

func (s *MockCompanyStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.companies[id]; !ok {
		return ErrNotFound
	}
	delete(s.companies, id)
	return nil
}

func (s *MockCompanyStore) List(_ context.Context, f CompanyFilter) ([]*Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*Company
	for _, c := range s.companies {
		if f.OwnerID != nil && c.OwnerID != *f.OwnerID {
			continue
		}
		if f.Slug != "" && c.Slug != f.Slug {
			continue
		}
		cp := *c
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockCompanyStore) ListForUser(_ context.Context, userID uuid.UUID) ([]*Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[uuid.UUID]bool)
	var results []*Company

	// Owner always sees their companies.
	for _, c := range s.companies {
		if c.OwnerID == userID {
			seen[c.ID] = true
			cp := *c
			results = append(results, &cp)
		}
	}

	// Also include companies where the user has a membership.
	if s.membershipStore != nil {
		s.membershipStore.mu.Lock()
		for _, m := range s.membershipStore.memberships {
			if m.UserID == userID && !seen[m.CompanyID] {
				if c, ok := s.companies[m.CompanyID]; ok {
					seen[m.CompanyID] = true
					cp := *c
					results = append(results, &cp)
				}
			}
		}
		s.membershipStore.mu.Unlock()
	}

	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return results, nil
}

// ---------------------------------------------------------------------------
// MockProjectStore
// ---------------------------------------------------------------------------

// MockProjectStore is an in-memory implementation of ProjectStore for testing.
type MockProjectStore struct {
	mu              sync.Mutex
	projects        map[uuid.UUID]*Project
	membershipStore *MockMembershipStore
}

// NewMockProjectStore creates a new MockProjectStore.
func NewMockProjectStore() *MockProjectStore {
	return &MockProjectStore{projects: make(map[uuid.UUID]*Project)}
}

// SetMembershipStore links a MockMembershipStore for ListForUser support.
func (s *MockProjectStore) SetMembershipStore(ms *MockMembershipStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.membershipStore = ms
}

func (s *MockProjectStore) Create(_ context.Context, p *Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	// Slug uniqueness within company.
	for _, existing := range s.projects {
		if existing.CompanyID == p.CompanyID && existing.Slug == p.Slug {
			return ErrDuplicate
		}
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	cp := *p
	s.projects[p.ID] = &cp
	return nil
}

func (s *MockProjectStore) Get(_ context.Context, id uuid.UUID) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *MockProjectStore) GetBySlug(_ context.Context, companyID uuid.UUID, slug string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projects {
		if p.CompanyID == companyID && p.Slug == slug {
			cp := *p
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockProjectStore) Update(_ context.Context, p *Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[p.ID]; !ok {
		return ErrNotFound
	}
	for id, existing := range s.projects {
		if id != p.ID && existing.CompanyID == p.CompanyID && existing.Slug == p.Slug {
			return ErrDuplicate
		}
	}
	p.UpdatedAt = time.Now()
	cp := *p
	s.projects[p.ID] = &cp
	return nil
}

func (s *MockProjectStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[id]; !ok {
		return ErrNotFound
	}
	delete(s.projects, id)
	return nil
}

func (s *MockProjectStore) List(_ context.Context, f ProjectFilter) ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*Project
	for _, p := range s.projects {
		if f.CompanyID != nil && p.CompanyID != *f.CompanyID {
			continue
		}
		if f.Slug != "" && p.Slug != f.Slug {
			continue
		}
		cp := *p
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockProjectStore) ListForUser(_ context.Context, userID uuid.UUID) ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[uuid.UUID]bool)
	var results []*Project

	if s.membershipStore != nil {
		s.membershipStore.mu.Lock()
		for _, m := range s.membershipStore.memberships {
			if m.UserID == userID && m.ProjectID != nil && !seen[*m.ProjectID] {
				if p, ok := s.projects[*m.ProjectID]; ok {
					seen[*m.ProjectID] = true
					cp := *p
					results = append(results, &cp)
				}
			}
		}
		s.membershipStore.mu.Unlock()
	}

	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return results, nil
}

// ---------------------------------------------------------------------------
// MockWorkflowStore
// ---------------------------------------------------------------------------

// MockWorkflowStore is an in-memory implementation of WorkflowStore for testing.
type MockWorkflowStore struct {
	mu        sync.Mutex
	workflows map[uuid.UUID]*WorkflowRecord
	versions  map[uuid.UUID][]*WorkflowRecord // version history keyed by workflow ID
}

// NewMockWorkflowStore creates a new MockWorkflowStore.
func NewMockWorkflowStore() *MockWorkflowStore {
	return &MockWorkflowStore{
		workflows: make(map[uuid.UUID]*WorkflowRecord),
		versions:  make(map[uuid.UUID][]*WorkflowRecord),
	}
}

func (s *MockWorkflowStore) Create(_ context.Context, w *WorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	for _, existing := range s.workflows {
		if existing.ProjectID == w.ProjectID && existing.Slug == w.Slug {
			return ErrDuplicate
		}
	}
	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.Version == 0 {
		w.Version = 1
	}
	cp := *w
	s.workflows[w.ID] = &cp
	// Store initial version.
	v := *w
	s.versions[w.ID] = []*WorkflowRecord{&v}
	return nil
}

func (s *MockWorkflowStore) Get(_ context.Context, id uuid.UUID) (*WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (s *MockWorkflowStore) GetBySlug(_ context.Context, projectID uuid.UUID, slug string) (*WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workflows {
		if w.ProjectID == projectID && w.Slug == slug {
			cp := *w
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockWorkflowStore) Update(_ context.Context, w *WorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.workflows[w.ID]
	if !ok {
		return ErrNotFound
	}
	for id, other := range s.workflows {
		if id != w.ID && other.ProjectID == w.ProjectID && other.Slug == w.Slug {
			return ErrDuplicate
		}
	}
	w.Version = existing.Version + 1
	w.UpdatedAt = time.Now()
	cp := *w
	s.workflows[w.ID] = &cp
	// Append new version to history.
	v := *w
	s.versions[w.ID] = append(s.versions[w.ID], &v)
	return nil
}

func (s *MockWorkflowStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflows[id]; !ok {
		return ErrNotFound
	}
	delete(s.workflows, id)
	delete(s.versions, id)
	return nil
}

func (s *MockWorkflowStore) List(_ context.Context, f WorkflowFilter) ([]*WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*WorkflowRecord
	for _, w := range s.workflows {
		if f.ProjectID != nil && w.ProjectID != *f.ProjectID {
			continue
		}
		if f.Status != "" && w.Status != f.Status {
			continue
		}
		if f.Slug != "" && w.Slug != f.Slug {
			continue
		}
		cp := *w
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockWorkflowStore) GetVersion(_ context.Context, id uuid.UUID, version int) (*WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history, ok := s.versions[id]
	if !ok {
		return nil, ErrNotFound
	}
	for _, v := range history {
		if v.Version == version {
			cp := *v
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockWorkflowStore) ListVersions(_ context.Context, id uuid.UUID) ([]*WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history, ok := s.versions[id]
	if !ok {
		return nil, ErrNotFound
	}
	results := make([]*WorkflowRecord, len(history))
	for i, v := range history {
		cp := *v
		results[i] = &cp
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// MockMembershipStore
// ---------------------------------------------------------------------------

// MockMembershipStore is an in-memory implementation of MembershipStore for testing.
type MockMembershipStore struct {
	mu          sync.Mutex
	memberships map[uuid.UUID]*Membership
}

// NewMockMembershipStore creates a new MockMembershipStore.
func NewMockMembershipStore() *MockMembershipStore {
	return &MockMembershipStore{memberships: make(map[uuid.UUID]*Membership)}
}

func (s *MockMembershipStore) Create(_ context.Context, m *Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	// Prevent duplicate memberships for same user/company/project.
	for _, existing := range s.memberships {
		if existing.UserID == m.UserID && existing.CompanyID == m.CompanyID {
			if m.ProjectID == nil && existing.ProjectID == nil {
				return ErrDuplicate
			}
			if m.ProjectID != nil && existing.ProjectID != nil && *m.ProjectID == *existing.ProjectID {
				return ErrDuplicate
			}
		}
	}
	now := time.Now()
	m.CreatedAt = now
	m.UpdatedAt = now
	cp := *m
	if m.ProjectID != nil {
		pid := *m.ProjectID
		cp.ProjectID = &pid
	}
	s.memberships[m.ID] = &cp
	return nil
}

func (s *MockMembershipStore) Get(_ context.Context, id uuid.UUID) (*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memberships[id]
	if !ok {
		return nil, ErrNotFound
	}
	return copyMembership(m), nil
}

func (s *MockMembershipStore) Update(_ context.Context, m *Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.memberships[m.ID]; !ok {
		return ErrNotFound
	}
	m.UpdatedAt = time.Now()
	cp := *m
	if m.ProjectID != nil {
		pid := *m.ProjectID
		cp.ProjectID = &pid
	}
	s.memberships[m.ID] = &cp
	return nil
}

func (s *MockMembershipStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.memberships[id]; !ok {
		return ErrNotFound
	}
	delete(s.memberships, id)
	return nil
}

func (s *MockMembershipStore) List(_ context.Context, f MembershipFilter) ([]*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*Membership
	for _, m := range s.memberships {
		if f.UserID != nil && m.UserID != *f.UserID {
			continue
		}
		if f.CompanyID != nil && m.CompanyID != *f.CompanyID {
			continue
		}
		if f.ProjectID != nil {
			if m.ProjectID == nil || *m.ProjectID != *f.ProjectID {
				continue
			}
		}
		if f.Role != "" && m.Role != f.Role {
			continue
		}
		results = append(results, copyMembership(m))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockMembershipStore) GetEffectiveRole(_ context.Context, userID, companyID uuid.UUID, projectID *uuid.UUID) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First try project-level membership if projectID is provided.
	if projectID != nil {
		for _, m := range s.memberships {
			if m.UserID == userID && m.CompanyID == companyID && m.ProjectID != nil && *m.ProjectID == *projectID {
				return m.Role, nil
			}
		}
	}

	// Fall back to company-level membership.
	for _, m := range s.memberships {
		if m.UserID == userID && m.CompanyID == companyID && m.ProjectID == nil {
			return m.Role, nil
		}
	}

	return "", ErrNotFound
}

func copyMembership(m *Membership) *Membership {
	cp := *m
	if m.ProjectID != nil {
		pid := *m.ProjectID
		cp.ProjectID = &pid
	}
	return &cp
}

// ---------------------------------------------------------------------------
// MockCrossWorkflowLinkStore
// ---------------------------------------------------------------------------

// MockCrossWorkflowLinkStore is an in-memory implementation of CrossWorkflowLinkStore for testing.
type MockCrossWorkflowLinkStore struct {
	mu    sync.Mutex
	links map[uuid.UUID]*CrossWorkflowLink
}

// NewMockCrossWorkflowLinkStore creates a new MockCrossWorkflowLinkStore.
func NewMockCrossWorkflowLinkStore() *MockCrossWorkflowLinkStore {
	return &MockCrossWorkflowLinkStore{links: make(map[uuid.UUID]*CrossWorkflowLink)}
}

func (s *MockCrossWorkflowLinkStore) Create(_ context.Context, l *CrossWorkflowLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	l.CreatedAt = time.Now()
	cp := *l
	s.links[l.ID] = &cp
	return nil
}

func (s *MockCrossWorkflowLinkStore) Get(_ context.Context, id uuid.UUID) (*CrossWorkflowLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *l
	return &cp, nil
}

func (s *MockCrossWorkflowLinkStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[id]; !ok {
		return ErrNotFound
	}
	delete(s.links, id)
	return nil
}

func (s *MockCrossWorkflowLinkStore) List(_ context.Context, f CrossWorkflowLinkFilter) ([]*CrossWorkflowLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*CrossWorkflowLink
	for _, l := range s.links {
		if f.SourceWorkflowID != nil && l.SourceWorkflowID != *f.SourceWorkflowID {
			continue
		}
		if f.TargetWorkflowID != nil && l.TargetWorkflowID != *f.TargetWorkflowID {
			continue
		}
		if f.LinkType != "" && l.LinkType != f.LinkType {
			continue
		}
		cp := *l
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

// ---------------------------------------------------------------------------
// MockSessionStore
// ---------------------------------------------------------------------------

// MockSessionStore is an in-memory implementation of SessionStore for testing.
type MockSessionStore struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*Session
}

// NewMockSessionStore creates a new MockSessionStore.
func NewMockSessionStore() *MockSessionStore {
	return &MockSessionStore{sessions: make(map[uuid.UUID]*Session)}
}

func (s *MockSessionStore) Create(_ context.Context, sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.ID == uuid.Nil {
		sess.ID = uuid.New()
	}
	sess.CreatedAt = time.Now()
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

func (s *MockSessionStore) Get(_ context.Context, id uuid.UUID) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *sess
	return &cp, nil
}

func (s *MockSessionStore) GetByToken(_ context.Context, token string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.Token == token {
			cp := *sess
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MockSessionStore) Update(_ context.Context, sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sess.ID]; !ok {
		return ErrNotFound
	}
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

func (s *MockSessionStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return ErrNotFound
	}
	delete(s.sessions, id)
	return nil
}

func (s *MockSessionStore) List(_ context.Context, f SessionFilter) ([]*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*Session
	for _, sess := range s.sessions {
		if f.UserID != nil && sess.UserID != *f.UserID {
			continue
		}
		if f.Active != nil && sess.Active != *f.Active {
			continue
		}
		cp := *sess
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockSessionStore) DeleteExpired(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var count int64
	for id, sess := range s.sessions {
		if sess.ExpiresAt.Before(now) {
			delete(s.sessions, id)
			count++
		}
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// MockExecutionStore
// ---------------------------------------------------------------------------

// MockExecutionStore is an in-memory implementation of ExecutionStore for testing.
type MockExecutionStore struct {
	mu         sync.Mutex
	executions map[uuid.UUID]*WorkflowExecution
	steps      map[uuid.UUID]*ExecutionStep
}

// NewMockExecutionStore creates a new MockExecutionStore.
func NewMockExecutionStore() *MockExecutionStore {
	return &MockExecutionStore{
		executions: make(map[uuid.UUID]*WorkflowExecution),
		steps:      make(map[uuid.UUID]*ExecutionStep),
	}
}

func (s *MockExecutionStore) CreateExecution(_ context.Context, e *WorkflowExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	e.StartedAt = time.Now()
	cp := *e
	s.executions[e.ID] = &cp
	return nil
}

func (s *MockExecutionStore) GetExecution(_ context.Context, id uuid.UUID) (*WorkflowExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.executions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (s *MockExecutionStore) UpdateExecution(_ context.Context, e *WorkflowExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.executions[e.ID]; !ok {
		return ErrNotFound
	}
	cp := *e
	s.executions[e.ID] = &cp
	return nil
}

func (s *MockExecutionStore) ListExecutions(_ context.Context, f ExecutionFilter) ([]*WorkflowExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*WorkflowExecution
	for _, e := range s.executions {
		if f.WorkflowID != nil && e.WorkflowID != *f.WorkflowID {
			continue
		}
		if f.Status != "" && e.Status != f.Status {
			continue
		}
		if f.Since != nil && e.StartedAt.Before(*f.Since) {
			continue
		}
		if f.Until != nil && e.StartedAt.After(*f.Until) {
			continue
		}
		cp := *e
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].StartedAt.Before(results[j].StartedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockExecutionStore) CreateStep(_ context.Context, step *ExecutionStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	cp := *step
	s.steps[step.ID] = &cp
	return nil
}

func (s *MockExecutionStore) UpdateStep(_ context.Context, step *ExecutionStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.steps[step.ID]; !ok {
		return ErrNotFound
	}
	cp := *step
	s.steps[step.ID] = &cp
	return nil
}

func (s *MockExecutionStore) ListSteps(_ context.Context, executionID uuid.UUID) ([]*ExecutionStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*ExecutionStep
	for _, step := range s.steps {
		if step.ExecutionID == executionID {
			cp := *step
			results = append(results, &cp)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].SequenceNum < results[j].SequenceNum })
	return results, nil
}

func (s *MockExecutionStore) CountByStatus(_ context.Context, workflowID uuid.UUID) (map[ExecutionStatus]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[ExecutionStatus]int)
	for _, e := range s.executions {
		if e.WorkflowID == workflowID {
			counts[e.Status]++
		}
	}
	return counts, nil
}

// ---------------------------------------------------------------------------
// MockLogStore
// ---------------------------------------------------------------------------

// MockLogStore is an in-memory implementation of LogStore for testing.
type MockLogStore struct {
	mu    sync.Mutex
	logs  []*ExecutionLog
	idSeq atomic.Int64
}

// NewMockLogStore creates a new MockLogStore.
func NewMockLogStore() *MockLogStore {
	return &MockLogStore{}
}

func (s *MockLogStore) Append(_ context.Context, l *ExecutionLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l.ID = s.idSeq.Add(1)
	l.CreatedAt = time.Now()
	cp := *l
	if l.ExecutionID != nil {
		eid := *l.ExecutionID
		cp.ExecutionID = &eid
	}
	s.logs = append(s.logs, &cp)
	return nil
}

func (s *MockLogStore) Query(_ context.Context, f LogFilter) ([]*ExecutionLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*ExecutionLog
	for _, l := range s.logs {
		if f.WorkflowID != nil && l.WorkflowID != *f.WorkflowID {
			continue
		}
		if f.ExecutionID != nil {
			if l.ExecutionID == nil || *l.ExecutionID != *f.ExecutionID {
				continue
			}
		}
		if f.Level != "" && l.Level != f.Level {
			continue
		}
		if f.ModuleName != "" && l.ModuleName != f.ModuleName {
			continue
		}
		if f.Since != nil && l.CreatedAt.Before(*f.Since) {
			continue
		}
		if f.Until != nil && l.CreatedAt.After(*f.Until) {
			continue
		}
		cp := *l
		if l.ExecutionID != nil {
			eid := *l.ExecutionID
			cp.ExecutionID = &eid
		}
		results = append(results, &cp)
	}
	return applyPagination(results, f.Pagination), nil
}

func (s *MockLogStore) CountByLevel(_ context.Context, workflowID uuid.UUID) (map[LogLevel]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[LogLevel]int)
	for _, l := range s.logs {
		if l.WorkflowID == workflowID {
			counts[l.Level]++
		}
	}
	return counts, nil
}

// ---------------------------------------------------------------------------
// MockAuditStore
// ---------------------------------------------------------------------------

// MockAuditStore is an in-memory implementation of AuditStore for testing.
type MockAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
	idSeq   atomic.Int64
}

// NewMockAuditStore creates a new MockAuditStore.
func NewMockAuditStore() *MockAuditStore {
	return &MockAuditStore{}
}

func (s *MockAuditStore) Record(_ context.Context, e *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = s.idSeq.Add(1)
	e.CreatedAt = time.Now()
	cp := *e
	if e.UserID != nil {
		uid := *e.UserID
		cp.UserID = &uid
	}
	if e.ResourceID != nil {
		rid := *e.ResourceID
		cp.ResourceID = &rid
	}
	s.entries = append(s.entries, &cp)
	return nil
}

func (s *MockAuditStore) Query(_ context.Context, f AuditFilter) ([]*AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*AuditEntry
	for _, e := range s.entries {
		if f.UserID != nil {
			if e.UserID == nil || *e.UserID != *f.UserID {
				continue
			}
		}
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		if f.ResourceType != "" && e.ResourceType != f.ResourceType {
			continue
		}
		if f.ResourceID != nil {
			if e.ResourceID == nil || *e.ResourceID != *f.ResourceID {
				continue
			}
		}
		if f.Since != nil && e.CreatedAt.Before(*f.Since) {
			continue
		}
		if f.Until != nil && e.CreatedAt.After(*f.Until) {
			continue
		}
		cp := *e
		if e.UserID != nil {
			uid := *e.UserID
			cp.UserID = &uid
		}
		if e.ResourceID != nil {
			rid := *e.ResourceID
			cp.ResourceID = &rid
		}
		results = append(results, &cp)
	}
	return applyPagination(results, f.Pagination), nil
}

// ---------------------------------------------------------------------------
// MockIAMStore
// ---------------------------------------------------------------------------

// MockIAMStore is an in-memory implementation of IAMStore for testing.
type MockIAMStore struct {
	mu        sync.Mutex
	providers map[uuid.UUID]*IAMProviderConfig
	mappings  map[uuid.UUID]*IAMRoleMapping
}

// NewMockIAMStore creates a new MockIAMStore.
func NewMockIAMStore() *MockIAMStore {
	return &MockIAMStore{
		providers: make(map[uuid.UUID]*IAMProviderConfig),
		mappings:  make(map[uuid.UUID]*IAMRoleMapping),
	}
}

func (s *MockIAMStore) CreateProvider(_ context.Context, p *IAMProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	cp := *p
	s.providers[p.ID] = &cp
	return nil
}

func (s *MockIAMStore) GetProvider(_ context.Context, id uuid.UUID) (*IAMProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.providers[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *MockIAMStore) UpdateProvider(_ context.Context, p *IAMProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.providers[p.ID]; !ok {
		return ErrNotFound
	}
	p.UpdatedAt = time.Now()
	cp := *p
	s.providers[p.ID] = &cp
	return nil
}

func (s *MockIAMStore) DeleteProvider(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.providers[id]; !ok {
		return ErrNotFound
	}
	delete(s.providers, id)
	return nil
}

func (s *MockIAMStore) ListProviders(_ context.Context, f IAMProviderFilter) ([]*IAMProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*IAMProviderConfig
	for _, p := range s.providers {
		if f.CompanyID != nil && p.CompanyID != *f.CompanyID {
			continue
		}
		if f.ProviderType != "" && p.ProviderType != f.ProviderType {
			continue
		}
		if f.Enabled != nil && p.Enabled != *f.Enabled {
			continue
		}
		cp := *p
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockIAMStore) CreateMapping(_ context.Context, m *IAMRoleMapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = time.Now()
	cp := *m
	s.mappings[m.ID] = &cp
	return nil
}

func (s *MockIAMStore) GetMapping(_ context.Context, id uuid.UUID) (*IAMRoleMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.mappings[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m
	return &cp, nil
}

func (s *MockIAMStore) DeleteMapping(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.mappings[id]; !ok {
		return ErrNotFound
	}
	delete(s.mappings, id)
	return nil
}

func (s *MockIAMStore) ListMappings(_ context.Context, f IAMRoleMappingFilter) ([]*IAMRoleMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*IAMRoleMapping
	for _, m := range s.mappings {
		if f.ProviderID != nil && m.ProviderID != *f.ProviderID {
			continue
		}
		if f.ExternalIdentifier != "" && m.ExternalIdentifier != f.ExternalIdentifier {
			continue
		}
		if f.ResourceType != "" && m.ResourceType != f.ResourceType {
			continue
		}
		if f.ResourceID != nil && m.ResourceID != *f.ResourceID {
			continue
		}
		cp := *m
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return applyPagination(results, f.Pagination), nil
}

func (s *MockIAMStore) ResolveRole(_ context.Context, providerID uuid.UUID, externalID string, resourceType string, resourceID uuid.UUID) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mappings {
		if m.ProviderID == providerID && m.ExternalIdentifier == externalID && m.ResourceType == resourceType && m.ResourceID == resourceID {
			return m.Role, nil
		}
	}
	return "", ErrNotFound
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var (
	_ UserStore              = (*MockUserStore)(nil)
	_ CompanyStore           = (*MockCompanyStore)(nil)
	_ ProjectStore           = (*MockProjectStore)(nil)
	_ WorkflowStore          = (*MockWorkflowStore)(nil)
	_ MembershipStore        = (*MockMembershipStore)(nil)
	_ CrossWorkflowLinkStore = (*MockCrossWorkflowLinkStore)(nil)
	_ SessionStore           = (*MockSessionStore)(nil)
	_ ExecutionStore         = (*MockExecutionStore)(nil)
	_ LogStore               = (*MockLogStore)(nil)
	_ AuditStore             = (*MockAuditStore)(nil)
	_ IAMStore               = (*MockIAMStore)(nil)
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func applyPagination[T any](items []*T, p Pagination) []*T {
	if len(items) == 0 {
		return items
	}
	start := p.Offset
	if start > len(items) {
		return nil
	}
	items = items[start:]
	if p.Limit > 0 && p.Limit < len(items) {
		items = items[:p.Limit]
	}
	return items
}
