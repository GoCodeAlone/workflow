// Package modularcompat provides a plugin that registers CrisisTextLine/modular
// framework module adapters: scheduler.modular, cache.modular, chimux.router,
// httpclient.modular, httpserver.modular, jsonschema.modular, logmasker.modular,
// letsencrypt.modular.
package modularcompat

import (
	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/cache"
	"github.com/CrisisTextLine/modular/modules/chimux"
	"github.com/CrisisTextLine/modular/modules/httpclient"
	"github.com/CrisisTextLine/modular/modules/httpserver"
	"github.com/CrisisTextLine/modular/modules/jsonschema"
	"github.com/CrisisTextLine/modular/modules/letsencrypt"
	"github.com/CrisisTextLine/modular/modules/logmasker"
	"github.com/CrisisTextLine/modular/modules/scheduler"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers modular framework compatibility module factories.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new modular compatibility plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "modular-compat",
				PluginVersion:     "1.0.0",
				PluginDescription: "CrisisTextLine/modular framework compatibility modules (scheduler, cache, chimux, httpclient, httpserver, jsonschema, logmasker, letsencrypt)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "modular-compat",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "CrisisTextLine/modular framework compatibility modules (scheduler, cache, chimux, httpclient, httpserver, jsonschema, logmasker, letsencrypt)",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"cache.modular",
					"chimux.router",
					"httpclient.modular",
					"httpserver.modular",
					"jsonschema.modular",
					"letsencrypt.modular",
					"logmasker.modular",
					"scheduler.modular",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "scheduler", Role: "provider", Priority: 30},
					{Name: "cache", Role: "provider", Priority: 30},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "scheduler",
			Description: "Job scheduling via CrisisTextLine/modular scheduler module",
		},
		{
			Name:        "cache",
			Description: "Caching via CrisisTextLine/modular cache module",
		},
	}
}

// ModuleFactories returns module factories that delegate to the modular framework modules.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"cache.modular": func(_ string, _ map[string]any) modular.Module {
			return cache.NewModule()
		},
		"chimux.router": func(_ string, _ map[string]any) modular.Module {
			return chimux.NewChiMuxModule()
		},
		"httpclient.modular": func(_ string, _ map[string]any) modular.Module {
			return httpclient.NewHTTPClientModule()
		},
		"httpserver.modular": func(_ string, _ map[string]any) modular.Module {
			return httpserver.NewHTTPServerModule()
		},
		"jsonschema.modular": func(_ string, _ map[string]any) modular.Module {
			return jsonschema.NewModule()
		},
		"letsencrypt.modular": func(_ string, cfg map[string]any) modular.Module {
			leCfg := &letsencrypt.LetsEncryptConfig{}
			if email, ok := cfg["email"].(string); ok {
				leCfg.Email = email
			}
			if storagePath, ok := cfg["storage_path"].(string); ok {
				leCfg.StoragePath = storagePath
			}
			if useStaging, ok := cfg["use_staging"].(bool); ok {
				leCfg.UseStaging = useStaging
			}
			if domains, ok := cfg["domains"].([]any); ok {
				for _, d := range domains {
					if s, ok := d.(string); ok {
						leCfg.Domains = append(leCfg.Domains, s)
					}
				}
			}
			mod, err := letsencrypt.New(leCfg)
			if err != nil {
				return nil
			}
			return mod
		},
		"logmasker.modular": func(_ string, _ map[string]any) modular.Module {
			return logmasker.NewModule()
		},
		"scheduler.modular": func(_ string, _ map[string]any) modular.Module {
			return scheduler.NewModule()
		},
	}
}
