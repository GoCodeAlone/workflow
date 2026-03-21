package module

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/modular"
)

// VerifyCheck describes a single verification check to run against a service.
type VerifyCheck struct {
	Type           string        // "http" or "metrics"
	Path           string        // HTTP path or metrics query
	ExpectedStatus int           // HTTP: expected status code (default 200)
	Threshold      float64       // Metrics: threshold value
	Window         time.Duration // Metrics: evaluation window
}

// VerifyCheckResult captures the outcome of a single check.
type VerifyCheckResult struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// MetricsProvider is an optional interface that a DeployDriver may implement
// to support step.deploy_verify metric checks.
type MetricsProvider interface {
	// QueryMetric evaluates a metrics query over the given window.
	// It returns the numeric result and nil on success, or an error on failure.
	QueryMetric(ctx context.Context, query string, window time.Duration) (float64, error)
}

// ─── step.deploy_verify ───────────────────────────────────────────────────────

// DeployVerifyStep runs a set of HTTP and/or metrics checks against a service
// to confirm it is operating correctly after a deployment.
type DeployVerifyStep struct {
	name    string
	service string
	checks  []VerifyCheck
	app     modular.Application
}

// NewDeployVerifyStepFactory returns a StepFactory for step.deploy_verify.
func NewDeployVerifyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("deploy_verify step %q: 'service' is required", name)
		}

		rawChecks, ok := cfg["checks"].([]any)
		if !ok || len(rawChecks) == 0 {
			return nil, fmt.Errorf("deploy_verify step %q: 'checks' must be a non-empty list", name)
		}

		var checks []VerifyCheck
		for i, rc := range rawChecks {
			cm, ok := rc.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("deploy_verify step %q: check %d must be a map", name, i)
			}
			vc := VerifyCheck{Type: "http", ExpectedStatus: http.StatusOK}
			if t, ok := cm["type"].(string); ok {
				vc.Type = t
			}
			vc.Path, _ = cm["path"].(string)
			if vc.Path == "" {
				vc.Path, _ = cm["query"].(string)
			}
			if es, ok := cm["expected_status"].(int); ok {
				vc.ExpectedStatus = es
			}
			if thr, ok := cm["threshold"].(float64); ok {
				vc.Threshold = thr
			}
			if win, ok := cm["window"].(string); ok {
				if d, err := time.ParseDuration(win); err == nil {
					vc.Window = d
				}
			}
			checks = append(checks, vc)
		}

		return &DeployVerifyStep{
			name:    name,
			service: service,
			checks:  checks,
			app:     app,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployVerifyStep) Name() string { return s.name }

// Execute runs all configured checks and returns a pass/fail summary.
func (s *DeployVerifyStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	driver, err := resolveDeployDriver(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}

	// Optional metrics provider.
	mp, _ := driver.(MetricsProvider)

	results := make([]VerifyCheckResult, 0, len(s.checks))
	allPassed := true

	for _, chk := range s.checks {
		res := VerifyCheckResult{Type: chk.Type, Path: chk.Path}
		var checkErr error

		switch chk.Type {
		case "http":
			checkErr = s.runHTTPCheck(ctx, driver, chk)
		case "metrics":
			if mp == nil {
				checkErr = fmt.Errorf("service does not implement MetricsProvider")
			} else {
				checkErr = s.runMetricsCheck(ctx, mp, chk)
			}
		default:
			checkErr = fmt.Errorf("unknown check type %q", chk.Type)
		}

		if checkErr != nil {
			allPassed = false
			res.Passed = false
			res.Message = checkErr.Error()
		} else {
			res.Passed = true
			res.Message = "ok"
		}
		results = append(results, res)
	}

	return &StepResult{Output: map[string]any{
		"service":     s.service,
		"all_passed":  allPassed,
		"check_count": len(results),
		"results":     results,
	}}, nil
}

func (s *DeployVerifyStep) runHTTPCheck(ctx context.Context, driver DeployDriver, chk VerifyCheck) error {
	return driver.HealthCheck(ctx, chk.Path)
}

func (s *DeployVerifyStep) runMetricsCheck(ctx context.Context, mp MetricsProvider, chk VerifyCheck) error {
	val, err := mp.QueryMetric(ctx, chk.Path, chk.Window)
	if err != nil {
		return fmt.Errorf("metric query %q failed: %w", chk.Path, err)
	}
	if chk.Threshold > 0 && val > chk.Threshold {
		return fmt.Errorf("metric %q value %.4f exceeds threshold %.4f", chk.Path, val, chk.Threshold)
	}
	return nil
}
