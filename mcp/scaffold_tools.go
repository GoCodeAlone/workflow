package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// registerScaffoldTools registers tools that generate workflow config sections
// for AI assistants helping users bootstrap new workflow applications.
func (s *Server) registerScaffoldTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("scaffold_ci",
			mcp.WithDescription("Generate a ci: YAML section for a workflow config based on the application type. "+
				"Produces build (binaries/containers/assets), test (unit/integration/e2e), and deploy sub-sections "+
				"with sensible defaults. Pass the existing YAML config and an app description."),
			mcp.WithString("yaml_content",
				mcp.Description("Existing workflow YAML config to analyze (optional, used to detect app type)"),
			),
			mcp.WithString("description",
				mcp.Required(),
				mcp.Description("Short description of the application (e.g. 'Go API server with Postgres', 'React+Go full-stack app')"),
			),
			mcp.WithString("binary_path",
				mcp.Description("Go binary entrypoint path (default: ./cmd/server)"),
			),
			mcp.WithArray("environments",
				mcp.Description("Deployment environment names to include (e.g. ['staging', 'production'])"),
			),
		),
		s.handleScaffoldCI,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("scaffold_environment",
			mcp.WithDescription("Generate an environments: YAML section for a workflow config. "+
				"Creates per-environment configuration blocks with provider, region, env vars, "+
				"secrets provider, and exposure method (tailscale, cloudflare, port-forward)."),
			mcp.WithString("provider",
				mcp.Required(),
				mcp.Description("Deployment provider: docker, kubernetes, aws-ecs, gcp-cloudrun, digitalocean"),
			),
			mcp.WithArray("environments",
				mcp.Description("Environment names to generate (default: ['local', 'staging', 'production'])"),
			),
			mcp.WithString("secrets_provider",
				mcp.Description("Secrets provider: env, aws-secrets-manager, gcp-secret-manager, vault (default: env)"),
			),
			mcp.WithString("exposure",
				mcp.Description("Exposure method for local: tailscale, cloudflare, port-forward (default: port-forward)"),
			),
		),
		s.handleScaffoldEnvironment,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("scaffold_infra",
			mcp.WithDescription("Generate an infra: YAML section by analyzing the workflow config's modules. "+
				"Detects database, cache, and messaging module types and suggests matching cloud resources "+
				"(e.g. database.postgres → RDS instance, cache.redis → ElastiCache cluster)."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("Workflow YAML config content to analyze for infrastructure needs"),
			),
			mcp.WithString("provider",
				mcp.Required(),
				mcp.Description("Cloud provider for resources: aws, gcp, azure, digitalocean"),
			),
			mcp.WithString("environment",
				mcp.Description("Environment name for resource sizing (default: production)"),
			),
		),
		s.handleScaffoldInfra,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("detect_secrets",
			mcp.WithDescription("Scan a workflow YAML config and return a list of secret candidates: "+
				"fields whose names (dsn, apiKey, token, password, signingKey, clientSecret) or values "+
				"(${...} env var references) indicate secrets that should be managed externally."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("Workflow YAML config content to scan"),
			),
		),
		s.handleDetectSecrets,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("detect_ports",
			mcp.WithDescription("Scan a workflow YAML config and return a list of ports used by the application. "+
				"Checks http.server, grpc.server, and other network-listening module configs."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("Workflow YAML config content to scan"),
			),
		),
		s.handleDetectPorts,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("generate_bootstrap",
			mcp.WithDescription("Generate a CI platform bootstrap file that calls wfctl ci run. "+
				"Reads the ci.deploy.environments section from the config to create per-environment "+
				"deploy jobs. Supports github-actions and gitlab-ci platforms."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("Workflow YAML config with a ci: section"),
			),
			mcp.WithString("platform",
				mcp.Required(),
				mcp.Description("CI platform: github-actions, gitlab-ci"),
			),
		),
		s.handleGenerateBootstrap,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("detect_infra_needs",
			mcp.WithDescription("Analyze a workflow YAML config's modules and suggest the infrastructure "+
				"components needed to run the application in production. Returns a structured list of "+
				"infrastructure requirements (databases, caches, message brokers, storage buckets) "+
				"with recommended cloud service types. If plugins_dir is provided, also consults "+
				"plugin.json manifests in that directory for plugin-declared infrastructure needs."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("Workflow YAML config content to analyze"),
			),
			mcp.WithString("provider",
				mcp.Description("Target cloud provider for service name suggestions: aws, gcp, azure, digitalocean (default: aws)"),
			),
			mcp.WithString("plugins_dir",
				mcp.Description("Directory containing installed plugin sub-directories, each with a plugin.json (optional)"),
			),
		),
		s.handleDetectInfraNeeds,
	)
}

// handleScaffoldCI generates a ci: YAML section.
func (s *Server) handleScaffoldCI(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	description := mcp.ParseString(req, "description", "")
	if description == "" {
		return mcp.NewToolResultError("description is required"), nil
	}
	binaryPath := mcp.ParseString(req, "binary_path", "./cmd/server")

	var envNames []string
	if rawEnvs, ok := req.GetArguments()["environments"]; ok {
		if arr, ok := rawEnvs.([]any); ok {
			for _, e := range arr {
				if s, ok := e.(string); ok {
					envNames = append(envNames, s)
				}
			}
		}
	}
	if len(envNames) == 0 {
		envNames = []string{"staging", "production"}
	}

	descLower := strings.ToLower(description)
	hasContainer := strings.Contains(descLower, "docker") || strings.Contains(descLower, "container")
	hasFrontend := strings.Contains(descLower, "react") || strings.Contains(descLower, "frontend") ||
		strings.Contains(descLower, "vue") || strings.Contains(descLower, "svelte")
	hasDB := strings.Contains(descLower, "postgres") || strings.Contains(descLower, "mysql") ||
		strings.Contains(descLower, "sqlite") || strings.Contains(descLower, "db")

	var b strings.Builder
	b.WriteString("ci:\n")
	b.WriteString("  build:\n")
	b.WriteString("    binaries:\n")
	b.WriteString("      - name: server\n")
	fmt.Fprintf(&b, "        path: %s\n", binaryPath)
	b.WriteString("        os: [linux]\n")
	b.WriteString("        arch: [amd64, arm64]\n")
	b.WriteString("        ldflags: \"-s -w -X main.version=${VERSION}\"\n")

	if hasContainer {
		b.WriteString("    containers:\n")
		b.WriteString("      - name: app\n")
		b.WriteString("        dockerfile: Dockerfile\n")
		b.WriteString("        context: .\n")
		b.WriteString("        tag: \"${VERSION}\"\n")
	}

	if hasFrontend {
		b.WriteString("    assets:\n")
		b.WriteString("      - name: frontend\n")
		b.WriteString("        build: npm run build\n")
		b.WriteString("        path: dist/\n")
	}

	b.WriteString("  test:\n")
	b.WriteString("    unit:\n")
	b.WriteString("      command: go test ./... -race -count=1\n")
	b.WriteString("      coverage: true\n")

	if hasDB {
		b.WriteString("    integration:\n")
		b.WriteString("      command: go test ./... -tags=integration -race\n")
		b.WriteString("      needs:\n")
		b.WriteString("        - postgres  # ephemeral Docker container\n")
	}

	b.WriteString("  deploy:\n")
	b.WriteString("    environments:\n")
	for _, env := range envNames {
		fmt.Fprintf(&b, "      %s:\n", env)
		if env == "production" || env == "prod" {
			b.WriteString("        provider: kubernetes\n")
			b.WriteString("        strategy: rolling\n")
			b.WriteString("        requireApproval: true\n")
		} else {
			b.WriteString("        provider: kubernetes\n")
			b.WriteString("        strategy: rolling\n")
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleScaffoldEnvironment generates an environments: YAML section.
func (s *Server) handleScaffoldEnvironment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	provider := mcp.ParseString(req, "provider", "")
	if provider == "" {
		return mcp.NewToolResultError("provider is required"), nil
	}
	secretsProvider := mcp.ParseString(req, "secrets_provider", "env")
	exposure := mcp.ParseString(req, "exposure", "port-forward")

	var envNames []string
	if rawEnvs, ok := req.GetArguments()["environments"]; ok {
		if arr, ok := rawEnvs.([]any); ok {
			for _, e := range arr {
				if sv, ok := e.(string); ok {
					envNames = append(envNames, sv)
				}
			}
		}
	}
	if len(envNames) == 0 {
		envNames = []string{"local", "staging", "production"}
	}

	var b strings.Builder
	b.WriteString("environments:\n")
	for _, env := range envNames {
		fmt.Fprintf(&b, "  %s:\n", env)
		if env == "local" {
			b.WriteString("    provider: docker\n")
			b.WriteString("    envVars:\n")
			b.WriteString("      LOG_LEVEL: debug\n")
			b.WriteString("      DATABASE_URL: \"${DATABASE_URL}\"\n")
			b.WriteString("    secretsProvider: env\n")
			switch exposure {
			case "tailscale":
				b.WriteString("    exposure:\n")
				b.WriteString("      method: tailscale\n")
				b.WriteString("      tailscale:\n")
				b.WriteString("        funnel: true\n")
				b.WriteString("        hostname: myapp-dev\n")
			case "cloudflare":
				b.WriteString("    exposure:\n")
				b.WriteString("      method: cloudflareTunnel\n")
				b.WriteString("      cloudflareTunnel:\n")
				b.WriteString("        tunnelName: myapp-dev\n")
				b.WriteString("        domain: dev.example.com\n")
			default:
				b.WriteString("    exposure:\n")
				b.WriteString("      method: portForward\n")
				b.WriteString("      portForward:\n")
				b.WriteString("        http: \"8080:8080\"\n")
			}
		} else {
			fmt.Fprintf(&b, "    provider: %s\n", provider)
			b.WriteString("    envVars:\n")
			b.WriteString("      LOG_LEVEL: info\n")
			fmt.Fprintf(&b, "    secretsProvider: %s\n", secretsProvider)
			if env == "production" || env == "prod" {
				b.WriteString("    approvalRequired: true\n")
			}
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleScaffoldInfra generates an infra: YAML section from detected module needs.
func (s *Server) handleScaffoldInfra(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}
	provider := mcp.ParseString(req, "provider", "aws")
	environment := mcp.ParseString(req, "environment", "production")

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parse YAML: %v", err)), nil
	}

	type infraResource struct {
		ModuleType  string
		ModuleName  string
		ResourceType string
		ServiceName  string
	}
	var resources []infraResource
	for _, mod := range cfg.Modules {
		switch {
		case mod.Type == "database.postgres" || mod.Type == "database.postgresql":
			resources = append(resources, infraResource{
				ModuleType:   mod.Type,
				ModuleName:   mod.Name,
				ResourceType: "database",
				ServiceName:  providerDBService(provider),
			})
		case mod.Type == "cache.redis":
			resources = append(resources, infraResource{
				ModuleType:   mod.Type,
				ModuleName:   mod.Name,
				ResourceType: "cache",
				ServiceName:  providerCacheService(provider),
			})
		case strings.HasPrefix(mod.Type, "messaging.") || mod.Type == "eventbus.nats" || mod.Type == "eventbus.kafka":
			resources = append(resources, infraResource{
				ModuleType:   mod.Type,
				ModuleName:   mod.Name,
				ResourceType: "messaging",
				ServiceName:  providerMessagingService(provider),
			})
		case strings.HasPrefix(mod.Type, "storage."):
			resources = append(resources, infraResource{
				ModuleType:   mod.Type,
				ModuleName:   mod.Name,
				ResourceType: "storage",
				ServiceName:  providerStorageService(provider),
			})
		}
	}

	if len(resources) == 0 {
		return mcp.NewToolResultText("# No infrastructure requirements detected.\n# " +
			"Add database, cache, messaging, or storage modules to generate infra: config.\n"), nil
	}

	var b strings.Builder
	b.WriteString("infra:\n")
	fmt.Fprintf(&b, "  # Auto-generated for %s environment on %s\n", environment, provider)
	b.WriteString("  resources:\n")
	for _, r := range resources {
		fmt.Fprintf(&b, "    - name: %s\n", r.ModuleName)
		fmt.Fprintf(&b, "      type: %s\n", r.ResourceType)
		fmt.Fprintf(&b, "      provider: %s\n", provider)
		fmt.Fprintf(&b, "      service: %s\n", r.ServiceName)
		fmt.Fprintf(&b, "      # References module: %s\n", r.ModuleType)
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleDetectSecrets scans a workflow config for secret-like fields.
func (s *Server) handleDetectSecrets(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parse YAML: %v", err)), nil
	}

	type secretCandidate struct {
		Module    string `json:"module"`
		Field     string `json:"field"`
		Reason    string `json:"reason"`
		Suggested string `json:"suggested_env_var"`
	}

	secretFields := []string{"dsn", "apiKey", "api_key", "token", "secret", "password",
		"signingKey", "signing_key", "clientSecret", "client_secret", "privateKey",
		"private_key", "accessKey", "access_key", "secretKey", "secret_key"}

	var candidates []secretCandidate
	for _, mod := range cfg.Modules {
		for field, val := range mod.Config {
			fieldLower := strings.ToLower(field)
			for _, secretField := range secretFields {
				if fieldLower == strings.ToLower(secretField) {
					valStr, _ := val.(string)
					reason := "field name matches secret pattern"
					suggested := strings.ToUpper(strings.ReplaceAll(field, ".", "_"))
					if strings.HasPrefix(valStr, "${") {
						reason = "value uses env var reference"
						suggested = strings.TrimSuffix(strings.TrimPrefix(valStr, "${"), "}")
					}
					candidates = append(candidates, secretCandidate{
						Module:    mod.Name,
						Field:     field,
						Reason:    reason,
						Suggested: suggested,
					})
					break
				}
			}
		}
	}

	result := map[string]any{
		"candidates": candidates,
		"count":      len(candidates),
		"recommendation": "Add a secrets: section to your workflow config and reference secrets " +
			"via environment variables using ${VAR_NAME} syntax.",
	}
	return marshalToolResult(result)
}

// handleDetectPorts scans a workflow config for declared network ports.
func (s *Server) handleDetectPorts(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parse YAML: %v", err)), nil
	}

	type portInfo struct {
		Module   string `json:"module"`
		Type     string `json:"type"`
		Port     any    `json:"port"`
		Protocol string `json:"protocol"`
	}

	portFields := []string{"port", "addr", "address", "listen", "listenAddr", "listenPort"}
	var ports []portInfo
	for _, mod := range cfg.Modules {
		for _, field := range portFields {
			if val, ok := mod.Config[field]; ok {
				protocol := "http"
				if strings.Contains(strings.ToLower(mod.Type), "grpc") {
					protocol = "grpc"
				} else if strings.Contains(strings.ToLower(mod.Type), "ws") {
					protocol = "ws"
				}
				ports = append(ports, portInfo{
					Module:   mod.Name,
					Type:     mod.Type,
					Port:     val,
					Protocol: protocol,
				})
				break
			}
		}
	}

	result := map[string]any{
		"ports": ports,
		"count": len(ports),
	}
	return marshalToolResult(result)
}

// handleGenerateBootstrap generates a CI platform bootstrap file.
func (s *Server) handleGenerateBootstrap(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}
	platform := mcp.ParseString(req, "platform", "")
	if platform == "" {
		return mcp.NewToolResultError("platform is required"), nil
	}

	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parse YAML: %v", err)), nil
	}

	switch platform {
	case "github-actions":
		return mcp.NewToolResultText(generateGitHubActionsBootstrap(&cfg)), nil
	case "gitlab-ci":
		return mcp.NewToolResultText(generateGitLabCIBootstrap(&cfg)), nil
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unsupported platform %q (valid: github-actions, gitlab-ci)", platform)), nil
	}
}

// handleDetectInfraNeeds analyzes modules and returns structured infra requirements.
func (s *Server) handleDetectInfraNeeds(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}
	provider := mcp.ParseString(req, "provider", "aws")
	pluginsDir := mcp.ParseString(req, "plugins_dir", "")

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parse YAML: %v", err)), nil
	}

	type infraNeed struct {
		Category    string `json:"category"`
		ModuleName  string `json:"module_name"`
		ModuleType  string `json:"module_type"`
		Service     string `json:"recommended_service"`
		Description string `json:"description"`
		Source      string `json:"source,omitempty"` // "builtin" or plugin name
	}

	serviceMap := map[string]func(string) infraNeed{
		"database.postgres": func(p string) infraNeed {
			return infraNeed{Category: "database", Service: providerDBService(p),
				Description: "Managed PostgreSQL database", Source: "builtin"}
		},
		"database.postgresql": func(p string) infraNeed {
			return infraNeed{Category: "database", Service: providerDBService(p),
				Description: "Managed PostgreSQL database", Source: "builtin"}
		},
		"cache.redis": func(p string) infraNeed {
			return infraNeed{Category: "cache", Service: providerCacheService(p),
				Description: "Managed Redis cache", Source: "builtin"}
		},
		"storage.s3": func(p string) infraNeed {
			return infraNeed{Category: "storage", Service: providerStorageService(p),
				Description: "Object storage bucket", Source: "builtin"}
		},
		"storage.sqlite": func(_ string) infraNeed {
			return infraNeed{Category: "storage", Service: "block-volume",
				Description: "Persistent block volume for SQLite (consider migrating to managed DB for production)", Source: "builtin"}
		},
	}

	var needs []infraNeed
	for _, mod := range cfg.Modules {
		if fn, ok := serviceMap[mod.Type]; ok {
			need := fn(provider)
			need.ModuleName = mod.Name
			need.ModuleType = mod.Type
			needs = append(needs, need)
		} else if strings.HasPrefix(mod.Type, "messaging.") || strings.HasPrefix(mod.Type, "eventbus.") {
			needs = append(needs, infraNeed{
				Category:    "messaging",
				ModuleName:  mod.Name,
				ModuleType:  mod.Type,
				Service:     providerMessagingService(provider),
				Description: "Managed message broker",
				Source:      "builtin",
			})
		}
	}

	// Consult plugin manifests for plugin-declared infrastructure needs.
	if pluginsDir != "" {
		pluginReqs, loadErr := loadPluginInfraNeeds(pluginsDir, cfg)
		if loadErr == nil {
			seen := make(map[string]bool, len(needs))
			for _, n := range needs {
				seen[n.ModuleType+":"+n.ModuleName] = true
			}
			for _, req := range pluginReqs {
				needs = append(needs, infraNeed{
					Category:    req.Type,
					ModuleType:  req.Type,
					Service:     req.Name,
					Description: req.Description,
					Source:      "plugin-manifest",
				})
			}
		}
	}

	result := map[string]any{
		"provider": provider,
		"needs":    needs,
		"count":    len(needs),
		"summary":  fmt.Sprintf("Detected %d infrastructure requirements for %d modules", len(needs), len(cfg.Modules)),
	}
	return marshalToolResult(result)
}

// loadPluginInfraNeeds reads plugin manifests from pluginsDir and returns
// infrastructure requirements declared by plugins whose module types are used in cfg.
func loadPluginInfraNeeds(pluginsDir string, cfg *config.WorkflowConfig) ([]config.InfraRequirement, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, err
	}

	// Collect all module types used in the config
	usedTypes := make(map[string]bool)
	for _, mod := range cfg.Modules {
		usedTypes[mod.Type] = true
	}
	for _, svc := range cfg.Services {
		if svc == nil {
			continue
		}
		for _, mod := range svc.Modules {
			usedTypes[mod.Type] = true
		}
	}

	seen := make(map[string]bool)
	var needs []config.InfraRequirement

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pluginsDir, entry.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		var manifest config.PluginManifestFile
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		for moduleType, spec := range manifest.ModuleInfraRequirements {
			if !usedTypes[moduleType] {
				continue
			}
			for _, req := range spec.Requires {
				key := req.Type + ":" + req.Name
				if seen[key] {
					continue
				}
				seen[key] = true
				needs = append(needs, req)
			}
		}
	}
	return needs, nil
}

// --- Helpers ---

func providerDBService(provider string) string {
	switch provider {
	case "aws":
		return "aws-rds-postgres"
	case "gcp":
		return "cloud-sql-postgres"
	case "azure":
		return "azure-database-postgres"
	case "digitalocean":
		return "do-managed-database"
	default:
		return "managed-postgres"
	}
}

func providerCacheService(provider string) string {
	switch provider {
	case "aws":
		return "aws-elasticache-redis"
	case "gcp":
		return "memorystore-redis"
	case "azure":
		return "azure-cache-redis"
	case "digitalocean":
		return "do-managed-redis"
	default:
		return "managed-redis"
	}
}

func providerMessagingService(provider string) string {
	switch provider {
	case "aws":
		return "aws-msk-kafka"
	case "gcp":
		return "cloud-pubsub"
	case "azure":
		return "azure-service-bus"
	default:
		return "managed-kafka"
	}
}

func providerStorageService(provider string) string {
	switch provider {
	case "aws":
		return "aws-s3"
	case "gcp":
		return "cloud-storage"
	case "azure":
		return "azure-blob-storage"
	case "digitalocean":
		return "do-spaces"
	default:
		return "object-storage"
	}
}

// generateGitHubActionsBootstrap produces a GitHub Actions workflow YAML.
func generateGitHubActionsBootstrap(cfg *config.WorkflowConfig) string {
	var b strings.Builder
	b.WriteString("# Generated by wfctl — customize as needed\n")
	b.WriteString("name: CI/CD\n\n")
	b.WriteString("on:\n")
	b.WriteString("  push:\n")
	b.WriteString("    branches: [main]\n")
	b.WriteString("  pull_request:\n\n")
	b.WriteString("jobs:\n")
	b.WriteString("  build-test:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("      - uses: GoCodeAlone/setup-wfctl@v1\n")
	b.WriteString("      - run: wfctl ci run --phase build,test\n")

	if cfg.CI != nil && cfg.CI.Deploy != nil {
		for envName, env := range cfg.CI.Deploy.Environments {
			b.WriteString("\n")
			fmt.Fprintf(&b, "  deploy-%s:\n", envName)
			b.WriteString("    runs-on: ubuntu-latest\n")
			b.WriteString("    needs: [build-test]\n")
			if env != nil && env.RequireApproval {
				b.WriteString("    environment:\n")
				fmt.Fprintf(&b, "      name: %s\n", envName)
			}
			b.WriteString("    if: github.ref == 'refs/heads/main'\n")
			b.WriteString("    steps:\n")
			b.WriteString("      - uses: actions/checkout@v4\n")
			b.WriteString("      - uses: GoCodeAlone/setup-wfctl@v1\n")
			fmt.Fprintf(&b, "      - run: wfctl ci run --phase deploy --env %s\n", envName)
		}
	}

	return b.String()
}

// generateGitLabCIBootstrap produces a GitLab CI YAML.
func generateGitLabCIBootstrap(cfg *config.WorkflowConfig) string {
	var b strings.Builder
	b.WriteString("# Generated by wfctl — customize as needed\n\n")
	b.WriteString("stages:\n")
	b.WriteString("  - build\n")
	b.WriteString("  - test\n")
	b.WriteString("  - deploy\n\n")
	b.WriteString("build:\n")
	b.WriteString("  stage: build\n")
	b.WriteString("  script:\n")
	b.WriteString("    - wfctl ci run --phase build\n\n")
	b.WriteString("test:\n")
	b.WriteString("  stage: test\n")
	b.WriteString("  script:\n")
	b.WriteString("    - wfctl ci run --phase test\n")

	if cfg.CI != nil && cfg.CI.Deploy != nil {
		for envName := range cfg.CI.Deploy.Environments {
			b.WriteString("\n")
			fmt.Fprintf(&b, "deploy-%s:\n", envName)
			b.WriteString("  stage: deploy\n")
			b.WriteString("  script:\n")
			fmt.Fprintf(&b, "    - wfctl ci run --phase deploy --env %s\n", envName)
			b.WriteString("  only:\n")
			b.WriteString("    - main\n")
		}
	}

	return b.String()
}
