package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

// PlatformGatewayCORSConfig holds CORS settings for a provisioned API gateway.
type PlatformGatewayCORSConfig struct {
	AllowOrigins []string `json:"allowOrigins"`
	AllowMethods []string `json:"allowMethods"`
	AllowHeaders []string `json:"allowHeaders"`
}

// PlatformGatewayRoute describes a single route managed by the API gateway provisioner.
type PlatformGatewayRoute struct {
	Path      string `json:"path"`
	Method    string `json:"method"`
	Target    string `json:"target"`
	RateLimit int    `json:"rateLimit"`
	AuthType  string `json:"authType"` // none, api_key, jwt
}

// PlatformGatewayPlan describes the changes needed to reach desired gateway state.
type PlatformGatewayPlan struct {
	Name    string                     `json:"name"`
	Stage   string                     `json:"stage"`
	Routes  []PlatformGatewayRoute     `json:"routes"`
	CORS    *PlatformGatewayCORSConfig `json:"cors,omitempty"`
	Changes []string                   `json:"changes"`
}

// PlatformGatewayState represents the current state of a provisioned API gateway.
type PlatformGatewayState struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Endpoint string                     `json:"endpoint"`
	Stage    string                     `json:"stage"`
	Routes   []PlatformGatewayRoute     `json:"routes"`
	CORS     *PlatformGatewayCORSConfig `json:"cors,omitempty"`
	Status   string                     `json:"status"` // pending, active, updating, deleted
}

// apigatewayBackend is the internal interface for gateway provisioning backends.
type apigatewayBackend interface {
	plan(m *PlatformAPIGateway) (*PlatformGatewayPlan, error)
	apply(m *PlatformAPIGateway) (*PlatformGatewayState, error)
	status(m *PlatformAPIGateway) (*PlatformGatewayState, error)
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
	name     string
	config   map[string]any
	account  string
	provider CloudCredentialProvider
	state    *PlatformGatewayState
	backend  apigatewayBackend
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
		svc, ok := app.SvcRegistry()[m.account]
		if !ok {
			return fmt.Errorf("platform.apigateway %q: account service %q not found", m.name, m.account)
		}
		if prov, ok := svc.(CloudCredentialProvider); ok {
			m.provider = prov
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

	m.state = &PlatformGatewayState{
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
func (m *PlatformAPIGateway) Plan() (*PlatformGatewayPlan, error) {
	return m.backend.plan(m)
}

// Apply provisions or updates the gateway.
func (m *PlatformAPIGateway) Apply() (*PlatformGatewayState, error) {
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

// platformRoutes parses routes from config.
func (m *PlatformAPIGateway) platformRoutes() []PlatformGatewayRoute {
	raw, ok := m.config["routes"].([]any)
	if !ok {
		return nil
	}
	var routes []PlatformGatewayRoute
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
		routes = append(routes, PlatformGatewayRoute{
			Path:      path,
			Method:    method,
			Target:    target,
			RateLimit: rateLimit,
			AuthType:  authType,
		})
	}
	return routes
}

// platformCORS parses CORS config.
func (m *PlatformAPIGateway) platformCORS() *PlatformGatewayCORSConfig {
	raw, ok := m.config["cors"].(map[string]any)
	if !ok {
		return nil
	}
	cfg := &PlatformGatewayCORSConfig{}
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

func (b *mockAPIGatewayBackend) plan(m *PlatformAPIGateway) (*PlatformGatewayPlan, error) {
	routes := m.platformRoutes()
	plan := &PlatformGatewayPlan{
		Name:   m.gatewayName(),
		Stage:  m.state.Stage,
		Routes: routes,
		CORS:   m.platformCORS(),
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

func (b *mockAPIGatewayBackend) apply(m *PlatformAPIGateway) (*PlatformGatewayState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}

	routes := m.platformRoutes()
	m.state.ID = fmt.Sprintf("mock-gw-%s", strings.ReplaceAll(m.gatewayName(), " ", "-"))
	m.state.Routes = routes
	m.state.CORS = m.platformCORS()
	m.state.Endpoint = fmt.Sprintf("https://mock.execute-api.example.com/%s", m.state.Stage)
	m.state.Status = "active"

	return m.state, nil
}

func (b *mockAPIGatewayBackend) status(m *PlatformAPIGateway) (*PlatformGatewayState, error) {
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

// ─── AWS APIGateway v2 backend ────────────────────────────────────────────────

// awsAPIGatewayBackend manages AWS API Gateway v2 (HTTP APIs) using
// aws-sdk-go-v2/service/apigatewayv2.
type awsAPIGatewayBackend struct{}

func (b *awsAPIGatewayBackend) plan(m *PlatformAPIGateway) (*PlatformGatewayPlan, error) {
	routes := m.platformRoutes()
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return &PlatformGatewayPlan{
			Name:    m.gatewayName(),
			Stage:   m.state.Stage,
			Routes:  routes,
			Changes: []string{fmt.Sprintf("create API Gateway %q with %d route(s)", m.gatewayName(), len(routes))},
		}, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("apigateway plan: AWS config: %w", err)
	}
	client := apigatewayv2.NewFromConfig(cfg)

	// Check if API already exists by name
	listOut, err := client.GetApis(context.Background(), &apigatewayv2.GetApisInput{})
	if err != nil {
		return nil, fmt.Errorf("apigateway plan: GetApis: %w", err)
	}

	for i := range listOut.Items {
		if listOut.Items[i].Name != nil && *listOut.Items[i].Name == m.gatewayName() {
			return &PlatformGatewayPlan{
				Name:    m.gatewayName(),
				Stage:   m.state.Stage,
				Routes:  routes,
				Changes: []string{fmt.Sprintf("noop: API Gateway %q already exists", m.gatewayName())},
			}, nil
		}
	}

	return &PlatformGatewayPlan{
		Name:    m.gatewayName(),
		Stage:   m.state.Stage,
		Routes:  routes,
		CORS:    m.platformCORS(),
		Changes: []string{fmt.Sprintf("create API Gateway %q with %d route(s) in stage %q", m.gatewayName(), len(routes), m.state.Stage)},
	}, nil
}

func (b *awsAPIGatewayBackend) apply(m *PlatformAPIGateway) (*PlatformGatewayState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return nil, fmt.Errorf("apigateway apply: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("apigateway apply: AWS config: %w", err)
	}
	client := apigatewayv2.NewFromConfig(cfg)

	// Check if API already exists
	apiID := m.state.ID
	if apiID == "" {
		listOut, _ := client.GetApis(context.Background(), &apigatewayv2.GetApisInput{})
		if listOut != nil {
			for i := range listOut.Items {
				if listOut.Items[i].Name != nil && *listOut.Items[i].Name == m.gatewayName() && listOut.Items[i].ApiId != nil {
					apiID = *listOut.Items[i].ApiId
					break
				}
			}
		}
	}

	if apiID == "" {
		createInput := &apigatewayv2.CreateApiInput{
			Name:         aws.String(m.gatewayName()),
			ProtocolType: apigwtypes.ProtocolTypeHttp,
		}
		if cors := m.platformCORS(); cors != nil {
			createInput.CorsConfiguration = &apigwtypes.Cors{
				AllowOrigins:  cors.AllowOrigins,
				AllowMethods:  cors.AllowMethods,
				AllowHeaders:  cors.AllowHeaders,
			}
		}
		apiOut, err := client.CreateApi(context.Background(), createInput)
		if err != nil {
			return nil, fmt.Errorf("apigateway apply: CreateApi: %w", err)
		}
		if apiOut.ApiId != nil {
			apiID = *apiOut.ApiId
		}
		if apiOut.ApiEndpoint != nil {
			m.state.Endpoint = *apiOut.ApiEndpoint
		}
	}
	m.state.ID = apiID

	// Create stage
	_, _ = client.CreateStage(context.Background(), &apigatewayv2.CreateStageInput{
		ApiId:      aws.String(apiID),
		StageName:  aws.String(m.state.Stage),
		AutoDeploy: aws.Bool(true),
	})

	// Create routes and integrations
	routes := m.platformRoutes()
	for _, route := range routes {
		// Create integration for the route target
		integOut, err := client.CreateIntegration(context.Background(), &apigatewayv2.CreateIntegrationInput{
			ApiId:             aws.String(apiID),
			IntegrationType:   apigwtypes.IntegrationTypeHttpProxy,
			IntegrationUri:    aws.String(route.Target),
			IntegrationMethod: aws.String(route.Method),
			PayloadFormatVersion: aws.String("1.0"),
		})
		if err != nil {
			continue
		}

		integID := ""
		if integOut.IntegrationId != nil {
			integID = *integOut.IntegrationId
		}

		routeKey := fmt.Sprintf("%s %s", strings.ToUpper(route.Method), route.Path)
		_, _ = client.CreateRoute(context.Background(), &apigatewayv2.CreateRouteInput{
			ApiId:    aws.String(apiID),
			RouteKey: aws.String(routeKey),
			Target:   optString(fmt.Sprintf("integrations/%s", integID)),
		})
	}

	m.state.Routes = routes
	m.state.CORS = m.platformCORS()
	m.state.Status = "active"
	if m.state.Endpoint == "" {
		m.state.Endpoint = fmt.Sprintf("https://%s.execute-api.amazonaws.com/%s", apiID, m.state.Stage)
	}

	return m.state, nil
}

func (b *awsAPIGatewayBackend) status(m *PlatformAPIGateway) (*PlatformGatewayState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return m.state, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return m.state, fmt.Errorf("apigateway status: AWS config: %w", err)
	}
	client := apigatewayv2.NewFromConfig(cfg)

	if m.state.ID == "" {
		m.state.Status = "not-found"
		return m.state, nil
	}

	out, getErr := client.GetApi(context.Background(), &apigatewayv2.GetApiInput{
		ApiId: aws.String(m.state.ID),
	})
	if getErr == nil {
		if out.ApiEndpoint != nil {
			m.state.Endpoint = *out.ApiEndpoint
		}
		m.state.Status = "active"
	} else {
		m.state.Status = "not-found"
	}
	return m.state, nil
}

func (b *awsAPIGatewayBackend) destroy(m *PlatformAPIGateway) error {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return fmt.Errorf("apigateway destroy: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return fmt.Errorf("apigateway destroy: AWS config: %w", err)
	}
	client := apigatewayv2.NewFromConfig(cfg)

	if m.state.ID == "" {
		return nil
	}

	_, err = client.DeleteApi(context.Background(), &apigatewayv2.DeleteApiInput{
		ApiId: aws.String(m.state.ID),
	})
	if err != nil {
		return fmt.Errorf("apigateway destroy: DeleteApi: %w", err)
	}

	m.state.Status = "deleted"
	m.state.ID = ""
	m.state.Endpoint = ""
	return nil
}
