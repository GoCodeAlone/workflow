package http

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// wiringHooks returns all post-init wiring functions for HTTP-related cross-module integrations.
func wiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "http-auth-provider-wiring",
			Priority: 90, // Run before static file server registration
			Hook:     wireAuthProviders,
		},
		{
			Name:     "http-static-fileserver-registration",
			Priority: 50,
			Hook:     wireStaticFileServers,
		},
		{
			Name:     "http-health-endpoint-registration",
			Priority: 40,
			Hook:     wireHealthEndpoints,
		},
		{
			Name:     "http-metrics-endpoint-registration",
			Priority: 40,
			Hook:     wireMetricsEndpoint,
		},
		{
			Name:     "http-log-endpoint-registration",
			Priority: 40,
			Hook:     wireLogEndpoint,
		},
		{
			Name:     "http-openapi-endpoint-registration",
			Priority: 40,
			Hook:     wireOpenAPIEndpoints,
		},
	}
}

// wireAuthProviders connects AuthProviders to AuthMiddleware instances post-init.
func wireAuthProviders(app modular.Application, _ *config.WorkflowConfig) error {
	var authMiddlewares []*module.AuthMiddleware
	var authProviders []module.AuthProvider
	for _, svc := range app.SvcRegistry() {
		if am, ok := svc.(*module.AuthMiddleware); ok {
			authMiddlewares = append(authMiddlewares, am)
		}
		if ap, ok := svc.(module.AuthProvider); ok {
			authProviders = append(authProviders, ap)
		}
	}
	for _, am := range authMiddlewares {
		for _, ap := range authProviders {
			am.RegisterProvider(ap)
		}
	}
	return nil
}

// wireStaticFileServers registers static file servers as catch-all routes on their associated routers.
func wireStaticFileServers(app modular.Application, cfg *config.WorkflowConfig) error {
	// Build lookup maps from config for intelligent wiring.
	routerNames := make(map[string]bool)
	serverToRouter := make(map[string]string)
	sfsDeps := make(map[string][]string)
	for _, modCfg := range cfg.Modules {
		switch modCfg.Type {
		case "http.router":
			routerNames[modCfg.Name] = true
			for _, dep := range modCfg.DependsOn {
				serverToRouter[dep] = modCfg.Name
			}
		case "static.fileserver":
			sfsDeps[modCfg.Name] = modCfg.DependsOn
		}
	}

	for _, svc := range app.SvcRegistry() {
		sfs, ok := svc.(*module.StaticFileServer)
		if !ok {
			continue
		}

		var targetRouter module.HTTPRouter
		targetName := sfs.RouterName()

		// 1) Explicit router name from config
		if targetName != "" {
			for svcName, routerSvc := range app.SvcRegistry() {
				if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == targetName {
					targetRouter = router
					break
				}
			}
		}

		// 2) Check dependsOn for a direct router reference
		if targetRouter == nil {
			for _, dep := range sfsDeps[sfs.Name()] {
				if routerNames[dep] {
					for svcName, routerSvc := range app.SvcRegistry() {
						if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == dep {
							targetRouter = router
							targetName = dep
							break
						}
					}
					if targetRouter != nil {
						break
					}
				}
			}
		}

		// 3) Check dependsOn for a server reference, then find that server's router
		if targetRouter == nil {
			for _, dep := range sfsDeps[sfs.Name()] {
				if rName, ok := serverToRouter[dep]; ok {
					for svcName, routerSvc := range app.SvcRegistry() {
						if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == rName {
							targetRouter = router
							targetName = rName
							break
						}
					}
					if targetRouter != nil {
						break
					}
				}
			}
		}

		// 4) Fall back to first available router
		if targetRouter == nil {
			for _, routerSvc := range app.SvcRegistry() {
				if router, ok := routerSvc.(module.HTTPRouter); ok {
					targetRouter = router
					break
				}
			}
		}

		if targetRouter != nil {
			targetRouter.AddRoute("GET", sfs.Prefix()+"{path...}", sfs)
			_ = fmt.Sprintf("Registered static file server %s on router %s at prefix: %s", sfs.Name(), targetName, sfs.Prefix())
		}
	}

	return nil
}

// wireHealthEndpoints registers health checker endpoints on the first available router.
func wireHealthEndpoints(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		hc, ok := svc.(*module.HealthChecker)
		if !ok {
			continue
		}

		// Register persistence health checks if any persistence stores exist
		for svcName, innerSvc := range app.SvcRegistry() {
			if ps, ok := innerSvc.(*module.PersistenceStore); ok {
				checkName := "persistence." + svcName
				psRef := ps // capture for closure
				hc.RegisterCheck(checkName, func(ctx context.Context) module.HealthCheckResult {
					if err := psRef.Ping(ctx); err != nil {
						return module.HealthCheckResult{Status: "degraded", Message: "database unreachable: " + err.Error()}
					}
					return module.HealthCheckResult{Status: "healthy", Message: "database connected"}
				})
			}
		}

		// Auto-discover any HealthCheckable services
		hc.DiscoverHealthCheckables()

		for _, routerSvc := range app.SvcRegistry() {
			if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
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
	}
	return nil
}

// wireMetricsEndpoint registers the metrics collector endpoint on the first available router.
func wireMetricsEndpoint(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		mc, ok := svc.(*module.MetricsCollector)
		if !ok {
			continue
		}
		metricsPath := mc.MetricsPath()
		for _, routerSvc := range app.SvcRegistry() {
			if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
				if !router.HasRoute("GET", metricsPath) {
					router.AddRoute("GET", metricsPath, &module.MetricsHTTPHandler{Handler: mc.Handler()})
				}
				break
			}
		}
	}
	return nil
}

// wireLogEndpoint registers the log collector endpoint on the first available router.
func wireLogEndpoint(app modular.Application, _ *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		lc, ok := svc.(*module.LogCollector)
		if !ok {
			continue
		}
		for _, routerSvc := range app.SvcRegistry() {
			if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
				if !router.HasRoute("GET", "/logs") {
					router.AddRoute("GET", "/logs", &module.LogHTTPHandler{Handler: lc.LogHandler()})
				}
				break
			}
		}
	}
	return nil
}

// wireOpenAPIEndpoints registers OpenAPI spec endpoints on the first available router.
func wireOpenAPIEndpoints(app modular.Application, cfg *config.WorkflowConfig) error {
	for _, svc := range app.SvcRegistry() {
		gen, ok := svc.(*module.OpenAPIGenerator)
		if !ok {
			continue
		}
		gen.BuildSpec(cfg.Workflows)

		for _, routerSvc := range app.SvcRegistry() {
			if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
				if !router.HasRoute("GET", "/api/openapi.json") {
					router.AddRoute("GET", "/api/openapi.json", &module.OpenAPIHTTPHandler{Handler: gen.ServeJSON})
					router.AddRoute("GET", "/api/openapi.yaml", &module.OpenAPIHTTPHandler{Handler: gen.ServeYAML})
				}
				break
			}
		}
	}
	return nil
}
