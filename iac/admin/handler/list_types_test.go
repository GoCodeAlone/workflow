package handler_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
)

// TestListResourceTypes_HappyPath verifies the handler returns one
// AdminResourceTypeMetadata per type registered in FieldSpecCatalog.
// Post-T7b the catalog has 13 typed Configs; this test asserts the
// shape rather than the exact count so a future catalog refresh
// doesn't break the assertion.
func TestListResourceTypes_HappyPath(t *testing.T) {
	cat := catalog.New()
	in := &adminpb.AdminListResourceTypesInput{Evidence: authzOK()}
	out, err := handler.ListResourceTypes(context.Background(), cat, nil, in)
	if err != nil {
		t.Fatalf("ListResourceTypes: %v", err)
	}
	if out.Error != "" {
		t.Errorf("unexpected error: %q", out.Error)
	}
	if len(out.Types) == 0 {
		t.Fatal("got 0 types — catalog should be populated post-T7b")
	}
	// Each entry must have a type name + at least one field.
	for _, ty := range out.Types {
		if ty.Type == "" {
			t.Errorf("AdminResourceTypeMetadata.type empty: %+v", ty)
		}
		if len(ty.Fields) == 0 {
			t.Errorf("AdminResourceTypeMetadata for %q has zero fields", ty.Type)
		}
	}
}

// TestListResourceTypes_DefaultDeny pins the authz contract — same
// shape as ListResources: refusal via Output.error, no resources
// leaked.
func TestListResourceTypes_DefaultDeny(t *testing.T) {
	cat := catalog.New()
	cases := []struct {
		name string
		ev   *adminpb.AdminAuthzEvidence
	}{
		{"nil evidence", nil},
		{"checked=false", &adminpb.AdminAuthzEvidence{AuthzChecked: false, AuthzAllowed: true}},
		{"allowed=false", &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := &adminpb.AdminListResourceTypesInput{Evidence: c.ev}
			out, _ := handler.ListResourceTypes(context.Background(), cat, nil, in)
			if out.Error == "" {
				t.Error("expected non-empty Error on default-deny")
			}
			if len(out.Types) != 0 {
				t.Errorf("expected empty Types on auth refusal, got %d", len(out.Types))
			}
		})
	}
}

// TestListResourceTypes_AllFieldsMatchProto checks every FieldSpec in
// the catalog projects into AdminFieldSpec field-for-field so the
// form-builder UI gets full metadata.
func TestListResourceTypes_AllFieldsMatchProto(t *testing.T) {
	cat := catalog.New()
	in := &adminpb.AdminListResourceTypesInput{Evidence: authzOK()}
	out, _ := handler.ListResourceTypes(context.Background(), cat, nil, in)

	var sawNonEmptyKind, sawEnumValues, sawDependsOn bool
	for _, ty := range out.Types {
		for _, f := range ty.Fields {
			if f.Name == "" {
				t.Errorf("type %q: field with empty name", ty.Type)
			}
			if f.Kind == "" {
				t.Errorf("type %q field %q: empty kind", ty.Type, f.Name)
			}
			if f.Kind != "" {
				sawNonEmptyKind = true
			}
			if len(f.EnumValues) > 0 {
				sawEnumValues = true
			}
			if f.DependsOnField != "" {
				sawDependsOn = true
			}
		}
	}
	if !sawNonEmptyKind {
		t.Error("no FieldSpec carried a non-empty Kind — projection dropped data")
	}
	if !sawEnumValues {
		t.Error("no FieldSpec carried EnumValues — projection dropped enum lists")
	}
	if !sawDependsOn {
		t.Error("no FieldSpec carried DependsOnField — projection dropped dependency edges")
	}
}
