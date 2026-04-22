package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeTenantReg is a test double for interfaces.TenantRegistry.
type fakeTenantReg struct {
	ensureResult      interfaces.Tenant
	ensureErr         error
	listResult        []interfaces.Tenant
	listErr           error
	getByDomainResult interfaces.Tenant
	getByDomainErr    error
	updateResult      interfaces.Tenant
	updateErr         error
	disableErr        error
	getByIDResult     interfaces.Tenant
	getByIDErr        error
	getBySlugResult   interfaces.Tenant
	getBySlugErr      error
}

func (f *fakeTenantReg) Ensure(spec interfaces.TenantSpec) (interfaces.Tenant, error) {
	return f.ensureResult, f.ensureErr
}
func (f *fakeTenantReg) GetByID(id string) (interfaces.Tenant, error) {
	return f.getByIDResult, f.getByIDErr
}
func (f *fakeTenantReg) GetByDomain(domain string) (interfaces.Tenant, error) {
	return f.getByDomainResult, f.getByDomainErr
}
func (f *fakeTenantReg) GetBySlug(slug string) (interfaces.Tenant, error) {
	return f.getBySlugResult, f.getBySlugErr
}
func (f *fakeTenantReg) List(filter interfaces.TenantFilter) ([]interfaces.Tenant, error) {
	return f.listResult, f.listErr
}
func (f *fakeTenantReg) Update(id string, patch interfaces.TenantPatch) (interfaces.Tenant, error) {
	return f.updateResult, f.updateErr
}
func (f *fakeTenantReg) Disable(id string) error {
	return f.disableErr
}

func TestTenantCLI_Ensure(t *testing.T) {
	reg := &fakeTenantReg{
		ensureResult: interfaces.Tenant{ID: "t1", Name: "Acme", Slug: "acme", IsActive: true},
	}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"ensure", "--name", "Acme", "--slug", "acme"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "t1") {
		t.Errorf("expected tenant ID in output, got: %s", buf.String())
	}
}

func TestTenantCLI_List(t *testing.T) {
	reg := &fakeTenantReg{
		listResult: []interfaces.Tenant{
			{ID: "t1", Slug: "acme", Name: "Acme", IsActive: true},
			{ID: "t2", Slug: "beta", Name: "Beta", IsActive: false},
		},
	}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"list"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "acme") {
		t.Errorf("expected slug 'acme' in output, got: %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected slug 'beta' in output, got: %s", out)
	}
}

func TestTenantCLI_List_JSONFormat(t *testing.T) {
	reg := &fakeTenantReg{
		listResult: []interfaces.Tenant{
			{ID: "t1", Slug: "acme", Name: "Acme", IsActive: true},
		},
	}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"list", "--format", "json"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if len(result) != 1 {
		t.Errorf("expected 1 tenant, got %d", len(result))
	}
}

func TestTenantCLI_Get(t *testing.T) {
	reg := &fakeTenantReg{
		getByDomainResult: interfaces.Tenant{ID: "t1", Slug: "acme", Name: "Acme", IsActive: true},
	}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"get", "--domain", "acme.example.com"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "t1") {
		t.Errorf("expected tenant ID in output, got: %s", buf.String())
	}
}

func TestTenantCLI_Update(t *testing.T) {
	reg := &fakeTenantReg{
		updateResult: interfaces.Tenant{ID: "t1", Slug: "acme", Name: "Acme Corp", IsActive: true},
	}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"update", "--id", "t1", "--name", "Acme Corp"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "t1") {
		t.Errorf("expected tenant ID in output, got: %s", buf.String())
	}
}

func TestTenantCLI_Disable(t *testing.T) {
	reg := &fakeTenantReg{}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"disable", "--id", "t1"}, &buf, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "t1") {
		t.Errorf("expected tenant ID in output, got: %s", buf.String())
	}
}

func TestTenantCLI_UnknownSubcommand(t *testing.T) {
	reg := &fakeTenantReg{}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{"frobnicate"}, &buf, reg)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestTenantCLI_NoSubcommand(t *testing.T) {
	reg := &fakeTenantReg{}
	var buf bytes.Buffer
	err := runTenantWithRegistry([]string{}, &buf, reg)
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}
