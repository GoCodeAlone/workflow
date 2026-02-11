# Gap Analysis: Example vs. Production E-Commerce System

This document compares what the e-commerce example application implements
against what a production e-commerce system would require. The goal is to
be transparent about the boundaries of the example while demonstrating that
the Workflow engine's architecture supports scaling to production.

---

## Feature Comparison

| Feature | This Example | Production System | Gap |
|---------|-------------|-------------------|-----|
| Database | SQLite (embedded, modernc.org/sqlite) | PostgreSQL cluster with read replicas | No replication, no connection pooling, single-file store |
| Message Broker | In-memory (`module/messaging.go`) | Kafka/RabbitMQ with persistence | Messages lost on restart, no dead letter queue, no ordering guarantees |
| Payment | Simulated (85% approve, random delays) | Stripe/PayPal with PCI DSS compliance | No real charge, no refunds, no webhooks, no idempotency keys |
| Shipping | Simulated (95% success, random tracking) | EasyPost/ShipStation with real carriers | No real labels, no tracking updates, no rate shopping |
| Email/SMS | Logged to stdout | SendGrid/SES/Twilio with delivery tracking | No actual delivery, no templates, no unsubscribe |
| Authentication | JWT with in-memory users (now with SQLite) | OAuth2/OIDC with external IdP (Auth0, Cognito) | No refresh tokens, no MFA, no password reset, no session management |
| Retry Logic | Simple exponential backoff (`processing.step`) | Circuit breaker + dead letter queue + exponential backoff with jitter | No circuit breaker, no DLQ, no jitter, no retry budget |
| Monitoring | Prometheus + Grafana (local Docker) | Distributed tracing (OpenTelemetry) + centralized logging (ELK/Datadog) | No distributed tracing, no log aggregation, no alerting rules |
| Scaling | Single process | Kubernetes with HPA, multiple replicas | No horizontal scaling, no load balancing, no session affinity |
| Secrets | Environment variables | HashiCorp Vault / AWS Secrets Manager | Secrets in env vars, no rotation, no audit logging |
| Rate Limiting | In-memory per-process | Distributed rate limiting (Redis-backed) | Rate limits reset on restart, no cross-instance coordination |
| Data Validation | Basic field presence checks | JSON Schema validation, input sanitization | No schema validation, minimal input validation |
| Error Recovery | Processing step retry + compensate | Saga pattern with compensating transactions | No full saga orchestration, no compensating transactions beyond state machine |
| Caching | None | Redis/Memcached for hot data | No caching layer, every read hits the store |
| Search | Sequential scan of in-memory data | Elasticsearch / database indexes | No search indexing, O(n) lookups |

---

## Detailed Gap Breakdown

### Database

**What we have**: SQLite via `modernc.org/sqlite` (pure Go, no CGo). The
database file lives at `./data/ecommerce.db`. All reads and writes happen
through a single connection.

**What production needs**:
- PostgreSQL or MySQL with connection pooling (pgxpool, sql.DB pool).
- Read replicas for query-heavy operations (product catalog, order history).
- Database migrations managed by a tool like `golang-migrate` or `atlas`.
- Automated backups with point-in-time recovery.
- Row-level locking or optimistic concurrency control for state transitions.

**Why it matters**: SQLite is single-writer. Under concurrent load, write
operations serialize on the database lock. A busy e-commerce site would see
latency spikes during high-order-volume periods.

### Message Broker

**What we have**: `module/messaging.go` implements a pub/sub broker using Go
channels. Messages exist only in memory and are lost if the process restarts.

**What production needs**:
- Kafka or RabbitMQ with persistent storage.
- Dead letter queues for messages that fail processing repeatedly.
- Exactly-once or at-least-once delivery guarantees.
- Message ordering within partitions (important for order state changes).
- Consumer group support for horizontal scaling of message handlers.

**Why it matters**: If the server crashes between "payment approved" and
"start shipping", the `order.paid` event is lost. The order would be stuck in
`paid` forever with no automatic recovery.

### Payment Processing

**What we have**: `payment_processor.go` simulates three outcomes with random
probability:
- 85% approved (returns transaction ID).
- 10% declined (returns `{status: "declined"}`).
- 5% transient error (returns Go error, triggers retry).

**What production needs**:
- Stripe/PayPal SDK integration with tokenized payment methods.
- PCI DSS compliance: card data never touches the application server.
- Idempotency keys to prevent double-charging on retries.
- Webhook handlers for async payment confirmations.
- Refund and partial refund support.
- 3D Secure / SCA (Strong Customer Authentication) for EU compliance.
- Fraud detection integration.

**Why it matters**: The simulated processor has no concept of real money. It
cannot be accidentally charged, but it also cannot demonstrate refund flows,
dispute handling, or the complexity of real payment reconciliation.

### Shipping

**What we have**: `shipping_service.go` generates fake tracking numbers with
95% success rate. Always returns "USPS" as the carrier.

**What production needs**:
- EasyPost/ShipStation API integration for real label generation.
- Multi-carrier support with rate shopping.
- Package dimension and weight calculation.
- Real-time tracking updates via webhooks.
- Returns processing with reverse shipping labels.
- Address validation (USPS Address Verification, Google Address API).
- International shipping with customs documentation.

**Why it matters**: Shipping is the most operationally complex part of
e-commerce. Real shipping involves physical-world constraints that cannot be
simulated -- carrier pickup schedules, package dimension limits, hazmat
restrictions, and customs declarations.

### Notifications

**What we have**: `notification_sender.go` logs what it would send to stdout.
It generates a notification ID and records the template name but delivers
nothing.

**What production needs**:
- SendGrid/SES for transactional email with branded templates.
- Twilio/SNS for SMS notifications.
- Push notification support for mobile apps.
- Delivery tracking and bounce handling.
- Unsubscribe management (CAN-SPAM / GDPR compliance).
- Notification preferences per user (email, SMS, push, none).

**Why it matters**: Customers expect order confirmation, shipping notification,
and delivery confirmation emails. Without real notifications, the user has no
visibility into their order status outside the web UI.

### Authentication

**What we have**: JWT tokens with a configurable secret, stored user records in
SQLite. Login and register endpoints with basic credential validation.

**What production needs**:
- OAuth2/OIDC integration with external identity providers.
- Refresh token rotation with short-lived access tokens.
- Multi-factor authentication (TOTP, WebAuthn, SMS).
- Password reset flow with email verification.
- Account lockout after failed attempts.
- Session management with revocation.
- RBAC or ABAC for fine-grained authorization.

**Why it matters**: The current JWT implementation uses a single secret with
24-hour token expiry. There is no refresh mechanism, no token revocation, and no
protection against token theft beyond expiration.

### Retry and Error Recovery

**What we have**: The `processing.step` module implements exponential backoff
with configurable retry count and base delay. On retry exhaustion, it fires a
compensate transition.

**What production needs**:
- Jitter added to backoff to prevent thundering herd.
- Circuit breaker pattern: stop calling a failing service entirely for a
  cooldown period rather than retrying individual requests.
- Dead letter queue: park failed operations for later reprocessing.
- Retry budget: limit the percentage of requests that are retries to prevent
  cascading failures.
- Saga pattern: coordinated compensating transactions across multiple services
  (e.g., refund payment if shipping fails).

**Why it matters**: Simple exponential backoff works for isolated transient
errors. In a distributed system, correlated failures (e.g., a payment gateway
outage) cause all retries to fire simultaneously, amplifying load on the
recovering service.

### Observability

**What we have**: Prometheus metrics exported at `/metrics`, with a Docker
Compose stack for local Grafana dashboards.

**What production needs**:
- Distributed tracing (OpenTelemetry) across all service boundaries.
- Centralized log aggregation (ELK stack, Datadog, Splunk).
- Alerting rules for error rate spikes, latency percentiles, and business
  metrics (order failure rate, payment decline rate).
- SLO/SLI definitions with error budget tracking.
- Real user monitoring (RUM) for frontend performance.

**Why it matters**: When an order fails in production, engineers need to trace
the request across all components to find the root cause. Without distributed
tracing, debugging requires correlating logs from multiple sources manually.

---

## What This Example Proves

Despite the gaps listed above, this example demonstrates several important
properties of the Workflow engine architecture:

### Declarative Module Composition Works for Real Applications

The entire application -- HTTP server, authentication, product catalog, order
processing, event messaging, metrics, and static file serving -- is defined in a
single `workflow.yaml` file. No Go code was written for the application logic.
The 30+ module types provided by the engine compose into a working application
through configuration alone.

### Dynamic Components Enable Runtime Integration Without Recompilation

The four dynamic components (inventory checker, payment processor, shipping
service, notification sender) are plain Go source files loaded at runtime by the
Yaegi interpreter. They can be modified, replaced, or hot-reloaded without
stopping the server. This proves the pattern works: in production, the same
mechanism loads real API integrations.

### Processing Steps Bridge External Services and State Machines

The `processing.step` module provides a clean abstraction between the messy
world of external service calls (timeouts, retries, partial failures) and the
deterministic world of state machine transitions. This separation means the
state machine definition stays simple and declarative while the processing step
handles operational complexity.

### State Machine Atomicity Ensures Consistent Order Processing

The state machine engine enforces that transitions are atomic and valid. An
order in `paying` state can only move to `paid` or `payment_failed` -- never
to `shipped` or `new`. This prevents the class of bugs where orders end up in
impossible states due to race conditions or programming errors. The
persistence layer ensures that state survives server restarts.

### The Same YAML-Driven Approach Scales from Simple Pipelines to Complex Applications

This e-commerce app uses the same configuration format as the simple examples in
`example/`. The difference is scale: more modules, more states, more
transitions, more processing steps. The engine does not care -- it processes the
YAML the same way whether there are 3 modules or 30.

---

## Next Steps to Production

If someone wanted to take this example and build a production e-commerce system
on the Workflow engine, here is what they would need to add, roughly in priority
order:

### Phase 1: Data Durability

1. **Replace SQLite with PostgreSQL** -- Add a PostgreSQL module type or use the
   existing `database.workflow` module with a `postgres` driver. Add connection
   pooling.
2. **Replace in-memory messaging with Kafka or RabbitMQ** -- Implement a
   persistent message broker module. Messages must survive restarts and support
   at-least-once delivery.
3. **Add database migrations** -- Track schema changes with versioned migration
   files. Run migrations on startup or via a CLI command.

### Phase 2: Real Integrations

4. **Integrate Stripe for payments** -- Replace `payment_processor.go` with a
   real Stripe SDK integration. Handle webhooks for async confirmations. Add
   idempotency keys.
5. **Integrate a shipping provider** -- Replace `shipping_service.go` with
   EasyPost or ShipStation. Add address validation. Support multiple carriers.
6. **Integrate email delivery** -- Replace `notification_sender.go` with
   SendGrid or SES. Create branded email templates. Handle bounces.

### Phase 3: Reliability

7. **Add circuit breakers** -- Wrap external service calls with circuit breaker
   logic. Use the circuit breaker state to fast-fail when dependencies are down.
8. **Add dead letter queues** -- Failed messages and operations should be parked
   for later reprocessing rather than silently dropped.
9. **Add jitter to retry backoff** -- Prevent thundering herd on correlated
   failures.
10. **Implement saga-based compensation** -- When shipping fails after payment,
    automatically trigger a refund.

### Phase 4: Security and Compliance

11. **External identity provider** -- Integrate Auth0 or AWS Cognito for
    production authentication. Add MFA support.
12. **Secrets management** -- Move JWT secrets and API keys to HashiCorp Vault
    or AWS Secrets Manager. Implement automatic rotation.
13. **Input validation** -- Add JSON Schema validation for all API inputs.
    Sanitize user-provided data.
14. **PCI DSS compliance** -- Ensure card data never touches the application.
    Use Stripe Elements for client-side tokenization.

### Phase 5: Operations

15. **Distributed tracing** -- Add OpenTelemetry instrumentation. Propagate
    trace context through all module boundaries and message broker deliveries.
16. **Centralized logging** -- Ship structured logs to ELK or Datadog. Add
    correlation IDs that span the entire order lifecycle.
17. **Alerting** -- Define alerts for error rate, latency P99, order failure
    rate, and payment decline rate. Page on-call for critical thresholds.
18. **Horizontal scaling** -- Deploy behind a load balancer. Ensure state
    machine transitions are safe under concurrent access from multiple
    instances. Use distributed locking or database-level compare-and-swap.

### Phase 6: Customer Experience

19. **Refund flow** -- Add `refunding` and `refunded` states. Integrate with
    Stripe refund API. Handle partial refunds.
20. **Order tracking page** -- Real-time tracking updates via shipping provider
    webhooks. Push notifications on delivery.
21. **Inventory management** -- Real-time stock levels. Back-in-stock
    notifications. Pre-order support.
22. **Search** -- Add Elasticsearch for product search with faceted filtering.
    Full-text search with typo tolerance.
