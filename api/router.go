package api

import (
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/iam"
	"github.com/GoCodeAlone/workflow/store"
)

// Config holds configuration for the API layer.
type Config struct {
	JWTSecret  string //nolint:gosec // G117: config field
	JWTIssuer  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration

	// AuthRateLimit is the maximum number of requests per minute per IP
	// allowed on the /auth/register and /auth/login endpoints.
	// Defaults to 10 when zero.
	AuthRateLimit int

	// OAuth providers keyed by provider name (e.g. "google", "okta").
	OAuthProviders map[string]*OAuthProviderConfig

	// Engine is an optional engine lifecycle manager used by the workflow
	// deploy/stop endpoints to actually start and stop workflow engines.
	Engine EngineRunner
}

// Stores groups all store interfaces needed by the API.
type Stores struct {
	Users       store.UserStore
	Sessions    store.SessionStore
	Companies   store.CompanyStore
	Projects    store.ProjectStore
	Workflows   store.WorkflowStore
	Memberships store.MembershipStore
	Links       store.CrossWorkflowLinkStore
	Executions  store.ExecutionStore
	Logs        store.LogStore
	Audit       store.AuditStore
	IAM         store.IAMStore
}

// NewRouter creates an http.Handler with all API v1 routes registered.
func NewRouter(stores Stores, cfg Config) http.Handler {
	return NewRouterWithIAM(stores, cfg, nil)
}

// NewRouterWithIAM creates an http.Handler with all API v1 routes and optional IAM resolver.
func NewRouterWithIAM(stores Stores, cfg Config, iamResolver *iam.IAMResolver) http.Handler {
	mux := http.NewServeMux()

	secret := []byte(cfg.JWTSecret)
	permissions := NewPermissionService(stores.Memberships, stores.Workflows, stores.Projects)
	mw := NewMiddleware(secret, stores.Users, permissions)

	// --- Auth ---
	authH := NewAuthHandler(stores.Users, stores.Sessions, secret, cfg.JWTIssuer, cfg.AccessTTL, cfg.RefreshTTL)
	authRL := mw.RateLimit(cfg.AuthRateLimit)
	mux.Handle("POST /api/v1/auth/register", authRL(http.HandlerFunc(authH.Register)))
	mux.Handle("POST /api/v1/auth/login", authRL(http.HandlerFunc(authH.Login)))
	mux.HandleFunc("POST /api/v1/auth/refresh", authH.Refresh)
	mux.Handle("POST /api/v1/auth/logout", mw.RequireAuth(http.HandlerFunc(authH.Logout)))
	mux.Handle("GET /api/v1/auth/me", mw.RequireAuth(http.HandlerFunc(authH.Me)))
	mux.Handle("PUT /api/v1/auth/me", mw.RequireAuth(http.HandlerFunc(authH.UpdateMe)))

	// --- OAuth2 ---
	if len(cfg.OAuthProviders) > 0 {
		oauthH := NewOAuthHandler(stores.Users, cfg.OAuthProviders, secret, cfg.JWTIssuer, cfg.AccessTTL, cfg.RefreshTTL)
		mux.HandleFunc("GET /api/v1/auth/oauth2/{provider}", oauthH.Authorize)
		mux.HandleFunc("GET /api/v1/auth/oauth2/{provider}/callback", oauthH.Callback)
	}

	// --- Companies ---
	compH := NewCompanyHandler(stores.Companies, stores.Memberships, permissions)
	mux.Handle("POST /api/v1/companies", mw.RequireAuth(http.HandlerFunc(compH.Create)))
	mux.Handle("GET /api/v1/companies", mw.RequireAuth(http.HandlerFunc(compH.List)))
	mux.Handle("GET /api/v1/companies/{id}", mw.RequireAuth(http.HandlerFunc(compH.Get)))
	mux.Handle("PUT /api/v1/companies/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(compH.Update))))
	mux.Handle("DELETE /api/v1/companies/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleOwner, "company", "id")(http.HandlerFunc(compH.Delete))))
	mux.Handle("POST /api/v1/companies/{id}/members", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(compH.AddMember))))
	mux.Handle("GET /api/v1/companies/{id}/members", mw.RequireAuth(http.HandlerFunc(compH.ListMembers)))
	mux.Handle("PUT /api/v1/companies/{id}/members/{uid}", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(compH.UpdateMember))))
	mux.Handle("DELETE /api/v1/companies/{id}/members/{uid}", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(compH.RemoveMember))))

	// --- Organizations ---
	orgH := NewOrgHandler(stores.Companies, stores.Memberships, permissions)
	mux.Handle("POST /api/v1/companies/{cid}/organizations", mw.RequireAuth(http.HandlerFunc(orgH.Create)))
	mux.Handle("GET /api/v1/companies/{cid}/organizations", mw.RequireAuth(http.HandlerFunc(orgH.List)))
	mux.Handle("GET /api/v1/organizations/{id}", mw.RequireAuth(http.HandlerFunc(orgH.Get)))
	mux.Handle("PUT /api/v1/organizations/{id}", mw.RequireAuth(http.HandlerFunc(orgH.Update)))
	mux.Handle("DELETE /api/v1/organizations/{id}", mw.RequireAuth(http.HandlerFunc(orgH.Delete)))

	// --- Projects ---
	projH := NewProjectHandler(stores.Projects, stores.Companies, stores.Memberships, permissions)
	mux.Handle("POST /api/v1/organizations/{oid}/projects", mw.RequireAuth(http.HandlerFunc(projH.Create)))
	mux.Handle("GET /api/v1/organizations/{oid}/projects", mw.RequireAuth(http.HandlerFunc(projH.List)))
	mux.Handle("GET /api/v1/projects/{id}", mw.RequireAuth(http.HandlerFunc(projH.Get)))
	mux.Handle("PUT /api/v1/projects/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleEditor, "project", "id")(http.HandlerFunc(projH.Update))))
	mux.Handle("DELETE /api/v1/projects/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleOwner, "project", "id")(http.HandlerFunc(projH.Delete))))
	mux.Handle("POST /api/v1/projects/{id}/members", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "project", "id")(http.HandlerFunc(projH.AddMember))))
	mux.Handle("GET /api/v1/projects/{id}/members", mw.RequireAuth(http.HandlerFunc(projH.ListMembers)))

	// --- Workflows ---
	wfH := NewWorkflowHandler(stores.Workflows, stores.Projects, stores.Memberships, permissions)
	if cfg.Engine != nil {
		wfH.WithEngine(cfg.Engine)
	}
	mux.Handle("POST /api/v1/projects/{pid}/workflows", mw.RequireAuth(http.HandlerFunc(wfH.Create)))
	mux.Handle("GET /api/v1/workflows", mw.RequireAuth(http.HandlerFunc(wfH.ListAll)))
	mux.Handle("GET /api/v1/projects/{pid}/workflows", mw.RequireAuth(http.HandlerFunc(wfH.ListInProject)))
	mux.Handle("GET /api/v1/workflows/{id}", mw.RequireAuth(http.HandlerFunc(wfH.Get)))
	mux.Handle("PUT /api/v1/workflows/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleEditor, "workflow", "id")(http.HandlerFunc(wfH.Update))))
	mux.Handle("DELETE /api/v1/workflows/{id}", mw.RequireAuth(
		mw.RequireRole(store.RoleOwner, "workflow", "id")(http.HandlerFunc(wfH.Delete))))
	mux.Handle("POST /api/v1/workflows/{id}/deploy", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "workflow", "id")(http.HandlerFunc(wfH.Deploy))))
	mux.Handle("POST /api/v1/workflows/{id}/stop", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "workflow", "id")(http.HandlerFunc(wfH.Stop))))
	mux.Handle("GET /api/v1/workflows/{id}/status", mw.RequireAuth(http.HandlerFunc(wfH.Status)))
	mux.Handle("GET /api/v1/workflows/{id}/versions", mw.RequireAuth(http.HandlerFunc(wfH.ListVersions)))
	mux.Handle("GET /api/v1/workflows/{id}/versions/{v}", mw.RequireAuth(http.HandlerFunc(wfH.GetVersion)))
	mux.Handle("POST /api/v1/workflows/{id}/permissions", mw.RequireAuth(
		mw.RequireRole(store.RoleAdmin, "workflow", "id")(http.HandlerFunc(wfH.SetPermission))))
	mux.Handle("GET /api/v1/workflows/{id}/permissions", mw.RequireAuth(http.HandlerFunc(wfH.ListPermissions)))

	// --- Cross-workflow links ---
	linkH := NewLinkHandler(stores.Links, stores.Workflows)
	mux.Handle("POST /api/v1/workflows/{id}/links", mw.RequireAuth(http.HandlerFunc(linkH.Create)))
	mux.Handle("GET /api/v1/workflows/{id}/links", mw.RequireAuth(http.HandlerFunc(linkH.List)))
	mux.Handle("DELETE /api/v1/workflows/{id}/links/{linkId}", mw.RequireAuth(http.HandlerFunc(linkH.Delete)))

	// --- Executions ---
	if stores.Executions != nil {
		execH := NewExecutionHandler(stores.Executions, stores.Workflows, permissions)
		mux.Handle("GET /api/v1/workflows/{id}/executions", mw.RequireAuth(http.HandlerFunc(execH.List)))
		mux.Handle("POST /api/v1/workflows/{id}/trigger", mw.RequireAuth(
			mw.RequireRole(store.RoleEditor, "workflow", "id")(http.HandlerFunc(execH.Trigger))))
		mux.Handle("GET /api/v1/executions/{id}", mw.RequireAuth(http.HandlerFunc(execH.Get)))
		mux.Handle("GET /api/v1/executions/{id}/steps", mw.RequireAuth(http.HandlerFunc(execH.Steps)))
		mux.Handle("POST /api/v1/executions/{id}/cancel", mw.RequireAuth(http.HandlerFunc(execH.Cancel)))
	}

	// --- Logs ---
	if stores.Logs != nil {
		logH := NewLogHandler(stores.Logs, permissions)
		mux.Handle("GET /api/v1/workflows/{id}/logs", mw.RequireAuth(http.HandlerFunc(logH.Query)))
		mux.Handle("GET /api/v1/workflows/{id}/logs/stream", mw.RequireAuth(http.HandlerFunc(logH.Stream)))
	}

	// --- Audit ---
	if stores.Audit != nil {
		auditH := NewAuditHandler(stores.Audit, permissions)
		mux.Handle("GET /api/v1/companies/{id}/audit", mw.RequireAuth(
			mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(auditH.Query))))
	}

	// --- Dashboard ---
	if stores.Executions != nil && stores.Logs != nil {
		dashH := NewDashboardHandler(stores.Executions, stores.Logs, stores.Workflows, stores.Projects, permissions)
		mux.Handle("GET /api/v1/dashboard", mw.RequireAuth(http.HandlerFunc(dashH.System)))
		mux.Handle("GET /api/v1/workflows/{id}/dashboard", mw.RequireAuth(http.HandlerFunc(dashH.Workflow)))
	}

	// --- Events ---
	if stores.Executions != nil && stores.Logs != nil {
		eventsH := NewEventsHandler(stores.Executions, stores.Logs, permissions)
		mux.Handle("GET /api/v1/workflows/{id}/events", mw.RequireAuth(http.HandlerFunc(eventsH.List)))
		mux.Handle("GET /api/v1/workflows/{id}/events/stream", mw.RequireAuth(http.HandlerFunc(eventsH.Stream)))
	}

	// --- IAM ---
	if stores.IAM != nil {
		resolver := iamResolver
		if resolver == nil {
			resolver = iam.NewIAMResolver(stores.IAM)
			resolver.RegisterProvider(&iam.AWSIAMProvider{})
			resolver.RegisterProvider(&iam.KubernetesProvider{})
			resolver.RegisterProvider(&iam.OIDCProvider{})
		}

		iamH := NewIAMHandler(stores.IAM, resolver, permissions)
		mux.Handle("POST /api/v1/companies/{id}/iam/providers", mw.RequireAuth(
			mw.RequireRole(store.RoleAdmin, "company", "id")(http.HandlerFunc(iamH.CreateProvider))))
		mux.Handle("GET /api/v1/companies/{id}/iam/providers", mw.RequireAuth(http.HandlerFunc(iamH.ListProviders)))
		mux.Handle("GET /api/v1/iam/providers/{id}", mw.RequireAuth(http.HandlerFunc(iamH.GetProvider)))
		mux.Handle("PUT /api/v1/iam/providers/{id}", mw.RequireAuth(http.HandlerFunc(iamH.UpdateProvider)))
		mux.Handle("DELETE /api/v1/iam/providers/{id}", mw.RequireAuth(http.HandlerFunc(iamH.DeleteProvider)))
		mux.Handle("POST /api/v1/iam/providers/{id}/test", mw.RequireAuth(http.HandlerFunc(iamH.TestConnection)))
		mux.Handle("POST /api/v1/iam/providers/{id}/mappings", mw.RequireAuth(http.HandlerFunc(iamH.CreateMapping)))
		mux.Handle("GET /api/v1/iam/providers/{id}/mappings", mw.RequireAuth(http.HandlerFunc(iamH.ListMappings)))
		mux.Handle("DELETE /api/v1/iam/mappings/{id}", mw.RequireAuth(http.HandlerFunc(iamH.DeleteMapping)))
	}

	return mux
}
