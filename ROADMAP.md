# Workflow Engine Roadmap

## Vision
A production-grade, AI-powered workflow orchestration engine with a visual builder UI, dynamic component hot-reload, comprehensive observability, and real-world application examples including a multi-service chat platform.

---

## Phase 1: Foundation (Complete)
*Commits: bcf15ee..8e23274*

### Core Engine
- [x] Workflow engine with BuildFromConfig, TriggerWorkflow lifecycle
- [x] HTTP/Messaging/State Machine/Event workflow handlers
- [x] HTTP/Schedule/Event trigger system
- [x] Module factory pattern for extensible module types

### Modular Framework Migration
- [x] Switch from GoCodeAlone/modular to CrisisTextLine/modular fork (v1.11.11)
- [x] Integrate all modular modules: httpserver, httpclient, chimux, scheduler, eventbus, eventlogger, cache, database, auth, jsonschema, reverseproxy

### Dynamic Component System (Yaegi)
- [x] Interpreter pool with sandboxed execution
- [x] Component registry with lifecycle management
- [x] File watcher for hot-reload
- [x] Source validation (stdlib-only imports)
- [x] Loader with directory/file/string sources

### AI-Powered Generation
- [x] WorkflowGenerator interface with LLM + Copilot SDK backends
- [x] Anthropic Claude direct API client with tool use
- [x] GitHub Copilot SDK integration with session management
- [x] Deploy service bridging AI generation to dynamic components
- [x] Prompt engineering: system prompt, component prompts, dynamic format

### ReactFlow UI
- [x] Drag-and-drop node palette with categorized modules
- [x] Property panel for node configuration
- [x] YAML import/export with round-trip fidelity
- [x] Undo/redo with history management
- [x] Validation (local + server)
- [x] Zustand state management

### Test Infrastructure
- [x] Unit tests: module 73%, ai/llm 85%, dynamic 74%
- [x] Playwright E2E: app-load, node-operations, connections, import-export, toolbar
- [x] Vitest component tests: 100 tests across 6 files

---

## Phase 2: Expanded Capabilities (Complete)

### Observability Foundation (WS1)
- [x] MetricsCollector - Prometheus metrics with 6 pre-registered vectors
- [x] HealthChecker - /health, /ready, /live endpoints
- [x] RequestIDMiddleware - X-Request-ID propagation with UUID generation

### Database Module (WS2)
- [x] WorkflowDatabase wrapping database/sql with Query/Execute/Insert/Update/Delete
- [x] DatabaseIntegrationConnector adapter for integration workflows
- [x] SQL builder helpers (BuildInsertSQL, BuildUpdateSQL, BuildDeleteSQL)

### Data Transformation & Webhooks (WS3)
- [x] DataTransformer with named pipelines (extract, map, filter, convert)
- [x] WebhookSender with exponential backoff retry and dead letter queue

### AI Validation Loop (WS4)
- [x] Validator with compile-test-retry cycle
- [x] ValidateAndFix integrating AI regeneration on failure
- [x] ContextEnrichedPrompt for module/service-aware generation

### Dynamic-to-Modular Bridge (WS5)
- [x] ModuleAdapter wrapping DynamicComponent as modular.Module
- [x] Configurable provides/requires for dependency injection
- [x] Engine integration via "dynamic.component" module type

### UI Updates
- [x] 2 new categories: Database, Observability
- [x] 6 new MODULE_TYPES (29 total)
- [x] Updated tests for new types/categories

---

## Phase 3: Quality, Testing & Stability (Complete)

### Copilot SDK Testing
- [x] Mock-based unit tests for all Copilot client methods
- [x] Tool handler invocation tests with realistic payloads
- [x] Session lifecycle tests (create, send, destroy)
- [x] Error path coverage (CLI not found, session failure, empty response, malformed JSON)
- [x] Integration verification with mock Copilot server

### E2E Test Expansion
- [x] Update moduleTypeMap in all e2e specs with 6 new module types
- [x] Update category count assertions (8 -> 10)
- [x] New category visibility tests (Database, Observability)
- [x] Drag-and-drop tests for new module types
- [x] Property panel tests for new module config fields
- [x] Complex workflow builder: multi-category, 5+ node workflows
- [x] Screenshot-driven visual regression for all categories
- [x] Keyboard shortcuts and accessibility testing

### Documentation
- [x] ROADMAP.md

---

## Phase 4: EventBus Integration (Complete)
*PR #18 merged*

### EventBus Bridge
- [x] EventBusBridge adapter (MessageBroker -> EventBus)
- [x] WorkflowEventEmitter with lifecycle events (workflow.started, workflow.completed, workflow.failed, step.started, step.completed, step.failed)

### EventBus Trigger
- [x] EventBusTrigger for native EventBus subscriptions
- [x] Configure with topics, event filtering, async mode
- [x] Start/Stop with subscription lifecycle management

### Engine Integration
- [x] Engine integration with workflow/step event emission
- [x] canHandleTrigger support for "eventbus" trigger type
- [x] TriggerWorkflow emits start/complete/fail events

### UI Updates
- [x] messaging.broker.eventbus module type in NodePalette (30 total)

---

## Phase 5: AI Server Bootstrap, Test Coverage & E2E Testing (Complete)
*PR #19 merged*

### AI Server Bootstrap (WS1)
- [x] cmd/server/main.go with HTTP mux and AI handler registration
- [x] CLI flags for config, address, AI provider configuration
- [x] Graceful shutdown with signal handling
- [x] initAIService with conditional Anthropic/Copilot provider registration
- [x] cmd/server/main_test.go with route verification tests

### Go Test Coverage (WS2)
- [x] Root package (engine_test.go): 68.6% -> 80%+
- [x] Module package: 77.1% -> 80%+
- [x] Dynamic package: 75.4% -> 80%+
- [x] AI packages: maintain 85%+

### Playwright E2E Tests (WS3)
- [x] Shared helpers (helpers.ts) with complete module type map
- [x] deep-module-coverage.spec.ts: All 30 module types verified
- [x] deep-complex-workflows.spec.ts: Multi-node workflow tests
- [x] deep-property-editing.spec.ts: All field types tested
- [x] deep-keyboard-shortcuts.spec.ts: Shortcut verification
- [x] deep-ai-panel.spec.ts: AI Copilot panel tests
- [x] deep-component-browser.spec.ts: Component Browser tests
- [x] deep-import-export.spec.ts: Complex round-trip tests
- [x] deep-edge-cases.spec.ts: Edge case coverage
- [x] deep-accessibility.spec.ts: A11y testing
- [x] deep-toast-notifications.spec.ts: Toast behavior tests
- [x] deep-visual-regression.spec.ts: Visual regression baselines

---

## Phase 6: Integration, Real-World Applications & Production Infrastructure (Complete)

### Module Expansion (48 Total Module Types)
- [x] HTTP modules (10): http.server, http.router, http.handler, http.middleware.auth/cors/logging/ratelimit/requestid, http.proxy, http.simple_proxy
- [x] Messaging modules (6): messaging.broker, messaging.broker.eventbus, messaging.handler, messaging.nats, messaging.kafka, notification.slack
- [x] State machine modules (4): statemachine.engine, state.tracker, state.connector, processing.step
- [x] Modular framework modules (10): httpserver, httpclient, chimux, scheduler, auth, eventbus, cache, database, eventlogger, jsonschema
- [x] Storage/persistence modules (4): database.workflow, persistence.store, storage.s3, static.fileserver
- [x] Observability modules (3): metrics.collector, health.checker, observability.otel
- [x] Data/integration modules (4): data.transformer, api.handler, webhook.sender, dynamic.component
- [x] Auth modules (2): auth.jwt, auth.modular
- [x] Reverse proxy modules (2): reverseproxy, http.proxy
- [x] Trigger types (5): http, schedule, event, eventbus, mock

### Order Processing Pipeline Example
- [x] 10+ module YAML config with HTTP servers, routers, handlers, data transformers, state machines, brokers, and observability
- [x] End-to-end pipeline demonstrating module composition

### Chat Platform Example (73 files)
- [x] Multi-service Docker Compose architecture (gateway, API, conversation, Kafka, Prometheus, Grafana)
- [x] 18 dynamic components: ai_summarizer, pii_encryptor, survey_engine, risk_tagger, conversation_router, message_processor, keyword_matcher, escalation_handler, followup_scheduler, data_retention, notification_sender, webchat_handler, twilio_provider, aws_provider, partner_provider
- [x] Full SPA with role-based views: admin, responder, supervisor dashboards
- [x] Queue health monitoring with per-program metrics
- [x] Conversation state machine (queued -> assigned -> active -> wrap_up -> closed)
- [x] Real-time risk assessment with keyword pattern matching (5 categories: self-harm, suicidal-ideation, crisis-immediate, substance-abuse, domestic-violence)
- [x] PII masking in UI (phone numbers, identifiers)
- [x] Webchat widget for web-based texters
- [x] Seed data system (users, affiliates, programs, keywords, surveys)
- [x] Architecture docs, user guide, and screenshots

### JWT Authentication
- [x] JWTAuthModule with user registration, login, token generation/validation
- [x] Seed file loading with bcrypt password hashing
- [x] Role-based metadata in JWT claims and login response
- [x] Auth middleware integration for endpoint protection

### PII Encryption at Rest
- [x] AES-256-GCM FieldEncryptor with SHA-256 key derivation
- [x] Configurable field-level encryption (encrypt specific fields, leave others plaintext)
- [x] Integration with PersistenceStore (encrypt on save, decrypt on load)
- [x] Integration with KafkaBroker (encrypt/decrypt Kafka message payloads)
- [x] 15 sub-tests covering encrypt/decrypt, round-trip, key rotation, edge cases

### REST API Handler Enhancements
- [x] View handler pattern: sourceResourceName + stateFilter for cross-resource views
- [x] Queue health endpoint with per-program aggregation
- [x] Sub-action support for nested resources (e.g., /conversations/{id}/messages)
- [x] Inline risk assessment on message append and conversation creation

### Docker Multi-Stage Builds
- [x] Dockerfile for chat-platform (builder/runtime stages, Alpine Linux, CGO_ENABLED=0)
- [x] Dockerfile for ecommerce-app
- [x] Docker Compose with health checks, volume mounts, and service dependencies

### Observability Stack
- [x] Prometheus metrics collection with custom dashboard
- [x] Grafana dashboards for chat platform monitoring
- [x] Health check endpoints integrated with Docker orchestration

---

## Phase 7: Remaining Work & Future Enhancements (In Progress)

### Dynamic Field Mapping
- [x] FieldMapping type with fallback chains and primary/resolve/set operations
- [x] Schema-agnostic field mapping for REST API handler modules (42+ references refactored)
- [x] Runtime field resolution from workflow context via FieldMapping.Resolve()
- [x] Configurable field aliases in YAML (fieldMapping, transitionMap, summaryFields)
- [x] Engine integration: fieldMapping/transitionMap/summaryFields wired from YAML config
- [x] 18 unit tests for FieldMapping type
- [ ] Eliminate hard-coded field references in dynamic components (future: component-level field contracts)

### YAML Config Validation
- [ ] JSON Schema generation from module configs
- [ ] Validation at load time with descriptive error messages
- [ ] Schema export endpoint for editor integration

### Handler Test Coverage
- [ ] IntegrationWorkflowHandler: database connector path
- [ ] ExecuteIntegrationWorkflow: retry logic, variable substitution
- [ ] ExecuteWorkflow: multi-step dispatch
- [ ] Service helper edge cases

### Performance & Scalability
- [ ] Interpreter pool sizing and benchmarks
- [ ] Concurrent workflow execution stress tests
- [ ] UI rendering performance with 50+ nodes

### Deployment
- [ ] Helm chart for Kubernetes
- [ ] Configuration via environment variables
- [ ] CI/CD pipeline with automated testing

### Security Hardening
- [ ] Input validation for all API endpoints
- [ ] Dynamic component resource limits (CPU, memory, timeout)
- [ ] Rate limiting and abuse prevention
- [ ] Audit logging for sensitive operations

### Documentation
- [ ] README.md rewrite with current project state (48 module types, chat platform example)
- [ ] CHANGELOG.md with Phase 2-6 entries
- [ ] API documentation for REST endpoints

---

## Module Type Summary

| Category | Count | Types |
|----------|-------|-------|
| HTTP | 10 | http.server, http.router, http.handler, http.middleware.{auth,cors,logging,ratelimit,requestid}, http.proxy, http.simple_proxy |
| Messaging | 6 | messaging.broker, messaging.broker.eventbus, messaging.handler, messaging.nats, messaging.kafka, notification.slack |
| State Machine | 4 | statemachine.engine, state.tracker, state.connector, processing.step |
| Modular Framework | 10 | httpserver, httpclient, chimux, scheduler, auth, eventbus, cache, database, eventlogger, jsonschema |
| Storage/Persistence | 4 | database.workflow, persistence.store, storage.s3, static.fileserver |
| Observability | 3 | metrics.collector, health.checker, observability.otel |
| Data/Integration | 4 | data.transformer, api.handler, webhook.sender, dynamic.component |
| Auth | 2 | auth.jwt, auth.modular |
| Reverse Proxy | 2 | reverseproxy, http.proxy |
| **Total** | **48** | |

## Trigger Types

| Type | Description |
|------|-------------|
| http | HTTP request triggers |
| schedule | Cron-based scheduled triggers |
| event | Generic event triggers |
| eventbus | Native EventBus subscription triggers |
| mock | Test/mock triggers |

## Example Configs

The `example/` directory contains 37+ YAML configurations demonstrating different workflow patterns, plus two full application examples:

| Example | Description |
|---------|-------------|
| `order-processing-pipeline.yaml` | E-commerce pipeline with 10+ modules across 5 categories |
| `chat-platform/` | Production-grade mental health chat platform with 73 files, multi-service Docker Compose, 18 dynamic components, full SPA |
