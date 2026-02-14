# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Dynamic field mapping with FieldMapping type supporting fallback chains and primary/resolve/set operations
- Schema-agnostic field resolution for REST API handler modules (42+ references refactored)
- Runtime field resolution from workflow context via FieldMapping.Resolve()
- Configurable field aliases in YAML (fieldMapping, transitionMap, summaryFields)
- Engine integration: fieldMapping/transitionMap/summaryFields wired from YAML config
- 18 unit tests for FieldMapping type
- Multi-chat UI view for responders with real-time conversation switching
- Updated screenshots and user guide documentation
- Go 1.26 upgrade and security fixes

---

## [0.5.0] - 2026-02-12

### Added
- **Module Expansion (48 Total Module Types)**
  - HTTP modules (10): http.server, http.router, http.handler, http.middleware.{auth,cors,logging,ratelimit,requestid}, http.proxy, http.simple_proxy
  - Messaging modules (6): messaging.broker, messaging.broker.eventbus, messaging.handler, messaging.nats, messaging.kafka, notification.slack
  - State machine modules (4): statemachine.engine, state.tracker, state.connector, processing.step
  - Modular framework modules (10): httpserver, httpclient, chimux, scheduler, auth, eventbus, cache, database, eventlogger, jsonschema
  - Storage/persistence modules (4): database.workflow, persistence.store, storage.s3, static.fileserver
  - Observability modules (3): metrics.collector, health.checker, observability.otel
  - Data/integration modules (4): data.transformer, api.handler, webhook.sender, dynamic.component
  - Auth modules (2): auth.jwt, auth.modular
  - Reverse proxy modules (2): reverseproxy, http.proxy
  - 5 trigger types: http, schedule, event, eventbus, mock
- **Order Processing Pipeline Example**
  - 10+ module YAML config with HTTP servers, routers, handlers, data transformers, state machines, brokers, and observability
  - End-to-end pipeline demonstrating module composition across 5 categories
- **Chat Platform Example (73 files)**
  - Multi-service Docker Compose architecture (gateway, API, conversation, Kafka, Prometheus, Grafana)
  - 18 dynamic components: ai_summarizer, pii_encryptor, survey_engine, risk_tagger, conversation_router, message_processor, keyword_matcher, escalation_handler, followup_scheduler, data_retention, notification_sender, webchat_handler, twilio_provider, aws_provider, partner_provider, and more
  - Full SPA with role-based views: admin, responder, and supervisor dashboards
  - Queue health monitoring with per-program metrics
  - Conversation state machine (queued -> assigned -> active -> wrap_up -> closed)
  - Real-time risk assessment with keyword pattern matching (5 categories)
  - PII masking in UI (phone numbers, identifiers)
  - Webchat widget for web-based texters
  - Seed data system (users, affiliates, programs, keywords, surveys)
  - Architecture docs, user guide, and screenshots
- **JWT Authentication**
  - JWTAuthModule with user registration, login, token generation/validation
  - Seed file loading with bcrypt password hashing
  - Role-based metadata in JWT claims and login response
  - Auth middleware integration for endpoint protection
- **PII Encryption at Rest**
  - AES-256-GCM FieldEncryptor with SHA-256 key derivation
  - Configurable field-level encryption (encrypt specific fields, leave others plaintext)
  - Integration with PersistenceStore (encrypt on save, decrypt on load)
  - Integration with KafkaBroker (encrypt/decrypt Kafka message payloads)
  - 15 sub-tests covering encrypt/decrypt, round-trip, key rotation, edge cases
- **REST API Handler Enhancements**
  - View handler pattern: sourceResourceName + stateFilter for cross-resource views
  - Queue health endpoint with per-program aggregation
  - Sub-action support for nested resources (e.g., /conversations/{id}/messages)
  - Inline risk assessment on message append and conversation creation
- **Docker Multi-Stage Builds**
  - Dockerfile for chat-platform (builder/runtime stages, Alpine Linux, CGO_ENABLED=0)
  - Dockerfile for ecommerce-app
  - Docker Compose with health checks, volume mounts, and service dependencies
- **Observability Stack**
  - Prometheus metrics collection with custom dashboard
  - Grafana dashboards for chat platform monitoring
  - Health check endpoints integrated with Docker orchestration

---

## [0.4.0] - 2026-02-11

### Added
- **AI Server Bootstrap**
  - cmd/server/main.go with HTTP mux and AI handler registration
  - CLI flags for config, address, AI provider configuration
  - Graceful shutdown with signal handling
  - initAIService with conditional Anthropic/Copilot provider registration
  - cmd/server/main_test.go with route verification tests
- **Go Test Coverage Push to 80%+**
  - Root package (engine_test.go): 68.6% -> 80%+
  - Module package: 77.1% -> 80%+
  - Dynamic package: 75.4% -> 80%+
  - AI packages: maintained at 85%+
- **11 New Playwright E2E Test Suites**
  - Shared helpers (helpers.ts) with complete module type map
  - deep-module-coverage.spec.ts: all 30 module types verified
  - deep-complex-workflows.spec.ts: multi-node workflow tests
  - deep-property-editing.spec.ts: all field types tested
  - deep-keyboard-shortcuts.spec.ts: shortcut verification
  - deep-ai-panel.spec.ts: AI Copilot panel tests
  - deep-component-browser.spec.ts: Component Browser tests
  - deep-import-export.spec.ts: complex round-trip tests
  - deep-edge-cases.spec.ts: edge case coverage
  - deep-accessibility.spec.ts: a11y testing
  - deep-toast-notifications.spec.ts: toast behavior tests
  - deep-visual-regression.spec.ts: visual regression baselines

---

## [0.3.0] - 2026-02-10

### Added
- **EventBus Bridge**
  - EventBusBridge adapter bridging MessageBroker interface to EventBus
  - WorkflowEventEmitter with lifecycle events (workflow.started, workflow.completed, workflow.failed, step.started, step.completed, step.failed)
- **EventBus Trigger**
  - EventBusTrigger for native EventBus subscriptions
  - Configurable topics, event filtering, and async mode
  - Start/Stop with subscription lifecycle management
- **Engine Integration**
  - Engine integration with workflow/step event emission
  - canHandleTrigger support for "eventbus" trigger type
  - TriggerWorkflow emits start/complete/fail events
- **UI Updates**
  - messaging.broker.eventbus module type in NodePalette (30 total)

---

## [0.2.0] - 2026-02-09

### Added
- **Observability Foundation**
  - MetricsCollector wrapping Prometheus with 6 pre-registered metric vectors and `/metrics` endpoint
  - HealthChecker with `/health`, `/ready`, `/live` endpoints and auto-discovery
  - RequestIDMiddleware with `X-Request-ID` propagation and UUID generation
- **Database Module**
  - WorkflowDatabase wrapping `database/sql` with Query, Execute, InsertRow, UpdateRows, DeleteRows
  - SQL builder helpers (BuildInsertSQL, BuildUpdateSQL, BuildDeleteSQL)
  - DatabaseIntegrationConnector adapter for integration workflows
- **Data Transformation**
  - DataTransformer with named pipelines of operations (extract, map, filter, convert)
  - Dot-notation JSON path extraction with array index support
- **Webhook Delivery**
  - WebhookSender with exponential backoff retry (configurable maxRetries, backoff, timeout)
  - Dead letter queue for exhausted retries with manual RetryDeadLetter support
- **AI Validation Loop**
  - Validator with compile-test-retry cycle (import validation, Yaegi compile, required function check)
  - ValidateAndFix integrating AI regeneration on validation failure
  - ContextEnrichedPrompt for module/service-aware generation
- **Dynamic-to-Modular Bridge**
  - ModuleAdapter wrapping DynamicComponent as modular.Module
  - Configurable provides/requires for dependency injection
  - Engine integration via `dynamic.component` module type
- **UI Updates**
  - 2 new categories: Database, Observability (10 total)
  - 6 new MODULE_TYPES: database.workflow, metrics.collector, health.checker, http.middleware.requestid, data.transformer, webhook.sender (29 total)
  - Updated component tests for new types and categories

---

## [0.1.0] - 2026-02-08

### Added
- **Core Engine**
  - Workflow engine with BuildFromConfig, TriggerWorkflow lifecycle
  - HTTP, Messaging, State Machine, Event workflow handlers
  - HTTP, Schedule, Event trigger system
  - Module factory pattern for extensible module types
- **Modular Framework**
  - Migration from GoCodeAlone/modular to CrisisTextLine/modular fork (v1.11.11)
  - Integration of all modular modules: httpserver, httpclient, chimux, scheduler, eventbus, eventlogger, cache, database, auth, jsonschema, reverseproxy
- **Dynamic Component System (Yaegi)**
  - Interpreter pool with sandboxed execution
  - Component registry with lifecycle management
  - File watcher for hot-reload
  - Source validation (stdlib-only imports)
- **AI-Powered Generation**
  - WorkflowGenerator interface with LLM + Copilot SDK backends
  - Anthropic Claude direct API client with tool use
  - GitHub Copilot SDK integration with session management
  - Deploy service bridging AI generation to dynamic components
- **ReactFlow UI**
  - Drag-and-drop node palette with 8 categorized module groups (23 types)
  - Property panel for node configuration
  - YAML import/export with round-trip fidelity
  - Undo/redo with history management
  - Validation (local + server) and Zustand state management
- **Test Infrastructure**
  - Go unit tests: module 73%, ai/llm 85%, dynamic 74%
  - Playwright E2E: app-load, node-operations, connections, import-export, toolbar
  - Vitest component tests: 100 tests across 6 files
- **CI/CD**
  - GitHub Actions: automated testing on Go 1.23/1.24, linting, multi-platform releases
  - Code coverage reporting via Codecov
  - Weekly dependency updates

### Changed
- Upgraded to Modular v1.3.9 with IsVerboseConfig, SetVerboseConfig, SetLogger support
- Improved error handling for service registration and I/O operations

### Fixed
- Critical error checking issues identified by linters
- HTTP response writing error handling
- Service registration error handling in engine

### Security
- Enhanced dependency management with automated updates
- Improved error handling to prevent potential runtime issues

## [Previous Versions]

Previous version history was not maintained in changelog format.
