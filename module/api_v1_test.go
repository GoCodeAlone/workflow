package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func setupTestStore(t *testing.T) *V1Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := OpenV1Store(dbPath)
	if err != nil {
		t.Fatalf("OpenV1Store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// --- V1Store Tests ---

func TestV1Store_CreateAndListCompanies(t *testing.T) {
	store := setupTestStore(t)

	c1, err := store.CreateCompany("Acme Corp", "", "user1")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if c1.Name != "Acme Corp" {
		t.Errorf("got name %q, want %q", c1.Name, "Acme Corp")
	}
	if c1.Slug != "acme-corp" {
		t.Errorf("got slug %q, want %q", c1.Slug, "acme-corp")
	}
	if c1.ID == "" {
		t.Error("expected non-empty ID")
	}

	c2, err := store.CreateCompany("Beta Inc", "beta", "user1")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if c2.Slug != "beta" {
		t.Errorf("got slug %q, want %q", c2.Slug, "beta")
	}

	companies, err := store.ListCompanies("user1")
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	// 2 created + 1 seeded default company
	if len(companies) != 3 {
		t.Errorf("got %d companies, want 3", len(companies))
	}

	// GetCompany
	got, err := store.GetCompany(c1.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("got name %q, want %q", got.Name, "Acme Corp")
	}
}

func TestV1Store_CreateAndListOrganizations(t *testing.T) {
	store := setupTestStore(t)

	company, err := store.CreateCompany("Parent Co", "", "user1")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}

	org1, err := store.CreateOrganization(company.ID, "Engineering", "", "user1")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if org1.ParentID != company.ID {
		t.Errorf("got parent_id %q, want %q", org1.ParentID, company.ID)
	}

	_, err = store.CreateOrganization(company.ID, "Marketing", "", "user1")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	orgs, err := store.ListOrganizations(company.ID)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 2 {
		t.Errorf("got %d orgs, want 2", len(orgs))
	}

	// Orgs should NOT appear in top-level company list
	companies, err := store.ListCompanies("user1")
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	// 1 created + 1 seeded default company
	if len(companies) != 2 {
		t.Errorf("got %d top-level companies, want 2", len(companies))
	}
}

func TestV1Store_CreateAndListProjects(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")

	p, err := store.CreateProject(org.ID, "My Project", "", "A cool project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.CompanyID != org.ID {
		t.Errorf("got company_id %q, want %q", p.CompanyID, org.ID)
	}
	if p.Description != "A cool project" {
		t.Errorf("got description %q, want %q", p.Description, "A cool project")
	}

	projects, err := store.ListProjects(org.ID)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("got %d projects, want 1", len(projects))
	}

	// GetProject
	got, err := store.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "My Project" {
		t.Errorf("got name %q, want %q", got.Name, "My Project")
	}
}

func TestV1Store_WorkflowCRUD(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")

	// Create
	wf, err := store.CreateWorkflow(proj.ID, "Test Workflow", "", "desc", "modules: []", "u1")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.Version != 1 {
		t.Errorf("got version %d, want 1", wf.Version)
	}
	if wf.Status != "draft" {
		t.Errorf("got status %q, want %q", wf.Status, "draft")
	}

	// Get
	got, err := store.GetWorkflow(wf.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.ConfigYAML != "modules: []" {
		t.Errorf("got config_yaml %q, want %q", got.ConfigYAML, "modules: []")
	}

	// Update (config changed → version bump)
	updated, err := store.UpdateWorkflow(wf.ID, "Updated Name", "", "modules: [http]", "u1")
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("got version %d, want 2", updated.Version)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("got name %q, want %q", updated.Name, "Updated Name")
	}

	// Update (name only, no config change → no version bump)
	updated2, err := store.UpdateWorkflow(wf.ID, "New Name", "", "", "u1")
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if updated2.Version != 2 {
		t.Errorf("got version %d, want 2 (no config change)", updated2.Version)
	}

	// List
	wfs, err := store.ListWorkflows(proj.ID)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(wfs) != 1 {
		t.Errorf("got %d workflows, want 1", len(wfs))
	}

	// SetWorkflowStatus
	deployed, err := store.SetWorkflowStatus(wf.ID, "active")
	if err != nil {
		t.Fatalf("SetWorkflowStatus: %v", err)
	}
	if deployed.Status != "active" {
		t.Errorf("got status %q, want %q", deployed.Status, "active")
	}

	// Delete
	err = store.DeleteWorkflow(wf.ID)
	if err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}

	wfs, _ = store.ListWorkflows(proj.ID)
	if len(wfs) != 0 {
		t.Errorf("got %d workflows after delete, want 0", len(wfs))
	}
}

func TestV1Store_WorkflowVersioning(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")
	wf, _ := store.CreateWorkflow(proj.ID, "WF", "", "", "v1 config", "u1")

	// Update config 3 times to create versions 2, 3, 4
	store.UpdateWorkflow(wf.ID, "", "", "v2 config", "u1")
	store.UpdateWorkflow(wf.ID, "", "", "v3 config", "u1")
	store.UpdateWorkflow(wf.ID, "", "", "v4 config", "u1")

	versions, err := store.ListVersions(wf.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	// Should have 3 version snapshots (created on update, not on initial create)
	if len(versions) != 3 {
		t.Errorf("got %d versions, want 3", len(versions))
	}

	// Versions should be ordered newest first
	if len(versions) >= 2 && versions[0].Version < versions[1].Version {
		t.Error("expected versions ordered newest first")
	}

	// Get a specific version
	v, err := store.GetVersion(wf.ID, 2)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.ConfigYAML != "v2 config" {
		t.Errorf("got config %q, want %q", v.ConfigYAML, "v2 config")
	}
}

func TestV1Store_EnsureSystemHierarchy(t *testing.T) {
	store := setupTestStore(t)

	companyID, orgID, projectID, workflowID, err := store.EnsureSystemHierarchy("admin1", "admin: config")
	if err != nil {
		t.Fatalf("EnsureSystemHierarchy: %v", err)
	}

	if companyID == "" || orgID == "" || projectID == "" || workflowID == "" {
		t.Error("expected non-empty IDs")
	}

	// Verify system company
	company, err := store.GetCompany(companyID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if !company.IsSystem {
		t.Error("expected system company")
	}
	if company.Name != "System" {
		t.Errorf("got company name %q, want %q", company.Name, "System")
	}

	// Verify system workflow
	wf, err := store.GetSystemWorkflow()
	if err != nil {
		t.Fatalf("GetSystemWorkflow: %v", err)
	}
	if !wf.IsSystem {
		t.Error("expected system workflow")
	}
	if wf.ConfigYAML != "admin: config" {
		t.Errorf("got config %q, want %q", wf.ConfigYAML, "admin: config")
	}

	// Cannot delete system workflow
	err = store.DeleteWorkflow(workflowID)
	if err == nil {
		t.Error("expected error deleting system workflow")
	}

	// Calling EnsureSystemHierarchy again should return existing IDs
	c2, o2, p2, w2, err := store.EnsureSystemHierarchy("admin1", "updated config")
	if err != nil {
		t.Fatalf("EnsureSystemHierarchy (second call): %v", err)
	}
	if w2 != workflowID {
		t.Errorf("expected same workflow ID %q, got %q", workflowID, w2)
	}
	_ = c2
	_ = o2
	_ = p2
}

func TestV1Store_ResetSystemWorkflow(t *testing.T) {
	store := setupTestStore(t)

	_, _, _, _, err := store.EnsureSystemHierarchy("admin1", "original config")
	if err != nil {
		t.Fatalf("EnsureSystemHierarchy: %v", err)
	}

	err = store.ResetSystemWorkflow("reset config")
	if err != nil {
		t.Fatalf("ResetSystemWorkflow: %v", err)
	}

	wf, err := store.GetSystemWorkflow()
	if err != nil {
		t.Fatalf("GetSystemWorkflow: %v", err)
	}
	if wf.ConfigYAML != "reset config" {
		t.Errorf("got config %q, want %q", wf.ConfigYAML, "reset config")
	}
	if wf.Version != 2 {
		t.Errorf("got version %d, want 2", wf.Version)
	}
}

func TestV1Store_DatabaseFile(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "nested", "data")
	dbPath := filepath.Join(subDir, "test.db")

	store, err := OpenV1Store(dbPath)
	if err != nil {
		t.Fatalf("OpenV1Store with nested dir: %v", err)
	}
	defer store.Close()

	// Verify file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("expected database file to exist")
	}
}

// --- V1APIHandler Tests ---

func generateTestToken(secret, userID, email, role string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"role":  role,
		"iss":   "test",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

func setupTestHandler(t *testing.T) (*V1APIHandler, *V1Store, string) {
	t.Helper()
	store := setupTestStore(t)
	secret := "test-secret-key"
	handler := NewV1APIHandler(store, secret)
	return handler, store, secret
}

func doRequest(handler *V1APIHandler, method, path, body, token string) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// Set auth claims in context (simulating what the auth middleware does)
	if token != "" {
		parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
			return []byte("test-secret-key"), nil
		})
		if err == nil {
			if claims, ok := parsed.Claims.(jwt.MapClaims); ok {
				claimsMap := make(map[string]any)
				for k, v := range claims {
					claimsMap[k] = v
				}
				ctx := context.WithValue(req.Context(), authClaimsContextKey, claimsMap)
				req = req.WithContext(ctx)
			}
		}
	}

	handler.HandleV1(rr, req)
	return rr
}

func TestV1Handler_ListCompanies(t *testing.T) {
	handler, store, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	// Admin sees seeded default company (is_system=1)
	rr := doRequest(handler, "GET", "/api/v1/companies", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var companies []V1Company
	json.NewDecoder(rr.Body).Decode(&companies)
	if len(companies) != 1 {
		t.Errorf("got %d companies, want 1 (seeded default)", len(companies))
	}

	// Create a company
	store.CreateCompany("Test Co", "", "1")

	rr = doRequest(handler, "GET", "/api/v1/companies", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusOK)
	}

	json.NewDecoder(rr.Body).Decode(&companies)
	if len(companies) != 2 {
		t.Errorf("got %d companies, want 2", len(companies))
	}
}

func TestV1Handler_CreateCompany(t *testing.T) {
	handler, _, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	rr := doRequest(handler, "POST", "/api/v1/companies", `{"name":"New Co"}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got status %d, want %d: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var company V1Company
	json.NewDecoder(rr.Body).Decode(&company)
	if company.Name != "New Co" {
		t.Errorf("got name %q, want %q", company.Name, "New Co")
	}
}

func TestV1Handler_SystemAccessControl(t *testing.T) {
	handler, store, secret := setupTestHandler(t)

	// Create system hierarchy
	store.EnsureSystemHierarchy("1", "admin config")
	sysWf, _ := store.GetSystemWorkflow()

	// Non-admin user token
	userToken := generateTestToken(secret, "2", "user@test.com", "user")

	// Non-admin should get 403 for system workflow
	rr := doRequest(handler, "GET", fmt.Sprintf("/api/v1/workflows/%s", sysWf.ID), "", userToken)
	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d for non-admin accessing system workflow", rr.Code, http.StatusForbidden)
	}

	// Non-admin should not see system companies in list
	rr = doRequest(handler, "GET", "/api/v1/companies", "", userToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusOK)
	}
	var companies []V1Company
	json.NewDecoder(rr.Body).Decode(&companies)
	for _, c := range companies {
		if c.IsSystem {
			t.Error("non-admin should not see system companies")
		}
	}

	// Admin should see system companies
	adminToken := generateTestToken(secret, "1", "admin@test.com", "admin")
	rr = doRequest(handler, "GET", "/api/v1/companies", "", adminToken)
	json.NewDecoder(rr.Body).Decode(&companies)
	hasSys := false
	for _, c := range companies {
		if c.IsSystem {
			hasSys = true
		}
	}
	if !hasSys {
		t.Error("admin should see system companies")
	}

	// Non-admin should not be able to delete system workflow
	rr = doRequest(handler, "DELETE", fmt.Sprintf("/api/v1/workflows/%s", sysWf.ID), "", userToken)
	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d for non-admin deleting system workflow", rr.Code, http.StatusForbidden)
	}

	// Even admin should not be able to delete system workflow (store-level protection)
	rr = doRequest(handler, "DELETE", fmt.Sprintf("/api/v1/workflows/%s", sysWf.ID), "", adminToken)
	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d for admin deleting system workflow", rr.Code, http.StatusForbidden)
	}
}

func TestV1Handler_WorkflowCRUD(t *testing.T) {
	handler, store, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	// Set up hierarchy
	company, _ := store.CreateCompany("Co", "", "1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")

	// Create workflow
	rr := doRequest(handler, "POST",
		fmt.Sprintf("/api/v1/projects/%s/workflows", proj.ID),
		`{"name":"My WF","config_yaml":"modules: []"}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create workflow: got status %d: %s", rr.Code, rr.Body.String())
	}

	var wf V1Workflow
	json.NewDecoder(rr.Body).Decode(&wf)
	if wf.Name != "My WF" {
		t.Errorf("got name %q, want %q", wf.Name, "My WF")
	}

	// Get workflow
	rr = doRequest(handler, "GET", fmt.Sprintf("/api/v1/workflows/%s", wf.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("get workflow: got status %d", rr.Code)
	}

	// Update workflow
	rr = doRequest(handler, "PUT", fmt.Sprintf("/api/v1/workflows/%s", wf.ID),
		`{"name":"Updated WF","config_yaml":"modules: [http]"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("update workflow: got status %d: %s", rr.Code, rr.Body.String())
	}

	var updated V1Workflow
	json.NewDecoder(rr.Body).Decode(&updated)
	if updated.Version != 2 {
		t.Errorf("got version %d, want 2", updated.Version)
	}

	// List versions
	rr = doRequest(handler, "GET", fmt.Sprintf("/api/v1/workflows/%s/versions", wf.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("list versions: got status %d", rr.Code)
	}

	var versions []V1WorkflowVersion
	json.NewDecoder(rr.Body).Decode(&versions)
	if len(versions) != 1 {
		t.Errorf("got %d versions, want 1", len(versions))
	}

	// Deploy
	rr = doRequest(handler, "POST", fmt.Sprintf("/api/v1/workflows/%s/deploy", wf.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("deploy: got status %d: %s", rr.Code, rr.Body.String())
	}

	var deployed V1Workflow
	json.NewDecoder(rr.Body).Decode(&deployed)
	if deployed.Status != "active" {
		t.Errorf("got status %q, want %q", deployed.Status, "active")
	}

	// Stop
	rr = doRequest(handler, "POST", fmt.Sprintf("/api/v1/workflows/%s/stop", wf.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("stop: got status %d", rr.Code)
	}

	var stopped V1Workflow
	json.NewDecoder(rr.Body).Decode(&stopped)
	if stopped.Status != "stopped" {
		t.Errorf("got status %q, want %q", stopped.Status, "stopped")
	}

	// Delete
	rr = doRequest(handler, "DELETE", fmt.Sprintf("/api/v1/workflows/%s", wf.ID), "", token)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: got status %d: %s", rr.Code, rr.Body.String())
	}

	// Verify deleted
	rr = doRequest(handler, "GET", fmt.Sprintf("/api/v1/workflows/%s", wf.ID), "", token)
	if rr.Code != http.StatusNotFound {
		t.Errorf("get after delete: got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestV1Handler_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	// No token
	rr := doRequest(handler, "GET", "/api/v1/companies", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Invalid token
	rr = doRequest(handler, "GET", "/api/v1/companies", "", "invalid-token")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d for invalid token", rr.Code, http.StatusUnauthorized)
	}
}

func TestV1Handler_WorkflowDeploy(t *testing.T) {
	handler, store, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	// Set up system hierarchy
	store.EnsureSystemHierarchy("1", "admin config yaml")

	// Track reload calls
	reloadCalled := false
	handler.SetReloadFunc(func(configYAML string) error {
		reloadCalled = true
		return nil
	})

	sysWf, _ := store.GetSystemWorkflow()

	// Deploy system workflow should trigger reload
	rr := doRequest(handler, "POST", fmt.Sprintf("/api/v1/workflows/%s/deploy", sysWf.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("deploy system workflow: got status %d: %s", rr.Code, rr.Body.String())
	}

	if !reloadCalled {
		t.Error("expected reload callback to be called for system workflow deploy")
	}
}

func TestV1Handler_Organizations(t *testing.T) {
	handler, store, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	company, _ := store.CreateCompany("Co", "", "1")

	// Create org
	rr := doRequest(handler, "POST",
		fmt.Sprintf("/api/v1/companies/%s/organizations", company.ID),
		`{"name":"Eng Team"}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create org: got status %d: %s", rr.Code, rr.Body.String())
	}

	var org V1Company
	json.NewDecoder(rr.Body).Decode(&org)
	if org.Name != "Eng Team" {
		t.Errorf("got name %q, want %q", org.Name, "Eng Team")
	}

	// List orgs
	rr = doRequest(handler, "GET",
		fmt.Sprintf("/api/v1/companies/%s/organizations", company.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("list orgs: got status %d", rr.Code)
	}

	var orgs []V1Company
	json.NewDecoder(rr.Body).Decode(&orgs)
	if len(orgs) != 1 {
		t.Errorf("got %d orgs, want 1", len(orgs))
	}
}

func TestV1Handler_Projects(t *testing.T) {
	handler, store, secret := setupTestHandler(t)
	token := generateTestToken(secret, "1", "admin@test.com", "admin")

	company, _ := store.CreateCompany("Co", "", "1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "1")

	// Create project
	rr := doRequest(handler, "POST",
		fmt.Sprintf("/api/v1/organizations/%s/projects", org.ID),
		`{"name":"My Project"}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create project: got status %d: %s", rr.Code, rr.Body.String())
	}

	// List projects
	rr = doRequest(handler, "GET",
		fmt.Sprintf("/api/v1/organizations/%s/projects", org.ID), "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("list projects: got status %d", rr.Code)
	}

	var projects []V1Project
	json.NewDecoder(rr.Body).Decode(&projects)
	if len(projects) != 1 {
		t.Errorf("got %d projects, want 1", len(projects))
	}
}

// --- Phase C: Execution, Step, Log, and Audit table tests ---

func TestV1Store_ExecutionLifecycle(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")
	wf, _ := store.CreateWorkflow(proj.ID, "WF", "", "", "modules: []", "u1")

	now := time.Now().UTC()
	execID := "exec-001"

	// Insert execution
	err := store.InsertExecution(execID, wf.ID, "manual", "pending", "u1", now)
	if err != nil {
		t.Fatalf("InsertExecution: %v", err)
	}

	// Verify via direct query
	var status string
	err = store.DB().QueryRow("SELECT status FROM workflow_executions WHERE id = ?", execID).Scan(&status)
	if err != nil {
		t.Fatalf("query execution: %v", err)
	}
	if status != "pending" {
		t.Errorf("got status %q, want %q", status, "pending")
	}

	// Complete execution
	completedAt := now.Add(5 * time.Second)
	err = store.CompleteExecution(execID, "completed", completedAt, 5000, "")
	if err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}

	err = store.DB().QueryRow("SELECT status FROM workflow_executions WHERE id = ?", execID).Scan(&status)
	if err != nil {
		t.Fatalf("query completed execution: %v", err)
	}
	if status != "completed" {
		t.Errorf("got status %q, want %q", status, "completed")
	}

	// Count executions
	counts, err := store.CountExecutionsByWorkflow(wf.ID)
	if err != nil {
		t.Fatalf("CountExecutionsByWorkflow: %v", err)
	}
	if counts["completed"] != 1 {
		t.Errorf("got completed count %d, want 1", counts["completed"])
	}
}

func TestV1Store_ExecutionStepLifecycle(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")
	wf, _ := store.CreateWorkflow(proj.ID, "WF", "", "", "modules: []", "u1")

	now := time.Now().UTC()
	execID := "exec-step-001"
	_ = store.InsertExecution(execID, wf.ID, "manual", "running", "u1", now)

	stepID := "step-001"
	err := store.InsertExecutionStep(stepID, execID, "parse-request", "step.request_parse", "running", 0, now)
	if err != nil {
		t.Fatalf("InsertExecutionStep: %v", err)
	}

	// Verify step exists
	var stepName string
	err = store.DB().QueryRow("SELECT step_name FROM execution_steps WHERE id = ?", stepID).Scan(&stepName)
	if err != nil {
		t.Fatalf("query step: %v", err)
	}
	if stepName != "parse-request" {
		t.Errorf("got step_name %q, want %q", stepName, "parse-request")
	}

	// Complete step
	err = store.CompleteExecutionStep(stepID, "completed", now.Add(time.Second), 1000, "")
	if err != nil {
		t.Fatalf("CompleteExecutionStep: %v", err)
	}

	var stepStatus string
	err = store.DB().QueryRow("SELECT status FROM execution_steps WHERE id = ?", stepID).Scan(&stepStatus)
	if err != nil {
		t.Fatalf("query completed step: %v", err)
	}
	if stepStatus != "completed" {
		t.Errorf("got status %q, want %q", stepStatus, "completed")
	}

	// Insert a second step with higher sequence
	step2ID := "step-002"
	err = store.InsertExecutionStep(step2ID, execID, "db-query", "step.db_query", "running", 1, now)
	if err != nil {
		t.Fatalf("InsertExecutionStep(2): %v", err)
	}

	// Verify ordering
	rows, err := store.DB().Query("SELECT id FROM execution_steps WHERE execution_id = ? ORDER BY sequence_num ASC", execID)
	if err != nil {
		t.Fatalf("query steps: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	if len(ids) != 2 || ids[0] != stepID || ids[1] != step2ID {
		t.Errorf("got step order %v, want [%s, %s]", ids, stepID, step2ID)
	}
}

func TestV1Store_ExecutionLogs(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")
	wf, _ := store.CreateWorkflow(proj.ID, "WF", "", "", "modules: []", "u1")

	now := time.Now().UTC()

	// Insert logs at various levels
	err := store.InsertLog(wf.ID, "exec-1", "info", "Workflow started", "engine", "{}", now)
	if err != nil {
		t.Fatalf("InsertLog(info): %v", err)
	}
	err = store.InsertLog(wf.ID, "exec-1", "error", "Step failed", "handler", `{"step":"validate"}`, now.Add(time.Second))
	if err != nil {
		t.Fatalf("InsertLog(error): %v", err)
	}
	err = store.InsertLog(wf.ID, "exec-1", "event", "user.created", "eventbus", "{}", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("InsertLog(event): %v", err)
	}

	// Count logs by level
	counts, err := store.CountLogsByWorkflow(wf.ID)
	if err != nil {
		t.Fatalf("CountLogsByWorkflow: %v", err)
	}
	if counts["info"] != 1 {
		t.Errorf("got info count %d, want 1", counts["info"])
	}
	if counts["error"] != 1 {
		t.Errorf("got error count %d, want 1", counts["error"])
	}
	if counts["event"] != 1 {
		t.Errorf("got event count %d, want 1", counts["event"])
	}

	// Verify log content
	var msg string
	err = store.DB().QueryRow("SELECT message FROM execution_logs WHERE level = 'error' AND workflow_id = ?", wf.ID).Scan(&msg)
	if err != nil {
		t.Fatalf("query error log: %v", err)
	}
	if msg != "Step failed" {
		t.Errorf("got message %q, want %q", msg, "Step failed")
	}
}

func TestV1Store_AuditLog(t *testing.T) {
	store := setupTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert audit entries directly (no convenience method exists)
	_, err := store.DB().Exec(
		"INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"user-1", "create", "workflow", "wf-123", `{"name":"test"}`, "127.0.0.1", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert audit_log: %v", err)
	}

	_, err = store.DB().Exec(
		"INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"user-1", "deploy", "workflow", "wf-123", "{}", "127.0.0.1", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert audit_log(2): %v", err)
	}

	// Query audit log
	var count int
	err = store.DB().QueryRow("SELECT COUNT(*) FROM audit_log WHERE resource_id = ?", "wf-123").Scan(&count)
	if err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if count != 2 {
		t.Errorf("got %d audit entries, want 2", count)
	}

	// Verify ordering (newest first)
	var action string
	err = store.DB().QueryRow("SELECT action FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&action)
	if err != nil {
		t.Fatalf("query latest audit: %v", err)
	}
	if action != "deploy" {
		t.Errorf("got latest action %q, want %q", action, "deploy")
	}
}

func TestV1Store_PhaseCTablesExist(t *testing.T) {
	store := setupTestStore(t)
	db := store.DB()

	tables := []string{
		"workflow_executions",
		"execution_steps",
		"execution_logs",
		"audit_log",
		"iam_provider_configs",
		"iam_role_mappings",
	}

	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestV1Store_ExecutionFailure(t *testing.T) {
	store := setupTestStore(t)

	company, _ := store.CreateCompany("Co", "", "u1")
	org, _ := store.CreateOrganization(company.ID, "Org", "", "u1")
	proj, _ := store.CreateProject(org.ID, "Proj", "", "")
	wf, _ := store.CreateWorkflow(proj.ID, "WF", "", "", "modules: []", "u1")

	now := time.Now().UTC()
	execID := "exec-fail-001"
	_ = store.InsertExecution(execID, wf.ID, "manual", "running", "u1", now)

	// Fail the execution with an error message
	err := store.CompleteExecution(execID, "failed", now.Add(3*time.Second), 3000, "timeout exceeded")
	if err != nil {
		t.Fatalf("CompleteExecution(failed): %v", err)
	}

	var status, errMsg string
	err = store.DB().QueryRow("SELECT status, error_message FROM workflow_executions WHERE id = ?", execID).Scan(&status, &errMsg)
	if err != nil {
		t.Fatalf("query failed execution: %v", err)
	}
	if status != "failed" {
		t.Errorf("got status %q, want %q", status, "failed")
	}
	if errMsg != "timeout exceeded" {
		t.Errorf("got error_message %q, want %q", errMsg, "timeout exceeded")
	}

	counts, _ := store.CountExecutionsByWorkflow(wf.ID)
	if counts["failed"] != 1 {
		t.Errorf("got failed count %d, want 1", counts["failed"])
	}
}
