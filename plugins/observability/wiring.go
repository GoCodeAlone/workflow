package observability

import (
	"context"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// wiringHooks returns post-init wiring functions that connect observability
// modules to the HTTP router.
func wiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			// Run at priority 100 (highest) so OTEL wraps all other middleware
			// and every request — including health, metrics, and pipeline routes
			// registered later — is captured in a trace span.
			Name:     "observability.otel-middleware",
			Priority: 100,
			Hook:     wireOTelMiddleware,
		},
		{
			Name:     "observability.health-endpoints",
			Priority: 50,
			Hook:     wireHealthEndpoints,
		},
		{
			Name:     "observability.metrics-endpoint",
			Priority: 50,
			Hook:     wireMetricsEndpoint,
		},
		{
			Name:     "observability.log-endpoint",
			Priority: 50,
			Hook:     wireLogEndpoint,
		},
		{
			Name:     "observability.openapi-endpoints",
			Priority: 40, // run after health/metrics so routes are stable
			Hook:     wireOpenAPIEndpoints,
		},
	}
}

// wireHealthEndpoints registers health check endpoints on any available router,
// discovers HealthCheckable services, and registers persistence health checks.
func wireHealthEndpoints(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		hc, ok := svc.(*module.HealthChecker)
		if !ok {
			continue
		}

		// Register persistence health checks
		for svcName, innerSvc := range app.SvcRegistry() {
			ps, ok := innerSvc.(*module.PersistenceStore)
			if !ok {
				continue
			}
			checkName := "persistence." + svcName
			psRef := ps
			hc.RegisterCheck(checkName, func(ctx context.Context) module.HealthCheckResult {
				if err := psRef.Ping(ctx); err != nil {
					return module.HealthCheckResult{Status: "degraded", Message: "database unreachable: " + err.Error()}
				}
				return module.HealthCheckResult{Status: "healthy", Message: "database connected"}
			})
		}

		// Auto-discover HealthCheckable services
		hc.DiscoverHealthCheckables()

		// Mark the health checker as started so /readyz returns 200.
		// Wiring hooks run after all modules are initialized and services
		// registered, so the application is ready to serve traffic.
		hc.SetStarted(true)

		// Wire endpoints onto the first available StandardHTTPRouter
		for _, routerSvc := range app.SvcRegistry() {
			router, ok := routerSvc.(*module.StandardHTTPRouter)
			if !ok {
				continue
			}
			healthPath := hc.HealthPath()
			readyPath := hc.ReadyPath()
			livePath := hc.LivePath()
			if !router.HasRoute("GET", healthPath) {
				router.AddRoute("GET", healthPath, &module.HealthHTTPHandler{Handler: hc.HealthHandler()})
				router.AddRoute("GET", readyPath, &module.HealthHTTPHandler{Handler: hc.ReadyHandler()})
				router.AddRoute("GET", livePath, &module.HealthHTTPHandler{Handler: hc.LiveHandler()})
			}
			break
		}
	}
	return nil
}

// wireMetricsEndpoint registers the metrics endpoint on any available router.
func wireMetricsEndpoint(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		mc, ok := svc.(*module.MetricsCollector)
		if !ok {
			continue
		}
		metricsPath := mc.MetricsPath()
		for _, routerSvc := range app.SvcRegistry() {
			router, ok := routerSvc.(*module.StandardHTTPRouter)
			if !ok {
				continue
			}
			if !router.HasRoute("GET", metricsPath) {
				router.AddRoute("GET", metricsPath, &module.MetricsHTTPHandler{Handler: mc.Handler()})
			}
			break
		}
	}
	return nil
}

// wireLogEndpoint registers the log collector endpoint on any available router.
func wireLogEndpoint(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		lc, ok := svc.(*module.LogCollector)
		if !ok {
			continue
		}
		for _, routerSvc := range app.SvcRegistry() {
			router, ok := routerSvc.(*module.StandardHTTPRouter)
			if !ok {
				continue
			}
			if !router.HasRoute("GET", "/logs") {
				router.AddRoute("GET", "/logs", &module.LogHTTPHandler{Handler: lc.LogHandler()})
			}
			break
		}
	}
	return nil
}

// wireOTelMiddleware registers any OTelMiddleware instances as global middleware
// on every available StandardHTTPRouter so that all inbound HTTP requests are
// wrapped in an OpenTelemetry trace span.
func wireOTelMiddleware(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		otelMw, ok := svc.(*module.OTelMiddleware)
		if !ok {
			continue
		}
		for _, routerSvc := range app.SvcRegistry() {
			router, ok := routerSvc.(*module.StandardHTTPRouter)
			if !ok {
				continue
			}
			router.AddGlobalMiddleware(otelMw)
		}
	}
	return nil
}

// wireOpenAPIEndpoints builds OpenAPI specs and registers the JSON/YAML endpoints
// on any available router.
func wireOpenAPIEndpoints(app modular.Application, cfg *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		gen, ok := svc.(*module.OpenAPIGenerator)
		if !ok {
			continue
		}
		gen.BuildSpec(cfg.Workflows)

		for _, routerSvc := range app.SvcRegistry() {
			router, ok := routerSvc.(*module.StandardHTTPRouter)
			if !ok {
				continue
			}
			if !router.HasRoute("GET", "/api/openapi.json") {
				router.AddRoute("GET", "/api/openapi.json", &module.OpenAPIHTTPHandler{Handler: gen.ServeJSON})
				router.AddRoute("GET", "/api/openapi.yaml", &module.OpenAPIHTTPHandler{Handler: gen.ServeYAML})
			}
			break
		}
	}
	return nil
}
