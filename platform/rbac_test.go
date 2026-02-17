package platform

import (
	"context"
	"testing"
)

func TestAdminCanAccessAllTiers(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	tiers := []Tier{TierInfrastructure, TierSharedPrimitive, TierApplication}
	ops := []string{"read", "write", "delete", "approve", "create", "update"}

	for _, tier := range tiers {
		for _, op := range ops {
			if err := a.Authorize(ctx, RoleTierAdmin, tier, op); err != nil {
				t.Errorf("admin should be allowed %s on %s, got: %v", op, tier, err)
			}
		}
	}
}

func TestAuthorCanAccessTier2And3ButNotTier1(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	// Tier 2 and 3 should be allowed for write.
	for _, tier := range []Tier{TierSharedPrimitive, TierApplication} {
		if err := a.Authorize(ctx, RoleTierAuthor, tier, "write"); err != nil {
			t.Errorf("author should be allowed write on %s, got: %v", tier, err)
		}
	}

	// Tier 1 should be denied.
	err := a.Authorize(ctx, RoleTierAuthor, TierInfrastructure, "write")
	if err == nil {
		t.Fatal("author should not be allowed to write on infrastructure tier")
	}

	tbe, ok := err.(*TierBoundaryError)
	if !ok {
		t.Fatalf("expected *TierBoundaryError, got %T", err)
	}
	if tbe.Operation != "write" {
		t.Errorf("expected operation 'write', got %q", tbe.Operation)
	}
}

func TestViewerCanReadButNotWrite(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	// Read should be allowed on all tiers.
	for _, tier := range []Tier{TierInfrastructure, TierSharedPrimitive, TierApplication} {
		if err := a.Authorize(ctx, RoleTierViewer, tier, "read"); err != nil {
			t.Errorf("viewer should be allowed read on %s, got: %v", tier, err)
		}
	}

	// Write should be denied on all tiers.
	for _, tier := range []Tier{TierInfrastructure, TierSharedPrimitive, TierApplication} {
		err := a.Authorize(ctx, RoleTierViewer, tier, "write")
		if err == nil {
			t.Errorf("viewer should not be allowed to write on %s", tier)
		}
	}
}

func TestTier3RoleCannotCreateTier1Resources(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	// The author role is scoped to Tier 2+3 and cannot touch Tier 1.
	err := a.Authorize(ctx, RoleTierAuthor, TierInfrastructure, "create")
	if err == nil {
		t.Fatal("author should not be allowed to create Tier 1 resources")
	}

	tbe, ok := err.(*TierBoundaryError)
	if !ok {
		t.Fatalf("expected *TierBoundaryError, got %T", err)
	}
	if tbe.SourceTier != TierInfrastructure {
		t.Errorf("expected source tier %d, got %d", TierInfrastructure, tbe.SourceTier)
	}
}

func TestCustomPolicyRegistration(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	custom := TierRole("deploy_bot")
	a.RegisterPolicy(AuthzPolicy{
		Role:              custom,
		AllowedTiers:      []Tier{TierApplication},
		AllowedOperations: []string{"read", "create", "update"},
	})

	// Should pass for Tier 3 create.
	if err := a.Authorize(ctx, custom, TierApplication, "create"); err != nil {
		t.Errorf("deploy_bot should be allowed create on application tier, got: %v", err)
	}

	// Should fail for Tier 1.
	if err := a.Authorize(ctx, custom, TierInfrastructure, "read"); err == nil {
		t.Error("deploy_bot should not be allowed to access infrastructure tier")
	}

	// Should fail for delete (not in allowed operations).
	if err := a.Authorize(ctx, custom, TierApplication, "delete"); err == nil {
		t.Error("deploy_bot should not be allowed to delete on application tier")
	}
}

func TestErrorMessagesAreDescriptive(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	// Unknown role.
	err := a.Authorize(ctx, TierRole("unknown"), TierInfrastructure, "read")
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	tbe, ok := err.(*TierBoundaryError)
	if !ok {
		t.Fatalf("expected *TierBoundaryError, got %T", err)
	}
	if tbe.Reason == "" {
		t.Error("reason should not be empty")
	}
	errStr := tbe.Error()
	if errStr == "" {
		t.Error("error string should not be empty")
	}

	// Tier denied.
	err = a.Authorize(ctx, RoleTierAuthor, TierInfrastructure, "write")
	if err == nil {
		t.Fatal("expected error for tier denial")
	}
	tbe = err.(*TierBoundaryError)
	if tbe.Reason == "" {
		t.Error("reason should describe the tier denial")
	}

	// Operation denied.
	err = a.Authorize(ctx, RoleTierViewer, TierApplication, "delete")
	if err == nil {
		t.Fatal("expected error for operation denial")
	}
	tbe = err.(*TierBoundaryError)
	if tbe.Reason == "" {
		t.Error("reason should describe the operation denial")
	}
}

func TestApproverRole(t *testing.T) {
	a := NewStdTierAuthorizer()
	ctx := context.Background()

	// Approver can approve on Tier 1 and 2.
	if err := a.Authorize(ctx, RoleTierApprover, TierInfrastructure, "approve"); err != nil {
		t.Errorf("approver should be able to approve on infrastructure tier: %v", err)
	}
	if err := a.Authorize(ctx, RoleTierApprover, TierSharedPrimitive, "approve"); err != nil {
		t.Errorf("approver should be able to approve on shared primitive tier: %v", err)
	}

	// Approver cannot approve on Tier 3.
	if err := a.Authorize(ctx, RoleTierApprover, TierApplication, "approve"); err == nil {
		t.Error("approver should not be able to approve on application tier")
	}

	// Approver cannot write.
	if err := a.Authorize(ctx, RoleTierApprover, TierInfrastructure, "write"); err == nil {
		t.Error("approver should not be able to write on infrastructure tier")
	}
}
