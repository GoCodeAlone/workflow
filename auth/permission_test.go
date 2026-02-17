package auth

import (
	"context"
	"fmt"
	"sort"
	"testing"
)

// mockProvider implements PermissionProvider for testing.
type mockProvider struct {
	name        string
	permissions map[string][]Permission // subject -> permissions
	checkFunc   func(ctx context.Context, subject, resource, action string) (bool, error)
	syncedRoles []RoleDefinition
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		name:        name,
		permissions: make(map[string][]Permission),
	}
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) CheckPermission(ctx context.Context, subject, resource, action string) (bool, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, subject, resource, action)
	}
	for _, p := range m.permissions[subject] {
		if p.Resource == resource && p.Action == action && p.Effect == "allow" {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockProvider) ListPermissions(_ context.Context, subject string) ([]Permission, error) {
	return m.permissions[subject], nil
}

func (m *mockProvider) SyncRoles(_ context.Context, roles []RoleDefinition) error {
	m.syncedRoles = roles
	return nil
}

// --- PermissionManager Tests ---

func TestPermissionManager_AddProvider_SetsPrimary(t *testing.T) {
	pm := NewPermissionManager()
	p := newMockProvider("test")
	p.permissions["alice"] = []Permission{{Resource: "workflows", Action: "read", Effect: "allow"}}

	pm.AddProvider(p)

	allowed, err := pm.Check(context.Background(), "alice", "workflows", "read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected permission to be allowed")
	}
}

func TestPermissionManager_Check_NoPrimary(t *testing.T) {
	pm := NewPermissionManager()
	_, err := pm.Check(context.Background(), "alice", "workflows", "read")
	if err == nil {
		t.Fatal("expected error when no provider configured")
	}
}

func TestPermissionManager_SetPrimary(t *testing.T) {
	pm := NewPermissionManager()

	p1 := newMockProvider("provider1")
	p1.checkFunc = func(_ context.Context, _, _, _ string) (bool, error) {
		return false, nil
	}

	p2 := newMockProvider("provider2")
	p2.checkFunc = func(_ context.Context, _, _, _ string) (bool, error) {
		return true, nil
	}

	pm.AddProvider(p1) // becomes primary
	pm.AddProvider(p2)

	// p1 is primary, should deny
	allowed, err := pm.Check(context.Background(), "alice", "workflows", "read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected denied with provider1 as primary")
	}

	// switch primary to p2
	if err := pm.SetPrimary("provider2"); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}

	allowed, err = pm.Check(context.Background(), "alice", "workflows", "read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with provider2 as primary")
	}
}

func TestPermissionManager_SetPrimary_NotFound(t *testing.T) {
	pm := NewPermissionManager()
	if err := pm.SetPrimary("nonexistent"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestPermissionManager_ListAll(t *testing.T) {
	pm := NewPermissionManager()

	p1 := newMockProvider("provider1")
	p1.permissions["alice"] = []Permission{
		{Resource: "workflows", Action: "read", Effect: "allow"},
	}

	p2 := newMockProvider("provider2")
	p2.permissions["alice"] = []Permission{
		{Resource: "modules", Action: "write", Effect: "allow"},
	}

	pm.AddProvider(p1)
	pm.AddProvider(p2)

	perms, err := pm.ListAll(context.Background(), "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}

	// Sort for deterministic comparison
	sort.Slice(perms, func(i, j int) bool {
		return perms[i].Resource < perms[j].Resource
	})
	if perms[0].Resource != "modules" || perms[1].Resource != "workflows" {
		t.Errorf("unexpected permissions: %+v", perms)
	}
}

func TestPermissionManager_ListAll_Error(t *testing.T) {
	pm := NewPermissionManager()

	p := newMockProvider("failing")
	p.permissions = nil // will cause nil map access to return nil, not error

	// Override ListPermissions to return an error
	errProvider := &errorListProvider{name: "failing"}
	pm.AddProvider(errProvider)

	_, err := pm.ListAll(context.Background(), "alice")
	if err == nil {
		t.Fatal("expected error from failing provider")
	}
}

func TestPermissionManager_Providers(t *testing.T) {
	pm := NewPermissionManager()
	pm.AddProvider(newMockProvider("alpha"))
	pm.AddProvider(newMockProvider("beta"))

	names := pm.Providers()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("expected [alpha beta], got %v", names)
	}
}

func TestPermissionManager_Provider(t *testing.T) {
	pm := NewPermissionManager()
	p := newMockProvider("test")
	pm.AddProvider(p)

	got, ok := pm.Provider("test")
	if !ok {
		t.Fatal("expected to find provider")
	}
	if got.Name() != "test" {
		t.Errorf("expected name 'test', got %q", got.Name())
	}

	_, ok = pm.Provider("missing")
	if ok {
		t.Error("expected not to find missing provider")
	}
}

func TestPermissionManager_CheckDenied(t *testing.T) {
	pm := NewPermissionManager()
	p := newMockProvider("test")
	// No permissions granted to bob
	pm.AddProvider(p)

	allowed, err := pm.Check(context.Background(), "bob", "workflows", "delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected permission denied for bob")
	}
}

// errorListProvider returns an error from ListPermissions.
type errorListProvider struct {
	name string
}

func (e *errorListProvider) Name() string { return e.name }
func (e *errorListProvider) CheckPermission(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (e *errorListProvider) ListPermissions(context.Context, string) ([]Permission, error) {
	return nil, fmt.Errorf("list failed")
}
func (e *errorListProvider) SyncRoles(context.Context, []RoleDefinition) error {
	return nil
}
