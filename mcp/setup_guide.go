package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// setupGuideContent is the workflow://docs/setup-guide resource content.
// It provides decision trees and step-by-step guidance for AI assistants
// helping users configure workflow applications.
const setupGuideContent = `# Workflow Setup Guide

## For AI Assistants

This guide provides decision trees and patterns for configuring workflow applications.
Follow the flows below based on what the user needs.

---

## Application Bootstrap Flow

When a user wants to create a new workflow application:

1. **Detect app type** — ask or infer from description
   - HTTP API server → modules: http.server, http.router, http.handler
   - Background worker → modules: eventbus, messaging.consumer
   - Full-stack app → HTTP modules + static.fileserver for frontend
   - Scheduled tasks → modules: scheduler

2. **Generate skeleton** — use ` + "`get_config_skeleton`" + ` with the detected module types

3. **Add persistence** — if the app needs a database:
   - PostgreSQL → ` + "`database.postgres`" + ` module
   - SQLite → ` + "`storage.sqlite`" + ` module (dev/simple use cases)
   - Redis cache → ` + "`cache.redis`" + ` module

4. **Generate CI config** — use ` + "`scaffold_ci`" + ` with the app description

5. **Generate environments** — use ` + "`scaffold_environment`" + ` with the target provider

6. **Detect secrets** — use ` + "`detect_secrets`" + ` on the generated config

---

## Infrastructure Setup Flow

When a user needs cloud infrastructure:

1. **Ask**: "What cloud provider?" → AWS | GCP | Azure | DigitalOcean | Local

2. **Detect needs** — use ` + "`detect_infra_needs`" + ` on the workflow config
   - Returns list of required services (DB, cache, messaging, storage)

3. **Generate infra section** — use ` + "`scaffold_infra`" + ` with provider name

4. **Apply** — user runs ` + "`wfctl infra apply`" + ` to provision resources

---

## CI/CD Setup Flow

When a user wants automated deployment:

1. **Ask**: "What CI platform?" → GitHub Actions | GitLab CI | Jenkins

2. **Check ci: section** — if absent, use ` + "`scaffold_ci`" + ` to generate it

3. **Generate bootstrap** — use ` + "`generate_bootstrap`" + ` with the platform name
   - Produces minimal YAML that calls ` + "`wfctl ci run`" + `

4. **Validate** — use ` + "`validate_config`" + ` to check the final config

---

## Secrets Management Flow

When a user asks about secrets/credentials:

1. **Detect secrets** — use ` + "`detect_secrets`" + ` on the existing config

2. **Choose provider**:
   - Local dev → ` + "`env`" + ` provider (reads from environment variables)
   - AWS → ` + "`aws-secrets-manager`" + `
   - GCP → ` + "`gcp-secret-manager`" + `
   - Self-hosted → ` + "`vault`" + `

3. **Add secrets: section** to the workflow config:
   ` + "```yaml" + `
   secrets:
     provider: env
     entries:
       - name: DATABASE_URL
         description: PostgreSQL connection string
       - name: JWT_SECRET
         description: JWT signing key
   ` + "```" + `

4. **Reference in modules** using ` + "`${SECRET_NAME}`" + ` syntax

---

## Module Type Quick Reference

| Need | Module Type |
|------|-------------|
| HTTP server | ` + "`http.server`" + ` |
| HTTP routing | ` + "`http.router`" + ` |
| HTTP handler | ` + "`http.handler`" + ` |
| PostgreSQL | ` + "`database.postgres`" + ` |
| SQLite | ` + "`storage.sqlite`" + ` |
| Redis | ` + "`cache.redis`" + ` |
| JWT auth | ` + "`auth.jwt`" + ` |
| NATS messaging | ` + "`eventbus.nats`" + ` |
| Kafka messaging | ` + "`messaging.kafka`" + ` |
| Static files | ` + "`static.fileserver`" + ` |
| Cron scheduler | ` + "`scheduler`" + ` |
| GitHub webhook | ` + "`git.webhook`" + ` |

---

## Common Patterns

### REST API with Auth

` + "```yaml" + `
modules:
  - name: server
    type: http.server
    config:
      port: 8080
  - name: router
    type: http.router
    config:
      server: server
  - name: db
    type: database.postgres
    config:
      dsn: "${DATABASE_URL}"
  - name: auth
    type: auth.jwt
    config:
      signingKey: "${JWT_SECRET}"
      algorithm: HS256
` + "```" + `

### Background Worker

` + "```yaml" + `
modules:
  - name: broker
    type: eventbus.nats
    config:
      url: "${NATS_URL}"
  - name: db
    type: database.postgres
    config:
      dsn: "${DATABASE_URL}"
` + "```" + `

---

## Validation Checklist

Before deploying a workflow config, verify:

1. ` + "`validate_config`" + ` — no structural errors
2. ` + "`validate_template_expressions`" + ` — no undefined step references
3. ` + "`detect_secrets`" + ` — no hardcoded credentials
4. ` + "`detect_ports`" + ` — port conflicts resolved
5. ` + "`diff_configs`" + ` (if updating) — no unexpected breaking changes
`

// registerSetupGuideResource registers the workflow://docs/setup-guide MCP resource.
func (s *Server) registerSetupGuideResource() {
	s.mcpServer.AddResource(
		mcp.NewResource(
			"workflow://docs/setup-guide",
			"Workflow Setup Guide for AI Assistants",
			mcp.WithResourceDescription("Decision trees and step-by-step guidance for AI assistants "+
				"helping users configure workflow applications: bootstrap flow, infrastructure setup, "+
				"CI/CD integration, secrets management, and common module patterns."),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleSetupGuide,
	)
}

// handleSetupGuide serves the setup guide resource.
func (s *Server) handleSetupGuide(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     setupGuideContent,
		},
	}, nil
}
