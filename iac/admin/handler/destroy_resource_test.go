package handler_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestDestroyResource_DefaultDeny asserts that evidence with checked=false
// returns a non-empty error (default-deny).
func TestDestroyResource_DefaultDeny(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	in := &adminpb.AdminDestroyInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: false},
		Refs: []*adminpb.AdminResourceRef{
			{Name: "vpc1", Type: "infra.vpc"},
		},
	}
	out, err := handler.DestroyResource(context.Background(), providers, nil, "operator", in)
	if err != nil {
		t.Fatalf("DestroyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("DestroyResource with evidence.checked=false should return non-empty error")
	}
}

// TestDestroyResource_AuthzDenies asserts that a subject denied
// infra:destroy by the Enforcer is rejected even with valid evidence.
func TestDestroyResource_AuthzDenies(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	enforcer := &testEnforcer{allow: map[string]bool{
		// viewer is NOT granted infra:destroy
	}}
	in := &adminpb.AdminDestroyInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs:     []*adminpb.AdminResourceRef{{Name: "vpc1", Type: "infra.vpc"}},
	}
	out, err := handler.DestroyResource(context.Background(), providers, enforcer, "viewer", in)
	if err != nil {
		t.Fatalf("DestroyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("DestroyResource should reject subject denied infra:destroy by server-side Enforcer")
	}
}

// TestDestroyResource_HappyPath asserts that a valid subject + refs + hash
// → destroyed[] with the ref names.
func TestDestroyResource_HappyPath(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	in := &adminpb.AdminDestroyInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs: []*adminpb.AdminResourceRef{
			{Name: "vpc1", Type: "infra.vpc"},
			{Name: "db1", Type: "infra.database"},
		},
	}
	out, err := handler.DestroyResource(context.Background(), providers, nil, "operator", in)
	if err != nil {
		t.Fatalf("DestroyResource: unexpected Go error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("DestroyResource: output error: %s", out.Error)
	}
	if len(out.Destroyed) != 2 {
		t.Errorf("DestroyResource: expected 2 destroyed, got %d", len(out.Destroyed))
	}
}
