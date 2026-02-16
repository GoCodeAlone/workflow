package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/provider"
)

// DeployStep executes a deployment through the deploy.Executor,
// bridging pipeline execution to cloud providers via deployment strategies.
type DeployStep struct {
	name              string
	environment       string
	strategy          string
	image             string
	providerName      string
	rollbackOnFailure bool
	healthCheck       provider.HealthCheckConfig
	strategyConfig    map[string]any
}

// NewDeployStepFactory returns a StepFactory that creates DeployStep instances.
func NewDeployStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		env, _ := config["environment"].(string)
		if env == "" {
			return nil, fmt.Errorf("deploy step %q: 'environment' is required", name)
		}

		strategy, _ := config["strategy"].(string)
		if strategy == "" {
			return nil, fmt.Errorf("deploy step %q: 'strategy' is required", name)
		}
		switch strategy {
		case "rolling", "blue_green", "canary":
			// valid
		default:
			return nil, fmt.Errorf("deploy step %q: invalid strategy %q (expected rolling, blue_green, or canary)", name, strategy)
		}

		image, _ := config["image"].(string)
		if image == "" {
			return nil, fmt.Errorf("deploy step %q: 'image' is required", name)
		}

		providerName, _ := config["provider"].(string)

		rollback, _ := config["rollback_on_failure"].(bool)

		hc := provider.HealthCheckConfig{}
		if hcRaw, ok := config["health_check"].(map[string]any); ok {
			hc.Path, _ = hcRaw["path"].(string)
			if ivl, ok := hcRaw["interval"].(string); ok && ivl != "" {
				d, err := time.ParseDuration(ivl)
				if err != nil {
					return nil, fmt.Errorf("deploy step %q: invalid health_check.interval %q: %w", name, ivl, err)
				}
				hc.Interval = d
			}
			if to, ok := hcRaw["timeout"].(string); ok && to != "" {
				d, err := time.ParseDuration(to)
				if err != nil {
					return nil, fmt.Errorf("deploy step %q: invalid health_check.timeout %q: %w", name, to, err)
				}
				hc.Timeout = d
			}
			if ht, ok := hcRaw["healthy_threshold"].(int); ok {
				hc.HealthyThreshold = ht
			}
			if uht, ok := hcRaw["unhealthy_threshold"].(int); ok {
				hc.UnhealthyThreshold = uht
			}
		}

		// Collect strategy-specific config (e.g., "rolling" key with max_surge, max_unavailable)
		strategyConfig := make(map[string]any)
		if sc, ok := config[strategy].(map[string]any); ok {
			for k, v := range sc {
				strategyConfig[k] = v
			}
		}

		return &DeployStep{
			name:              name,
			environment:       env,
			strategy:          strategy,
			image:             image,
			providerName:      providerName,
			rollbackOnFailure: rollback,
			healthCheck:       hc,
			strategyConfig:    strategyConfig,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployStep) Name() string { return s.name }

// Execute builds a deploy request and delegates to the deploy.Executor.
func (s *DeployStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Look up executor from pipeline metadata or fail
	var executor *deploy.Executor
	if ex, ok := pc.Metadata["deploy_executor"]; ok {
		executor, _ = ex.(*deploy.Executor)
	}
	if executor == nil {
		return nil, fmt.Errorf("deploy step %q: deploy executor not found in pipeline context", s.name)
	}

	// Merge strategy-specific config into the request config
	reqConfig := make(map[string]any)
	for k, v := range s.strategyConfig {
		reqConfig[k] = v
	}
	reqConfig["rollback_on_failure"] = s.rollbackOnFailure

	req := provider.DeployRequest{
		Environment: s.environment,
		Strategy:    s.strategy,
		Image:       s.image,
		Config:      reqConfig,
		HealthCheck: s.healthCheck,
	}

	// Determine which provider to use
	provName := s.providerName
	if provName == "" {
		if pn, ok := pc.Current["provider"].(string); ok {
			provName = pn
		}
	}
	if provName == "" {
		return nil, fmt.Errorf("deploy step %q: no provider specified", s.name)
	}

	// Verify provider exists before deploying
	if _, ok := executor.GetProvider(provName); !ok {
		return nil, fmt.Errorf("deploy step %q: unknown provider %q", s.name, provName)
	}

	result, err := executor.Deploy(ctx, provName, req)
	if err != nil {
		return nil, fmt.Errorf("deploy step %q: deployment failed: %w", s.name, err)
	}

	output := map[string]any{
		"deploy_id":   result.DeployID,
		"status":      result.Status,
		"message":     result.Message,
		"environment": s.environment,
		"strategy":    s.strategy,
		"provider":    provName,
	}

	return &StepResult{Output: output}, nil
}
