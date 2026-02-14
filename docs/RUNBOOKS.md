# Workflow Engine Runbooks

Operational runbooks for responding to alerts from the workflow engine.

## HighErrorRate

**Alert:** More than 5% of workflow executions are failing.

### Investigation

1. Check which workflow types are failing:
   ```promql
   sum(rate(workflow_executions_total{status="error"}[5m])) by (workflow_type)
   ```

2. Check application logs for error details:
   ```bash
   kubectl logs -l app=workflow --tail=200 | grep -i error
   ```

3. Check if a recent deployment or config change was made:
   ```bash
   kubectl rollout history deployment/workflow
   ```

### Resolution

- If a specific workflow type is failing, check its configuration in the YAML config.
- If all workflow types are affected, check shared dependencies (database, message broker, external APIs).
- If caused by a bad deployment, roll back:
  ```bash
  kubectl rollout undo deployment/workflow
  ```

---

## SlowWorkflows

**Alert:** Workflow p99 latency exceeds 5 seconds.

### Investigation

1. Identify which workflow types are slow:
   ```promql
   histogram_quantile(0.99, sum(rate(workflow_duration_seconds_bucket[5m])) by (le, workflow_type))
   ```

2. Check for resource contention:
   ```promql
   process_resident_memory_bytes{job="workflow"}
   go_goroutines{job="workflow"}
   ```

3. Check downstream service health (database, message broker, external APIs).

### Resolution

- If a specific workflow type is slow, check for N+1 queries or missing database indexes.
- If system-wide, check for CPU/memory pressure and consider scaling up or out.
- Check if the event bus or message broker has a backlog.

---

## QueueBacklog

**Alert:** More than 100 workflows are active concurrently.

### Investigation

1. Check which workflow types are accumulating:
   ```promql
   active_workflows
   ```

2. Check if workflows are completing or getting stuck:
   ```promql
   rate(workflow_executions_total{status="success"}[5m])
   rate(workflow_executions_total{status="error"}[5m])
   ```

3. Check for deadlocks or blocked goroutines in logs.

### Resolution

- If workflows are stuck, check for external dependency failures (database timeouts, API outages).
- Scale horizontally if the workload is legitimate:
  ```bash
  kubectl scale deployment/workflow --replicas=3
  ```
- Check if triggers are producing events faster than the engine can consume them.

---

## ComponentTimeout

**Alert:** A module is reporting timeout errors.

### Investigation

1. Identify the affected module:
   ```promql
   sum(rate(module_operations_total{status="timeout"}[5m])) by (module)
   ```

2. For dynamic components, check recent hot-reload events:
   ```promql
   sum(rate(module_operations_total{module="dynamic", operation="reload"}[5m]))
   ```

3. Check the module's dependencies (external APIs, databases, etc.).

### Resolution

- If a dynamic component is timing out, check the component source code for blocking operations.
- Increase timeout configuration if the operations are legitimately slow.
- If an external dependency is the cause, check its health and consider circuit-breaking.

---

## HighMemoryUsage

**Alert:** Process memory usage exceeds 85% of available memory.

### Investigation

1. Check memory trends:
   ```promql
   process_resident_memory_bytes{job="workflow"}
   go_memstats_alloc_bytes{job="workflow"}
   go_memstats_heap_inuse_bytes{job="workflow"}
   ```

2. Check goroutine count for leaks:
   ```promql
   go_goroutines{job="workflow"}
   ```

3. Check for large numbers of active workflows or dynamic components.

### Resolution

- If memory is growing steadily, there may be a memory leak. Check for:
  - Goroutine leaks (goroutine count growing unbounded)
  - Dynamic component interpreter instances not being cleaned up
  - Large response bodies being held in memory
- Restart the pod as an immediate mitigation:
  ```bash
  kubectl rollout restart deployment/workflow
  ```
- Increase memory limits if the workload is legitimate:
  ```yaml
  resources:
    limits:
      memory: 512Mi
  ```

---

## HighHTTPErrorRate

**Alert:** More than 5% of HTTP requests are returning 5xx errors.

### Investigation

1. Check which endpoints are failing:
   ```promql
   sum(rate(http_requests_total{status_code=~"5.."}[5m])) by (path, method)
   ```

2. Check application logs for panic/error stack traces.

3. Check if the management API or the workflow engine HTTP server is affected.

### Resolution

- If specific endpoints are failing, check the corresponding handler code.
- If all endpoints are affected, check for a systemic issue (database down, out of memory).
- Check the health endpoints (`/healthz`, `/readyz`) for degradation indicators.

---

## HighHTTPLatency

**Alert:** HTTP p99 latency exceeds 2 seconds.

### Investigation

1. Identify slow endpoints:
   ```promql
   histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, path))
   ```

2. Check if the latency correlates with high traffic or resource usage.

3. Look for slow database queries or external API calls in the logs.

### Resolution

- Add caching for frequently accessed endpoints.
- Optimize slow database queries (add indexes, reduce N+1 patterns).
- Consider adding request timeouts if they are not already configured.
