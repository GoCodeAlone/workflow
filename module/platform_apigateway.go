package module

import (
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// IaCCORSConfig holds CORS settings for an IaC-managed API gateway.
type IaCCORSConfig struct {
	AllowOrigins []string `json:"allowOrigins"`
	AllowMethods []string `json:"allowMethods"`
	AllowHeaders []string `json:"allowHeaders"`
}

// IaCGatewayRoute describes a single route managed by the IaC API gateway provisioner.
type IaCGatewayRoute struct {
	Path      string `json:"path"`
	Method    string `json:"method"`
	Target    string `json:"target"`
	RateLimit int    `json:"rateLimit"`
	AuthType  string `json:"authType"` // none, api_key, jwt
}

// IaCGatewayPlan describes the changes needed to reach desired gateway state.
type IaCGatewayPlan struct {
	Name    string            `json:"name"`
	Stage   string            `json:"stage"`
	Routes  []IaCGatewayRoute `json:"routes"`
	CORS    *IaCCORSConfig    `json:"cors,omitempty"`
	Changes []string          `json:"changes"`
}

// IaCGatewayState represents the current state of a provisioned API gateway.
type IaCGatewayState struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Endpoint string            `json:"endpoint"`
	Stage    string            `json:"stage"`
	Routes   []IaCGatewayRoute `json:"routes"`
	CORS     *IaCCORSConfig    `json:"cors,omitempty"`
	Status   string            `json:"status"` // pending, active, updating, deleted
}

// apigatewayBackend is the internal interface for gateway backends.
type apigatewayBackend interface {
	plan(m *PlatformAPIGateway) (*IaCGatewayPlan, error)
	apply(m *PlatformAPIGateway) (*IaCGatewayState, error)
	status(m *PlatformAPIGateway) (*IaCGatewayState, error)
	destroy(m *PlatformAPIGateway) error
}

// PlatformAPIGateway manages API gateway provisioning via pluggable backends.
// Config:
//
//	account:  name of a cloud.account module (optional for mock)
//	provider: mock | aws
//	name:     gateway name
//	stage:    deployment stage (dev, staging, prod)
//	cors:     CORS configuration
//	routes:   list of route definitions
type PlatformAPIGateway struct {
	name    string
	config  map[string]any
	account string
	state   *IaCGatewayState
	backend apigatewayBackend
}

// NewPlatformAPIGateway creates a new PlatformAPIGateway module.
func NewPlatformAPIGateway(name string, cfg map[string]any) *PlatformAPIGateway {
	return &PlatformAPIGateway{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformAPIGateway) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformAPIGateway) Init(app modular.Application) error {
	m.account, _ = m.config["account"].(string)
	if m.account != "" {
		if _, ok := app.SvcRegistry()[m.account]; !ok {
			return fmt.Errorf("platform.apigateway %q: account service %q not found", m.name, m.account)
		}
	}

	provider, _ := m.config["provider"].(string)
	if provider == "" {
		provider = "mock"
	}

	switch provider {
	case "mock":
		m.backend = &mockAPIGatewayBackend{}
	case "aws":
		m.backend = &awsAPIGatewayBackend{}
	default:
		return fmt.Errorf("platform.apigateway %q: unsupported provider %q", m.name, provider)
	}

	gwName, _ := m.config["name"].(string)
	if gwName == "" {
		gwName = m.name
	}
	stage, _ := m.config["stage"].(string)
	if stage == "" {
		stage = "dev"
	}

	m.state = &IaCGatewayState{
		ID:     "",
		Name:   gwName,
		Stage:  stage,
		Status: "pending",
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformAPIGateway) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "API Gateway: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name.
func (m *PlatformAPIGateway) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the proposed changes.
func (m *PlatformAPIGateway) Plan() (*IaCGatewayPlan, error) {
	return m.backend.plan(m)
}

// Apply provisions or updates the gateway.
func (m *PlatformAPIGateway) Apply() (*IaCGatewayState, error) {
	return m.backend.apply(m)
}

// Status returns the current gateway state.
func (m *PlatformAPIGateway) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy tears down the gateway.
func (m *PlatformAPIGateway) Destroy() error {
	return m.backend.destroy(m)
}

// gatewayName returns the configured gateway name, falling back to the module name.
func (m *PlatformAPIGateway) gatewayName() string {
	if n, ok := m.config["name"].(string); ok && n != "" {
		return n
	}
	return m.name
}

// routes parses routes from config.
func (m *PlatformAPIGateway) routes() []IaCGatewayRoute {
	raw, ok := m.config["routes"].([]any)
	if !ok {
		return nil
	}
	var routes []IaCGatewayRoute
	for _, item := range raw {
		r, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := r["path"].(string)
		method, _ := r["method"].(string)
		target, _ := r["target"].(string)
		authType, _ := r["auth_type"].(string)
		rateLimit, _ := intFromAny(r["rate_limit"])
		routes = append(routes, IaCGatewayRoute{
			Path:      path,
			Method:    method,
			Target:    target,
			RateLimit: rateLimit,
			AuthType:  authType,
		})
	}
	return routes
}

// cors parses CORS config.
func (m *PlatformAPIGateway) cors() *IaCCORSConfig {
	raw, ok := m.config["cors"].(map[string]any)
	if !ok {
		return nil
	}
	cfg := &IaCCORSConfig{}
	if origins, ok := raw["allow_origins"].([]any); ok {
		for _, o := range origins {
			if s, ok := o.(string); ok {
				cfg.AllowOrigins = append(cfg.AllowOrigins, s)
			}
		}
	}
	if methods, ok := raw["allow_methods"].([]any); ok {
		for _, me := range methods {
			if s, ok := me.(string); ok {
				cfg.AllowMethods = append(cfg.AllowMethods, s)
			}
		}
	}
	if headers, ok := raw["allow_headers"].([]any); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				cfg.AllowHeaders = append(cfg.AllowHeaders, s)
			}
		}
	}
	return cfg
}

// ─── Mock backend ─────────────────────────────────────────────────────────────

// mockAPIGatewayBackend implements apigatewayBackend with in-memory state.
type mockAPIGatewayBackend struct{}

func (b *mockAPIGatewayBackend) plan(m *PlatformAPIGateway) (*IaCGatewayPlan, error) {
	routes := m.routes()
	plan := &IaCGatewayPlan{
		Name:   m.gatewayName(),
		Stage:  m.state.Stage,
		Routes: routes,
		CORS:   m.cors(),
	}

	switch m.state.Status {
	case "pending", "deleted":
		plan.Changes = []string{
			fmt.Sprintf("create gateway %q in stage %q with %d route(s)", m.gatewayName(), m.state.Stage, len(routes)),
		}
	case "active":
		plan.Changes = []string{"gateway already active, no changes"}
	default:
		plan.Changes = []string{fmt.Sprintf("gateway status=%s, no action", m.state.Status)}
	}

	return plan, nil
}

func (b *mockAPIGatewayBackend) apply(m *PlatformAPIGateway) (*IaCGatewayState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}

	routes := m.routes()
	m.state.ID = fmt.Sprintf("mock-gw-%s", strings.ReplaceAll(m.gatewayName(), " ", "-"))
	m.state.Routes = routes
	m.state.CORS = m.cors()
	m.state.Endpoint = fmt.Sprintf("https://mock.execute-api.example.com/%s", m.state.Stage)
	m.state.Status = "active"

	return m.state, nil
}

func (b *mockAPIGatewayBackend) status(m *PlatformAPIGateway) (*IaCGatewayState, error) {
	return m.state, nil
}

func (b *mockAPIGatewayBackend) destroy(m *PlatformAPIGateway) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.Endpoint = ""
	m.state.ID = ""
	return nil
}

// ─── AWS stub ─────────────────────────────────────────────────────────────────

// awsAPIGatewayBackend is a stub for AWS API Gateway v2.
// Real implementation would use aws-sdk-go-v2/service/apigatewayv2.
type awsAPIGatewayBackend struct{}

func (b *awsAPIGatewayBackend) plan(m *PlatformAPIGateway) (*IaCGatewayPlan, error) {
	return &IaCGatewayPlan{
		Name:    m.gatewayName(),
		Stage:   m.state.Stage,
		Routes:  m.routes(),
		Changes: []string{"AWS API Gateway (stub — use Terraform or aws-sdk-go-v2/service/apigatewayv2)"},
	}, nil
}

func (b *awsAPIGatewayBackend) apply(m *PlatformAPIGateway) (*IaCGatewayState, error) {
	return nil, fmt.Errorf("aws apigateway backend: not implemented — use Terraform or aws-sdk-go-v2/service/apigatewayv2")
}

func (b *awsAPIGatewayBackend) status(m *PlatformAPIGateway) (*IaCGatewayState, error) {
	m.state.Status = "unknown"
	return m.state, nil
}

func (b *awsAPIGatewayBackend) destroy(m *PlatformAPIGateway) error {
	return fmt.Errorf("aws apigateway backend: not implemented — use Terraform or aws-sdk-go-v2/service/apigatewayv2")
}
