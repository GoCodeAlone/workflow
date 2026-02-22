# Workflow Engine Examples

This directory demonstrates various approaches for building applications with the Workflow Engine.

## Workflow-Native Module Approach (Recommended)

All application components are configured using workflow-native module types. This is the simplest and most consistent approach.

**Examples:**
- `simple-workflow-config.yaml` - Basic HTTP server with routing
- `api-server-config.yaml` - REST API with custom handlers
- `event-processor-config.yaml` - Simple event processing
- `realtime-messaging-modular-config.yaml` - Real-time messaging with EventBus bridge
- `api-gateway-config.yaml` - API gateway with reverse proxy
- `api-gateway-modular-config.yaml` - API gateway with reverse proxy (alternate config)

**Benefits:**
- Easy YAML-based configuration with inline config blocks
- Built-in workflow routing system
- Great for rapid prototyping and production use
- Simple JSON response handlers

## Modular Framework Modules (Legacy Support)

A few CrisisTextLine/modular framework modules are still supported but may be replaced by workflow-native equivalents in the future:

- `scheduler.modular` - CrisisTextLine/modular scheduler for cron-based job execution
- `cache.modular` - CrisisTextLine/modular caching module
- `database.modular` - CrisisTextLine/modular database module
- `reverseproxy` - CrisisTextLine/modular reverse proxy (not deprecated)

**Examples:**
- `scheduled-jobs-modular-config.yaml` - Advanced job scheduling with cron

## Available Module Types

### HTTP
- `http.server` - HTTP server that listens on a network address
- `http.router` - Routes HTTP requests to handlers
- `http.handler` - Handles HTTP requests and produces responses
- `http.proxy` / `reverseproxy` - Reverse proxy
- `http.simple_proxy` - Simple prefix-based reverse proxy
- `static.fileserver` - Serves static files with optional SPA fallback
- `http.middleware.auth` - Authentication middleware
- `http.middleware.logging` - Request logging middleware
- `http.middleware.ratelimit` - Rate limiting middleware
- `http.middleware.cors` - CORS headers middleware
- `http.middleware.requestid` - Request ID middleware
- `http.middleware.securityheaders` - Security headers middleware

### API
- `api.query` - CQRS query handler
- `api.command` - CQRS command handler
- `api.handler` - REST API handler
- `api.gateway` - API gateway with routing, rate limiting, CORS, auth

### Messaging
- `messaging.broker` - In-memory message broker
- `messaging.broker.eventbus` - EventBus bridge for pub/sub
- `messaging.handler` - Message handlers
- `messaging.nats` - NATS broker
- `messaging.kafka` - Kafka broker

### State Machine
- `statemachine.engine` - State machine engine
- `state.tracker` - State tracking
- `state.connector` - State machine connector

### Storage & Database
- `storage.sqlite` - SQLite storage
- `storage.local` - Local filesystem storage
- `storage.s3` - S3 storage
- `storage.gcs` - GCS storage
- `database.workflow` - SQL database for workflow state
- `persistence.store` - Persistence store

### Observability
- `metrics.collector` - Prometheus-compatible metrics
- `health.checker` - Health check endpoints
- `log.collector` - Log collection
- `observability.otel` - OpenTelemetry tracing

### Feature Flags
- `featureflag.service` - Feature flag management with generic or LaunchDarkly provider
- `step.feature_flag` - Pipeline step to evaluate a feature flag
- `step.ff_gate` - Pipeline step combining flag evaluation with conditional routing

### Other
- `auth.jwt` - JWT authentication
- `auth.user-store` - User store
- `data.transformer` - Data transformation
- `webhook.sender` - Webhook sending
- `notification.slack` - Slack notifications
- `dynamic.component` - Yaegi hot-reload components
- `processing.step` - Processing steps
- `secrets.vault` - HashiCorp Vault secrets
- `secrets.aws` - AWS Secrets Manager
- `openapi.generator` - OpenAPI spec generation
- `openapi.consumer` - OpenAPI spec consumption
- `workflow.registry` - Workflow registry

## Legacy Examples

The following examples demonstrate various workflow patterns:

### State Machine & Event Processing
- `state-machine-workflow.yaml` - E-commerce order processing states
- `event-driven-workflow.yaml` - Complex event pattern detection
- `event-processor-config.yaml` - Basic event processing

### Integration & APIs
- `integration-workflow.yaml` - Third-party service integration
- `sms-chat-config.yaml` - SMS-based messaging workflow

### Scheduling & Jobs
- `advanced-scheduler-workflow.yaml` - Complex scheduling scenarios
- `scheduled-jobs-config.yaml` - Recurring task management
- `data-pipeline-config.yaml` - Data processing workflows

### Feature Flags
- `feature-flag-workflow.yaml` - Feature flag evaluation and gating in pipelines

### Patterns & Examples
- `multi-workflow-config.yaml` - Multiple parallel workflows
- `dependency-injection-example.yaml` - Service injection patterns
- `trigger-workflow-example.yaml` - Event trigger demonstrations

## Running Examples

Option 1: Specify configuration file
```bash
go run main.go -config <configuration-file>.yaml
```

Option 2: Interactive selection menu
```bash
go run main.go
```

This displays a numbered list of available configurations.
