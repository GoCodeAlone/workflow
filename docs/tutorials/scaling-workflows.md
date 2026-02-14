# Scaling Workflows

## Overview

The workflow engine provides several mechanisms for production-scale deployment: worker pools, tenant quotas, multi-region routing, caching, and observability.

## Worker Pools

Configure the worker pool for concurrent workflow execution:

```go
import "github.com/GoCodeAlone/workflow/scale"

pool := scale.NewWorkerPool(scale.WorkerPoolConfig{
    MinWorkers:  4,
    MaxWorkers:  32,
    QueueSize:   1000,
    IdleTimeout: 30 * time.Second,
})
pool.Start()
defer pool.Stop()
```

Benchmarks show ~28K workflows/second with 10 workers.

## Consistent Hashing

Partition workflows across shards by tenant or conversation ID:

```go
hash := scale.NewConsistentHash(100) // 100 virtual nodes
hash.AddNode("shard-1")
hash.AddNode("shard-2")

target := hash.GetNode("tenant-123") // Consistent routing
```

## Per-Tenant Quotas

Enforce resource limits per tenant:

```go
import "github.com/GoCodeAlone/workflow/tenant"

registry := tenant.NewQuotaRegistry()
registry.SetQuota("tenant-1", tenant.TenantQuota{
    MaxWorkflowsPerMinute:    100,
    MaxConcurrentWorkflows:   10,
    MaxStorageBytes:          1 << 30, // 1GB
    MaxAPIRequestsPerMinute:  1000,
})
```

Use the middleware for HTTP enforcement:

```go
enforcer := tenant.NewQuotaEnforcer(registry)
mux.Handle("/api/", enforcer.Middleware(apiHandler))
```

## Caching

LRU cache with TTL for hot data:

```go
import "github.com/GoCodeAlone/workflow/cache"

c := cache.NewCacheLayer(cache.CacheConfig{
    MaxSize:    10000,
    DefaultTTL: 5 * time.Minute,
})

// Cache-aside pattern
value, err := c.GetOrSet("key", func() (interface{}, error) {
    return fetchFromDB("key")
})
```

## Kubernetes Deployment

Deploy with Helm:

```bash
helm install workflow deploy/helm/workflow/ \
  --set image.tag=latest \
  --set replicas=3 \
  --set monitoring.enabled=true
```

## Observability

- **Metrics**: Prometheus at /metrics
- **Tracing**: OpenTelemetry with OTLP export
- **Dashboards**: Pre-built Grafana dashboards in deploy/grafana/
- **Alerts**: Prometheus rules in deploy/prometheus/alerts.yml
- **SLA**: GET /api/sla/status for uptime and error budget tracking

## Environment Promotion

Promote configs through environments:

```bash
# Deploy to staging
curl -X POST http://localhost:8081/api/promote \
  -d '{"workflow": "my-app", "from": "dev", "to": "staging"}'

# Promote to prod (requires approval)
curl -X POST http://localhost:8081/api/promote \
  -d '{"workflow": "my-app", "from": "staging", "to": "prod"}'
```
