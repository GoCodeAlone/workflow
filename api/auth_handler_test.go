package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// --- mock stores ---

type mockUserStore struct {
	users   map[uuid.UUID]*store.User
	byEmail map[string]*store.User
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users:   make(map[uuid.UUID]*store.User),
		byEmail: make(map[string]*store.User),
	}
}

func (m *mockUserStore) Create(_ context.Context, u *store.User) error {
	if _, ok := m.byEmail[u.Email]; ok {
		return store.ErrDuplicate
	}
	m.users[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

func (m *mockUserStore) Get(_ context.Context, id uuid.UUID) (*store.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func (m *mockUserStore) GetByEmail(_ context.Context, email string) (*store.User, error) {
	u, ok := m.byEmail[email]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func (m *mockUserStore) GetByOAuth(_ context.Context, _ store.OAuthProvider, _ string) (*store.User, error) {
	return nil, store.ErrNotFound
}

func (m *mockUserStore) Update(_ context.Context, u *store.User) error {
	if _, ok := m.users[u.ID]; !ok {
		return store.ErrNotFound
	}
	m.users[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

func (m *mockUserStore) Delete(_ context.Context, id uuid.UUID) error {
	u, ok := m.users[id]
	if !ok {
		return store.ErrNotFound
	}
	delete(m.users, id)
	delete(m.byEmail, u.Email)
	return nil
}

func (m *mockUserStore) List(_ context.Context, _ store.UserFilter) ([]*store.User, error) {
	var result []*store.User
	for _, u := range m.users {
		result = append(result, u)
	}
	return result, nil
}

type mockSessionStore struct {
	sessions map[uuid.UUID]*store.Session
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: make(map[uuid.UUID]*store.Session)}
}

func (m *mockSessionStore) Create(_ context.Context, s *store.Session) error {
	m.sessions[s.ID] = s
	return nil
}

func (m *mockSessionStore) Get(_ context.Context, id uuid.UUID) (*store.Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return s, nil
}

func (m *mockSessionStore) GetByToken(_ context.Context, _ string) (*store.Session, error) {
	return nil, store.ErrNotFound
}

func (m *mockSessionStore) Update(_ context.Context, s *store.Session) error {
	m.sessions[s.ID] = s
	return nil
}

func (m *mockSessionStore) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.sessions, id)
	return nil
}

func (m *mockSessionStore) List(_ context.Context, _ store.SessionFilter) ([]*store.Session, error) {
	var result []*store.Session
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

func (m *mockSessionStore) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

// --- helpers ---

const testSecret = "test-secret-key-for-jwt-signing!"

func newTestAuthHandler() (*AuthHandler, *mockUserStore, *mockSessionStore) {
	users := newMockUserStore()
	sessions := newMockSessionStore()
	h := NewAuthHandler(users, sessions, []byte(testSecret), "test", 1*time.Hour, 24*time.Hour)
	return h, users, sessions
}

func makeJSON(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return body
}

// --- tests ---

func TestRegister(t *testing.T) {
	h, users, _ := newTestAuthHandler()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/register",
			makeJSON(map[string]string{
				"email":        "test@example.com",
				"password":     "Password123!",
				"display_name": "Test User",
			}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Register(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeBody(t, w.Result())
		data, _ := body["data"].(map[string]any)
		if data["access_token"] == nil {
			t.Fatal("expected access_token in response")
		}
		if data["refresh_token"] == nil {
			t.Fatal("expected refresh_token in response")
		}

		// Verify user was stored
		if _, err := users.GetByEmail(context.Background(), "test@example.com"); err != nil {
			t.Fatalf("user not found in store: %v", err)
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/register",
			makeJSON(map[string]string{
				"email":    "test@example.com",
				"password": "Password123!",
			}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Register(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", w.Code)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/register",
			makeJSON(map[string]string{"email": "x@y.com"}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Register(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestLogin(t *testing.T) {
	h, users, _ := newTestAuthHandler()

	// Create a user
	hash, _ := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
	now := time.Now()
	u := &store.User{
		ID:           uuid.New(),
		Email:        "login@example.com",
		PasswordHash: string(hash),
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_ = users.Create(context.Background(), u)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login",
			makeJSON(map[string]string{
				"email":    "login@example.com",
				"password": "Password123!",
			}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Login(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeBody(t, w.Result())
		data, _ := body["data"].(map[string]any)
		if data["access_token"] == nil {
			t.Fatal("expected access_token")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login",
			makeJSON(map[string]string{
				"email":    "login@example.com",
				"password": "WrongPass",
			}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Login(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login",
			makeJSON(map[string]string{
				"email":    "nobody@example.com",
				"password": "Password123!",
			}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Login(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

func TestRefresh(t *testing.T) {
	h, users, _ := newTestAuthHandler()

	// Create a user
	now := time.Now()
	u := &store.User{
		ID:        uuid.New(),
		Email:     "refresh@example.com",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = users.Create(context.Background(), u)

	// Generate a refresh token
	refreshClaims := jwt.MapClaims{
		"sub":  u.ID.String(),
		"type": "refresh",
		"iat":  now.Unix(),
		"exp":  now.Add(24 * time.Hour).Unix(),
	}
	refreshToken, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString([]byte(testSecret))

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh",
			makeJSON(map[string]string{"refresh_token": refreshToken}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Refresh(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh",
			makeJSON(map[string]string{"refresh_token": "garbage"}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Refresh(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("access token rejected as refresh", func(t *testing.T) {
		accessClaims := jwt.MapClaims{
			"sub":  u.ID.String(),
			"type": "access",
			"iat":  now.Unix(),
			"exp":  now.Add(1 * time.Hour).Unix(),
		}
		accessToken, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString([]byte(testSecret))

		req := httptest.NewRequest("POST", "/api/v1/auth/refresh",
			makeJSON(map[string]string{"refresh_token": accessToken}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Refresh(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

func TestMe(t *testing.T) {
	h, _, _ := newTestAuthHandler()

	t.Run("authenticated", func(t *testing.T) {
		user := &store.User{
			ID:          uuid.New(),
			Email:       "me@example.com",
			DisplayName: "Me User",
			Active:      true,
		}
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Me(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		body := decodeBody(t, w.Result())
		data, _ := body["data"].(map[string]any)
		if data["email"] != "me@example.com" {
			t.Fatalf("expected email me@example.com, got %v", data["email"])
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		w := httptest.NewRecorder()
		h.Me(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

func TestUpdateMe(t *testing.T) {
	h, users, _ := newTestAuthHandler()

	now := time.Now()
	user := &store.User{
		ID:          uuid.New(),
		Email:       "update@example.com",
		DisplayName: "Original",
		Active:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_ = users.Create(context.Background(), user)

	req := httptest.NewRequest("PUT", "/api/v1/auth/me",
		makeJSON(map[string]string{"display_name": "Updated"}))
	req.Header.Set("Content-Type", "application/json")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify in store
	updated, _ := users.Get(context.Background(), user.ID)
	if updated.DisplayName != "Updated" {
		t.Fatalf("expected display_name Updated, got %s", updated.DisplayName)
	}
}

// --- Middleware tests ---

func TestRequireAuth(t *testing.T) {
	users := newMockUserStore()
	now := time.Now()
	user := &store.User{
		ID:        uuid.New(),
		Email:     "auth@example.com",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = users.Create(context.Background(), user)

	_ = newMockSessionStore()
	perms := NewPermissionService(&mockMembershipStore{}, &mockWorkflowStore{}, &mockProjectStore{})
	mw := NewMiddleware([]byte(testSecret), users, perms)

	t.Run("valid token", func(t *testing.T) {
		accessClaims := jwt.MapClaims{
			"sub":  user.ID.String(),
			"type": "access",
			"iat":  now.Unix(),
			"exp":  now.Add(1 * time.Hour).Unix(),
		}
		token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString([]byte(testSecret))

		called := false
		handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			u := UserFromContext(r.Context())
			if u == nil || u.ID != user.ID {
				t.Fatal("expected user in context")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if !called {
			t.Fatal("handler was not called")
		}
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("no token", func(t *testing.T) {
		handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		method := method
		t.Run("rejected algorithm "+method.Alg(), func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub":  user.ID.String(),
				"type": "access",
				"iat":  now.Unix(),
				"exp":  now.Add(1 * time.Hour).Unix(),
			}
			tok, _ := jwt.NewWithClaims(method, claims).SignedString([]byte(testSecret))

			handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be called")
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: expected 401, got %d", method.Alg(), w.Code)
			}
		})
	}
}

// --- minimal mock stores for permission service in tests ---

type mockMembershipStore struct {
	memberships []*store.Membership
}

func (m *mockMembershipStore) Create(_ context.Context, mem *store.Membership) error {
	m.memberships = append(m.memberships, mem)
	return nil
}

func (m *mockMembershipStore) Get(_ context.Context, id uuid.UUID) (*store.Membership, error) {
	for _, mem := range m.memberships {
		if mem.ID == id {
			return mem, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockMembershipStore) Update(_ context.Context, mem *store.Membership) error {
	for i, existing := range m.memberships {
		if existing.ID == mem.ID {
			m.memberships[i] = mem
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *mockMembershipStore) Delete(_ context.Context, id uuid.UUID) error {
	for i, mem := range m.memberships {
		if mem.ID == id {
			m.memberships = append(m.memberships[:i], m.memberships[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *mockMembershipStore) List(_ context.Context, f store.MembershipFilter) ([]*store.Membership, error) {
	var result []*store.Membership
	for _, mem := range m.memberships {
		if f.CompanyID != nil && mem.CompanyID != *f.CompanyID {
			continue
		}
		if f.UserID != nil && mem.UserID != *f.UserID {
			continue
		}
		result = append(result, mem)
	}
	return result, nil
}

func (m *mockMembershipStore) GetEffectiveRole(_ context.Context, userID uuid.UUID, companyID uuid.UUID, projectID *uuid.UUID) (store.Role, error) {
	// Check project-level first, then company-level
	for _, mem := range m.memberships {
		if mem.UserID != userID || mem.CompanyID != companyID {
			continue
		}
		if projectID != nil && mem.ProjectID != nil && *mem.ProjectID == *projectID {
			return mem.Role, nil
		}
	}
	// Fallback to company-level
	for _, mem := range m.memberships {
		if mem.UserID == userID && mem.CompanyID == companyID && mem.ProjectID == nil {
			return mem.Role, nil
		}
	}
	return "", errors.New("no membership")
}

type mockWorkflowStore struct {
	workflows map[uuid.UUID]*store.WorkflowRecord
}

func (m *mockWorkflowStore) Create(_ context.Context, w *store.WorkflowRecord) error {
	if m.workflows == nil {
		m.workflows = make(map[uuid.UUID]*store.WorkflowRecord)
	}
	m.workflows[w.ID] = w
	return nil
}

func (m *mockWorkflowStore) Get(_ context.Context, id uuid.UUID) (*store.WorkflowRecord, error) {
	if m.workflows == nil {
		return nil, store.ErrNotFound
	}
	w, ok := m.workflows[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return w, nil
}

func (m *mockWorkflowStore) GetBySlug(_ context.Context, _ uuid.UUID, _ string) (*store.WorkflowRecord, error) {
	return nil, store.ErrNotFound
}

func (m *mockWorkflowStore) Update(_ context.Context, w *store.WorkflowRecord) error {
	if m.workflows == nil {
		return store.ErrNotFound
	}
	m.workflows[w.ID] = w
	return nil
}

func (m *mockWorkflowStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.workflows == nil {
		return store.ErrNotFound
	}
	delete(m.workflows, id)
	return nil
}

func (m *mockWorkflowStore) List(_ context.Context, f store.WorkflowFilter) ([]*store.WorkflowRecord, error) {
	var result []*store.WorkflowRecord
	for _, w := range m.workflows {
		if f.ProjectID != nil && w.ProjectID != *f.ProjectID {
			continue
		}
		result = append(result, w)
	}
	return result, nil
}

func (m *mockWorkflowStore) GetVersion(_ context.Context, _ uuid.UUID, _ int) (*store.WorkflowRecord, error) {
	return nil, store.ErrNotFound
}

func (m *mockWorkflowStore) ListVersions(_ context.Context, _ uuid.UUID) ([]*store.WorkflowRecord, error) {
	return nil, nil
}

type mockProjectStore struct {
	projects map[uuid.UUID]*store.Project
}

func (m *mockProjectStore) Create(_ context.Context, p *store.Project) error {
	if m.projects == nil {
		m.projects = make(map[uuid.UUID]*store.Project)
	}
	m.projects[p.ID] = p
	return nil
}

func (m *mockProjectStore) Get(_ context.Context, id uuid.UUID) (*store.Project, error) {
	if m.projects == nil {
		return nil, store.ErrNotFound
	}
	p, ok := m.projects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return p, nil
}

func (m *mockProjectStore) GetBySlug(_ context.Context, _ uuid.UUID, _ string) (*store.Project, error) {
	return nil, store.ErrNotFound
}

func (m *mockProjectStore) Update(_ context.Context, p *store.Project) error {
	if m.projects == nil {
		return store.ErrNotFound
	}
	m.projects[p.ID] = p
	return nil
}

func (m *mockProjectStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.projects == nil {
		return store.ErrNotFound
	}
	delete(m.projects, id)
	return nil
}

func (m *mockProjectStore) List(_ context.Context, _ store.ProjectFilter) ([]*store.Project, error) {
	var result []*store.Project
	for _, p := range m.projects {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockProjectStore) ListForUser(_ context.Context, _ uuid.UUID) ([]*store.Project, error) {
	return nil, nil
}
