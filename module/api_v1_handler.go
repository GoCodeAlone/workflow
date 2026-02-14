package module

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// V1APIHandler handles the /api/v1/ CRUD endpoints for companies, projects,
// and workflows. It is wired into the modular engine via SetHandleFunc on an
// admin-v1-api http.handler module.
type V1APIHandler struct {
	store     *V1Store
	jwtSecret string
	reloadFn  func(configYAML string) error // callback to reload engine with new admin config
}

// NewV1APIHandler creates a new handler backed by the given store.
func NewV1APIHandler(store *V1Store, jwtSecret string) *V1APIHandler {
	return &V1APIHandler{
		store:     store,
		jwtSecret: jwtSecret,
	}
}

// SetReloadFunc sets the callback invoked when deploying the system workflow.
func (h *V1APIHandler) SetReloadFunc(fn func(configYAML string) error) {
	h.reloadFn = fn
}

// HandleV1 dispatches v1 API requests based on method and path.
// This is the function wired via SimpleHTTPHandler.SetHandleFunc.
func (h *V1APIHandler) HandleV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	method := r.Method

	switch {
	// --- Companies ---
	case method == http.MethodGet && strings.HasSuffix(path, "/companies") && !strings.Contains(path, "/organizations"):
		h.handleListCompanies(w, r)
	case method == http.MethodPost && strings.HasSuffix(path, "/companies"):
		h.handleCreateCompany(w, r)
	case method == http.MethodGet && matchPath(path, "/companies/", "/organizations"):
		h.handleListOrganizations(w, r)
	case method == http.MethodPost && matchPath(path, "/companies/", "/organizations"):
		h.handleCreateOrganization(w, r)
	case method == http.MethodGet && matchPathExact(path, "/companies/"):
		h.handleGetCompany(w, r)

	// --- Projects ---
	case method == http.MethodGet && matchPath(path, "/organizations/", "/projects"):
		h.handleListProjects(w, r)
	case method == http.MethodPost && matchPath(path, "/organizations/", "/projects"):
		h.handleCreateProject(w, r)

	// --- Workflows (nested under projects) ---
	case method == http.MethodGet && matchPath(path, "/projects/", "/workflows"):
		h.handleListWorkflowsByProject(w, r)
	case method == http.MethodPost && matchPath(path, "/projects/", "/workflows"):
		h.handleCreateWorkflow(w, r)

	// --- Dashboard ---
	case method == http.MethodGet && strings.HasSuffix(path, "/dashboard") && !strings.Contains(path, "/workflows/"):
		h.handleDashboard(w, r)

	// --- Workflows (direct) ---
	case method == http.MethodGet && strings.HasSuffix(path, "/workflows") && !strings.Contains(path, "/projects/"):
		h.handleListAllWorkflows(w, r)
	case method == http.MethodGet && matchPath(path, "/workflows/", "/versions"):
		h.handleListVersions(w, r)
	case method == http.MethodPost && matchPath(path, "/workflows/", "/deploy"):
		h.handleDeployWorkflow(w, r)
	case method == http.MethodPost && matchPath(path, "/workflows/", "/stop"):
		h.handleStopWorkflow(w, r)
	case method == http.MethodGet && matchPathExact(path, "/workflows/"):
		h.handleGetWorkflow(w, r)
	case method == http.MethodPut && matchPathExact(path, "/workflows/"):
		h.handleUpdateWorkflow(w, r)
	case method == http.MethodDelete && matchPathExact(path, "/workflows/"):
		h.handleDeleteWorkflow(w, r)

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

func (h *V1APIHandler) requireAdmin(w http.ResponseWriter, r *http.Request) *userClaims {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return nil
	}
	if claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return nil
	}
	return claims
}

// --- Path helpers ---

// matchPath checks if path contains prefix + id + suffix pattern.
func matchPath(path, prefix, suffix string) bool {
	idx := strings.Index(path, prefix)
	if idx < 0 {
		return false
	}
	rest := path[idx+len(prefix):]
	return strings.HasSuffix(rest, suffix)
}

// matchPathExact checks if path ends with prefix + id (no trailing path segments beyond the id).
func matchPathExact(path, prefix string) bool {
	idx := strings.Index(path, prefix)
	if idx < 0 {
		return false
	}
	rest := path[idx+len(prefix):]
	// rest should be the ID only — no slashes
	return rest != "" && !strings.Contains(rest, "/")
}

// extractID extracts the path parameter after the given prefix.
// e.g. extractID("/api/v1/companies/abc-123/organizations", "/companies/") returns "abc-123"
func extractID(path, prefix string) string {
	idx := strings.Index(path, prefix)
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(prefix):]
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return rest[:slashIdx]
	}
	return rest
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// --- Companies ---

func (h *V1APIHandler) handleListCompanies(w http.ResponseWriter, r *http.Request) {
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
		for _, c := range companies {
			if !c.IsSystem {
				filtered = append(filtered, c)
			}
		}
		companies = filtered
	}

	if companies == nil {
		companies = []V1Company{}
	}
	writeJSON(w, http.StatusOK, companies)
}

func (h *V1APIHandler) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
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

func (h *V1APIHandler) handleGetCompany(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/companies/")
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

// --- Organizations ---

func (h *V1APIHandler) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	companyID := extractID(r.URL.Path, "/companies/")
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

func (h *V1APIHandler) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	companyID := extractID(r.URL.Path, "/companies/")
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

// --- Projects ---

func (h *V1APIHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	orgID := extractID(r.URL.Path, "/organizations/")
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

func (h *V1APIHandler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	orgID := extractID(r.URL.Path, "/organizations/")
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

// --- Workflows ---

func (h *V1APIHandler) handleListWorkflowsByProject(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	projectID := extractID(r.URL.Path, "/projects/")
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

func (h *V1APIHandler) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	projectID := extractID(r.URL.Path, "/projects/")
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

func (h *V1APIHandler) handleListAllWorkflows(w http.ResponseWriter, r *http.Request) {
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
		for _, wf := range wfs {
			if !wf.IsSystem {
				filtered = append(filtered, wf)
			}
		}
		wfs = filtered
	}

	if wfs == nil {
		wfs = []V1Workflow{}
	}
	writeJSON(w, http.StatusOK, wfs)
}

func (h *V1APIHandler) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/workflows/")
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

func (h *V1APIHandler) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/workflows/")
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

func (h *V1APIHandler) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/workflows/")
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

func (h *V1APIHandler) handleDeployWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/workflows/")
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

	updated, err := h.store.SetWorkflowStatus(id, "active")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// For system workflows, trigger engine reload
	if wf.IsSystem && h.reloadFn != nil {
		if err := h.reloadFn(wf.ConfigYAML); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("deploy failed: %v", err)})
			return
		}
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *V1APIHandler) handleStopWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	id := extractID(r.URL.Path, "/workflows/")
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

	updated, err := h.store.SetWorkflowStatus(id, "stopped")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// --- Dashboard ---

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
		for _, wf := range wfs {
			if !wf.IsSystem {
				filtered = append(filtered, wf)
			}
		}
		wfs = filtered
	}

	summaries := make([]dashboardSummary, 0, len(wfs))
	for _, wf := range wfs {
		summaries = append(summaries, dashboardSummary{
			WorkflowID:   wf.ID,
			WorkflowName: wf.Name,
			Status:       wf.Status,
			Executions:   map[string]int{},
			LogCounts:    map[string]int{},
		})
	}

	resp := dashboardResponse{
		TotalWorkflows:    len(wfs),
		WorkflowSummaries: summaries,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *V1APIHandler) handleListVersions(w http.ResponseWriter, r *http.Request) {
	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	workflowID := extractID(r.URL.Path, "/workflows/")
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
