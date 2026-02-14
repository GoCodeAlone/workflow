package rbac

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Permission Tests ---

func TestPermission_String(t *testing.T) {
	p := Permission{Resource: ResourceWorkflows, Action: ActionRead}
	if got := p.String(); got != "workflows:read" {
		t.Errorf("expected 'workflows:read', got %q", got)
	}
}

func TestParsePermission_Valid(t *testing.T) {
	p, err := ParsePermission("modules:write")
	if err != nil {
		t.Fatalf("ParsePermission: %v", err)
	}
	if p.Resource != ResourceModules || p.Action != ActionWrite {
		t.Errorf("expected modules:write, got %s:%s", p.Resource, p.Action)
	}
}

func TestParsePermission_Invalid(t *testing.T) {
	_, err := ParsePermission("invalid")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

// --- Role Tests ---

func TestRole_HasPermission_Direct(t *testing.T) {
	if !RoleViewer.HasPermission(ResourceWorkflows, ActionRead) {
		t.Error("viewer should have read access to workflows")
	}
}

func TestRole_HasPermission_NoAccess(t *testing.T) {
	if RoleViewer.HasPermission(ResourceWorkflows, ActionWrite) {
		t.Error("viewer should not have write access to workflows")
	}
}

func TestRole_HasPermission_Admin(t *testing.T) {
	if !RoleAdmin.HasPermission(ResourceWorkflows, ActionRead) {
		t.Error("admin should have read access to workflows")
	}
	if !RoleAdmin.HasPermission(ResourceWorkflows, ActionDelete) {
		t.Error("admin should have delete access to workflows")
	}
	if !RoleAdmin.HasPermission(ResourceUsers, ActionAdmin) {
		t.Error("admin should have admin access to users")
	}
}

func TestRole_HasPermission_Operator(t *testing.T) {
	if !RoleOperator.HasPermission(ResourceWorkflows, ActionDelete) {
		t.Error("operator should have delete access to workflows")
	}
	if RoleOperator.HasPermission(ResourceUsers, ActionWrite) {
		t.Error("operator should not have write access to users")
	}
}

func TestRole_HasPermission_Editor(t *testing.T) {
	if !RoleEditor.HasPermission(ResourceWorkflows, ActionWrite) {
		t.Error("editor should have write access to workflows")
	}
	if RoleEditor.HasPermission(ResourceWorkflows, ActionDelete) {
		t.Error("editor should not have delete access to workflows")
	}
}

func TestBuiltinRoles(t *testing.T) {
	roles := BuiltinRoles()
	if len(roles) != 4 {
		t.Fatalf("expected 4 built-in roles, got %d", len(roles))
	}

	names := map[string]bool{}
	for _, r := range roles {
		names[r.Name] = true
	}
	for _, expected := range []string{"viewer", "editor", "operator", "admin"} {
		if !names[expected] {
			t.Errorf("expected built-in role %q", expected)
		}
	}
}

// --- PolicyEngine Tests ---

func TestPolicyEngine_BuiltinRoles(t *testing.T) {
	pe := NewPolicyEngine()

	if !pe.Allowed("admin", ResourceWorkflows, ActionDelete) {
		t.Error("admin should be allowed all actions")
	}
	if !pe.Allowed("viewer", ResourceWorkflows, ActionRead) {
		t.Error("viewer should be allowed to read workflows")
	}
	if pe.Allowed("viewer", ResourceWorkflows, ActionWrite) {
		t.Error("viewer should not be allowed to write workflows")
	}
}

func TestPolicyEngine_RegisterCustomRole(t *testing.T) {
	pe := NewPolicyEngine()
	pe.RegisterRole(&Role{
		Name:        "deployer",
		Description: "Can read and deploy workflows",
		Permissions: []Permission{
			{Resource: ResourceWorkflows, Action: ActionRead},
			{Resource: ResourceWorkflows, Action: ActionWrite},
		},
	})

	if !pe.Allowed("deployer", ResourceWorkflows, ActionWrite) {
		t.Error("deployer should be allowed to write workflows")
	}
	if pe.Allowed("deployer", ResourceWorkflows, ActionDelete) {
		t.Error("deployer should not be allowed to delete workflows")
	}
}

func TestPolicyEngine_UnknownRole(t *testing.T) {
	pe := NewPolicyEngine()
	if pe.Allowed("nonexistent", ResourceWorkflows, ActionRead) {
		t.Error("unknown role should not be allowed")
	}
}

func TestPolicyEngine_GetRole(t *testing.T) {
	pe := NewPolicyEngine()

	r, ok := pe.GetRole("admin")
	if !ok {
		t.Fatal("expected to find admin role")
	}
	if r.Name != "admin" {
		t.Errorf("expected role name 'admin', got %q", r.Name)
	}

	_, ok = pe.GetRole("nonexistent")
	if ok {
		t.Error("should not find nonexistent role")
	}
}

func TestPolicyEngine_ListRoles(t *testing.T) {
	pe := NewPolicyEngine()
	roles := pe.ListRoles()
	if len(roles) < 4 {
		t.Errorf("expected at least 4 roles, got %d", len(roles))
	}
}

// --- Context Tests ---

func TestContextRole(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithRole(ctx, "editor")

	role, ok := RoleFromContext(ctx)
	if !ok {
		t.Fatal("expected role in context")
	}
	if role != "editor" {
		t.Errorf("expected 'editor', got %q", role)
	}
}

func TestContextRole_Missing(t *testing.T) {
	_, ok := RoleFromContext(context.Background())
	if ok {
		t.Error("expected no role in empty context")
	}
}

func TestContextUserID(t *testing.T) {
	ctx := ContextWithUserID(context.Background(), "user-123")
	id, ok := UserIDFromContext(ctx)
	if !ok || id != "user-123" {
		t.Errorf("expected 'user-123', got %q (ok=%v)", id, ok)
	}
}

func TestContextUserID_Missing(t *testing.T) {
	_, ok := UserIDFromContext(context.Background())
	if ok {
		t.Error("expected no user ID in empty context")
	}
}

// --- Middleware Tests ---

func TestMiddleware_Allowed(t *testing.T) {
	pe := NewPolicyEngine()

	handler := Middleware(pe, ResourceWorkflows, ActionRead, HeaderRoleExtractor("X-Role"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok {
				t.Error("expected role in context")
			}
			if role != "viewer" {
				t.Errorf("expected 'viewer', got %q", role)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
	req.Header.Set("X-Role", "viewer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_Forbidden(t *testing.T) {
	pe := NewPolicyEngine()

	handler := Middleware(pe, ResourceWorkflows, ActionDelete, HeaderRoleExtractor("X-Role"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be called")
		}),
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/workflows/1", nil)
	req.Header.Set("X-Role", "viewer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestMiddleware_Unauthorized(t *testing.T) {
	pe := NewPolicyEngine()

	handler := Middleware(pe, ResourceWorkflows, ActionRead, HeaderRoleExtractor("X-Role"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be called")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
	// No X-Role header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_AdminAllowed(t *testing.T) {
	pe := NewPolicyEngine()

	handler := Middleware(pe, ResourceUsers, ActionAdmin, HeaderRoleExtractor("X-Role"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("X-Role", "admin")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- RoleExtractor Tests ---

func TestHeaderRoleExtractor_Present(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-Role", "editor")

	role, err := HeaderRoleExtractor("X-User-Role")(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "editor" {
		t.Errorf("expected 'editor', got %q", role)
	}
}

func TestHeaderRoleExtractor_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := HeaderRoleExtractor("X-User-Role")(req)
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestContextRoleExtractor_Present(t *testing.T) {
	ctx := ContextWithRole(context.Background(), "operator")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)

	role, err := ContextRoleExtractor()(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "operator" {
		t.Errorf("expected 'operator', got %q", role)
	}
}

func TestContextRoleExtractor_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := ContextRoleExtractor()(req)
	if err == nil {
		t.Fatal("expected error for missing context role")
	}
}

// --- Middleware with custom extractor that returns error ---

func TestMiddleware_ExtractorError(t *testing.T) {
	pe := NewPolicyEngine()

	errExtractor := func(r *http.Request) (string, error) {
		return "", errors.New("auth service down")
	}

	handler := Middleware(pe, ResourceWorkflows, ActionRead, errExtractor)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be called")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
