// Package admin provides an EnginePlugin that serves the admin dashboard UI
// and loads admin config routes. It encapsulates admin concerns — static file
// serving, config merging, and service delegate wiring — as a self-contained
// plugin rather than hard-wired logic in cmd/server/main.go.
package admin

import (
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/admin"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides admin-specific module types and wiring hooks:
//   - admin.dashboard  — serves the admin UI static files via a static.fileserver
//   - admin.config_loader — loads admin/config.yaml and merges routes into the engine
type Plugin struct {
	plugin.BaseEnginePlugin

	// UIDir overrides the static file root for the admin dashboard.
	// Empty string means use the default from admin/config.yaml.
	UIDir string

	// Logger for wiring hook diagnostics.
	Logger *slog.Logger
}

// New creates a new admin plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "admin",
				PluginVersion:     "1.0.0",
				PluginDescription: "Admin dashboard UI and config-driven admin routes",
			},
			Manifest: plugin.PluginManifest{
				Name:        "admin",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Admin dashboard UI and config-driven admin routes",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"admin.dashboard",
					"admin.config_loader",
				},
				WiringHooks: []string{
					"admin-config-merge",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "admin-ui", Role: "provider", Priority: 10},
					{Name: "admin-config", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// WithUIDir sets the static file root for the admin dashboard.
func (p *Plugin) WithUIDir(dir string) *Plugin {
	p.UIDir = dir
	return p
}

// WithLogger sets the logger for wiring hook diagnostics.
func (p *Plugin) WithLogger(logger *slog.Logger) *Plugin {
	p.Logger = logger
	return p
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "admin-ui",
			Description: "Serves the admin dashboard UI as static files with SPA fallback",
		},
		{
			Name:        "admin-config",
			Description: "Loads and merges admin config routes into the workflow engine",
		},
	}
}

// ModuleFactories returns factories for admin module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"admin.dashboard": func(name string, cfg map[string]any) modular.Module {
			root := ""
			if r, ok := cfg["root"].(string); ok {
				root = r
			}
			root = config.ResolvePathInConfig(cfg, root)
			if p.UIDir != "" {
				root = p.UIDir
			}
			prefix := "/"
			if pfx, ok := cfg["prefix"].(string); ok {
				prefix = pfx
			}
			// SPA fallback is enabled by default for the admin dashboard UI.
			spaFallback := true
			if sf, ok := cfg["spaFallback"].(bool); ok {
				spaFallback = sf
			}
			var opts []module.StaticFileServerOption
			if spaFallback {
				opts = append(opts, module.WithSPAFallback())
			}
			if cma, ok := cfg["cacheMaxAge"].(int); ok {
				opts = append(opts, module.WithCacheMaxAge(cma))
			} else if cma, ok := cfg["cacheMaxAge"].(float64); ok {
				opts = append(opts, module.WithCacheMaxAge(int(cma)))
			}
			sfs := module.NewStaticFileServer(name, root, prefix, opts...)
			if routerName, ok := cfg["router"].(string); ok && routerName != "" {
				sfs.SetRouterName(routerName)
			}
			return sfs
		},
		"admin.config_loader": func(name string, _ map[string]any) modular.Module {
			return newConfigLoaderModule(name)
		},
	}
}

// ModuleSchemas returns UI schema definitions for admin module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "admin.dashboard",
			Label:       "Admin Dashboard",
			Category:    "admin",
			Description: "Serves the admin UI static files with SPA fallback",
			Inputs:      []schema.ServiceIODef{{Name: "http_request", Type: "http.Request", Description: "HTTP request for admin UI"}},
			Outputs:     []schema.ServiceIODef{{Name: "http_response", Type: "http.Response", Description: "Static file or SPA fallback"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "root", Label: "UI Root Directory", Type: schema.FieldTypeString, Description: "Path to admin UI static assets directory", Placeholder: "ui/dist"},
			},
		},
		{
			Type:        "admin.config_loader",
			Label:       "Admin Config Loader",
			Category:    "admin",
			Description: "Loads the embedded admin config and merges routes into the engine",
			Inputs:      []schema.ServiceIODef{},
			Outputs:     []schema.ServiceIODef{{Name: "config", Type: "WorkflowConfig", Description: "Merged admin configuration"}},
		},
	}
}

// WiringHooks returns post-init wiring functions that merge admin config
// into the running engine.
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "admin-config-merge",
			Priority: 100, // run early so admin routes are available
			Hook: func(_ modular.Application, cfg *config.WorkflowConfig) error {
				return p.mergeAdminConfig(cfg)
			},
		},
	}
}

// mergeAdminConfig loads the embedded admin config and merges it into the
// primary config. If UIDir is set, the static fileserver root is overridden.
func (p *Plugin) mergeAdminConfig(cfg *config.WorkflowConfig) error {
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Skip merge if admin modules are already present
	for _, m := range cfg.Modules {
		if m.Name == "admin-server" {
			logger.Info("Config already contains admin modules, skipping merge")
			if p.UIDir != "" {
				injectUIRoot(cfg, p.UIDir)
				logger.Info("Admin UI root overridden", "uiDir", p.UIDir)
			}
			return nil
		}
	}

	adminCfg, err := admin.LoadConfig()
	if err != nil {
		return fmt.Errorf("admin plugin: load config: %w", err)
	}

	if p.UIDir != "" {
		injectUIRoot(adminCfg, p.UIDir)
		logger.Info("Admin UI root overridden", "uiDir", p.UIDir)
	}

	admin.MergeInto(cfg, adminCfg)
	logger.Info("Admin UI enabled via admin plugin")
	return nil
}

// injectUIRoot updates every static.fileserver and admin.dashboard module
// config in cfg to serve from the given root directory.
func injectUIRoot(cfg *config.WorkflowConfig, uiRoot string) {
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "static.fileserver" || cfg.Modules[i].Type == "admin.dashboard" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = make(map[string]any)
			}
			cfg.Modules[i].Config["root"] = uiRoot
		}
	}
}

// configLoaderModule is a minimal modular.Module that represents the admin
// config loading concern. It is used as a dependency anchor — other modules
// can depend on it to ensure admin config is loaded first.
type configLoaderModule struct {
	name string
}

func newConfigLoaderModule(name string) *configLoaderModule {
	return &configLoaderModule{name: name}
}

func (m *configLoaderModule) Name() string                                  { return m.name }
func (m *configLoaderModule) Dependencies() []string                        { return nil }
func (m *configLoaderModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (m *configLoaderModule) RequiresServices() []modular.ServiceDependency { return nil }
func (m *configLoaderModule) RegisterConfig(_ modular.Application) error    { return nil }
func (m *configLoaderModule) Init(_ modular.Application) error              { return nil }
func (m *configLoaderModule) Start(_ modular.Application) error             { return nil }
func (m *configLoaderModule) Stop(_ modular.Application) error              { return nil }
