package module

// infra_admin.go (T15) hosts the engine-side `infra.admin` workflow
// module — the integration centerpiece for the host-side IaC admin
// surface. The module wires together every prior task's deliverable:
//
//   * Handler library (T5/T6) — pure read-side functions taking
//     state / providers / catalogs / proto inputs and returning
//     typed proto outputs.
//   * State store (T1) — interfaces.IaCStateStore resolved from a
//     declared iac.state module via app.GetService.
//   * Provider loader (T2/T3) — interfaces.IaCProvider map resolved
//     from each declared iac.provider module via app.GetService.
//   * providerTypeByModule map (T6 F1) — populated at Init by
//     walking the loaded *config.WorkflowConfig via
//     app.GetConfigSection("workflow") and reading each
//     iac.provider module's config["provider"] string. This is the
//     stable identifier handler.ListProviders uses to key the
//     region + engine catalogs — provider.Name() returns the
//     plugin's display name and would not match the catalogs.
//   * FieldSpec + Region + Engine catalogs (T7a/T7b/T8) — three
//     in-process tables driving the new-resource form-builder UI.
//   * AssetFS (T13) — embedded UI pages served via http.FileServerFS
//     at the module's asset_prefix.
//   * Audit writer (T14) — protojson-shaped AdminAuditEntry JSONL
//     opened at Init when access_log_path is non-empty, closed at
//     Stop. FATAL on open failure per design Security Review.
//
// Lifecycle (per design §Module lifecycle):
//   * Init resolves state/providers/router/security-headers
//     services + the providerTypeByModule map. Catalogs are
//     instantiated in-process.
//   * Start resolves the workflowEngine service (registered AFTER
//     module.Init by engine.configureTriggers), mounts the typed
//     API routes + asset routes under the configured prefixes with
//     explicit security-headers middleware, then fires the three
//     admin-plugin contribution registration pipelines via
//     engine.TriggerWorkflow.
//   * Stop closes the audit writer (if open).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/admin"
	"github.com/GoCodeAlone/workflow/iac/admin/audit"
	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// InfraAdminConfig is the YAML-config shape the host expects under
// the `infra.admin` module entry. Field names use snake_case yaml
// tags to match the rest of the workflow config; defaults match the
// design's reference app config.
type InfraAdminConfig struct {
	// RoutePrefix is the URL prefix for typed API routes (e.g.
	// /api/infra-admin/resources). Default: "/api/infra-admin".
	RoutePrefix string `yaml:"route_prefix" json:"route_prefix"`

	// AssetPrefix is the URL prefix for the embedded UI pages
	// (e.g. /admin/infra-admin/resources.html). Default:
	// "/admin/infra-admin".
	AssetPrefix string `yaml:"asset_prefix" json:"asset_prefix"`

	// StateModule names the host's iac.state module to resolve
	// via app.GetService for the IaCStateStore handle.
	StateModule string `yaml:"state_module" json:"state_module"`

	// HTTPModule names the *StandardHTTPRouter to mount routes on
	// (typically "http-router"). Resolved via app.GetService at
	// Init and type-asserted at Start before AddRouteWithMiddleware
	// calls.
	HTTPModule string `yaml:"http_module" json:"http_module"`

	// SecurityHeadersModule names the HTTPMiddleware module to
	// attach explicitly on every registered API + asset route.
	// Per design §Security Review the SAMEORIGIN + restrictive CSP
	// must wrap admin responses even if the host's global
	// middleware ordering changes.
	SecurityHeadersModule string `yaml:"security_headers_module" json:"security_headers_module"`

	// AuthModule names the HTTPMiddleware module that enforces
	// authentication on every infra-admin API + asset route. Per
	// design §Security Review: "All /api/infra-admin/* and
	// /admin/infra-admin/* sit behind the host's auth route
	// filter (same as /admin/*)". The middleware MUST reject
	// unauthenticated requests with 401 before the handler runs;
	// without it the handler-side AdminAuthzEvidence default-deny
	// is trivially bypassable because the client supplies the
	// evidence in the request body. Resolved via app.GetService
	// at Init. Empty disables auth (test-only / single-tenant
	// dev mode); production deployments MUST set this.
	AuthModule string `yaml:"auth_module" json:"auth_module"`

	// ProviderModules lists the iac.provider module names to
	// resolve. Each is resolved to an interfaces.IaCProvider via
	// app.GetService at Init.
	ProviderModules []string `yaml:"provider_modules" json:"provider_modules"`

	// AccessLogPath is the on-disk path for the audit JSONL file.
	// Empty disables the audit writer; non-empty opens the writer
	// at Init and propagates open errors as a module-init failure
	// (FATAL per design Security Review).
	AccessLogPath string `yaml:"access_log_path" json:"access_log_path"`

	// AllowUnauthenticated opts into insecure single-tenant mode.
	// When false (the default) and AuthModule is empty, Init returns
	// an error requiring auth_module. Mutation routes are NEVER
	// registered without a real AuthModule; with AllowUnauthenticated:true
	// only read routes are active and a prominent warning is logged.
	AllowUnauthenticated bool `yaml:"allow_unauthenticated" json:"allow_unauthenticated"`

	// AuthzModule names the authz.casbin (or compatible) module to
	// resolve for server-side RBAC on mutation routes. When non-empty,
	// infra.admin resolves the module as an Enforcer at Init and calls
	// Enforce(subject,"infra:apply"/"infra:destroy"/"infra:read","allow")
	// on every request. When empty, authentication is required but RBAC
	// is skipped (authn-only single-tenant posture).
	AuthzModule string `yaml:"authz_module" json:"authz_module"`
}

// Enforcer is the server-side RBAC interface satisfied by the
// authz.casbin module wrapper. The variadic extra ...string matches
// the concrete Casbin wrapper's method signature (plan-review C-NEW-1),
// so a non-variadic declaration would not be satisfied by the wrapper.
type Enforcer interface {
	Enforce(sub, obj, act string, extra ...string) (bool, error)
}

// InfraAdmin is the engine-side workflow module. Implements
// modular.Module + the Init/Start/Stop lifecycle hooks.
type InfraAdmin struct {
	name   string
	config InfraAdminConfig

	// Resolved at Init.
	app                  modular.Application
	state                interfaces.IaCStateStore
	providers            map[string]interfaces.IaCProvider
	providerTypeByModule map[string]string
	router               *StandardHTTPRouter
	secHdrs              HTTPMiddleware
	auth                 HTTPMiddleware
	authz                Enforcer // nil when authz_module not configured

	// Catalogs are instantiated in-process at Init.
	fieldCatalog  *catalog.FieldSpecCatalog
	regionCatalog *catalog.RegionCatalog
	engineCatalog *catalog.EngineCatalog

	// audit is non-nil iff config.AccessLogPath != "" and Open
	// succeeded at Init.
	audit *audit.Writer

	// Resolved at Start (workflowEngine is registered by engine.
	// configureTriggers AFTER app.Init returns).
	engine WorkflowEngine
}

// (The shared WorkflowEngine interface — TriggerWorkflow only —
// already exists in module/http_trigger.go; we reuse it here so
// the package has a single definition.)

// NewInfraAdmin is the module factory the engine's BuildFromConfig
// dispatches to for `type: infra.admin` entries. T18 registers this
// with the engine via AddModuleType. The factory decodes the loose
// config map into the typed InfraAdminConfig + applies defaults so
// callers can omit fields with sensible fallbacks.
func NewInfraAdmin(name string, cfg map[string]any) modular.Module {
	c := InfraAdminConfig{
		RoutePrefix: "/api/infra-admin",
		AssetPrefix: "/admin/infra-admin",
	}
	// Round-trip the loose map through JSON to populate the typed
	// struct — uses the same json tags the proto/wfctlhelpers layers
	// use elsewhere in the codebase. Map keys not present in the
	// struct are silently ignored (e.g. `_config_dir` injected by
	// engine.BuildFromConfig at line 612).
	if raw, err := json.Marshal(cfg); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	return &InfraAdmin{
		name:      name,
		config:    c,
		providers: map[string]interfaces.IaCProvider{},
	}
}

// Name returns the host-side module name.
func (m *InfraAdmin) Name() string { return m.name }

// Dependencies returns the names of modules that MUST initialise
// before this one — the modular framework uses this for its
// init-order DAG. Per design §Module lifecycle: state + http +
// security-headers + every declared provider.
func (m *InfraAdmin) Dependencies() []string {
	deps := []string{}
	if m.config.StateModule != "" {
		deps = append(deps, m.config.StateModule)
	}
	if m.config.HTTPModule != "" {
		deps = append(deps, m.config.HTTPModule)
	}
	if m.config.SecurityHeadersModule != "" {
		deps = append(deps, m.config.SecurityHeadersModule)
	}
	if m.config.AuthModule != "" {
		deps = append(deps, m.config.AuthModule)
	}
	if m.config.AuthzModule != "" {
		deps = append(deps, m.config.AuthzModule)
	}
	deps = append(deps, m.config.ProviderModules...)
	return deps
}

// RequiresServices declares the same set as Dependencies, but
// shaped for the service-dependency resolver. Both are needed —
// Dependencies drives Init ordering; RequiresServices drives
// service-graph wiring.
//
// NB: workflowEngine is intentionally NOT listed here — it's
// registered by engine.configureTriggers AFTER app.Init returns,
// so listing it as a required service would cause Init to fail
// when modular's resolver runs before the engine has registered
// itself. Resolved at Start instead. Per design line 749-750.
func (m *InfraAdmin) RequiresServices() []modular.ServiceDependency {
	deps := []modular.ServiceDependency{}
	if m.config.StateModule != "" {
		deps = append(deps, modular.ServiceDependency{Name: m.config.StateModule})
	}
	if m.config.HTTPModule != "" {
		deps = append(deps, modular.ServiceDependency{Name: m.config.HTTPModule})
	}
	if m.config.SecurityHeadersModule != "" {
		deps = append(deps, modular.ServiceDependency{Name: m.config.SecurityHeadersModule})
	}
	if m.config.AuthModule != "" {
		deps = append(deps, modular.ServiceDependency{Name: m.config.AuthModule})
	}
	if m.config.AuthzModule != "" {
		deps = append(deps, modular.ServiceDependency{Name: m.config.AuthzModule})
	}
	for _, pm := range m.config.ProviderModules {
		deps = append(deps, modular.ServiceDependency{Name: pm})
	}
	return deps
}

// ProvidesServices is nil — this module is a sink, not a source.
// Per design §Module lifecycle: external consumers do not call
// this module via service-graph lookup; they hit it via the HTTP
// routes mounted in Start.
func (m *InfraAdmin) ProvidesServices() []modular.ServiceProvider { return nil }

// Init resolves the host-side service dependencies + populates the
// providerTypeByModule map + opens the audit writer.
//
// Per design line 749: workflowEngine is NOT resolved here — see
// Start. The intermediate state where the engine isn't yet
// registered is real and intentional; Init must succeed in that
// window so app.Init() returns and configureTriggers can register
// the engine.
func (m *InfraAdmin) Init(app modular.Application) error {
	m.app = app

	// T4 (#29): require auth_module unless the operator explicitly
	// opted into insecure single-tenant mode. Mutation routes are
	// NEVER registered without auth regardless of this flag.
	if m.config.AuthModule == "" && !m.config.AllowUnauthenticated {
		return fmt.Errorf("infra.admin: auth_module required (set allow_unauthenticated:true to opt into insecure single-tenant mode)")
	}
	if m.config.AuthModule == "" && m.config.AllowUnauthenticated {
		app.Logger().Warn("infra.admin: mutation routes DISABLED (no auth_module); reads only")
	}

	// State store.
	if m.config.StateModule != "" {
		if err := app.GetService(m.config.StateModule, &m.state); err != nil {
			return fmt.Errorf("infra.admin: state module %q: %w", m.config.StateModule, err)
		}
	}

	// HTTP router — type-assert to *StandardHTTPRouter so we can
	// call AddRouteWithMiddleware at Start. The interface alone
	// (HTTPRouter) doesn't expose middleware-aware route
	// registration; the design explicitly requires the typed
	// concrete (per §Module lifecycle "ProvidesServices the
	// *StandardHTTPRouter typed instance"). We resolve via the
	// generic interface{} and then type-assert so the failure
	// message is operator-actionable.
	if m.config.HTTPModule != "" {
		var router any
		if err := app.GetService(m.config.HTTPModule, &router); err != nil {
			return fmt.Errorf("infra.admin: http module %q: %w", m.config.HTTPModule, err)
		}
		sr, ok := router.(*StandardHTTPRouter)
		if !ok {
			return fmt.Errorf("infra.admin: http module %q is %T, need *StandardHTTPRouter", m.config.HTTPModule, router)
		}
		m.router = sr
	}

	// Security headers middleware.
	if m.config.SecurityHeadersModule != "" {
		var mw any
		if err := app.GetService(m.config.SecurityHeadersModule, &mw); err != nil {
			return fmt.Errorf("infra.admin: security-headers module %q: %w", m.config.SecurityHeadersModule, err)
		}
		secMw, ok := mw.(HTTPMiddleware)
		if !ok {
			return fmt.Errorf("infra.admin: security-headers module %q is %T, need HTTPMiddleware", m.config.SecurityHeadersModule, mw)
		}
		m.secHdrs = secMw
	}

	// Auth middleware — per design §Security Review the
	// /api/infra-admin/* and /admin/infra-admin/* routes MUST
	// sit behind the host's auth route filter (same as
	// /admin/*). Without it, the handler-side AdminAuthzEvidence
	// default-deny is bypassable: the client supplies
	// {authz_checked, authz_allowed} in the request body, so an
	// unauthenticated network actor can send
	// {evidence:{authz_checked:true,authz_allowed:true}} and the
	// handler accepts it. The auth middleware rejects requests
	// without a valid Bearer token at 401 before the handler ever
	// runs, closing that gap.
	if m.config.AuthModule != "" {
		var mw any
		if err := app.GetService(m.config.AuthModule, &mw); err != nil {
			return fmt.Errorf("infra.admin: auth module %q: %w", m.config.AuthModule, err)
		}
		authMw, ok := mw.(HTTPMiddleware)
		if !ok {
			return fmt.Errorf("infra.admin: auth module %q is %T, need HTTPMiddleware", m.config.AuthModule, mw)
		}
		m.auth = authMw
	}

	// Authz enforcer (optional — for server-side write-tier RBAC).
	if m.config.AuthzModule != "" {
		var authzSvc any
		if err := app.GetService(m.config.AuthzModule, &authzSvc); err != nil {
			return fmt.Errorf("infra.admin: authz module %q: %w", m.config.AuthzModule, err)
		}
		enforcer, ok := authzSvc.(Enforcer)
		if !ok {
			return fmt.Errorf("infra.admin: authz module %q is %T, need Enforcer", m.config.AuthzModule, authzSvc)
		}
		m.authz = enforcer
	}

	// Per-provider IaCProvider handles.
	for _, pm := range m.config.ProviderModules {
		var p interfaces.IaCProvider
		if err := app.GetService(pm, &p); err != nil {
			return fmt.Errorf("infra.admin: provider %q: %w", pm, err)
		}
		m.providers[pm] = p
	}

	// Populate providerTypeByModule from the loaded WorkflowConfig
	// per spec-reviewer T6 F1 + design cycle-5/6: handler.
	// ListProviders needs the YAML-config `provider:` string, NOT
	// the plugin's display name from provider.Name().
	if err := m.populateProviderTypes(app); err != nil {
		return fmt.Errorf("infra.admin: populate provider types: %w", err)
	}

	// In-process catalogs.
	m.fieldCatalog = catalog.New()
	m.regionCatalog = catalog.NewRegionCatalog()
	m.engineCatalog = catalog.NewEngineCatalog()

	// Audit writer (optional — empty path disables; non-empty path
	// MUST succeed per design Security Review).
	if m.config.AccessLogPath != "" {
		w, err := audit.Open(m.config.AccessLogPath)
		if err != nil {
			return fmt.Errorf("infra.admin: open audit log %q: %w", m.config.AccessLogPath, err)
		}
		m.audit = w
	}
	return nil
}

// populateProviderTypes walks the loaded WorkflowConfig and captures
// each iac.provider module's config["provider"] string keyed by
// module name. The result feeds handler.ListProviders' provider_type
// + region/engine-catalog-key parameter.
//
// Per spec-reviewer T6 F1: this string is the stable identifier;
// provider.Name() returns the plugin's display name and would fail
// region/engine catalog lookups.
//
// The WorkflowConfig is registered as a config-section under the
// "workflow" name by engine.go:672. Module fall-back (no config
// section) leaves the map empty, which degrades gracefully —
// handler.ListProviders emits per-module entries with empty
// provider_type + empty regions/engines per
// TestListProviders_MissingProviderTypeByModule_DegradesGracefully.
func (m *InfraAdmin) populateProviderTypes(app modular.Application) error {
	m.providerTypeByModule = map[string]string{}

	section, err := app.GetConfigSection("workflow")
	if err != nil || section == nil {
		// Config section missing — graceful degradation per design:
		// UI shows empty region/engine dropdowns rather than the
		// admin module refusing to start. The `err` is intentionally
		// not propagated; the section's absence is a normal state in
		// unit-test fakes and during early bootstrap.
		return nil //nolint:nilerr // intentional: graceful degradation, see comment
	}
	wfCfg, ok := section.GetConfig().(*config.WorkflowConfig)
	if !ok || wfCfg == nil {
		return nil
	}
	for i := range wfCfg.Modules {
		mod := &wfCfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		modCfg := config.ExpandEnvInMap(mod.Config)
		pt, _ := modCfg["provider"].(string)
		if pt == "" {
			continue
		}
		m.providerTypeByModule[mod.Name] = pt
	}
	return nil
}

// Start resolves the workflowEngine service (registered after
// app.Init by engine.configureTriggers), mounts the typed API +
// asset routes with the explicit security-headers middleware, and
// fires the three admin-plugin contribution registration pipelines
// via engine.TriggerWorkflow.
//
// Per design line 820-882: the workflowEngine resolution MUST be
// here, not Init.
func (m *InfraAdmin) Start(ctx context.Context) error {
	if m.app == nil {
		return fmt.Errorf("infra.admin: Start called before Init")
	}
	if err := m.app.GetService("workflowEngine", &m.engine); err != nil {
		return fmt.Errorf("infra.admin: workflowEngine: %w", err)
	}

	if m.router == nil {
		return fmt.Errorf("infra.admin: router unresolved — Init failed silently?")
	}

	// Middleware chain: auth FIRST so unauthenticated requests
	// short-circuit at 401 before any handler / security-headers
	// processing runs. Per design §Security Review +
	// AddRouteWithMiddleware contract (http_router.go:228-235):
	// middlewares execute in slice order, so [auth, secHdrs] means
	// auth wraps secHdrs wraps the handler.
	mws := []HTTPMiddleware{}
	if m.auth != nil {
		mws = append(mws, m.auth)
	}
	if m.secHdrs != nil {
		mws = append(mws, m.secHdrs)
	}

	// Typed API routes.
	apiRoutes := []struct {
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"POST", m.config.RoutePrefix + "/resources", m.handleListResources},
		{"POST", m.config.RoutePrefix + "/resources/{name}", m.handleGetResource},
		{"POST", m.config.RoutePrefix + "/types", m.handleListResourceTypes},
		{"POST", m.config.RoutePrefix + "/providers", m.handleListProviders},
		{"POST", m.config.RoutePrefix + "/generate-config", m.handleGenerateConfig},
		{"GET", m.config.RoutePrefix + "/audit", m.handleAuditTail},
	}
	for _, r := range apiRoutes {
		adapter := NewHTTPHandlerAdapter(r.handler)
		m.router.AddRouteWithMiddleware(r.method, r.path, adapter, mws)
	}

	// Asset routes — http.FileServer over the embedded admin.AssetFS.
	// fs.Sub strips the leading "ui_dist/" so a request for
	// /admin/infra-admin/resources.html (after StripPrefix removes
	// /admin/infra-admin) resolves to ui_dist/resources.html inside
	// the embed FS. Without the Sub, FileServer would look for
	// resources.html at the FS root and 404.
	uiSub, err := fs.Sub(admin.AssetFS, "ui_dist")
	if err != nil {
		return fmt.Errorf("infra.admin: subfs ui_dist: %w", err)
	}
	assetHandler := http.StripPrefix(m.config.AssetPrefix, http.FileServer(http.FS(uiSub)))
	assetAdapter := NewHTTPHandlerAdapter(assetHandler)
	m.router.AddRouteWithMiddleware("GET", m.config.AssetPrefix+"/{rest...}", assetAdapter, mws)

	// Admin-plugin contribution registration pipelines. The admin
	// plugin defines three pipelines that accept contributions
	// (resource list / resource detail / new-resource form); each
	// fires once at module Start so the admin dashboard renders
	// the entries after the host comes up.
	contributions := []struct {
		pipelineName string
		payload      map[string]any
	}{
		{"register-infra-admin-resources", map[string]any{
			"module": "admin",
			"contribution": map[string]any{
				"id":          "infra.resources",
				"title":       "Infra Resources",
				"category":    "infra",
				"path":        m.config.AssetPrefix + "/resources.html",
				"render_mode": "iframe",
				"permissions": []map[string]any{{
					"resource": "infra", "action": "read", "permission": "infra:read",
				}},
			},
		}},
		{"register-infra-admin-resource-detail", map[string]any{
			"module": "admin",
			"contribution": map[string]any{
				"id":          "infra.resource-detail",
				"title":       "Resource Detail",
				"category":    "infra",
				"path":        m.config.AssetPrefix + "/resource.html",
				"render_mode": "iframe",
				"permissions": []map[string]any{{
					"resource": "infra", "action": "read", "permission": "infra:read",
				}},
			},
		}},
		{"register-infra-admin-new-resource", map[string]any{
			"module": "admin",
			"contribution": map[string]any{
				"id":          "infra.new",
				"title":       "Draft New Resource",
				"category":    "infra",
				"path":        m.config.AssetPrefix + "/new.html",
				"render_mode": "iframe",
				"permissions": []map[string]any{{
					"resource": "infra", "action": "read", "permission": "infra:read",
				}},
			},
		}},
	}
	for _, c := range contributions {
		if err := m.engine.TriggerWorkflow(ctx, "pipeline:"+c.pipelineName, "", c.payload); err != nil {
			return fmt.Errorf("infra.admin: register contribution via pipeline:%s: %w", c.pipelineName, err)
		}
	}
	return nil
}

// Stop closes the audit writer (idempotent — double-Stop is a
// no-op because audit.Writer.Close is idempotent).
func (m *InfraAdmin) Stop(_ context.Context) error {
	if m.audit != nil {
		return m.audit.Close()
	}
	return nil
}

// ── HTTP handlers ───────────────────────────────────────────────

// marshalOpts is the protojson configuration every handler uses on
// the response path. UseProtoNames=true emits snake_case JSON keys
// matching the proto field names — required by the asset JS pages
// (T10-T12) which access r.provider_module / r.applied_config_json
// / etc. Per spec-reviewer's cross-task contract.
var marshalOpts = protojson.MarshalOptions{UseProtoNames: true}

// unmarshalOpts is the protojson decode configuration. We allow
// unknown fields so the host can ride out a v1.1 client emitting
// new request fields the handler hasn't seen yet — strict refusal
// would create a backward-compat trap.
var unmarshalOpts = protojson.UnmarshalOptions{DiscardUnknown: true}

// readAdminBody reads the request body up to a sensible cap (256KB)
// so a pathological client can't OOM the host. AdminListResources
// Input and friends are tiny structs; 256KB is generous headroom.
// Named distinctly from module/api_v1_featureflags.go's readBody
// to avoid the package-level collision.
func readAdminBody(r *http.Request) ([]byte, error) {
	const maxBody = 256 * 1024
	return io.ReadAll(io.LimitReader(r.Body, maxBody))
}

// auditAccess writes one AdminAuditEntry to the audit log if the
// writer is configured. Errors are logged via stderr (via the
// audit package) but never propagate — the access log is a
// best-effort observability surface, not a request-path
// dependency.
//
// The result string distinguishes outcomes per the proto field's
// semantic intent: "ok" for served requests, "denied" for authz
// refusals (handler's Output.error non-empty). Per spec-reviewer
// T15 F2 (commit 60971783d): hardcoding "ok" hid real denial
// attempts in the access log, defeating the audit log's
// security-review purpose.
// subjectFromRequest extracts the authenticated subject from the
// request context. The auth middleware stores JWT claims as
// map[string]any under authClaimsContextKey; sub is the standard
// JWT claim for the principal. Returns "" when no claims are present
// (e.g. allow_unauthenticated mode or auth middleware not wired).
func (m *InfraAdmin) subjectFromRequest(r *http.Request) string {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok || claims == nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

func (m *InfraAdmin) auditAccess(r *http.Request, action string, ev *adminpb.AdminAuthzEvidence, result string) {
	if m.audit == nil {
		return
	}
	subject := ""
	if ev != nil {
		subject = ev.GetSubject()
	}
	entry := &audit.Entry{
		TsUnix:  nowUnix(),
		Subject: subject,
		Action:  action,
		Result:  result,
	}
	_ = m.audit.Write(entry)
	_ = r // r reserved for future targets/app_context extraction
}

// auditResultFor maps a handler output's Error field to the
// audit log's `result` value. Empty error → "ok"; non-empty →
// "denied" (the handler library's primary refusal path is
// authz-default-deny, so "denied" is the most informative
// label for v1; future v1.1 might split into "denied"/"error"
// /"not_found" but the proto field is a free-form string).
func auditResultFor(errMsg string) string {
	if errMsg == "" {
		return "ok"
	}
	return "denied"
}

// nowUnix is a package-level var so tests can substitute a fixed
// clock without touching time. Default is defaultNowUnix (declared
// in infra_admin_clock.go) → time.Now().UTC().Unix().
var nowUnix = defaultNowUnix

func (m *InfraAdmin) handleListResources(w http.ResponseWriter, r *http.Request) {
	body, err := readAdminBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var in adminpb.AdminListResourcesInput
	if len(body) > 0 {
		if err := unmarshalOpts.Unmarshal(body, &in); err != nil {
			http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	out, _ := handler.ListResources(r.Context(), m.state, m.providers, m.fieldCatalog, &in)
	writeProtoMsg(w, out)
	m.auditAccess(r, "list_resources", in.GetEvidence(), auditResultFor(out.GetError()))
}

func (m *InfraAdmin) handleGetResource(w http.ResponseWriter, r *http.Request) {
	body, err := readAdminBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var in adminpb.AdminGetResourceInput
	if len(body) > 0 {
		if err := unmarshalOpts.Unmarshal(body, &in); err != nil {
			http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	// Route-level path param: /resources/{name}. Falls through to
	// body-level Name when path param absent (e.g. tests posting
	// directly).
	if v := r.PathValue("name"); v != "" {
		in.Name = v
	}
	out, _ := handler.GetResource(r.Context(), m.state, &in)
	writeProtoMsg(w, out)
	m.auditAccess(r, "get_resource", in.GetEvidence(), auditResultFor(out.GetError()))
}

func (m *InfraAdmin) handleListResourceTypes(w http.ResponseWriter, r *http.Request) {
	body, err := readAdminBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var in adminpb.AdminListResourceTypesInput
	if len(body) > 0 {
		if err := unmarshalOpts.Unmarshal(body, &in); err != nil {
			http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	out, _ := handler.ListResourceTypes(r.Context(), m.fieldCatalog, m.providers, &in)
	writeProtoMsg(w, out)
	m.auditAccess(r, "list_types", in.GetEvidence(), auditResultFor(out.GetError()))
}

func (m *InfraAdmin) handleListProviders(w http.ResponseWriter, r *http.Request) {
	body, err := readAdminBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var in adminpb.AdminListProvidersInput
	if len(body) > 0 {
		if err := unmarshalOpts.Unmarshal(body, &in); err != nil {
			http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	out, _ := handler.ListProviders(
		r.Context(),
		m.providers,
		m.providerTypeByModule,
		m.fieldCatalog,
		m.regionCatalog,
		m.engineCatalog,
		&in,
	)
	writeProtoMsg(w, out)
	m.auditAccess(r, "list_providers", in.GetEvidence(), auditResultFor(out.GetError()))
}

func (m *InfraAdmin) handleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	body, err := readAdminBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var in adminpb.AdminGenerateConfigInput
	if len(body) > 0 {
		if err := unmarshalOpts.Unmarshal(body, &in); err != nil {
			http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	out, _ := handler.GenerateConfig(r.Context(), m.fieldCatalog, &in)
	writeProtoMsg(w, out)
	m.auditAccess(r, "generate_config", in.GetEvidence(), auditResultFor(out.GetError()))
}

// handleAuditTail streams the audit log file as ndjson when the
// audit writer is enabled. Honors `?since=<unix>&limit=N` query
// params per design §Security Review row "Access logging":
// "GET /api/infra-admin/audit?since=<unix>&limit=N returning
// ndjson". The CLI's `wfctl infra admin audit-tail --since 1h`
// translates the duration to a unix timestamp; the host filters
// by ts_unix > since AND emits at most `limit` entries (0 = no
// limit). Per spec-reviewer T15 F1 (commit 60971783d).
//
// Implementation: scans the file line-by-line via bufio.Scanner
// (1MB max line, same as the CLI decoder), protojson-decodes each
// line to read ts_unix, drops out-of-window entries, forwards the
// rest as ndjson. Lines that fail to decode are skipped silently
// — append-only file may contain partial writes mid-rotation; the
// audit-tail consumer treats those as benign.
//
// Status semantics: opens the file with os.Open BEFORE writing
// any response headers so a missing/permission-denied file
// produces a clean 404 / 500. ServeFile's pre-WriteHeader contract
// is the source of the F3 collision the prior draft had.
func (m *InfraAdmin) handleAuditTail(w http.ResponseWriter, r *http.Request) {
	if m.audit == nil {
		http.Error(w, "audit log not configured (set access_log_path on infra.admin module)", http.StatusNotFound)
		return
	}

	// Parse query params. Empty / unparseable values default to 0
	// (no filter / no limit) — matches the design's permissive shape.
	q := r.URL.Query()
	var sinceUnix int64
	if v := q.Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			sinceUnix = n
		}
	}
	var limit int
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	f, err := os.Open(m.config.AccessLogPath)
	if err != nil {
		// File missing or permission denied — 404 mirrors
		// http.ServeFile's IsNotExist branch; 500 covers other
		// I/O failures. Body is plain text since the CLI's
		// renderAuditTable surfaces the error string verbatim.
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "audit log file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "open audit log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// Only set headers once we know the file is readable. Header
	// + status MUST be set before the first body write, but we
	// stream after — clearing F3.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var emitted int
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Filter by ts_unix when since is set. Decode just enough
		// to read the field; ignore decode errors so partial-write
		// lines don't truncate the stream for downstream entries.
		if sinceUnix > 0 {
			var entry adminpb.AdminAuditEntry
			if err := protojson.Unmarshal(line, &entry); err != nil {
				continue
			}
			if entry.GetTsUnix() < sinceUnix {
				continue
			}
		}
		// Forward the line as-is so the protojson byte sequence is
		// preserved byte-for-byte (the CLI's decoder expects the
		// exact wire format the writer emitted, not a re-marshaled
		// shape — preserves the int64-as-decimal-string convention).
		if _, werr := w.Write(append(line, '\n')); werr != nil {
			return
		}
		emitted++
		if limit > 0 && emitted >= limit {
			return
		}
	}
	// Scanner errors mid-stream get swallowed — the client already
	// received bytes so we can't change the HTTP status. The next
	// audit-tail request will re-attempt.
}

// writeProtoMsg marshals a proto message via the shared protojson
// MarshalOptions (UseProtoNames=true so snake_case keys match the
// asset JS pages' expectations per the cross-task wire contract).
// On marshal failure, returns 500 with a plain-text body so the
// client always sees an actionable status code.
func writeProtoMsg(w http.ResponseWriter, msg proto.Message) {
	data, err := marshalOpts.Marshal(msg)
	if err != nil {
		http.Error(w, "marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
