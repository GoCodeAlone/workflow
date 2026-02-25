package http

import (
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/reverseproxy/v2"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// moduleFactories returns factory functions for all HTTP module types.
func moduleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"http.server":  httpServerFactory,
		"http.router":  httpRouterFactory,
		"http.handler": httpHandlerFactory,

		"http.proxy":        httpProxyFactory,
		"reverseproxy":      httpProxyFactory,
		"http.simple_proxy": httpSimpleProxyFactory,

		"static.fileserver": staticFileServerFactory,

		"http.middleware.auth":            authMiddlewareFactory,
		"http.middleware.logging":         loggingMiddlewareFactory,
		"http.middleware.ratelimit":       rateLimitMiddlewareFactory,
		"http.middleware.cors":            corsMiddlewareFactory,
		"http.middleware.requestid":       requestIDMiddlewareFactory,
		"http.middleware.securityheaders": securityHeadersMiddlewareFactory,
	}
}

func httpServerFactory(name string, cfg map[string]any) modular.Module {
	address := ""
	if addr, ok := cfg["address"].(string); ok {
		address = addr
	}
	srv := module.NewStandardHTTPServer(name, address)

	parseDuration := func(key string) time.Duration {
		if v, ok := cfg[key].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				return d
			}
		}
		return 0
	}
	srv.SetTimeouts(parseDuration("readTimeout"), parseDuration("writeTimeout"), parseDuration("idleTimeout"))
	return srv
}

func httpRouterFactory(name string, _ map[string]any) modular.Module {
	return module.NewStandardHTTPRouter(name)
}

func httpHandlerFactory(name string, cfg map[string]any) modular.Module {
	contentType := "application/json"
	if ct, ok := cfg["contentType"].(string); ok {
		contentType = ct
	}
	return module.NewSimpleHTTPHandler(name, contentType)
}

func httpProxyFactory(_ string, _ map[string]any) modular.Module {
	return reverseproxy.NewModule()
}

func httpSimpleProxyFactory(name string, cfg map[string]any) modular.Module {
	sp := module.NewSimpleProxy(name)
	if targets, ok := cfg["targets"].(map[string]any); ok {
		ts := make(map[string]string, len(targets))
		for prefix, backend := range targets {
			if s, ok := backend.(string); ok {
				ts[prefix] = s
			}
		}
		// Ignore error here — validation happens at Init time
		_ = sp.SetTargets(ts)
	}
	return sp
}

func staticFileServerFactory(name string, cfg map[string]any) modular.Module {
	root := ""
	if r, ok := cfg["root"].(string); ok {
		root = r
	}
	root = config.ResolvePathInConfig(cfg, root)
	prefix := "/"
	if p, ok := cfg["prefix"].(string); ok && p != "" {
		prefix = p
	}
	var opts []module.StaticFileServerOption
	spaFallback := false
	if sf, ok := cfg["spaFallback"].(bool); ok {
		spaFallback = sf
	} else if sf, ok := cfg["spa"].(bool); ok {
		spaFallback = sf
	}
	if spaFallback {
		opts = append(opts, module.WithSPAFallback())
	}
	if cma, ok := cfg["cacheMaxAge"].(int); ok {
		opts = append(opts, module.WithCacheMaxAge(cma))
	} else if cma, ok := cfg["cacheMaxAge"].(float64); ok {
		opts = append(opts, module.WithCacheMaxAge(int(cma)))
	}
	routerName := ""
	if rn, ok := cfg["router"].(string); ok {
		routerName = rn
	}
	sfs := module.NewStaticFileServer(name, root, prefix, opts...)
	if routerName != "" {
		sfs.SetRouterName(routerName)
	}
	return sfs
}

func authMiddlewareFactory(name string, cfg map[string]any) modular.Module {
	authType := "Bearer"
	if at, ok := cfg["authType"].(string); ok {
		authType = at
	}
	return module.NewAuthMiddleware(name, authType)
}

func loggingMiddlewareFactory(name string, cfg map[string]any) modular.Module {
	logLevel := "info"
	if ll, ok := cfg["logLevel"].(string); ok {
		logLevel = ll
	}
	return module.NewLoggingMiddleware(name, logLevel)
}

func rateLimitMiddlewareFactory(name string, cfg map[string]any) modular.Module {
	burstSize := 10
	if bs, ok := cfg["burstSize"].(int); ok {
		if bs > 0 {
			burstSize = bs
		}
	} else if bs, ok := cfg["burstSize"].(float64); ok {
		if intBS := int(bs); intBS > 0 {
			burstSize = intBS
		}
	}

	// requestsPerHour takes precedence over requestsPerMinute for low-frequency
	// endpoints (e.g. registration) where fractional per-minute rates are needed.
	if rph, ok := cfg["requestsPerHour"].(int); ok {
		if rph > 0 {
			return module.NewRateLimitMiddlewareWithHourlyRate(name, rph, burstSize)
		}
	} else if rph, ok := cfg["requestsPerHour"].(float64); ok {
		if intRPH := int(rph); intRPH > 0 {
			return module.NewRateLimitMiddlewareWithHourlyRate(name, intRPH, burstSize)
		}
	}

	requestsPerMinute := 60
	if rpm, ok := cfg["requestsPerMinute"].(int); ok {
		if rpm > 0 {
			requestsPerMinute = rpm
		}
	} else if rpm, ok := cfg["requestsPerMinute"].(float64); ok {
		if intRPM := int(rpm); intRPM > 0 {
			requestsPerMinute = intRPM
		}
	}

	return module.NewRateLimitMiddleware(name, requestsPerMinute, burstSize)
}

func corsMiddlewareFactory(name string, cfg map[string]any) modular.Module {
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
}

func requestIDMiddlewareFactory(name string, _ map[string]any) modular.Module {
	return module.NewRequestIDMiddleware(name)
}

func securityHeadersMiddlewareFactory(name string, cfg map[string]any) modular.Module {
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
}
