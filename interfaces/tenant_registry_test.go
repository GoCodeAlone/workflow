package interfaces_test

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestTenantSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    interfaces.TenantSpec
		wantErr error
	}{
		{
			name:    "empty slug and name",
			spec:    interfaces.TenantSpec{},
			wantErr: interfaces.ErrValidation,
		},
		{
			name:    "missing slug",
			spec:    interfaces.TenantSpec{Name: "Acme"},
			wantErr: interfaces.ErrValidation,
		},
		{
			name:    "missing name",
			spec:    interfaces.TenantSpec{Slug: "acme"},
			wantErr: interfaces.ErrValidation,
		},
		{
			name:    "valid",
			spec:    interfaces.TenantSpec{Name: "Acme", Slug: "acme"},
			wantErr: nil,
		},
		{
			name:    "uppercase slug",
			spec:    interfaces.TenantSpec{Name: "Acme", Slug: "Acme"},
			wantErr: interfaces.ErrValidation,
		},
		{
			name:    "mixed-case slug",
			spec:    interfaces.TenantSpec{Name: "Acme", Slug: "ACME-Corp"},
			wantErr: interfaces.ErrValidation,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTenantRegistry_Interface(t *testing.T) {
	// Compile-time check that stubTenantRegistry satisfies TenantRegistry.
	var _ interfaces.TenantRegistry = (*stubTenantRegistry)(nil)
}

// stubTenantRegistry satisfies interfaces.TenantRegistry for compile checks.
type stubTenantRegistry struct{}

func (s *stubTenantRegistry) Ensure(_ interfaces.TenantSpec) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (s *stubTenantRegistry) GetByID(_ string) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (s *stubTenantRegistry) GetByDomain(_ string) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (s *stubTenantRegistry) GetBySlug(_ string) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (s *stubTenantRegistry) List(_ interfaces.TenantFilter) ([]interfaces.Tenant, error) {
	return nil, nil
}
func (s *stubTenantRegistry) Update(_ string, _ interfaces.TenantPatch) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (s *stubTenantRegistry) Disable(_ string) error {
	return nil
}
