package handlers

import (
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/reverseproxy/v2"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
)

// safePlugins returns the 9 engine plugins that do NOT import the handlers
// package, avoiding an import cycle. The remaining 6 plugins (http, messaging,
// statemachine, scheduler, integration, pipelinesteps) import handlers so their
// factories are registered manually in registerCyclicPluginFactories.
func safePlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginobs.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginai.New(),
	}
}

// registerCyclicPluginFactories manually registers the module factories that
// would normally come from the 5 plugins that import handlers (http, messaging,
// statemachine, scheduler, integration). This avoids the import cycle while
// still making all module types available during BuildFromConfig.
// The factory logic is copied from the respective plugins/*/modules.go files.
func registerCyclicPluginFactories(engine *workflow.StdEngine) {
	// From plugins/http/modules.go
	engine.AddModuleType("http.server", func(name string, cfg map[string]any) modular.Module {
		address := ""
		if addr, ok := cfg["address"].(string); ok {
			address = addr
		}
		return module.NewStandardHTTPServer(name, address)
	})
	engine.AddModuleType("http.router", func(name string, _ map[string]any) modular.Module {
		return module.NewStandardHTTPRouter(name)
	})
	engine.AddModuleType("http.handler", func(name string, cfg map[string]any) modular.Module {
		contentType := "application/json"
		if ct, ok := cfg["contentType"].(string); ok {
			contentType = ct
		}
		return module.NewSimpleHTTPHandler(name, contentType)
	})
	engine.AddModuleType("http.proxy", func(_ string, _ map[string]any) modular.Module {
		return reverseproxy.NewModule()
	})
	engine.AddModuleType("reverseproxy", func(_ string, _ map[string]any) modular.Module {
		return reverseproxy.NewModule()
	})
	engine.AddModuleType("http.simple_proxy", func(name string, cfg map[string]any) modular.Module {
		sp := module.NewSimpleProxy(name)
		if targets, ok := cfg["targets"].(map[string]any); ok {
			ts := make(map[string]string, len(targets))
			for prefix, backend := range targets {
				if s, ok := backend.(string); ok {
					ts[prefix] = s
				}
			}
			_ = sp.SetTargets(ts)
		}
		return sp
	})
	engine.AddModuleType("static.fileserver", func(name string, cfg map[string]any) modular.Module {
		root := ""
		if r, ok := cfg["root"].(string); ok {
			root = r
		}
		prefix := "/"
		if p, ok := cfg["prefix"].(string); ok && p != "" {
			prefix = p
		}
		spaFallback := true
		if sf, ok := cfg["spaFallback"].(bool); ok {
			spaFallback = sf
		}
		cacheMaxAge := 3600
		if cma, ok := cfg["cacheMaxAge"].(int); ok {
			cacheMaxAge = cma
		} else if cma, ok := cfg["cacheMaxAge"].(float64); ok {
			cacheMaxAge = int(cma)
		}
		routerName := ""
		if rn, ok := cfg["router"].(string); ok {
			routerName = rn
		}
		sfs := module.NewStaticFileServer(name, root, prefix, spaFallback, cacheMaxAge)
		if routerName != "" {
			sfs.SetRouterName(routerName)
		}
		return sfs
	})
	engine.AddModuleType("http.middleware.auth", func(name string, cfg map[string]any) modular.Module {
		authType := "Bearer"
		if at, ok := cfg["authType"].(string); ok {
			authType = at
		}
		return module.NewAuthMiddleware(name, authType)
	})
	engine.AddModuleType("http.middleware.logging", func(name string, cfg map[string]any) modular.Module {
		logLevel := "info"
		if ll, ok := cfg["logLevel"].(string); ok {
			logLevel = ll
		}
		return module.NewLoggingMiddleware(name, logLevel)
	})
	engine.AddModuleType("http.middleware.ratelimit", func(name string, cfg map[string]any) modular.Module {
		requestsPerMinute := 60
		burstSize := 10
		if rpm, ok := cfg["requestsPerMinute"].(int); ok {
			requestsPerMinute = rpm
		} else if rpm, ok := cfg["requestsPerMinute"].(float64); ok {
			requestsPerMinute = int(rpm)
		}
		if bs, ok := cfg["burstSize"].(int); ok {
			burstSize = bs
		} else if bs, ok := cfg["burstSize"].(float64); ok {
			burstSize = int(bs)
		}
		return module.NewRateLimitMiddleware(name, requestsPerMinute, burstSize)
	})
	engine.AddModuleType("http.middleware.cors", func(name string, cfg map[string]any) modular.Module {
		allowedOrigins := []string{"*"}
		allowedMethods := []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
		if origins, ok := cfg["allowedOrigins"].([]any); ok {
			allowedOrigins = make([]string, len(origins))
			for i, origin := range origins {
				if str, ok := origin.(string); ok {
					allowedOrigins[i] = str
				}
			}
		}
		if methods, ok := cfg["allowedMethods"].([]any); ok {
			allowedMethods = make([]string, len(methods))
			for i, method := range methods {
				if str, ok := method.(string); ok {
					allowedMethods[i] = str
				}
			}
		}
		return module.NewCORSMiddleware(name, allowedOrigins, allowedMethods)
	})
	engine.AddModuleType("http.middleware.requestid", func(name string, _ map[string]any) modular.Module {
		return module.NewRequestIDMiddleware(name)
	})
	engine.AddModuleType("http.middleware.securityheaders", func(name string, cfg map[string]any) modular.Module {
		secCfg := module.SecurityHeadersConfig{}
		if v, ok := cfg["contentSecurityPolicy"].(string); ok {
			secCfg.ContentSecurityPolicy = v
		}
		if v, ok := cfg["frameOptions"].(string); ok {
			secCfg.FrameOptions = v
		}
		if v, ok := cfg["contentTypeOptions"].(string); ok {
			secCfg.ContentTypeOptions = v
		}
		if v, ok := cfg["hstsMaxAge"].(int); ok {
			secCfg.HSTSMaxAge = v
		}
		if v, ok := cfg["referrerPolicy"].(string); ok {
			secCfg.ReferrerPolicy = v
		}
		if v, ok := cfg["permissionsPolicy"].(string); ok {
			secCfg.PermissionsPolicy = v
		}
		return module.NewSecurityHeadersMiddleware(name, secCfg)
	})

	// From plugins/messaging/plugin.go
	engine.AddModuleType("messaging.broker", func(name string, cfg map[string]any) modular.Module {
		broker := module.NewInMemoryMessageBroker(name)
		if maxQ, ok := cfg["maxQueueSize"].(float64); ok {
			broker.SetMaxQueueSize(int(maxQ))
		}
		if timeout, ok := cfg["deliveryTimeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				broker.SetDeliveryTimeout(d)
			}
		}
		return broker
	})
	engine.AddModuleType("messaging.broker.eventbus", func(name string, _ map[string]any) modular.Module {
		return module.NewEventBusBridge(name)
	})
	engine.AddModuleType("messaging.handler", func(name string, _ map[string]any) modular.Module {
		return module.NewSimpleMessageHandler(name)
	})
	engine.AddModuleType("messaging.nats", func(name string, _ map[string]any) modular.Module {
		return module.NewNATSBroker(name)
	})
	engine.AddModuleType("messaging.kafka", func(name string, cfg map[string]any) modular.Module {
		kb := module.NewKafkaBroker(name)
		if brokers, ok := cfg["brokers"].([]any); ok {
			bs := make([]string, 0, len(brokers))
			for _, b := range brokers {
				if s, ok := b.(string); ok {
					bs = append(bs, s)
				}
			}
			if len(bs) > 0 {
				kb.SetBrokers(bs)
			}
		}
		if groupID, ok := cfg["groupId"].(string); ok && groupID != "" {
			kb.SetGroupID(groupID)
		}
		return kb
	})
	engine.AddModuleType("notification.slack", func(name string, _ map[string]any) modular.Module {
		return module.NewSlackNotification(name)
	})
	engine.AddModuleType("webhook.sender", func(name string, cfg map[string]any) modular.Module {
		webhookConfig := module.WebhookConfig{}
		if mr, ok := cfg["maxRetries"].(float64); ok {
			webhookConfig.MaxRetries = int(mr)
		}
		return module.NewWebhookSender(name, webhookConfig)
	})

	// From plugins/statemachine/plugin.go
	engine.AddModuleType("statemachine.engine", func(name string, cfg map[string]any) modular.Module {
		smEngine := module.NewStateMachineEngine(name)
		if maxInst, ok := cfg["maxInstances"].(float64); ok {
			smEngine.SetMaxInstances(int(maxInst))
		}
		if ttl, ok := cfg["instanceTTL"].(string); ok {
			if d, err := time.ParseDuration(ttl); err == nil {
				smEngine.SetInstanceTTL(d)
			}
		}
		return smEngine
	})
	engine.AddModuleType("state.tracker", func(name string, cfg map[string]any) modular.Module {
		tracker := module.NewStateTracker(name)
		if rd, ok := cfg["retentionDays"].(float64); ok {
			tracker.SetRetentionDays(int(rd))
		}
		return tracker
	})
	engine.AddModuleType("state.connector", func(name string, _ map[string]any) modular.Module {
		return module.NewStateMachineStateConnector(name)
	})

	// plugins/scheduler — no module factories (only workflow handler + trigger)
	// plugins/integration — no module factories (only workflow handler)

	// From plugins/pipelinesteps — step factories (pipelinesteps imports handlers,
	// so we register its step types here instead of calling LoadPlugin on it).
	engine.AddStepType("step.validate", module.NewValidateStepFactory())
	engine.AddStepType("step.transform", module.NewTransformStepFactory())
	engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
	engine.AddStepType("step.set", module.NewSetStepFactory())
	engine.AddStepType("step.log", module.NewLogStepFactory())
	engine.AddStepType("step.delegate", module.NewDelegateStepFactory())
	engine.AddStepType("step.jq", module.NewJQStepFactory())
	engine.AddStepType("step.publish", module.NewPublishStepFactory())
	engine.AddStepType("step.http_call", module.NewHTTPCallStepFactory())
	engine.AddStepType("step.request_parse", module.NewRequestParseStepFactory())
	engine.AddStepType("step.db_query", module.NewDBQueryStepFactory())
	engine.AddStepType("step.db_exec", module.NewDBExecStepFactory())
	engine.AddStepType("step.json_response", module.NewJSONResponseStepFactory())

	// Workflow handlers from the cyclic plugins are registered directly by
	// individual tests via engine.RegisterWorkflowHandler(), so we don't
	// duplicate them here.
}

// loadAllPlugins loads the 9 safe plugins and manually registers module/step
// factories from the 6 cyclic plugins to provide all module types for
// BuildFromConfig without causing import cycles.
func loadAllPlugins(t *testing.T, engine *workflow.StdEngine) {
	t.Helper()
	for _, p := range safePlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}
	registerCyclicPluginFactories(engine)
}
