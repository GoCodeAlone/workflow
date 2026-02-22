# Deployment and Operations Guide

This guide covers building, deploying, configuring, and operating the Workflow Engine.

---

## Table of Contents

1. [Building the Application](#1-building-the-application)
2. [Running Modes](#2-running-modes)
3. [Configuration](#3-configuration)
4. [Secrets Management](#4-secrets-management)
5. [Database Configuration](#5-database-configuration)
6. [Docker Deployment](#6-docker-deployment)
7. [Kubernetes Deployment](#7-kubernetes-deployment)
8. [Monitoring and Observability](#8-monitoring-and-observability)
9. [Scaling](#9-scaling)
10. [Backup and Recovery](#10-backup-and-recovery)
11. [Troubleshooting](#11-troubleshooting)

---

## 1. Building the Application

The workflow engine is a Go binary with an embedded React UI. Building requires compiling the UI assets and then building the Go binary with those assets embedded.

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.25+ | Server binary |
| Node.js | 18+ | UI build |
| Docker | 24+ | Container builds (optional) |
| golangci-lint | latest | Go linting (development) |

### Step 1: Build the UI

```bash
cd ui
npm install
npm run build
```

This produces optimized static assets in `ui/dist/`.

### Step 2: Build the Server Binary

```bash
go build -o server ./cmd/server
```

For a smaller production binary:

```bash
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server
```

- `CGO_ENABLED=0` -- static binary, no libc dependency (required for Alpine containers)
- `-ldflags="-s -w"` -- strips debug symbols, ~30% smaller binary

### Docker Build

The multi-stage Dockerfile handles everything automatically:

```bash
docker build -t workflow .
```

The three stages are:
1. **node:22-alpine** -- `npm ci` and `npx vite build` for the UI
2. **golang:1.26-alpine** -- `go mod download`, copy UI assets, `go build`
3. **alpine:3.21** -- copies only the binary, adds CA certs and tzdata, runs as non-root (UID 65532)

Final image size is approximately 30MB.

### Complete Build Script

```bash
#!/bin/bash
set -euo pipefail

cd ui && npm ci --silent && npx vite build && cd ..
go fmt ./...
golangci-lint run
go test -race ./...
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server
```

---

## 2. Running Modes

### Application Mode

Runs a single workflow configuration:

```bash
./server -config workflow.yaml
```

The workflow engine listens on the address from the config's `http.server` module (default `:8080`). The admin UI is automatically available on port 8081.

### Admin Mode

Without a config file, the server runs the admin platform only:

```bash
./server
```

The admin UI (port 8081) provides a configuration editor, module schema browser, AI-assisted component generation, dynamic component management, and real-time engine status.

### Development Mode

Run backend and frontend separately for UI hot-reload:

```bash
# Terminal 1: Backend
./server -config example/order-processing-pipeline.yaml

# Terminal 2: UI dev server (proxies API to backend)
cd ui && npm run dev
```

The Vite dev server runs at `http://localhost:5173`.

### Environment Variable Overrides

Flags can be set via environment variables (used when the flag is not on the command line):

| Flag | Environment Variable | Default |
|------|---------------------|---------|
| `-config` | `WORKFLOW_CONFIG` | (none) |
| `-addr` | `WORKFLOW_ADDR` | `:8080` |
| `-anthropic-key` | `WORKFLOW_AI_API_KEY` | (none) |
| `-anthropic-model` | `WORKFLOW_AI_MODEL` | (none) |
| `-jwt-secret` | `WORKFLOW_JWT_SECRET` | (none) |
| `-data-dir` | `WORKFLOW_DATA_DIR` | `./data` |

### Other Flags

| Flag | Description |
|------|-------------|
| `-copilot-cli` | Path to Copilot CLI binary |
| `-copilot-model` | Model for Copilot SDK |
| `-database-dsn` | PostgreSQL DSN for multi-workflow mode |
| `-admin-email` | Bootstrap admin email (first run) |
| `-admin-password` | Bootstrap admin password (first run) |
| `-restore-admin` | Restore admin config to embedded default |

---

## 3. Configuration

### YAML Config Structure

Every config file has three required top-level sections plus an optional `pipelines` section:

```yaml
modules:
  - name: httpServer
    type: http.server
    config:
      address: ":8080"

  - name: httpRouter
    type: http.router
    dependsOn:
      - httpServer

  - name: userHandler
    type: http.handler
    dependsOn:
      - httpRouter
    config:
      contentType: "application/json"

workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: userHandler

triggers:
  http:
    - name: api-trigger
      type: http
      workflow: http

pipelines:  # optional
  order-pipeline:
    steps:
      - name: validate
        type: step.validate
        config:
          rules:
            - field: email
              rule: required
```

Each module has a `name`, `type`, optional `config` map, and optional `dependsOn` list for initialization ordering.

### Environment Variable Expansion

Config values support `${VAR}` references resolved at startup:

```yaml
config:
  driver: postgres
  dsn: "${DATABASE_URL}"
```

Supported patterns:
- `${VAR_NAME}` -- environment variable (backward-compatible)
- `${env:VAR_NAME}` -- explicit env resolution
- `${vault:path/to/secret#field}` -- HashiCorp Vault
- `${aws-sm:secret-name#field}` -- AWS Secrets Manager

Unresolvable references preserve the original `${...}` string.

### Config Validation

```bash
curl -X POST http://localhost:8081/api/v1/admin/engine/validate \
  -H "Content-Type: application/yaml" \
  --data-binary @workflow.yaml
```

### Config Reload

Apply a new configuration without restarting:

```bash
curl -X POST http://localhost:8081/api/v1/admin/engine/reload \
  -H "Content-Type: application/yaml" \
  --data-binary @updated-workflow.yaml
```

The reload stops the current engine, builds and starts a new one, and rolls back on failure. In-flight requests may be dropped -- use Kubernetes rolling deployments for zero-downtime updates in production.

### Example Configurations

The `example/` directory contains 36+ working configs. Run any of them:

```bash
./server -config example/order-processing-pipeline.yaml
./server -config example/chat-platform/api-gateway.yaml
./server -config example/ecommerce-app/order-service.yaml
```

---

## 4. Secrets Management

The engine includes a multi-provider secrets system using a `MultiResolver` that dispatches `${...}` references based on URI scheme.

### Environment Variables (Default)

```yaml
config:
  secretKey: "${JWT_SECRET}"
```

```bash
export JWT_SECRET="my-signing-key"
./server -config workflow.yaml
```

The `env` provider is registered by default. Keys are case-sensitive.

### File Provider (Kubernetes Secrets)

Reads secrets from a directory where each filename is the key and content is the value. Compatible with Kubernetes Secret volume mounts:

```yaml
volumes:
  - name: secrets
    secret:
      secretName: workflow-secrets
volumeMounts:
  - name: secrets
    mountPath: /etc/secrets
    readOnly: true
```

### HashiCorp Vault

Uses the Vault HTTP API with token-based authentication:

```yaml
config:
  dsn: "${vault:secret/data/myapp#database_url}"
  apiKey: "${vault:secret/data/myapp#api_key}"
```

Format: `${vault:path#field}` -- `path` is the Vault secret path, `field` is an optional JSON field.

Provider configuration (programmatic):

```go
vaultProvider, err := secrets.NewVaultProviderHTTP(secrets.VaultConfig{
    Address:   "https://vault.example.com:8200",
    Token:     os.Getenv("VAULT_TOKEN"),
    MountPath: "secret",       // KV v2 mount (default)
    Namespace: "my-namespace", // Enterprise (optional)
})
engine.SecretsResolver().Register("vault", vaultProvider)
```

For production, use AppRole authentication:

```bash
export VAULT_TOKEN=$(vault write -field=token auth/approle/login \
  role_id="$VAULT_ROLE_ID" secret_id="$VAULT_SECRET_ID")
```

### AWS Secrets Manager

Uses the HTTP API with AWS Signature V4 signing (no AWS SDK dependency):

```yaml
config:
  dsn: "${aws-sm:prod/database#url}"
```

Format: `${aws-sm:secret-name#field}`. Falls back to `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` environment variables. For EKS, use IRSA service accounts instead of static credentials.

### Multi-Provider Example

Mix providers in a single config:

```yaml
config:
  logLevel: "${LOG_LEVEL}"                         # env
  dbPassword: "${vault:secret/data/myapp#db_pass}" # Vault
  stripeKey: "${aws-sm:prod/stripe#api_key}"       # AWS
```

Each reference is dispatched to its scheme's provider independently.

### Security Best Practices

1. Never commit secrets in YAML configs -- use `${...}` references
2. Use short-lived credentials (AppRole with TTL, IAM roles)
3. Restrict Vault provider to `read` capability on configured paths
4. Enable Vault audit logging and AWS CloudTrail
5. Rotate secrets via config reload API without full restarts

---

## 5. Database Configuration

### SQLite (Default, Development)

```bash
./server -data-dir ./data
```

Creates `./data/workflow.db` automatically using `modernc.org/sqlite` (pure Go, no CGO).

```yaml
modules:
  - name: my-database
    type: database.workflow
    config:
      driver: sqlite
      dsn: "./data/workflow.db"
      maxOpenConns: 1    # SQLite supports one writer
```

SQLite does not support multiple writers. Do not run multiple replicas with SQLite.

### PostgreSQL (Production)

```yaml
modules:
  - name: my-database
    type: database.workflow
    config:
      driver: postgres
      dsn: "${DATABASE_URL}"
      maxOpenConns: 25
      maxIdleConns: 10
      connMaxLifetime: "30m"
```

```bash
export DATABASE_URL="postgres://user:password@host:5432/workflow?sslmode=require"
```

### Schema Management

The engine auto-creates tables on startup. Migrations are idempotent and transactional. The system hierarchy (Company -> Org -> Project -> Workflow) is created automatically.

### Connection Pooling

For production, use PgBouncer between the application and PostgreSQL:

```ini
[pgbouncer]
pool_mode = transaction
default_pool_size = 100
```

Point the engine at PgBouncer with `sslmode=disable` (TLS terminates at PgBouncer).

---

## 6. Docker Deployment

### Building and Running

```bash
docker build -t workflow:latest .

# Run with a config file
docker run -p 8080:8080 -p 8081:8081 \
  -v $(pwd)/workflow.yaml:/etc/workflow/config.yaml:ro \
  workflow:latest -config /etc/workflow/config.yaml

# Run with environment variables
docker run -p 8080:8080 -p 8081:8081 \
  -e WORKFLOW_CONFIG=/etc/workflow/config.yaml \
  -e JWT_SECRET=my-secret \
  -v $(pwd)/workflow.yaml:/etc/workflow/config.yaml:ro \
  -v workflow-data:/data \
  workflow:latest
```

### Docker Compose

```yaml
version: "3.8"
services:
  workflow:
    build: .
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      WORKFLOW_CONFIG: /etc/workflow/config.yaml
      WORKFLOW_JWT_SECRET: "${JWT_SECRET:-dev-secret}"
      DATABASE_URL: "postgres://workflow:workflow@postgres:5432/workflow?sslmode=disable"
    volumes:
      - ./workflow.yaml:/etc/workflow/config.yaml:ro
      - workflow-data:/data
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: workflow
      POSTGRES_PASSWORD: workflow
      POSTGRES_DB: workflow
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U workflow"]
      interval: 5s
      retries: 5

  prometheus:
    image: prom/prometheus:v2.51.0
    ports:
      - "9090:9090"
    volumes:
      - ./deploy/prometheus/alerts.yml:/etc/prometheus/alerts.yml:ro
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro

  grafana:
    image: grafana/grafana:11.0.0
    ports:
      - "3000:3000"
    volumes:
      - ./deploy/grafana:/etc/grafana/provisioning/dashboards/json:ro

volumes:
  workflow-data:
  postgres-data:
```

Create `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
rule_files:
  - /etc/prometheus/alerts.yml
scrape_configs:
  - job_name: workflow
    static_configs:
      - targets: ["workflow:8081"]
    metrics_path: /metrics
```

```bash
docker compose up -d
```

### Container Security

```bash
docker run --read-only \
  --tmpfs /tmp:rw,noexec,nosuid \
  -v workflow-data:/data \
  --security-opt=no-new-privileges \
  --cap-drop=ALL \
  workflow:latest -config /etc/workflow/config.yaml
```

The image runs as UID 65532 (nonroot) with a read-only root filesystem.

---

## 7. Kubernetes Deployment

### Helm Chart

The chart is at `deploy/helm/workflow/` (version 0.5.0).

```bash
helm install my-workflow deploy/helm/workflow/ \
  --namespace workflow --create-namespace \
  -f my-values.yaml
```

### Key Values

```yaml
replicaCount: 1
image:
  repository: ghcr.io/gocodealong/workflow
  tag: ""  # defaults to appVersion
mode: monolith  # or "distributed" (requires Kafka)

service:
  type: ClusterIP
  port: 8080       # workflow engine
  mgmtPort: 8081   # admin UI + metrics

resources:
  limits:   { cpu: 500m, memory: 256Mi }
  requests: { cpu: 100m, memory: 128Mi }

autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 10
  targetCPUUtilizationPercentage: 80

config:
  inline: ""   # workflow YAML (mounted as ConfigMap)
  path: ""     # or path to existing config file

envFromSecret: ""  # Kubernetes Secret name for env vars

monitoring:
  enabled: false
  serviceMonitor:
    enabled: false
    interval: 30s
```

### Production Values Example

```yaml
replicaCount: 3
image:
  tag: "0.5.0"
resources:
  limits:   { cpu: "1", memory: 512Mi }
  requests: { cpu: 250m, memory: 256Mi }
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: workflow.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: workflow-tls
      hosts: [workflow.example.com]
config:
  inline: |
    modules:
      - name: httpServer
        type: http.server
        config:
          address: ":8080"
      - name: my-database
        type: database.workflow
        config:
          driver: postgres
          dsn: "${DATABASE_URL}"
envFromSecret: workflow-secrets
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 15s
    labels:
      release: prometheus
```

### Creating Secrets

```bash
kubectl create secret generic workflow-secrets \
  --namespace workflow \
  --from-literal=JWT_SECRET="$(openssl rand -base64 32)" \
  --from-literal=DATABASE_URL="postgres://user:pass@postgres:5432/workflow?sslmode=require"
```

### Health Probes

The deployment configures probes automatically:

| Endpoint | Purpose | Probe |
|----------|---------|-------|
| `/healthz` | Process is running | Liveness (initialDelay: 10s, period: 15s) |
| `/readyz` | All modules ready | Readiness (initialDelay: 5s, period: 10s) |
| `/livez` | Process responsive | Liveness (alternative) |

These paths are configurable via the `observability.health` module.

### ConfigMap and Rolling Updates

When `config.inline` is set, the chart creates a ConfigMap mounted at `/etc/workflow/workflow.yaml`. A `checksum/config` pod annotation triggers rolling updates when the config changes.

### ServiceMonitor

When monitoring is enabled, a ServiceMonitor scrapes `/metrics` on the management port (8081).

### Upgrades and Rollbacks

```bash
helm upgrade workflow deploy/helm/workflow/ -n workflow -f production-values.yaml
kubectl rollout status deployment/workflow -n workflow
helm rollback workflow -n workflow          # previous release
helm rollback workflow 3 -n workflow        # specific revision
```

---

## 8. Monitoring and Observability

### Prometheus Metrics

Exposed at `/metrics` on port 8081:

| Metric | Type | Description |
|--------|------|-------------|
| `workflow_executions_total` | Counter | Executions by type and status |
| `workflow_duration_seconds` | Histogram | Execution duration |
| `active_workflows` | Gauge | Currently active workflows |
| `http_requests_total` | Counter | Requests by method, path, status |
| `http_request_duration_seconds` | Histogram | Request latency |
| `module_operations_total` | Counter | Operations by module and status |

Configure via the metrics module:

```yaml
modules:
  - name: metrics
    type: observability.metrics
    config:
      namespace: workflow
      metricsPath: /metrics
      enabledMetrics: [workflow, http, module, active_workflows]
```

### Prometheus Alerts

Pre-configured alerts at `deploy/prometheus/alerts.yml`:

| Alert | Severity | Condition |
|-------|----------|-----------|
| `HighErrorRate` | critical | > 5% workflow failures for 5m |
| `SlowWorkflows` | warning | p99 > 5s for 10m |
| `QueueBacklog` | warning | > 100 active workflows for 5m |
| `ComponentTimeout` | warning | Module timeouts > 0.1/s for 5m |
| `HighMemoryUsage` | critical | Memory > 85% for 10m |
| `HighHTTPErrorRate` | critical | > 5% HTTP 5xx for 5m |
| `HighHTTPLatency` | warning | HTTP p99 > 2s for 10m |

Each alert links to `docs/RUNBOOKS.md` with investigation and resolution steps.

### Grafana Dashboards

Three dashboards in `deploy/grafana/`:

| File | Purpose |
|------|---------|
| `workflow-overview.json` | Execution rates, latency, errors, resources |
| `chat-platform.json` | Chat-specific metrics |
| `dynamic-components.json` | Hot-reload component metrics |

### OpenTelemetry Tracing

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://jaeger:4318"
export OTEL_SERVICE_NAME="workflow-engine"
```

### Log Collection

Structured logs go to stdout via Go's `slog` (text format by default, DEBUG level):

```
time=2025-01-15T10:30:00Z level=INFO msg="Admin UI on http://localhost:8081"
time=2025-01-15T10:30:00Z level=INFO msg="Workflow engine on :8080"
```

---

## 9. Scaling

### Single-Instance

SQLite + in-memory event bus + in-process state. Suitable for up to ~100 concurrent workflows.

### Horizontal Scaling

```
Load Balancer ---> workflow-1 --+
               |-> workflow-2 --+--> PostgreSQL
               |-> workflow-3 --+
```

Requirements:
1. PostgreSQL (shared state)
2. External message broker (NATS or Kafka) instead of in-memory
3. Shared JWT secret across instances
4. Sticky sessions are NOT required

### HPA

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 75
```

### Message Broker Scaling

| Size | Broker | Notes |
|------|--------|-------|
| 1 pod | In-memory | Default |
| 2-5 pods | NATS | `messaging.nats` module, jetstream |
| 5+ pods | Kafka | `messaging.kafka` module, partitioned topics |

### Current Limitations

1. **State machine locking is in-process.** Go mutexes only work within one process. Planned fix: PostgreSQL advisory locks.
2. **In-memory event bus is default.** Does not sync across processes.
3. **Dynamic components are per-process.** Each replica loads its own copy.
4. **Config reload is per-instance.** Use rolling deployments to update all replicas.

### Resource Sizing

| Workload | CPU | Memory | Replicas |
|----------|-----|--------|----------|
| Development | 100m | 128Mi | 1 |
| Light (< 50 req/s) | 250m | 256Mi | 2 |
| Medium (50-500 req/s) | 500m | 512Mi | 3-5 |
| Heavy (500+ req/s) | 1000m | 1Gi | 5-10 |

---

## 10. Backup and Recovery

### PostgreSQL Backups

```bash
# Backup
pg_dump -h postgres -U workflow -d workflow -F custom -f backup-$(date +%Y%m%d).dump

# Restore
pg_restore -h postgres -U workflow -d workflow -c backup-20250115.dump
```

Automated via Kubernetes CronJob:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: workflow-db-backup
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: backup
              image: postgres:16-alpine
              command: ["/bin/sh", "-c"]
              args:
                - pg_dump -h $DB_HOST -U $DB_USER -d workflow -F custom
                  -f /backups/workflow-$(date +%Y%m%d-%H%M%S).dump
              env:
                - name: PGPASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: workflow-secrets
                      key: DB_PASSWORD
              volumeMounts:
                - { name: backups, mountPath: /backups }
          volumes:
            - name: backups
              persistentVolumeClaim:
                claimName: workflow-backups
          restartPolicy: OnFailure
```

### SQLite Backups

```bash
sqlite3 data/workflow.db ".backup backups/workflow-$(date +%Y%m%d).db"
```

### Config Version History

```bash
curl http://localhost:8081/api/v1/admin/workflows                          # list
curl http://localhost:8081/api/v1/admin/workflows/{id}/versions/{version}  # get version
```

### Rollback Procedures

**Workflow configuration:**

```bash
curl http://localhost:8081/api/v1/admin/workflows/{id}/versions/{prev} > rollback.yaml
curl -X POST http://localhost:8081/api/v1/admin/engine/reload \
  -H "Content-Type: application/yaml" --data-binary @rollback.yaml
```

**Kubernetes deployment:**

```bash
kubectl rollout undo deployment/workflow -n workflow
```

**Helm release:**

```bash
helm rollback workflow 5 -n workflow
```

### Disaster Recovery Checklist

1. Database backup verified -- restore to test environment monthly
2. Configuration stored in version control (not only in ConfigMap)
3. Secrets rotatable without data loss
4. Helm values in version control
5. Runbook accessible to on-call engineers

---

## 11. Troubleshooting

### Common Startup Issues

**Port in use:**
```bash
lsof -i :8080
./server -config workflow.yaml -addr :9090  # use alternate port
```

**Missing config file:**
```bash
./server -config /absolute/path/to/config.yaml
```

**Database permissions (Docker):**
```bash
# Named volumes work automatically; bind mounts may need:
chown -R 65532:65532 ./data
```

**Module dependency failure:**
```
failed to build workflow: module 'userHandler' depends on 'httpRouter' which is not defined
```
Check for typos in module names (case-sensitive). Validate before deploying:
```bash
curl -X POST http://localhost:8081/api/v1/admin/engine/validate \
  -H "Content-Type: application/yaml" --data-binary @workflow.yaml
```

**JWT secret not configured:**
```bash
export WORKFLOW_JWT_SECRET="your-secret-key"
# or: kubectl create secret generic workflow-secrets --from-literal=JWT_SECRET="$(openssl rand -base64 32)"
```

### Checking Logs

```bash
# Kubernetes
kubectl logs -l app=workflow -n workflow --tail=100
kubectl logs -l app=workflow -n workflow -f                        # follow
kubectl logs -l app=workflow -n workflow --tail=500 | grep "level=ERROR"
kubectl logs workflow-xxxxx -n workflow --previous                 # after crash
```

Key log messages:
- `msg="Admin UI on http://localhost:8081"` -- successful startup
- `msg="System hierarchy ready"` -- database initialized
- `msg="Registered Anthropic AI provider"` -- AI configured
- `msg="Engine shutdown error"` -- investigate

### Pipeline Debugging

Insert `step.log` to inspect data between pipeline steps:

```yaml
steps:
  - name: debug
    type: step.log
    config:
      message: "After validation"
      level: debug
```

This outputs the full pipeline context to the structured logger.

### Admin Dashboard

Access the admin UI at `http://localhost:8081` (or via port-forward):

```bash
kubectl port-forward svc/workflow 8081:8081 -n workflow
```

Provides: engine status, module browser, config editor, dynamic components, AI integration.

### Common Runtime Issues

**OOM Kill:**
```bash
kubectl describe pod workflow-xxxxx -n workflow | grep -A5 "Last State"
```
Increase memory limits. Monitor `go_goroutines` for leaks. See `docs/RUNBOOKS.md#highmemoryusage`.

**Stuck workflows:** Monitor `active_workflows` metric. Check external dependencies. See `docs/RUNBOOKS.md#queuebacklog`.

**Config reload fails:** Validate first, check logs for the specific error. Reload is atomic -- the old engine keeps running on failure.

### Getting Help

1. `docs/RUNBOOKS.md` -- investigation and resolution for all alerts
2. `example/` -- 36+ working configurations
3. `GET /api/v1/module-schemas` -- full schema for every module type
4. Debug logging is on by default (`slog.LevelDebug`)

---

## Quick Reference

### Ports

| Port | Service |
|------|---------|
| 8080 | Workflow engine (routes, APIs, triggers) |
| 8081 | Admin UI, metrics, management API |

### Key Endpoints

| Endpoint | Port | Method | Purpose |
|----------|------|--------|---------|
| `/healthz` | 8080 | GET | Liveness probe |
| `/readyz` | 8080 | GET | Readiness probe |
| `/livez` | 8080 | GET | Liveness (alt) |
| `/metrics` | 8081 | GET | Prometheus metrics |
| `/api/v1/admin/engine/status` | 8081 | GET | Engine status |
| `/api/v1/admin/engine/validate` | 8081 | POST | Validate config |
| `/api/v1/admin/engine/reload` | 8081 | POST | Reload config |
| `/api/v1/module-schemas` | 8081 | GET | Module schemas |
| `/api/dynamic/components` | 8081 | ALL | Dynamic components |

### File Locations

| Path (container) | Purpose |
|------------------|---------|
| `/app/server` | Binary |
| `/etc/workflow/workflow.yaml` | Config (from ConfigMap) |
| `/data/` | SQLite DB / persistent data |

| Path (development) | Purpose |
|--------------------|---------|
| `cmd/server/` | Entry point |
| `ui/` | React UI source |
| `ui/dist/` | Built UI assets (generated by `npm run build`) |
| `data/` | SQLite DB (default) |
| `example/` | Example configs |
| `deploy/helm/workflow/` | Helm chart |
| `deploy/grafana/` | Grafana dashboards |
| `deploy/prometheus/` | Alert rules |
| `docs/RUNBOOKS.md` | Operational runbooks |
