package rbac

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/auth"
	coreRBAC "github.com/GoCodeAlone/workflow/auth/rbac"
)

func TestBuiltinProvider_Name(t *testing.T) {
	p := NewBuiltinProvider(coreRBAC.NewPolicyEngine())
	if p.Name() != "builtin" {
		t.Errorf("expected name 'builtin', got %q", p.Name())
	}
}

func TestBuiltinProvider_CheckPermission_BuiltinRoles(t *testing.T) {
	p := NewBuiltinProvider(coreRBAC.NewPolicyEngine())
	ctx := context.Background()

	tests := []struct {
		subject  string
		resource string
		action   string
		want     bool
	}{
		{"admin", "workflows", "read", true},
		{"admin", "users", "admin", true},
		{"viewer", "workflows", "read", true},
		{"viewer", "workflows", "write", false},
		{"editor", "workflows", "write", true},
		{"editor", "workflows", "delete", false},
		{"operator", "workflows", "delete", true},
		{"operator", "users", "write", false},
		{"nonexistent", "workflows", "read", false},
	}

	for _, tt := range tests {
		got, err := p.CheckPermission(ctx, tt.subject, tt.resource, tt.action)
		if err != nil {
			t.Errorf("CheckPermission(%q, %q, %q): unexpected error: %v", tt.subject, tt.resource, tt.action, err)
			continue
		}
		if got != tt.want {
			t.Errorf("CheckPermission(%q, %q, %q) = %v, want %v", tt.subject, tt.resource, tt.action, got, tt.want)
		}
	}
}

func TestBuiltinProvider_ListPermissions(t *testing.T) {
	p := NewBuiltinProvider(coreRBAC.NewPolicyEngine())
	ctx := context.Background()

	perms, err := p.ListPermissions(ctx, "viewer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 3 {
		t.Fatalf("expected 3 permissions for viewer, got %d", len(perms))
	}
	for _, perm := range perms {
		if perm.Effect != "allow" {
			t.Errorf("expected effect 'allow', got %q", perm.Effect)
		}
		if perm.Action != "read" {
			t.Errorf("expected action 'read' for viewer, got %q", perm.Action)
		}
	}
}

func TestBuiltinProvider_ListPermissions_Unknown(t *testing.T) {
	p := NewBuiltinProvider(coreRBAC.NewPolicyEngine())
	ctx := context.Background()

	perms, err := p.ListPermissions(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms != nil {
		t.Errorf("expected nil for unknown role, got %v", perms)
	}
}

func TestBuiltinProvider_SyncRoles(t *testing.T) {
	engine := coreRBAC.NewPolicyEngine()
	p := NewBuiltinProvider(engine)
	ctx := context.Background()

	roles := []auth.RoleDefinition{
		{
			Name:        "plugin-manager",
			Description: "Can manage plugins",
			Permissions: []auth.Permission{
				{Resource: "plugins", Action: "read", Effect: "allow"},
				{Resource: "plugins", Action: "write", Effect: "allow"},
				{Resource: "plugins", Action: "delete", Effect: "allow"},
			},
		},
	}

	if err := p.SyncRoles(ctx, roles); err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}

	// Verify the role was registered in the engine
	allowed, err := p.CheckPermission(ctx, "plugin-manager", "plugins", "write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected plugin-manager to have write access to plugins")
	}

	// Verify the role doesn't have permissions not granted
	allowed, err = p.CheckPermission(ctx, "plugin-manager", "workflows", "read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected plugin-manager to not have access to workflows")
	}
}

func TestBuiltinProvider_SyncRoles_OverridesExisting(t *testing.T) {
	engine := coreRBAC.NewPolicyEngine()
	p := NewBuiltinProvider(engine)
	ctx := context.Background()

	// Viewer normally has read-only. Override with write access.
	roles := []auth.RoleDefinition{
		{
			Name:        "viewer",
			Description: "Upgraded viewer with write",
			Permissions: []auth.Permission{
				{Resource: "workflows", Action: "read", Effect: "allow"},
				{Resource: "workflows", Action: "write", Effect: "allow"},
			},
		},
	}

	if err := p.SyncRoles(ctx, roles); err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}

	allowed, err := p.CheckPermission(ctx, "viewer", "workflows", "write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected overridden viewer to have write access")
	}
}

func TestBuiltinProvider_CustomPermissionPatterns(t *testing.T) {
	engine := coreRBAC.NewPolicyEngine()
	p := NewBuiltinProvider(engine)
	ctx := context.Background()

	roles := []auth.RoleDefinition{
		{
			Name:        "workflow-admin",
			Description: "Custom workflow permissions",
			Permissions: []auth.Permission{
				{Resource: "plugins", Action: "manage", Effect: "allow"},
				{Resource: "workflows", Action: "create", Effect: "allow"},
				{Resource: "environments", Action: "deploy", Effect: "allow"},
			},
		},
	}

	if err := p.SyncRoles(ctx, roles); err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}

	tests := []struct {
		resource string
		action   string
		want     bool
	}{
		{"plugins", "manage", true},
		{"workflows", "create", true},
		{"environments", "deploy", true},
		{"plugins", "delete", false},
	}

	for _, tt := range tests {
		got, err := p.CheckPermission(ctx, "workflow-admin", tt.resource, tt.action)
		if err != nil {
			t.Errorf("CheckPermission(workflow-admin, %q, %q): %v", tt.resource, tt.action, err)
			continue
		}
		if got != tt.want {
			t.Errorf("CheckPermission(workflow-admin, %q, %q) = %v, want %v", tt.resource, tt.action, got, tt.want)
		}
	}
}

// --- Integration: BuiltinProvider with PermissionManager ---

func TestBuiltinProvider_WithPermissionManager(t *testing.T) {
	engine := coreRBAC.NewPolicyEngine()
	bp := NewBuiltinProvider(engine)

	pm := auth.NewPermissionManager()
	pm.AddProvider(bp)

	ctx := context.Background()

	allowed, err := pm.Check(ctx, "admin", "workflows", "delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected admin to be allowed through PermissionManager")
	}

	perms, err := pm.ListAll(ctx, "editor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) == 0 {
		t.Error("expected non-empty permissions for editor")
	}
}

func TestStubProviders_ReturnErrors(t *testing.T) {
	ctx := context.Background()

	permit := NewPermitProvider("key", "https://pdp.permit.io")
	if permit.Name() != "permit" {
		t.Errorf("expected name 'permit', got %q", permit.Name())
	}
	if _, err := permit.CheckPermission(ctx, "s", "r", "a"); err == nil {
		t.Error("expected error from permit stub")
	}
	if _, err := permit.ListPermissions(ctx, "s"); err == nil {
		t.Error("expected error from permit stub")
	}
	if err := permit.SyncRoles(ctx, nil); err == nil {
		t.Error("expected error from permit stub")
	}

	aws := NewAWSIAMProvider("us-east-1", "arn:aws:iam::role/test")
	if aws.Name() != "aws-iam" {
		t.Errorf("expected name 'aws-iam', got %q", aws.Name())
	}
}
