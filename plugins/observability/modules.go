package observability

import (
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// moduleFactories returns factories for all observability module types.
func moduleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"metrics.collector":    metricsCollectorFactory,
		"health.checker":       healthCheckerFactory,
		"log.collector":        logCollectorFactory,
		"observability.otel":   otelTracingFactory,
		"openapi.generator":    openAPIGeneratorFactory,
		"http.middleware.otel": otelMiddlewareFactory,
		"tracing.propagation":  tracePropagationFactory,
	}
}

func metricsCollectorFactory(name string, cfg map[string]any) modular.Module {
	mcCfg := module.DefaultMetricsCollectorConfig()
	if v, ok := cfg["namespace"].(string); ok {
		mcCfg.Namespace = v
	}
	if v, ok := cfg["subsystem"].(string); ok {
		mcCfg.Subsystem = v
	}
	if v, ok := cfg["metricsPath"].(string); ok {
		mcCfg.MetricsPath = v
	}
	if v, ok := cfg["enabledMetrics"].([]any); ok {
		enabled := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				enabled = append(enabled, s)
			}
		}
		if len(enabled) > 0 {
			mcCfg.EnabledMetrics = enabled
		}
	}
	return module.NewMetricsCollectorWithConfig(name, mcCfg)
}

func healthCheckerFactory(name string, cfg map[string]any) modular.Module {
	hcMod := module.NewHealthChecker(name)
	hcCfg := module.HealthCheckerConfig{AutoDiscover: true}
	if v, ok := cfg["healthPath"].(string); ok {
		hcCfg.HealthPath = v
	}
	if v, ok := cfg["readyPath"].(string); ok {
		hcCfg.ReadyPath = v
	}
	if v, ok := cfg["livePath"].(string); ok {
		hcCfg.LivePath = v
	}
	if v, ok := cfg["checkTimeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			hcCfg.CheckTimeout = d
		}
	}
	if v, ok := cfg["autoDiscover"].(bool); ok {
		hcCfg.AutoDiscover = v
	}
	hcMod.SetConfig(hcCfg)
	return hcMod
}

func logCollectorFactory(name string, cfg map[string]any) modular.Module {
	lcCfg := module.LogCollectorConfig{}
	if v, ok := cfg["logLevel"].(string); ok {
		lcCfg.LogLevel = v
	}
	if v, ok := cfg["outputFormat"].(string); ok {
		lcCfg.OutputFormat = v
	}
	if v, ok := cfg["retentionDays"].(int); ok {
		lcCfg.RetentionDays = v
	}
	return module.NewLogCollector(name, lcCfg)
}

func otelTracingFactory(name string, cfg map[string]any) modular.Module {
	m := module.NewOTelTracing(name)
	if cfg == nil {
		return m
	}
	if v, ok := cfg["endpoint"].(string); ok && v != "" {
		m.SetEndpoint(v)
	}
	if v, ok := cfg["serviceName"].(string); ok && v != "" {
		m.SetServiceName(v)
	}
	return m
}

func openAPIGeneratorFactory(name string, cfg map[string]any) modular.Module {
	genConfig := module.OpenAPIGeneratorConfig{}
	if title, ok := cfg["title"].(string); ok {
		genConfig.Title = title
	}
	if version, ok := cfg["version"].(string); ok {
		genConfig.Version = version
	}
	if desc, ok := cfg["description"].(string); ok {
		genConfig.Description = desc
	}
	if servers, ok := cfg["servers"].([]any); ok {
		for _, s := range servers {
			if str, ok := s.(string); ok {
				genConfig.Servers = append(genConfig.Servers, str)
			}
		}
	}
	return module.NewOpenAPIGenerator(name, genConfig)
}

func otelMiddlewareFactory(name string, cfg map[string]any) modular.Module {
	serverName := "workflow-http"
	if v, ok := cfg["serverName"].(string); ok && v != "" {
		serverName = v
	}
	return module.NewOTelMiddleware(name, serverName)
}

func tracePropagationFactory(name string, cfg map[string]any) modular.Module {
	return module.NewTracePropagationModule(name, cfg)
}
