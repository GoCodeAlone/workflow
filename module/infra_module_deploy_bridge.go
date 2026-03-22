package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── Optional provider interfaces ─────────────────────────────────────────────

// DeployDriverProvider is an optional extension of interfaces.IaCProvider.
// Providers that support rolling deployments can implement this to expose
// a per-resource DeployDriver that deploy steps can use directly.
type DeployDriverProvider interface {
	// ProvideDeployDriver returns a DeployDriver for the named resource, or nil.
	ProvideDeployDriver(resourceName string) DeployDriver
}

// BlueGreenDriverProvider is an optional extension of interfaces.IaCProvider
// for providers that support blue/green deployments.
type BlueGreenDriverProvider interface {
	// ProvideBlueGreenDriver returns a BlueGreenDriver for the named resource, or nil.
	ProvideBlueGreenDriver(resourceName string) BlueGreenDriver
}

// CanaryDriverProvider is an optional extension of interfaces.IaCProvider
// for providers that support canary deployments.
type CanaryDriverProvider interface {
	// ProvideCanaryDriver returns a CanaryDriver for the named resource, or nil.
	ProvideCanaryDriver(resourceName string) CanaryDriver
}

// ─── Generic adapter ──────────────────────────────────────────────────────────

// infraDeployAdapter wraps an InfraModule and exposes it as a DeployDriver by
// delegating to the underlying interfaces.ResourceDriver. This lets any
// infra.container_service (or similar) module act as the target for
// step.deploy_rolling without requiring provider-specific registration.
type infraDeployAdapter struct {
	im *InfraModule
}

// Update updates the running image by calling the resource driver's Update
// with the image injected into the resource config.
func (a *infraDeployAdapter) Update(ctx context.Context, image string) error {
	cfg := a.im.ResourceConfig()
	cfg["image"] = image
	_, err := a.im.driver.Update(ctx,
		interfaces.ResourceRef{Name: a.im.name, Type: a.im.infraType},
		interfaces.ResourceSpec{Name: a.im.name, Type: a.im.infraType, Config: cfg, Size: a.im.size, Hints: a.im.hints},
	)
	if err != nil {
		return fmt.Errorf("infra deploy %q: update image: %w", a.im.name, err)
	}
	return nil
}

// HealthCheck delegates to the resource driver's HealthCheck and translates
// the result to the DeployDriver contract (nil = healthy, error = unhealthy).
// The path parameter is accepted for interface compatibility but unused.
func (a *infraDeployAdapter) HealthCheck(ctx context.Context, _ string) error {
	result, err := a.im.driver.HealthCheck(ctx, interfaces.ResourceRef{Name: a.im.name, Type: a.im.infraType})
	if err != nil {
		return fmt.Errorf("infra deploy %q: health check: %w", a.im.name, err)
	}
	if !result.Healthy {
		return fmt.Errorf("infra deploy %q: unhealthy: %s", a.im.name, result.Message)
	}
	return nil
}

// CurrentImage reads the resource state and returns the "image" output field.
func (a *infraDeployAdapter) CurrentImage(ctx context.Context) (string, error) {
	out, err := a.im.driver.Read(ctx, interfaces.ResourceRef{Name: a.im.name, Type: a.im.infraType})
	if err != nil {
		return "", fmt.Errorf("infra deploy %q: read current image: %w", a.im.name, err)
	}
	img, _ := out.Outputs["image"].(string)
	return img, nil
}

// ReplicaCount reads the resource state and returns the desired replica count
// from the "desired_count" or "replicas" output field.
func (a *infraDeployAdapter) ReplicaCount(ctx context.Context) (int, error) {
	out, err := a.im.driver.Read(ctx, interfaces.ResourceRef{Name: a.im.name, Type: a.im.infraType})
	if err != nil {
		return 0, fmt.Errorf("infra deploy %q: read replica count: %w", a.im.name, err)
	}
	if v, ok := out.Outputs["desired_count"].(int32); ok {
		return int(v), nil
	}
	if v, ok := out.Outputs["replicas"].(int); ok {
		return v, nil
	}
	return 1, nil
}

var _ DeployDriver = (*infraDeployAdapter)(nil)
