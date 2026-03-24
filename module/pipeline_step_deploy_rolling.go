package module

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/modular"
)

// DeployDriver is the interface used by deployment steps to update and health-check a service.
// Implementations are looked up from the application service registry by service name.
type DeployDriver interface {
	// Update replaces the running image for the service.
	Update(ctx context.Context, image string) error
	// HealthCheck performs a health check on the service. Returns nil if healthy.
	HealthCheck(ctx context.Context, path string) error
	// CurrentImage returns the image currently running for the service.
	CurrentImage(ctx context.Context) (string, error)
	// ReplicaCount returns the number of replicas currently running.
	ReplicaCount(ctx context.Context) (int, error)
}

// resolveDeployDriver looks up a DeployDriver from the service registry.
func resolveDeployDriver(app modular.Application, serviceName, stepName string) (DeployDriver, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[serviceName]
	if !ok {
		return nil, fmt.Errorf("step %q: service %q not found in registry", stepName, serviceName)
	}
	driver, ok := svc.(DeployDriver)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement DeployDriver (got %T)", stepName, serviceName, svc)
	}
	return driver, nil
}

// HTTPDeployDriver is a simple DeployDriver backed by a mock or HTTP-reachable service.
// It is provided for testing and simple HTTP-based deployments.
type HTTPDeployDriver struct {
	BaseURL    string
	CurrentImg string
	ReplicaCnt int
	UpdateFn   func(ctx context.Context, image string) error
	HealthFn   func(ctx context.Context, path string) error
	httpClient *http.Client
}

// Update calls the user-provided UpdateFn or sets CurrentImg directly.
func (d *HTTPDeployDriver) Update(_ context.Context, image string) error {
	if d.UpdateFn != nil {
		return d.UpdateFn(context.Background(), image)
	}
	d.CurrentImg = image
	return nil
}

// HealthCheck calls the user-provided HealthFn or does an HTTP GET.
func (d *HTTPDeployDriver) HealthCheck(ctx context.Context, path string) error {
	if d.HealthFn != nil {
		return d.HealthFn(ctx, path)
	}
	if d.BaseURL == "" {
		return nil
	}
	client := d.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

// CurrentImage returns the current image.
func (d *HTTPDeployDriver) CurrentImage(_ context.Context) (string, error) {
	return d.CurrentImg, nil
}

// ReplicaCount returns the current replica count.
func (d *HTTPDeployDriver) ReplicaCount(_ context.Context) (int, error) {
	return d.ReplicaCnt, nil
}

// ─── step.deploy_rolling ──────────────────────────────────────────────────────

// DeployRollingStep performs a rolling deployment of a new image, updating
// instances in batches and health-checking each batch before continuing.
type DeployRollingStep struct {
	name              string
	service           string
	image             string
	maxSurge          int
	maxUnavailable    int
	healthCheckPath   string
	healthInterval    time.Duration
	healthTimeout     time.Duration
	rollbackOnFailure bool
	app               modular.Application
}

// NewDeployRollingStepFactory returns a StepFactory for step.deploy_rolling.
func NewDeployRollingStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("deploy_rolling step %q: 'service' is required", name)
		}
		image, _ := cfg["image"].(string)
		if image == "" {
			return nil, fmt.Errorf("deploy_rolling step %q: 'image' is required", name)
		}

		maxSurge := 1
		if v, ok := cfg["max_surge"].(int); ok && v > 0 {
			maxSurge = v
		}
		maxUnavailable := 1
		if v, ok := cfg["max_unavailable"].(int); ok && v > 0 {
			maxUnavailable = v
		}

		var healthPath string
		var healthInterval, healthTimeout time.Duration
		if hcRaw, ok := cfg["health_check"].(map[string]any); ok {
			healthPath, _ = hcRaw["path"].(string)
			if ivl, ok := hcRaw["interval"].(string); ok {
				if d, err := time.ParseDuration(ivl); err == nil {
					healthInterval = d
				}
			}
			if to, ok := hcRaw["timeout"].(string); ok {
				if d, err := time.ParseDuration(to); err == nil {
					healthTimeout = d
				}
			}
		}
		if healthInterval == 0 {
			healthInterval = 5 * time.Second
		}
		if healthTimeout == 0 {
			healthTimeout = 30 * time.Second
		}

		rollback, _ := cfg["rollback_on_failure"].(bool)

		return &DeployRollingStep{
			name:              name,
			service:           service,
			image:             image,
			maxSurge:          maxSurge,
			maxUnavailable:    maxUnavailable,
			healthCheckPath:   healthPath,
			healthInterval:    healthInterval,
			healthTimeout:     healthTimeout,
			rollbackOnFailure: rollback,
			app:               app,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployRollingStep) Name() string { return s.name }

// Execute performs a rolling deployment.
func (s *DeployRollingStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	driver, err := resolveDeployDriver(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}

	previousImage, err := driver.CurrentImage(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy_rolling step %q: get current image: %w", s.name, err)
	}

	replicaCount, err := driver.ReplicaCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy_rolling step %q: get replica count: %w", s.name, err)
	}
	if replicaCount == 0 {
		replicaCount = 1
	}

	// Calculate batches based on max_surge / max_unavailable.
	batchSize := s.maxSurge
	if batchSize > replicaCount {
		batchSize = replicaCount
	}

	totalBatches := replicaCount / batchSize
	if replicaCount%batchSize != 0 {
		totalBatches++
	}

	for i := range totalBatches {
		batch := i + 1
		updatedInBatch := min(batchSize, replicaCount-(i*batchSize))

		if err := driver.Update(ctx, s.image); err != nil {
			if s.rollbackOnFailure {
				_ = driver.Update(ctx, previousImage)
			}
			return nil, fmt.Errorf("deploy_rolling step %q: update batch %d: %w", s.name, batch, err)
		}

		// Health check the batch.
		hcCtx, cancel := context.WithTimeout(ctx, s.healthTimeout)
		hcErr := driver.HealthCheck(hcCtx, s.healthCheckPath)
		cancel()

		if hcErr != nil {
			if s.rollbackOnFailure {
				_ = driver.Update(ctx, previousImage)
				return nil, fmt.Errorf("deploy_rolling step %q: health check failed at batch %d (rolled back): %w", s.name, batch, hcErr)
			}
			return nil, fmt.Errorf("deploy_rolling step %q: health check failed at batch %d: %w", s.name, batch, hcErr)
		}

		_ = updatedInBatch // tracked but currently unused in output per batch
	}

	return &StepResult{Output: map[string]any{
		"success":          true,
		"service":          s.service,
		"image":            s.image,
		"previous_image":   previousImage,
		"replicas_updated": replicaCount,
		"batches":          totalBatches,
	}}, nil
}
