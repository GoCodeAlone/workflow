# Tier 3 Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `wfctl dev` for local development clusters (docker-compose, process, minikube modes), local exposure via Tailscale/Cloudflare Tunnel, and a Bubbletea TUI wizard for interactive setup.

**Architecture:** `wfctl dev up` reads the workflow config (single-service or multi-service), generates a docker-compose.dev.yml or k8s manifests, starts infrastructure + services, and sets up networking. Exposure integrations shell out to `tailscale`/`cloudflared` CLIs. The TUI wizard wraps the MCP scaffold tool logic in a Bubbletea interactive form.

**Tech Stack:** Go 1.26, Docker Compose, minikube, Tailscale CLI, Cloudflare Tunnel CLI, charmbracelet/bubbletea v2

**Design Doc:** `docs/plans/2026-03-28-platform-vision-design.md` (Feature 5 + Tier 3 items)

---

### Task 1: wfctl dev up — docker-compose mode

**Files:**
- Create: `cmd/wfctl/dev.go`
- Create: `cmd/wfctl/dev_compose.go`
- Create: `cmd/wfctl/dev_test.go`
- Modify: `cmd/wfctl/main.go` — register "dev" command

`wfctl dev` subcommands: `up`, `down`, `logs`, `status`, `restart`.

`wfctl dev up` (default docker-compose mode):
1. Parse workflow config
2. Detect infrastructure modules (database.postgres → postgres:16 image, nosql.redis → redis:latest, messaging.nats → nats:latest)
3. If `services:` section present, generate per-service entries; otherwise single service
4. Generate `docker-compose.dev.yml` with:
   - Infrastructure containers (postgres, redis, nats, etc.)
   - Service containers (build from Dockerfile or binary)
   - Port mappings from `expose:` config
   - Environment variables from `environments.local`
   - Volume mounts for persistent data
5. Run `docker compose -f docker-compose.dev.yml up -d`
6. Wait for health checks
7. Print service URLs

```go
// cmd/wfctl/dev.go
func runDev(args []string) error {
    if len(args) < 1 { return devUsage() }
    switch args[0] {
    case "up":     return runDevUp(args[1:])
    case "down":   return runDevDown(args[1:])
    case "logs":   return runDevLogs(args[1:])
    case "status": return runDevStatus(args[1:])
    case "restart": return runDevRestart(args[1:])
    default:       return devUsage()
    }
}
```

```go
// cmd/wfctl/dev_compose.go
// moduleToDockerImage maps workflow module types to Docker images
var moduleToDockerImage = map[string]string{
    "database.postgres": "postgres:16",
    "database.workflow": "postgres:16",
    "nosql.redis":       "redis:7-alpine",
    "cache.redis":       "redis:7-alpine",
    "messaging.nats":    "nats:latest",
    "messaging.kafka":   "confluentinc/cp-kafka:latest",
}

func generateDevCompose(cfg *config.WorkflowConfig) (string, error) {
    // Build compose YAML from config
}
```

`wfctl dev down`: runs `docker compose -f docker-compose.dev.yml down`
`wfctl dev logs [--service name]`: runs `docker compose logs [-f] [service]`
`wfctl dev status`: runs `docker compose ps` + health check each service

Run: `go test ./cmd/wfctl/ -run TestDev -v`
Commit: `feat: wfctl dev up/down/logs/status — docker-compose mode`

---

### Task 2: wfctl dev up — process mode

**Files:**
- Create: `cmd/wfctl/dev_process.go`
- Modify: `cmd/wfctl/dev.go` — add `--local` flag

`wfctl dev up --local` runs services as local Go processes:
1. Infrastructure deps still run as Docker containers (postgres, redis, nats)
2. Application services compile and run as local processes
3. Hot-reload: watches Go files with `fsnotify`, rebuilds + restarts on change
4. Log multiplexing: prefixes each service's stdout with `[service-name]` in color

```go
// cmd/wfctl/dev_process.go
func runDevProcess(cfg *config.WorkflowConfig, verbose bool) error {
    // 1. Start infrastructure via docker compose (just the infra containers)
    // 2. For each service, compile the binary
    // 3. Start each binary as a subprocess with env vars
    // 4. Set up file watcher for hot-reload
    // 5. Multiplex stdout/stderr with colored prefixes
}
```

Run: `go test ./cmd/wfctl/ -run TestDevProcess -v`
Commit: `feat: wfctl dev up --local — process mode with hot-reload`

---

### Task 3: wfctl dev up — minikube mode

**Files:**
- Create: `cmd/wfctl/dev_k8s.go`
- Modify: `cmd/wfctl/dev.go` — add `--k8s` flag

`wfctl dev up --k8s` deploys to local minikube:
1. Verify minikube is running (`minikube status`)
2. Build container images and load into minikube (`minikube image load`)
3. Generate k8s manifests (reuse `generateK8sManifests` from deploy_providers.go)
4. Apply manifests to a `dev` namespace
5. Set up port-forwards for exposed services
6. Print service URLs

```go
func runDevK8s(cfg *config.WorkflowConfig, verbose bool) error {
    // Check minikube running
    // Build + load images: eval $(minikube docker-env) && docker build
    // Apply manifests to dev namespace
    // Port-forward exposed services
}
```

`wfctl dev down --k8s`: deletes the dev namespace
`wfctl dev status --k8s`: shows pod status + port-forward health

Run: `go test ./cmd/wfctl/ -run TestDevK8s -v`
Commit: `feat: wfctl dev up --k8s — minikube mode`

---

### Task 4: Local exposure — Tailscale, Cloudflare Tunnel, ngrok

**Files:**
- Create: `cmd/wfctl/dev_expose.go`
- Create: `cmd/wfctl/dev_expose_test.go`
- Modify: `cmd/wfctl/dev.go` — add `--expose` flag

`wfctl dev up --expose tailscale` exposes services via Tailscale Funnel:
```go
func exposeTailscale(services []ExposedService, tsCfg *config.TailscaleConfig) error {
    // For each service with exposed ports:
    // tailscale funnel --bg <port>
    // Print: https://<hostname>.ts.net
}
```

`wfctl dev up --expose cloudflare` exposes via Cloudflare Tunnel:
```go
func exposeCloudflare(services []ExposedService, cfCfg *config.CloudflareTunnelConfig) error {
    // cloudflared tunnel --url http://localhost:<port>
    // Print the tunnel URL
}
```

`wfctl dev up --expose ngrok`:
```go
func exposeNgrok(services []ExposedService) error {
    // ngrok http <port>
    // Parse the public URL from ngrok API
}
```

Auto-detect from `environments.local.exposure.method` if `--expose` not specified.

Run: `go test ./cmd/wfctl/ -run TestExpose -v`
Commit: `feat: wfctl dev --expose — Tailscale Funnel, Cloudflare Tunnel, ngrok`

---

### Task 5: Bubbletea TUI wizard

**Files:**
- Create: `cmd/wfctl/wizard.go`
- Create: `cmd/wfctl/wizard_models.go`
- Create: `cmd/wfctl/wizard_test.go`
- Modify: `cmd/wfctl/main.go` — register "init" with `--wizard` flag or separate "wizard" command

Interactive TUI using Bubbletea v2 that walks users through project setup:

```go
// cmd/wfctl/wizard.go
func runWizard(args []string) error {
    p := tea.NewProgram(newWizardModel())
    result, err := p.Run()
    // Write generated config to app.yaml
}
```

Wizard flow (Bubbletea screens):
1. **Project info**: name, description
2. **Services**: single or multi-service? How many? Names?
3. **Infrastructure**: database? cache? message queue? (checkboxes)
4. **Environments**: local, staging, production (checkboxes)
5. **Deployment**: provider per environment (dropdown)
6. **Secrets**: detect from chosen modules, configure provider
7. **CI/CD**: generate bootstrap? which platform?
8. **Review**: show generated YAML, confirm
9. **Write**: save to app.yaml + generate CI bootstrap

Each screen is a Bubbletea model with `Init()`, `Update()`, `View()`. Navigation: Enter to advance, Esc to go back, Tab to toggle options.

The wizard reuses the same logic as the MCP scaffold tools (shared functions in a `scaffold` package or direct calls to the MCP tool handlers).

Dependencies: `go get github.com/charmbracelet/bubbletea/v2@latest github.com/charmbracelet/lipgloss/v2@latest`

Run: `go build ./cmd/wfctl/` (TUI is hard to unit test — verify it compiles and starts)
Commit: `feat: wfctl wizard — Bubbletea TUI for interactive project setup`

---

### Task 6: Documentation + integration tests

**Files:**
- Modify: `docs/WFCTL.md` — add dev up/down/logs/status/restart, --local, --k8s, --expose, wizard
- Modify: `docs/dsl-reference.md` + embedded copy — ensure environments.local.exposure documented
- Modify: `CHANGELOG.md`
- Create: `cmd/wfctl/dev_integration_test.go` — integration test that generates compose YAML from a fixture config

Write an integration test that:
1. Creates a minimal workflow config with postgres + http.server
2. Calls `generateDevCompose()` → validates the output has postgres and app services
3. Validates port mappings match the config

Run: `go test ./cmd/wfctl/ -run TestDevIntegration -v`
Commit: `docs: wfctl dev + wizard commands, integration tests`

---

## Summary

| Task | Scope | Key Deliverable |
|------|-------|-----------------|
| 1 | CLI | wfctl dev up/down/logs/status — docker-compose generation |
| 2 | CLI | wfctl dev up --local — process mode with hot-reload |
| 3 | CLI | wfctl dev up --k8s — minikube mode |
| 4 | CLI | --expose flag: Tailscale Funnel, Cloudflare Tunnel, ngrok |
| 5 | CLI | Bubbletea TUI wizard for interactive project setup |
| 6 | Docs | WFCTL docs, integration tests, changelog |
