# Reconfiguration Testing Report

Results of testing the distributed e-commerce application's resilience to
configuration changes, container restarts, and infrastructure failures.

## Critical Bug Found: Persistence Not Loaded on Restart

**Severity**: High — data loss on every restart

**Root Cause**: The `RESTAPIHandler` and `JWTAuthModule` looked up the
`persistence` service during `Init()`, but the modular framework initializes
modules alphabetically. Since `orders-api` and `auth` come before `persistence`
alphabetically, the persistence service wasn't registered yet when they tried
to find it. The lookup silently failed because persistence is declared as an
optional dependency (`Required: false`).

The framework correctly skips optional dependencies when building the
initialization dependency graph (see `application.go:1473`):

```go
if !svcDep.Required || svcDep.MatchByInterface {
    continue // Skip optional or interface-based dependencies
}
```

**Impact**: On every container restart, the in-memory store started empty.
Orders, users, and products were lost despite SQLite data being on a
Docker volume. New resource IDs reset to 1.

**Fix Applied**: Added late-binding in `Start()` for both modules. Since all
modules complete `Init()` before any `Start()` is called, the persistence
service is guaranteed to be in the registry by start time:

```go
// In RESTAPIHandler.Start() and JWTAuthModule.Start():
if h.persistence == nil && h.app != nil {
    var ps interface{}
    if err := h.app.GetService("persistence", &ps); err == nil && ps != nil {
        if store, ok := ps.(*PersistenceStore); ok {
            h.persistence = store
        }
    }
}
```

**Files changed**: `module/api_handlers.go`, `module/jwt_auth.go`

---

## Test Results

### Test 1: Container Restart (No Config Change)

| Aspect | Before Fix | After Fix |
|--------|-----------|-----------|
| Orders survive restart | NO (0 orders) | YES (all orders) |
| User accounts survive | NO | YES |
| ID counter continuity | Reset to 1 | Continues from last ID |
| New orders after restart | Work, but no history | Work with full history |

### Test 2: Remove Processing Step (Shipping)

Removed `shipping-service`, `step-shipping`, and all shipping-related states
(`shipping`, `shipped`, `ship_failed`) from the state machine definition.
Changed `paid` to auto-transition directly to `delivered`.

| Aspect | Result |
|--------|--------|
| Container starts | YES — healthy |
| Existing orders (old states) | Readable — `delivered` orders unaffected |
| New orders | Use shortened pipeline (paid → delivered) |
| Pipeline data | No tracking number/carrier (expected) |
| Crash on unknown states | NO — graceful |

**Key finding**: The state machine does NOT validate that persisted instances
have states matching the current definition. This is good for backwards
compatibility but means orphaned intermediate-state orders would be silently
stuck forever.

### Test 3: State Machine Modification

Tested as part of Test 2. Removing states and transitions from the definition
does not cause errors for existing data. The state machine is forward-compatible.

### Test 4: JWT Secret Mismatch

Tested by sending a token signed with the wrong secret to the orders endpoint.

| Aspect | Result |
|--------|--------|
| Good token | 200 OK |
| Bad token (wrong secret) | `Invalid credentials` (rejected) |
| No token | `Authorization header required` |

**Finding**: JWT validation correctly rejects mismatched tokens. If the orders
container were configured with a different `JWT_SECRET` than users-products,
all authenticated order operations would fail with clear error messages.

### Test 5: Gateway Proxy Target Down

Stopped the orders container while the gateway was running.

| Aspect | Result |
|--------|--------|
| Products API (users-products) | 200 OK (unaffected) |
| Auth API (users-products) | 200 OK (unaffected) |
| Orders GET | 502 Bad Gateway (empty body) |
| Orders POST | 502 Bad Gateway (empty body) |
| Gateway logs | `proxy error: dial tcp: lookup orders ... i/o timeout` |

**Finding**: Failures are isolated — one backend going down doesn't affect
others. However, the 502 response has an empty body, providing no diagnostic
information to the client.

### Test 6: Kafka Unavailable

Stopped the Kafka container while the orders container was running.

| Aspect | Result |
|--------|--------|
| Order creation | Succeeds |
| Pipeline completion | Succeeds (all states reached, including `delivered`) |
| Event publishing | Silently fails |
| Consumer error | Logged: `client has run out of available brokers` |
| Kafka recovery | Container reconnects after restart |

**Finding**: The order processing pipeline runs entirely in-process via the
state machine and auto-transitions. Kafka is used for event distribution to
external consumers (notifications), not for driving the core pipeline. This
makes the system resilient to Kafka outages but means event-driven side effects
(notifications, audit logs) will be missed during outages without any user-facing
indication.

---

## Summary of Issues Found

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | Data loss on restart (persistence init order) | High | **Fixed** |
| 2 | 502 empty body on proxy failure | Low | Noted |
| 3 | Kafka failures silent to users | Medium | By design |
| 4 | No validation of persisted states vs definition | Low | By design |
| 5 | Monolith prometheus target showing DOWN | Low | **Fixed** (earlier) |

## Recommendations

1. **Add health check that verifies persistence**: The `/healthz` endpoint
   should verify that persistence is wired and the database is accessible,
   not just that the HTTP server responds.

2. **Add Kafka health to readiness probe**: If Kafka is configured but
   unreachable, the readiness probe should report degraded status.

3. **Return JSON error body on proxy failure**: The SimpleProxy module should
   return `{"error": "backend unavailable", "service": "orders"}` instead
   of an empty 502.

4. **Warn on orphaned workflow instances**: On startup, if persisted instances
   have states not in the current definition, log a warning. This would catch
   configuration drift.

5. **Consider making persistence Required**: For production configs that
   include a database module, the persistence dependency should be Required
   rather than Optional to ensure proper init ordering without relying on
   late-binding.
