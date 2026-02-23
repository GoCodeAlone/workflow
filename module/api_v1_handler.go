package module

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/bundle"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// V1APIHandler handles the /api/v1/admin/ CRUD endpoints for companies, projects,
// and workflows. It is wired as a fallback on the admin-queries and
// admin-commands CQRS handler modules.
type V1APIHandler struct {
	store              *V1Store
	jwtSecret          string
	dataDir            string                        // base data directory for workspace extraction
	reloadFn           func(configYAML string) error // callback to reload engine with new admin config
	runtimeManager     *RuntimeManager               // optional runtime manager for deploy/stop
	workspaceHandler   *WorkspaceHandler             // optional workspace file management handler
	featureFlagService FeatureFlagAdmin              // optional feature flag admin service
}

// NewV1APIHandler creates a new handler backed by the given store.
func NewV1APIHandler(store *V1Store, jwtSecret string) *V1APIHandler {
	return &V1APIHandler{
		store:     store,
		jwtSecret: jwtSecret,
	}
}

// SetWorkspaceHandler sets the optional workspace file management handler.
func (h *V1APIHandler) SetWorkspaceHandler(wh *WorkspaceHandler) {
	h.workspaceHandler = wh
}

// SetReloadFunc sets the callback invoked when deploying the system workflow.
func (h *V1APIHandler) SetReloadFunc(fn func(configYAML string) error) {
	h.reloadFn = fn
}

// SetRuntimeManager sets the runtime manager used for deploy/stop operations.
func (h *V1APIHandler) SetRuntimeManager(rm *RuntimeManager) {
	h.runtimeManager = rm
}

// SetDataDir sets the base data directory used for workspace extraction during import.
func (h *V1APIHandler) SetDataDir(dir string) {
	h.dataDir = dir
}

// ServeHTTP implements http.Handler for config-driven delegate dispatch.
func (h *V1APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.HandleV1(w, r)
}

// HandleV1 dispatches v1 API requests by parsing path segments and delegating
// to resource-specific handlers. Each handler is self-contained and manages
// its own HTTP method routing.
func (h *V1APIHandler) HandleV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path

	// Workspaces: delegate entirely to the workspace handler.
	if strings.Contains(path, "/workspaces/") && h.workspaceHandler != nil {
		h.workspaceHandler.HandleWorkspace(w, r)
		return
	}

	// Parse path segments after the last "/v1/" prefix (or "/admin/").
	// Typical paths:
	//   /api/v1/companies
	//   /api/v1/companies/{id}
	//   /api/v1/companies/{id}/organizations
	//   /api/v1/organizations/{id}/projects
	//   /api/v1/projects/{id}/workflows
	//   /api/v1/workflows
	//   /api/v1/workflows/{id}
	//   /api/v1/workflows/{id}/versions
	//   /api/v1/workflows/{id}/deploy
	//   /api/v1/workflows/{id}/stop
	//   /api/v1/dashboard
	segments := parsePathSegments(path)

	if len(segments) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// Dispatch based on the first resource segment.
	switch segments[0] {
	case "companies":
		h.handleCompanies(w, r, segments[1:])
	case "organizations":
		h.handleOrganizations(w, r, segments[1:])
	case "projects":
		h.handleProjects(w, r, segments[1:])
	case "workflows":
		h.handleWorkflows(w, r, segments[1:])
	case "dashboard":
		h.handleDashboard(w, r)
	case "feature-flags":
		h.handleFeatureFlags(w, r, segments[1:])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// parsePathSegments extracts the meaningful path segments after the API prefix.
// It finds the last occurrence of a known resource keyword and returns from
// that point onward. For example:
//
//	"/api/v1/companies/abc/organizations" -> ["companies", "abc", "organizations"]
//	"/api/v1/workflows/xyz/deploy"        -> ["workflows", "xyz", "deploy"]
//	"/api/v1/dashboard"                   -> ["dashboard"]
func parsePathSegments(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Walk backwards to find the first resource keyword — this is the
	// start of the resource path we care about.
	resources := map[string]bool{
		"companies": true, "organizations": true,
		"projects": true, "workflows": true, "dashboard": true,
		"feature-flags": true,
	}

	startIdx := -1
	for i, p := range parts {
		if resources[p] {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return nil
	}
	return parts[startIdx:]
}

// --- handleCompanies dispatches company-level operations ---
//
// Handles:
//
//	GET    /companies          -> list companies
//	POST   /companies          -> create company
//	GET    /companies/{id}     -> get company
//	GET    /companies/{id}/organizations  -> list orgs (delegates)
//	POST   /companies/{id}/organizations  -> create org (delegates)
func (h *V1APIHandler) handleCompanies(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	// /companies (no ID)
	case len(rest) == 0:
		switch r.Method {
		case http.MethodGet:
			h.listCompanies(w, r)
		case http.MethodPost:
			h.createCompany(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /companies/{id}
	case len(rest) == 1:
		switch r.Method {
		case http.MethodGet:
			h.getCompany(w, r, rest[0])
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /companies/{id}/organizations[/...]
	case len(rest) >= 2 && rest[1] == "organizations":
		companyID := rest[0]
		h.handleCompanyOrganizations(w, r, companyID, rest[2:])

	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// handleCompanyOrganizations handles organization operations nested under a company.
//
// Handles:
//
//	GET    /companies/{companyID}/organizations  -> list orgs
//	POST   /companies/{companyID}/organizations  -> create org
func (h *V1APIHandler) handleCompanyOrganizations(w http.ResponseWriter, r *http.Request, companyID string, rest []string) {
	if len(rest) != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listOrganizations(w, r, companyID)
	case http.MethodPost:
		h.createOrganization(w, r, companyID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- handleOrganizations dispatches organization-level operations ---
//
// Handles:
//
//	GET    /organizations/{id}/projects  -> list projects
//	POST   /organizations/{id}/projects  -> create project
func (h *V1APIHandler) handleOrganizations(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) < 2 || rest[1] != "projects" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	orgID := rest[0]
	remaining := rest[2:]

	if len(remaining) != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listProjects(w, r, orgID)
	case http.MethodPost:
		h.createProject(w, r, orgID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- handleProjects dispatches project-level operations ---
//
// Handles:
//
//	GET    /projects/{id}/workflows  -> list workflows by project
//	POST   /projects/{id}/workflows  -> create workflow
func (h *V1APIHandler) handleProjects(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) < 2 || rest[1] != "workflows" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	projectID := rest[0]
	remaining := rest[2:]

	if len(remaining) != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listWorkflowsByProject(w, r, projectID)
	case http.MethodPost:
		h.createWorkflow(w, r, projectID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- handleWorkflows dispatches workflow-level operations ---
//
// Handles:
//
//	GET    /workflows             -> list all workflows
//	GET    /workflows/{id}        -> get workflow
//	PUT    /workflows/{id}        -> update workflow
//	DELETE /workflows/{id}        -> delete workflow
//	GET    /workflows/{id}/versions -> list versions
//	POST   /workflows/{id}/deploy   -> deploy workflow
//	POST   /workflows/{id}/stop     -> stop workflow
func (h *V1APIHandler) handleWorkflows(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	// /workflows (no ID)
	case len(rest) == 0:
		if r.Method == http.MethodGet {
			h.listAllWorkflows(w, r)
		} else {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /workflows/load-from-path (special action, not an ID)
	case len(rest) == 1 && rest[0] == "load-from-path":
		if r.Method == http.MethodPost {
			h.loadWorkflowFromPath(w, r)
		} else {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /workflows/import (bundle import)
	case len(rest) == 1 && rest[0] == "import":
		if r.Method == http.MethodPost {
			h.importWorkflow(w, r)
		} else {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /workflows/{id}
	case len(rest) == 1:
		workflowID := rest[0]
		switch r.Method {
		case http.MethodGet:
			h.getWorkflow(w, r, workflowID)
		case http.MethodPut:
			h.updateWorkflow(w, r, workflowID)
		case http.MethodDelete:
			h.deleteWorkflow(w, r, workflowID)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	// /workflows/{id}/{action}
	case len(rest) == 2:
		workflowID := rest[0]
		action := rest[1]
		switch action {
		case "versions":
			if r.Method == http.MethodGet {
				h.listVersions(w, r, workflowID)
			} else {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			}
		case "deploy":
			if r.Method == http.MethodPost {
				h.deployWorkflow(w, r, workflowID)
			} else {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			}
		case "stop":
			if r.Method == http.MethodPost {
				h.stopWorkflow(w, r, workflowID)
			} else {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			}
		case "export":
			if r.Method == http.MethodGet {
				h.exportWorkflow(w, r, workflowID)
			} else {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			}
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}

	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// --- Auth helpers ---

type userClaims struct {
	UserID string
	Email  string
	Role   string
}

func (h *V1APIHandler) extractClaims(r *http.Request) (*userClaims, error) {
	// The auth middleware has already validated the token and put claims in context.
	// Try context first.
	if claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any); ok {
		uc := &userClaims{}
		if sub, ok := claims["sub"].(string); ok {
			uc.UserID = sub
		}
		if email, ok := claims["email"].(string); ok {
			uc.Email = email
		}
		if role, ok := claims["role"].(string); ok {
			uc.Role = role
		}
		return uc, nil
	}

	// Fallback: parse JWT directly from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("no authorization header")
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return nil, fmt.Errorf("bearer token required")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(h.jwtSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid claims")
	}

	uc := &userClaims{}
	if sub, ok := claims["sub"].(string); ok {
		uc.UserID = sub
	}
	if email, ok := claims["email"].(string); ok {
		uc.Email = email
	}
	if role, ok := claims["role"].(string); ok {
		uc.Role = role
	}
	return uc, nil
}

func (h *V1APIHandler) requireAuth(w http.ResponseWriter, r *http.Request) *userClaims {
	claims, err := h.extractClaims(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil
	}
	return claims
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// =============================================================================
// Company operations
// =============================================================================

func (h *V1APIHandler) listCompanies(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	companies, err := h.store.ListCompanies(claims.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Filter: non-admins don't see system companies
	if claims.Role != "admin" {
		filtered := make([]V1Company, 0, len(companies))
		for i := range companies {
			if !companies[i].IsSystem {
				filtered = append(filtered, companies[i])
			}
		}
		companies = filtered
	}

	if companies == nil {
		companies = []V1Company{}
	}
	writeJSON(w, http.StatusOK, companies)
}

func (h *V1APIHandler) createCompany(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	c, err := h.store.CreateCompany(req.Name, req.Slug, claims.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *V1APIHandler) getCompany(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "company ID required"})
		return
	}

	c, err := h.store.GetCompany(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "company not found"})
		return
	}

	if c.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	writeJSON(w, http.StatusOK, c)
}

// =============================================================================
// Organization operations
// =============================================================================

func (h *V1APIHandler) listOrganizations(w http.ResponseWriter, r *http.Request, companyID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if companyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "company ID required"})
		return
	}

	orgs, err := h.store.ListOrganizations(companyID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if orgs == nil {
		orgs = []V1Company{}
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *V1APIHandler) createOrganization(w http.ResponseWriter, r *http.Request, companyID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if companyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "company ID required"})
		return
	}

	// Block creating orgs under system companies for non-admins
	company, err := h.store.GetCompany(companyID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "company not found"})
		return
	}
	if company.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	org, err := h.store.CreateOrganization(companyID, req.Name, req.Slug, claims.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

// =============================================================================
// Project operations
// =============================================================================

func (h *V1APIHandler) listProjects(w http.ResponseWriter, r *http.Request, orgID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "organization ID required"})
		return
	}

	projects, err := h.store.ListProjects(orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if projects == nil {
		projects = []V1Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (h *V1APIHandler) createProject(w http.ResponseWriter, r *http.Request, orgID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "organization ID required"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	proj, err := h.store.CreateProject(orgID, req.Name, req.Slug, req.Description)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, proj)
}

// =============================================================================
// Workflow operations
// =============================================================================

func (h *V1APIHandler) listWorkflowsByProject(w http.ResponseWriter, r *http.Request, projectID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project ID required"})
		return
	}

	wfs, err := h.store.ListWorkflows(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if wfs == nil {
		wfs = []V1Workflow{}
	}
	writeJSON(w, http.StatusOK, wfs)
}

func (h *V1APIHandler) createWorkflow(w http.ResponseWriter, r *http.Request, projectID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project ID required"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		ConfigYAML  string `json:"config_yaml"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	// Use email for version history readability; fall back to user ID
	createdBy := claims.Email
	if createdBy == "" {
		createdBy = claims.UserID
	}
	wf, err := h.store.CreateWorkflow(projectID, req.Name, req.Slug, req.Description, req.ConfigYAML, createdBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, wf)
}

func (h *V1APIHandler) listAllWorkflows(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	projectID := r.URL.Query().Get("project_id")
	wfs, err := h.store.ListWorkflows(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Filter system workflows for non-admins
	if claims.Role != "admin" {
		filtered := make([]V1Workflow, 0, len(wfs))
		for i := range wfs {
			if !wfs[i].IsSystem {
				filtered = append(filtered, wfs[i])
			}
		}
		wfs = filtered
	}

	if wfs == nil {
		wfs = []V1Workflow{}
	}
	writeJSON(w, http.StatusOK, wfs)
}

func (h *V1APIHandler) getWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	wf, err := h.store.GetWorkflow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	if wf.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	writeJSON(w, http.StatusOK, wf)
}

func (h *V1APIHandler) updateWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	// Check system workflow access
	existing, err := h.store.GetWorkflow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}
	if existing.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ConfigYAML  string `json:"config_yaml"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Use email for version history readability; fall back to user ID
	updatedBy := claims.Email
	if updatedBy == "" {
		updatedBy = claims.UserID
	}
	wf, err := h.store.UpdateWorkflow(id, req.Name, req.Description, req.ConfigYAML, updatedBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (h *V1APIHandler) deleteWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	// System workflow check is done inside store.DeleteWorkflow
	if err := h.store.DeleteWorkflow(id); err != nil {
		if strings.Contains(err.Error(), "system") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *V1APIHandler) deployWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	wf, err := h.store.GetWorkflow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	if wf.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	// For system workflows, trigger engine reload
	if wf.IsSystem && h.reloadFn != nil {
		if err := h.reloadFn(wf.ConfigYAML); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("deploy failed: %v", err)})
			return
		}
	}

	// For non-system workflows, start as a runtime instance
	if !wf.IsSystem && h.runtimeManager != nil && wf.ConfigYAML != "" {
		if launchErr := h.runtimeManager.LaunchFromWorkspace(r.Context(), id, wf.Name, wf.ConfigYAML, wf.WorkspaceDir); launchErr != nil {
			_, _ = h.store.SetWorkflowStatus(id, "error")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("launch failed: %v", launchErr)})
			return
		}
	}

	updated, err := h.store.SetWorkflowStatus(id, "active")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *V1APIHandler) stopWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	wf, err := h.store.GetWorkflow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	if wf.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	// Stop the runtime instance if running
	if h.runtimeManager != nil {
		if inst, ok := h.runtimeManager.GetInstance(id); ok && inst.Status == "running" {
			if stopErr := h.runtimeManager.StopWorkflow(r.Context(), id); stopErr != nil {
				// Log but don't fail — the DB status update should still proceed
				log.Printf("workflow engine: failed to stop workflow %s: %v", id, stopErr)
			}
		}
	}

	updated, err := h.store.SetWorkflowStatus(id, "stopped")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// loadWorkflowFromPath reads a workflow config from a server-local file path
// and creates a workflow record in the store.
func (h *V1APIHandler) loadWorkflowFromPath(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Path      string `json:"path"`
		ProjectID string `json:"project_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if req.ProjectID == "" {
		// Use the default seeded project
		req.ProjectID = "00000000-0000-0000-0000-000000000002"
	}

	// Resolve the config file path
	configPath := filepath.Clean(req.Path)
	info, err := os.Stat(configPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("path not found: %s", configPath)})
		return
	}

	// If directory, look for workflow.yaml
	if info.IsDir() {
		yamlPath := filepath.Join(configPath, "workflow.yaml")
		if _, err := os.Stat(yamlPath); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("no workflow.yaml in %s", configPath)})
			return
		}
		configPath = yamlPath
	}

	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read file: %v", err)})
		return
	}

	// Derive name from the directory or file
	name := filepath.Base(filepath.Dir(configPath))
	if name == "." || name == "/" {
		name = strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	}

	createdBy := claims.Email
	if createdBy == "" {
		createdBy = claims.UserID
	}

	// Set workspace_dir to the config file's directory so relative paths resolve
	workspaceDir := filepath.Dir(configPath)

	wf, err := h.store.CreateWorkflow(req.ProjectID, name, name, fmt.Sprintf("Loaded from %s", req.Path), string(data), createdBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update workspace_dir on the record
	wf.WorkspaceDir = workspaceDir
	if updateErr := h.store.UpdateWorkflowWorkspaceDir(wf.ID, workspaceDir); updateErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to set workspace_dir: %v", updateErr)})
		return
	}

	writeJSON(w, http.StatusCreated, wf)
}

// =============================================================================
// Import / Export
// =============================================================================

func (h *V1APIHandler) exportWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	wf, err := h.store.GetWorkflow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	if wf.IsSystem && claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	filename := wf.Slug + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	if err := bundle.Export(wf.ConfigYAML, wf.WorkspaceDir, w); err != nil {
		// Headers already sent, best effort error
		http.Error(w, fmt.Sprintf("export failed: %v", err), http.StatusInternalServerError)
		return
	}
}

func (h *V1APIHandler) importWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid multipart form: %v", err)})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()

	projectID := r.FormValue("project_id")

	// Generate a workspace ID and determine the destination directory
	workspaceID := uuid.New().String()
	dataDir := h.dataDir
	if dataDir == "" {
		dataDir = "data"
	}
	destDir := filepath.Join(dataDir, "workspaces", workspaceID)

	// Extract the bundle
	manifest, workflowPath, err := bundle.Import(file, destDir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("import failed: %v", err)})
		return
	}

	// Read the extracted workflow.yaml
	yamlData, err := os.ReadFile(workflowPath) //nolint:gosec // G703: path from trusted workspace extraction
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read extracted workflow.yaml: %v", err)})
		return
	}

	name := manifest.Name
	if name == "" {
		name = "imported-workflow"
	}
	slug := toSlug(name)

	createdBy := claims.Email
	if createdBy == "" {
		createdBy = claims.UserID
	}

	// Idempotency: check if a workflow with the same slug exists in the project
	if projectID != "" {
		if existing, err := h.store.GetWorkflowBySlugAndProject(slug, projectID); err == nil && existing != nil {
			// Update existing workflow
			existing.ConfigYAML = string(yamlData)
			existing.WorkspaceDir = destDir
			updated, updateErr := h.store.UpdateWorkflow(existing.ID, name, existing.Description, string(yamlData), createdBy)
			if updateErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": updateErr.Error()})
				return
			}
			// Also update workspace_dir
			_ = h.store.SetWorkspaceDir(updated.ID, destDir)
			updated.WorkspaceDir = destDir
			writeJSON(w, http.StatusOK, updated)
			return
		}
	}

	// If no project_id provided, try to find the first available project
	if projectID == "" {
		projects, listErr := h.store.ListAllProjects()
		if listErr == nil && len(projects) > 0 {
			projectID = projects[0].ID
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id is required (no projects found)"})
			return
		}
	}

	// Create new workflow
	wf, err := h.store.CreateWorkflow(projectID, name, slug, fmt.Sprintf("Imported from bundle: %s", name), string(yamlData), createdBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Set workspace_dir
	_ = h.store.SetWorkspaceDir(wf.ID, destDir)
	wf.WorkspaceDir = destDir

	writeJSON(w, http.StatusCreated, wf)
}

// =============================================================================
// Dashboard
// =============================================================================

type dashboardSummary struct {
	WorkflowID   string         `json:"workflow_id"`
	WorkflowName string         `json:"workflow_name"`
	Status       string         `json:"status"`
	Executions   map[string]int `json:"executions"`
	LogCounts    map[string]int `json:"log_counts"`
}

type dashboardResponse struct {
	TotalWorkflows    int                `json:"total_workflows"`
	WorkflowSummaries []dashboardSummary `json:"workflow_summaries"`
}

func (h *V1APIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	wfs, err := h.store.ListWorkflows("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Filter system workflows for non-admins
	if claims.Role != "admin" {
		filtered := make([]V1Workflow, 0, len(wfs))
		for i := range wfs {
			if !wfs[i].IsSystem {
				filtered = append(filtered, wfs[i])
			}
		}
		wfs = filtered
	}

	summaries := make([]dashboardSummary, 0, len(wfs))
	for i := range wfs {
		execCounts, _ := h.store.CountExecutionsByWorkflow(wfs[i].ID)
		logCounts, _ := h.store.CountLogsByWorkflow(wfs[i].ID)
		if execCounts == nil {
			execCounts = map[string]int{}
		}
		if logCounts == nil {
			logCounts = map[string]int{}
		}
		summaries = append(summaries, dashboardSummary{
			WorkflowID:   wfs[i].ID,
			WorkflowName: wfs[i].Name,
			Status:       wfs[i].Status,
			Executions:   execCounts,
			LogCounts:    logCounts,
		})
	}

	resp := dashboardResponse{
		TotalWorkflows:    len(wfs),
		WorkflowSummaries: summaries,
	}
	writeJSON(w, http.StatusOK, resp)
}

// =============================================================================
// Versions
// =============================================================================

func (h *V1APIHandler) listVersions(w http.ResponseWriter, r *http.Request, workflowID string) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	if workflowID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workflow ID required"})
		return
	}

	versions, err := h.store.ListVersions(workflowID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if versions == nil {
		versions = []V1WorkflowVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}
