package auth

import (
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides authentication capabilities: auth.jwt, auth.user-store, and
// auth.oauth2 modules plus the wiring hook that connects AuthProviders to
// AuthMiddleware.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new Auth plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "auth",
				PluginVersion:     "1.0.0",
				PluginDescription: "JWT authentication, OAuth2, user store, and auth middleware wiring",
			},
			Manifest: plugin.PluginManifest{
				Name:        "auth",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "JWT authentication, OAuth2, user store, and auth middleware wiring",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"auth.jwt",
					"auth.user-store",
					"auth.oauth2",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "authentication", Role: "provider", Priority: 10},
					{Name: "user-management", Role: "provider", Priority: 10},
				},
				WiringHooks: []string{"auth-provider-wiring", "oauth2-jwt-wiring"},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "authentication",
			Description: "JWT-based authentication with token signing, verification, and user management",
		},
		{
			Name:        "user-management",
			Description: "User store for CRUD operations on user accounts",
		},
	}
}

// ModuleFactories returns factories for auth.jwt, auth.user-store, and auth.oauth2.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"auth.jwt": func(name string, cfg map[string]any) modular.Module {
			secret := ""
			if s, ok := cfg["secret"].(string); ok {
				secret = s
			}
			tokenExpiry := 24 * time.Hour
			if te, ok := cfg["tokenExpiry"].(string); ok && te != "" {
				if d, err := time.ParseDuration(te); err == nil {
					tokenExpiry = d
				}
			}
			issuer := "workflow"
			if iss, ok := cfg["issuer"].(string); ok && iss != "" {
				issuer = iss
			}
			authMod := module.NewJWTAuthModule(name, secret, tokenExpiry, issuer)
			if sf, ok := cfg["seedFile"].(string); ok && sf != "" {
				sf = config.ResolvePathInConfig(cfg, sf)
				authMod.SetSeedFile(sf)
			}
			if rf, ok := cfg["responseFormat"].(string); ok && rf != "" {
				authMod.SetResponseFormat(rf)
			}
			return authMod
		},
		"auth.user-store": func(name string, _ map[string]any) modular.Module {
			return module.NewUserStore(name)
		},
		"auth.oauth2": func(name string, cfg map[string]any) modular.Module {
			var providerCfgs []module.OAuth2ProviderConfig
			if raw, ok := cfg["providers"]; ok {
				if providers, ok := raw.([]any); ok {
					for _, p := range providers {
						if pm, ok := p.(map[string]any); ok {
							pc := module.OAuth2ProviderConfig{
								Name:         stringFromMap(pm, "name"),
								ClientID:     stringFromMap(pm, "clientId"),
								ClientSecret: stringFromMap(pm, "clientSecret"),
								AuthURL:      stringFromMap(pm, "authUrl"),
								TokenURL:     stringFromMap(pm, "tokenUrl"),
								UserInfoURL:  stringFromMap(pm, "userInfoUrl"),
								RedirectURL:  stringFromMap(pm, "redirectUrl"),
							}
							if scopes, ok := pm["scopes"].([]any); ok {
								for _, s := range scopes {
									if sv, ok := s.(string); ok {
										pc.Scopes = append(pc.Scopes, sv)
									}
								}
							}
							providerCfgs = append(providerCfgs, pc)
						}
					}
				}
			}
			// jwtAuth will be wired during the wiring hook.
			return module.NewOAuth2Module(name, providerCfgs, nil)
		},
	}
}

// stringFromMap is a helper that extracts a string value from a map by key.
func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// WiringHooks returns the hooks that wire AuthProviders to AuthMiddleware instances
// and JWTAuthModule into OAuth2Module instances.
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "auth-provider-wiring",
			Priority: 50,
			Hook: func(app modular.Application, _ *config.WorkflowConfig) error {
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
			},
		},
		{
			Name:     "oauth2-jwt-wiring",
			Priority: 60,
			Hook: func(app modular.Application, _ *config.WorkflowConfig) error {
				var jwtAuth *module.JWTAuthModule
				for _, svc := range app.SvcRegistry() {
					if j, ok := svc.(*module.JWTAuthModule); ok {
						jwtAuth = j
						break
					}
				}
				if jwtAuth == nil {
					return nil
				}
				for _, svc := range app.SvcRegistry() {
					if om, ok := svc.(*module.OAuth2Module); ok {
						om.SetJWTAuth(jwtAuth)
					}
				}
				return nil
			},
		},
	}
}

// ModuleSchemas returns UI schema definitions for auth module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "auth.jwt",
			Label:       "JWT Auth",
			Category:    "middleware",
			Description: "JWT-based authentication with token signing, verification, and user management",
			Inputs:      []schema.ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "Login credentials or JWT token to verify"}},
			Outputs:     []schema.ServiceIODef{{Name: "auth", Type: "AuthService", Description: "JWT authentication service with token signing and verification"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "secret", Label: "JWT Secret", Type: schema.FieldTypeString, Required: true, Description: "Secret key for signing JWT tokens (supports $ENV_VAR expansion)", Placeholder: "$JWT_SECRET", Sensitive: true},
				{Key: "tokenExpiry", Label: "Token Expiry", Type: schema.FieldTypeDuration, DefaultValue: "24h", Description: "Token expiration duration (e.g. 1h, 24h, 7d)", Placeholder: "24h"},
				{Key: "issuer", Label: "Issuer", Type: schema.FieldTypeString, DefaultValue: "workflow", Description: "Token issuer claim", Placeholder: "workflow"},
				{Key: "seedFile", Label: "Seed Users File", Type: schema.FieldTypeString, Description: "Path to JSON file with initial user accounts", Placeholder: "data/users.json"},
				{Key: "responseFormat", Label: "Response Format", Type: schema.FieldTypeSelect, Options: []string{"standard", "oauth2"}, Description: "Format of authentication response payloads"},
			},
			DefaultConfig: map[string]any{"tokenExpiry": "24h", "issuer": "workflow"},
		},
		{
			Type:         "auth.user-store",
			Label:        "User Store",
			Category:     "infrastructure",
			Description:  "In-memory user store with optional persistence write-through for user CRUD operations",
			Inputs:       []schema.ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "User credentials for CRUD operations"}},
			Outputs:      []schema.ServiceIODef{{Name: "user-store", Type: "UserStore", Description: "User storage service for auth modules"}},
			ConfigFields: []schema.ConfigFieldDef{},
		},
		{
			Type:        "auth.oauth2",
			Label:       "OAuth2",
			Category:    "middleware",
			Description: "OAuth2 authorization code flow supporting Google, GitHub, and generic OIDC providers",
			Inputs:      []schema.ServiceIODef{{Name: "auth-code", Type: "AuthorizationCode", Description: "Authorization code from OAuth2 provider"}},
			Outputs:     []schema.ServiceIODef{{Name: "jwt", Type: "JWTToken", Description: "Signed JWT token issued after successful OAuth2 login"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "providers", Label: "Providers", Type: schema.FieldTypeJSON, Required: true, Description: "List of OAuth2 provider configurations (google, github, or custom)"},
			},
			DefaultConfig: map[string]any{"providers": []any{}},
		},
	}
}
