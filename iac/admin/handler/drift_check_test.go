package handler_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestDriftCheckResource_DefaultDeny asserts that evidence with checked=false
// returns a non-empty error and no drift payload.
func TestDriftCheckResource_DefaultDeny(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	in := &adminpb.AdminDriftInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: false},
		Refs: []*adminpb.AdminResourceRef{
			{Name: "vpc1", Type: "infra.vpc"},
		},
	}
	out, err := handler.DriftCheckResource(context.Background(), providers, in)
	if err != nil {
		t.Fatalf("DriftCheckResource: unexpected error: %v", err)
	}
	if out.Error == "" {
		t.Error("DriftCheckResource with evidence.checked=false should return non-empty error")
	}
	if len(out.Drift) > 0 {
		t.Error("DriftCheckResource with denial should return no drift payload")
	}
}

// TestDriftCheckResource_ReturnsNotDrifted asserts that the stub provider's
// DetectDrift (Drifted:false) maps to AdminDriftResult with Drifted:false.
func TestDriftCheckResource_ReturnsNotDrifted(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	in := &adminpb.AdminDriftInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs: []*adminpb.AdminResourceRef{
			{Name: "vpc1", Type: "infra.vpc"},
			{Name: "db1", Type: "infra.database"},
		},
	}
	out, err := handler.DriftCheckResource(context.Background(), providers, in)
	if err != nil {
		t.Fatalf("DriftCheckResource: unexpected error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("DriftCheckResource: unexpected output error: %s", out.Error)
	}
	if len(out.Drift) != 2 {
		t.Fatalf("DriftCheckResource: expected 2 drift results, got %d", len(out.Drift))
	}
	for _, r := range out.Drift {
		if r.Drifted {
			t.Errorf("DriftCheckResource: expected Drifted:false for %q, got true", r.ResourceName)
		}
	}
}

// TestDriftCheckResource_NoProviderError asserts that calling DriftCheckResource
// with no providers returns an error via output.error.
func TestDriftCheckResource_NoProviderError(t *testing.T) {
	in := &adminpb.AdminDriftInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
		Refs: []*adminpb.AdminResourceRef{
			{Name: "vpc1", Type: "infra.vpc"},
		},
	}
	out, err := handler.DriftCheckResource(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("DriftCheckResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("DriftCheckResource with no providers should return non-empty error")
	}
}
