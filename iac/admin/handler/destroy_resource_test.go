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

// TestDestroyResource_HappyPath asserts that a valid subject + refs + correct
// confirm_hash → destroyed[] with the ref names.
func TestDestroyResource_HappyPath(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	refs := []*adminpb.AdminResourceRef{
		{Name: "vpc1", Type: "infra.vpc"},
		{Name: "db1", Type: "infra.database"},
	}
	in := &adminpb.AdminDestroyInput{
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs:        refs,
		ConfirmHash: handler.HashDestroyRefs(refs), // TOCTOU: echo server-computed hash
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

// TestDestroyResource_MismatchedConfirmHash asserts that a wrong or empty
// confirm_hash → TOCTOU error, no destroy operation performed (IMPORTANT-1).
func TestDestroyResource_MismatchedConfirmHash(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	refs := []*adminpb.AdminResourceRef{
		{Name: "vpc1", Type: "infra.vpc"},
	}

	// Empty confirm_hash — should be rejected.
	in := &adminpb.AdminDestroyInput{
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs:        refs,
		ConfirmHash: "", // deliberately empty
	}
	out, err := handler.DestroyResource(context.Background(), providers, nil, "operator", in)
	if err != nil {
		t.Fatalf("DestroyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("DestroyResource with empty confirm_hash should return TOCTOU error")
	}

	// Wrong confirm_hash — should also be rejected.
	in2 := &adminpb.AdminDestroyInput{
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs:        refs,
		ConfirmHash: "wrong-hash-stale",
	}
	out2, err := handler.DestroyResource(context.Background(), providers, nil, "operator", in2)
	if err != nil {
		t.Fatalf("DestroyResource: unexpected Go error: %v", err)
	}
	if out2.Error == "" {
		t.Error("DestroyResource with wrong confirm_hash should return TOCTOU error")
	}
	if len(out2.Destroyed) > 0 {
		t.Error("DestroyResource: no resources should be destroyed when confirm_hash mismatches")
	}
}
