package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/digitalocean/godo"
)

// DOAppState holds the current state of a DigitalOcean App Platform app.
type DOAppState struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Region      string    `json:"region"`
	Status      string    `json:"status"` // pending, deploying, running, error, deleted
	LiveURL     string    `json:"liveUrl"`
	Instances   int       `json:"instances"`
	Image       string    `json:"image"`
	DeployedAt  time.Time `json:"deployedAt"`
	DeploymentID string   `json:"deploymentId"`
}

// doAppBackend is the interface DO App Platform backends implement.
type doAppBackend interface {
	deploy(m *PlatformDOApp) (*DOAppState, error)
	status(m *PlatformDOApp) (*DOAppState, error)
	logs(m *PlatformDOApp) (string, error)
	scale(m *PlatformDOApp, instances int) (*DOAppState, error)
	destroy(m *PlatformDOApp) error
}

// PlatformDOApp manages DigitalOcean App Platform applications.
// Config:
//
//	account:   name of a cloud.account module (provider=digitalocean)
//	provider:  digitalocean | mock
//	name:      app name
//	region:    DO region slug (e.g. nyc)
//	image:     container image reference
//	instances: number of instances (default: 1)
//	http_port: container HTTP port (default: 8080)
//	envs:      environment variables map
type PlatformDOApp struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *DOAppState
	backend  doAppBackend
}

// NewPlatformDOApp creates a new PlatformDOApp module.
func NewPlatformDOApp(name string, cfg map[string]any) *PlatformDOApp {
	return &PlatformDOApp{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformDOApp) Name() string { return m.name }

// Init resolves the cloud.account service and initializes the backend.
func (m *PlatformDOApp) Init(app modular.Application) error {
	appName, _ := m.config["name"].(string)
	if appName == "" {
		appName = m.name
	}

	region, _ := m.config["region"].(string)
	if region == "" {
		region = "nyc"
	}

	image, _ := m.config["image"].(string)

	instances, _ := intFromAny(m.config["instances"])
	if instances == 0 {
		instances = 1
	}

	accountName, _ := m.config["account"].(string)
	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.do_app %q: account service %q not found", m.name, accountName)
		}
		prov, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.do_app %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = prov
		if providerType == "mock" {
			providerType = prov.Provider()
		}
	}

	m.state = &DOAppState{
		Name:      appName,
		Region:    region,
		Image:     image,
		Instances: instances,
		Status:    "pending",
	}

	switch providerType {
	case "mock":
		m.backend = &doAppMockBackend{}
	case "digitalocean":
		acc, ok := app.SvcRegistry()[accountName].(*CloudAccount)
		if !ok {
			return fmt.Errorf("platform.do_app %q: account %q is not a *CloudAccount", m.name, accountName)
		}
		client, err := acc.doClient()
		if err != nil {
			return fmt.Errorf("platform.do_app %q: %w", m.name, err)
		}
		m.backend = &doAppRealBackend{client: client}
	default:
		return fmt.Errorf("platform.do_app %q: unsupported provider %q", m.name, providerType)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformDOApp) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DO App: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil.
func (m *PlatformDOApp) RequiresServices() []modular.ServiceDependency { return nil }

// Deploy deploys the application to App Platform.
func (m *PlatformDOApp) Deploy() (*DOAppState, error) { return m.backend.deploy(m) }

// Status returns the current app deployment state.
func (m *PlatformDOApp) Status() (*DOAppState, error) { return m.backend.status(m) }

// Logs retrieves recent application logs.
func (m *PlatformDOApp) Logs() (string, error) { return m.backend.logs(m) }

// Scale sets the number of app instances.
func (m *PlatformDOApp) Scale(instances int) (*DOAppState, error) {
	return m.backend.scale(m, instances)
}

// Destroy tears down the application.
func (m *PlatformDOApp) Destroy() error { return m.backend.destroy(m) }

// envVars parses environment variable config.
func (m *PlatformDOApp) envVars() map[string]string {
	result := make(map[string]string)
	raw, ok := m.config["envs"].(map[string]any)
	if !ok {
		return result
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// httpPort returns the configured HTTP port.
func (m *PlatformDOApp) httpPort() int {
	if p, ok := intFromAny(m.config["http_port"]); ok && p > 0 {
		return p
	}
	return 8080
}

// buildAppSpec constructs a godo AppSpec from module config.
func (m *PlatformDOApp) buildAppSpec() *godo.AppSpec {
	envs := m.envVars()
	var appEnvs []*godo.AppVariableDefinition
	for k, v := range envs {
		appEnvs = append(appEnvs, &godo.AppVariableDefinition{
			Key:   k,
			Value: v,
			Scope: godo.AppVariableScope_RunTime,
		})
	}

	return &godo.AppSpec{
		Name:   m.state.Name,
		Region: m.state.Region,
		Services: []*godo.AppServiceSpec{
			{
				Name: m.state.Name,
				Image: &godo.ImageSourceSpec{
					RegistryType: godo.ImageSourceSpecRegistryType_DockerHub,
					Repository:   m.state.Image,
				},
				InstanceCount:      int64(m.state.Instances),
				InstanceSizeSlug:   "basic-xxs",
				HTTPPort:           int64(m.httpPort()),
				Envs:               appEnvs,
			},
		},
	}
}

// ─── mock backend ──────────────────────────────────────────────────────────────

type doAppMockBackend struct{}

func (b *doAppMockBackend) deploy(m *PlatformDOApp) (*DOAppState, error) {
	m.state.ID = fmt.Sprintf("mock-app-%s", m.state.Name)
	m.state.DeploymentID = fmt.Sprintf("mock-deploy-%s-%d", m.state.Name, time.Now().Unix())
	m.state.LiveURL = fmt.Sprintf("https://%s.ondigitalocean.app", m.state.Name)
	m.state.Status = "running"
	m.state.DeployedAt = time.Now()
	return m.state, nil
}

func (b *doAppMockBackend) status(m *PlatformDOApp) (*DOAppState, error) {
	return m.state, nil
}

func (b *doAppMockBackend) logs(m *PlatformDOApp) (string, error) {
	if m.state.Status == "pending" {
		return "", fmt.Errorf("do_app %q: app not deployed", m.state.Name)
	}
	return fmt.Sprintf("[mock] %s: app running on %s with %d instance(s)", m.state.Name, m.state.LiveURL, m.state.Instances), nil
}

func (b *doAppMockBackend) scale(m *PlatformDOApp, instances int) (*DOAppState, error) {
	m.state.Instances = instances
	return m.state, nil
}

func (b *doAppMockBackend) destroy(m *PlatformDOApp) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.LiveURL = ""
	return nil
}

// ─── real backend ──────────────────────────────────────────────────────────────

type doAppRealBackend struct {
	client *godo.Client
}

func (b *doAppRealBackend) deploy(m *PlatformDOApp) (*DOAppState, error) {
	spec := m.buildAppSpec()

	if m.state.ID != "" {
		// Update existing app.
		updated, _, err := b.client.Apps.Update(context.Background(), m.state.ID, &godo.AppUpdateRequest{Spec: spec})
		if err != nil {
			return nil, fmt.Errorf("do_app update: %w", err)
		}
		return doAppToState(updated), nil
	}

	// Create new app.
	created, _, err := b.client.Apps.Create(context.Background(), &godo.AppCreateRequest{Spec: spec})
	if err != nil {
		return nil, fmt.Errorf("do_app create: %w", err)
	}
	state := doAppToState(created)
	m.state.ID = state.ID
	m.state.LiveURL = state.LiveURL
	m.state.Status = state.Status
	m.state.DeployedAt = state.DeployedAt
	return m.state, nil
}

func (b *doAppRealBackend) status(m *PlatformDOApp) (*DOAppState, error) {
	if m.state.ID == "" {
		return m.state, nil
	}
	a, _, err := b.client.Apps.Get(context.Background(), m.state.ID)
	if err != nil {
		return nil, fmt.Errorf("do_app get: %w", err)
	}
	state := doAppToState(a)
	m.state.Status = state.Status
	m.state.LiveURL = state.LiveURL
	return m.state, nil
}

func (b *doAppRealBackend) logs(m *PlatformDOApp) (string, error) {
	if m.state.ID == "" || m.state.DeploymentID == "" {
		return "", fmt.Errorf("do_app: not deployed")
	}
	logInfo, _, err := b.client.Apps.GetLogs(
		context.Background(),
		m.state.ID,
		m.state.DeploymentID,
		m.state.Name,
		godo.AppLogTypeRun,
		true,
		100,
	)
	if err != nil {
		return "", fmt.Errorf("do_app logs: %w", err)
	}
	if logInfo != nil && logInfo.LiveURL != "" {
		return fmt.Sprintf("live log stream: %s", logInfo.LiveURL), nil
	}
	return "(no live log URL)", nil
}

func (b *doAppRealBackend) scale(m *PlatformDOApp, instances int) (*DOAppState, error) {
	if m.state.ID == "" {
		return nil, fmt.Errorf("do_app scale: app not deployed")
	}
	spec := m.buildAppSpec()
	if len(spec.Services) > 0 {
		spec.Services[0].InstanceCount = int64(instances)
	}
	updated, _, err := b.client.Apps.Update(context.Background(), m.state.ID, &godo.AppUpdateRequest{Spec: spec})
	if err != nil {
		return nil, fmt.Errorf("do_app scale: %w", err)
	}
	m.state.Instances = instances
	return doAppToState(updated), nil
}

func (b *doAppRealBackend) destroy(m *PlatformDOApp) error {
	if m.state.ID == "" {
		return nil
	}
	_, err := b.client.Apps.Delete(context.Background(), m.state.ID)
	if err != nil {
		return fmt.Errorf("do_app destroy: %w", err)
	}
	m.state.Status = "deleted"
	m.state.LiveURL = ""
	return nil
}

// doAppToState converts a godo.App to DOAppState.
func doAppToState(a *godo.App) *DOAppState {
	state := &DOAppState{
		ID:     a.ID,
		Status: "pending",
	}
	if a.Spec != nil {
		state.Name = a.Spec.Name
		state.Region = a.Spec.Region
		if len(a.Spec.Services) > 0 {
			state.Instances = int(a.Spec.Services[0].InstanceCount)
			if a.Spec.Services[0].Image != nil {
				state.Image = a.Spec.Services[0].Image.Repository
			}
		}
	}
	if a.LiveURL != "" {
		state.LiveURL = a.LiveURL
	}
	if a.ActiveDeployment != nil {
		state.DeploymentID = a.ActiveDeployment.ID
		state.DeployedAt = a.ActiveDeployment.CreatedAt
		switch a.ActiveDeployment.Phase {
		case godo.DeploymentPhase_Active:
			state.Status = "running"
		case godo.DeploymentPhase_Deploying, godo.DeploymentPhase_PendingDeploy,
			godo.DeploymentPhase_Building, godo.DeploymentPhase_PendingBuild:
			state.Status = "deploying"
		case godo.DeploymentPhase_Error:
			state.Status = "error"
		}
	}
	return state
}
