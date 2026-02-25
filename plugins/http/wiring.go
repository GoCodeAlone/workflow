package http

import (
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// wiringHooks returns all post-init wiring functions for HTTP-related cross-module integrations.
// Observability endpoint wiring (health, metrics, log, openapi) is handled by
// the observability plugin to avoid duplicate registrations.
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
			prefix := sfs.Prefix()
			// Ensure the wildcard segment is at a path boundary (Go net/http requirement).
			// e.g. "/ui" → "/ui/{path...}", "/" → "/{path...}"
			routePattern := strings.TrimSuffix(prefix, "/") + "/{path...}"
			targetRouter.AddRoute("GET", routePattern, sfs)
			_ = fmt.Sprintf("Registered static file server %s on router %s at prefix: %s", sfs.Name(), targetName, prefix)
		}
	}

	return nil
}
