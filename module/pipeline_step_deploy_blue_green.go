package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

// BlueGreenDriver extends DeployDriver with the ability to manage two environments
// (blue/green) and switch traffic between them.
type BlueGreenDriver interface {
	DeployDriver
	// CreateGreen creates a new "green" environment with the given image.
	CreateGreen(ctx context.Context, image string) error
	// SwitchTraffic routes all traffic to the green environment.
	SwitchTraffic(ctx context.Context) error
	// DestroyBlue tears down the old "blue" environment.
	DestroyBlue(ctx context.Context) error
	// GreenEndpoint returns the URL/address of the green environment.
	GreenEndpoint(ctx context.Context) (string, error)
}

// resolveBlueGreenDriver looks up a BlueGreenDriver from the service registry.
func resolveBlueGreenDriver(app modular.Application, serviceName, stepName string) (BlueGreenDriver, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[serviceName]
	if !ok {
		return nil, fmt.Errorf("step %q: service %q not found in registry", stepName, serviceName)
	}
	driver, ok := svc.(BlueGreenDriver)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement BlueGreenDriver (got %T)", stepName, serviceName, svc)
	}
	return driver, nil
}

// ─── step.deploy_blue_green ───────────────────────────────────────────────────

// DeployBlueGreenStep creates a new "green" environment, validates it via health
// checks, switches traffic, and destroys the old "blue" environment.
type DeployBlueGreenStep struct {
	name            string
	service         string
	image           string
	healthCheckPath string
	healthTimeout   time.Duration
	trafficSwitch   string // "dns" or "lb"
	app             modular.Application
}

// NewDeployBlueGreenStepFactory returns a StepFactory for step.deploy_blue_green.
func NewDeployBlueGreenStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("deploy_blue_green step %q: 'service' is required", name)
		}
		image, _ := cfg["image"].(string)
		if image == "" {
			return nil, fmt.Errorf("deploy_blue_green step %q: 'image' is required", name)
		}

		var healthPath string
		var healthTimeout time.Duration
		if hcRaw, ok := cfg["health_check"].(map[string]any); ok {
			healthPath, _ = hcRaw["path"].(string)
			if to, ok := hcRaw["timeout"].(string); ok {
				if d, err := time.ParseDuration(to); err == nil {
					healthTimeout = d
				}
			}
		}
		if healthTimeout == 0 {
			healthTimeout = 30 * time.Second
		}

		trafficSwitch, _ := cfg["traffic_switch"].(string)
		if trafficSwitch == "" {
			trafficSwitch = "lb"
		}

		return &DeployBlueGreenStep{
			name:            name,
			service:         service,
			image:           image,
			healthCheckPath: healthPath,
			healthTimeout:   healthTimeout,
			trafficSwitch:   trafficSwitch,
			app:             app,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployBlueGreenStep) Name() string { return s.name }

// Execute performs a blue/green deployment.
func (s *DeployBlueGreenStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	driver, err := resolveBlueGreenDriver(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}

	// 1. Create green environment.
	if err := driver.CreateGreen(ctx, s.image); err != nil {
		return nil, fmt.Errorf("deploy_blue_green step %q: create green: %w", s.name, err)
	}

	// 2. Health check green.
	hcCtx, cancel := context.WithTimeout(ctx, s.healthTimeout)
	hcErr := driver.HealthCheck(hcCtx, s.healthCheckPath)
	cancel()
	if hcErr != nil {
		return nil, fmt.Errorf("deploy_blue_green step %q: health check on green failed: %w", s.name, hcErr)
	}

	// 3. Get green endpoint before switching traffic.
	greenEndpoint, err := driver.GreenEndpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy_blue_green step %q: get green endpoint: %w", s.name, err)
	}

	// 4. Switch traffic.
	cutoverTime := nowUTC()
	if err := driver.SwitchTraffic(ctx); err != nil {
		return nil, fmt.Errorf("deploy_blue_green step %q: switch traffic: %w", s.name, err)
	}

	// 5. Destroy blue.
	if err := driver.DestroyBlue(ctx); err != nil {
		return nil, fmt.Errorf("deploy_blue_green step %q: destroy blue: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"success":        true,
		"service":        s.service,
		"image":          s.image,
		"green_endpoint": greenEndpoint,
		"traffic_switch": s.trafficSwitch,
		"cutover_time":   cutoverTime,
	}}, nil
}
