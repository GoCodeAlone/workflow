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

## Phase 7: Quality, Documentation & Production Readiness (Complete)

### Dynamic Field Mapping
- [x] FieldMapping type with fallback chains and primary/resolve/set operations
- [x] Schema-agnostic field mapping for REST API handler modules (42+ references refactored)
- [x] Runtime field resolution from workflow context via FieldMapping.Resolve()
- [x] Configurable field aliases in YAML (fieldMapping, transitionMap, summaryFields)
- [x] Engine integration: fieldMapping/transitionMap/summaryFields wired from YAML config
- [x] 18 unit tests for FieldMapping type
- [x] Component-level field contracts: FieldContract type, ContractRegistry, pre-execution validation
- [x] 4 chat platform components updated with contracts (keyword_matcher, conversation_router, escalation_handler, ai_summarizer)

### YAML Config Validation
- [x] JSON Schema generation from WorkflowConfig structs (schema/ package)
- [x] Validation at load time with descriptive error messages (integrated into BuildFromConfig)
- [x] Schema export endpoint (GET /api/schema)
- [x] 41 tests covering schema generation, validation rules, and HTTP endpoint

### Handler Test Coverage
- [x] IntegrationWorkflowHandler: database connector path, nil/stale connector
- [x] ExecuteIntegrationWorkflow: retry logic, variable substitution, error handlers
- [x] ExecuteWorkflow: multi-step dispatch
- [x] Service helper edge cases (FixMessagingHandlerServices, PatchAppServiceCalls)
- [x] 29 new tests, integration.go/service_helper.go/app_helper.go at 100%

### Performance & Scalability
- [x] Interpreter pool benchmarks (creation ~2.4ms, execute ~1.5us, pool contention negligible)
- [x] Concurrent workflow stress tests (100+ concurrent, ~28K workflows/sec, zero goroutine leaks)
- [x] UI rendering performance with 50+ nodes (Playwright E2E)

### Deployment
- [x] Helm chart for Kubernetes (monolith/distributed modes, HPA, ServiceMonitor)
- [x] Configuration via environment variables (WORKFLOW_CONFIG, WORKFLOW_ADDR, etc.)
- [x] CI/CD pipelines (test matrix, Docker build, Helm lint, release workflow)
- [x] Multi-stage Dockerfile

### Security Hardening
- [x] Input validation middleware (size limits, content-type, JSON well-formedness)
- [x] Dynamic component resource limits (execution timeout, output size)
- [x] Rate limiting with IP/token/combined strategies and stale cleanup
- [x] Audit logging with structured JSON (auth, admin, escalation, data access events)

### Documentation
- [x] README.md rewrite with 48 module types, chat platform, full feature set
- [x] CHANGELOG.md with Phase 2-6 entries
- [x] API documentation for REST endpoints (docs/API.md)

---

## Phase 8: Advanced Features & Ecosystem (Complete)

### Multi-Chat & Collaboration UI
- [x] Responder-to-Responder direct messaging (DM threads with real-time polling)
- [x] Supervisor-to-Responder real-time chat (shared DM system)
- [x] Shared resource panel (15 canned responses, search/filter, copy/insert)
- [x] Conversation transfer with live chat handoff (visual indicators, history)

### Plugin Ecosystem
- [x] Plugin registry with manifest validation and HTTP CRUD API
- [x] Component versioning with semver constraints and compatibility checking
- [x] Plugin SDK with template generator and documentation generator
- [x] Community submission validator with 8 checks and review checklist

### Advanced AI Integration
- [x] Response suggestion engine with LLM + template fallback and caching
- [x] Conversation classifier with 4 categories, priority scoring, 14 rules
- [x] Sentiment analysis with lexicon fallback, trend detection, sharp-drop alerts
- [x] Supervisor alert engine with hybrid rule-based + AI detection, 4 alert types

### Multi-Tenancy & Scale
- [x] Worker pool with auto-scaling and consistent hash partitioning
- [x] Per-tenant quota enforcement with token bucket rate limiting
- [x] Multi-region routing with data residency compliance
- [x] Generic connection pool and LRU cache with TTL

### Observability & Operations
- [x] OpenTelemetry distributed tracing (HTTP spans, workflow spans, context propagation)
- [x] 3 Grafana dashboards (workflow overview, chat platform, dynamic components)
- [x] 7 Prometheus alerting rules with runbooks
- [x] SLA monitoring with uptime/latency/error budget tracking

---

## Phase 9: Production Hardening & Developer Experience (Complete)

### Testing & Reliability
- [x] Integration test suite (12 E2E tests: config loading, lifecycle, workflow execution, module wiring)
- [x] Chaos testing (6 tests: random component failures, concurrent chaos, recovery validation)
- [x] Load testing (6 tests: ~27K workflows/sec, concurrent dispatch, cache/pool performance)
- [x] Regression testing (10 tests: validates all 34 example YAML configs load correctly)

### Developer Experience
- [x] CLI tool `wfctl` with validate, inspect, run, plugin, schema subcommands
- [x] Interactive workflow debugger with breakpoints, step/continue, variable inspection, HTTP API
- [x] Plugin development hot-reload watcher (file system monitoring, auto-reload on change)

### Platform Features
- [x] Webhook retry with exponential backoff, max retries, dead letter store with HTTP endpoints
- [x] Cron scheduler with job CRUD, expression parsing, concurrent execution control
- [x] Workflow versioning with version store, rollback, diff comparison, HTTP API
- [x] Environment promotion pipeline (dev -> staging -> prod) with approval gates

### Security & Compliance
- [x] OAuth2/OIDC provider with token validation, auth code flow, JWKS endpoint
- [x] RBAC middleware with permission model (action + resource), role definitions, enforcement
- [x] Compliance reporting for SOC2 and HIPAA (23 controls, evidence collection, PDF-ready output)
- [x] Secret management with provider interface (env, file, Vault stub), secret:// URI resolver

### Documentation & Community
- [x] OpenAPI 3.0 specification (2245 lines, 65+ endpoints, full schema definitions)
- [x] Tutorial series: getting started, building plugins, scaling workflows, chat platform walkthrough
- [x] 5 Architecture Decision Records (YAML config, modular framework, Yaegi, hybrid AI, field contracts)
- [x] CONTRIBUTING.md with development setup, PR process, coding standards
- [x] CODE_OF_CONDUCT.md (Contributor Covenant v2.1)

---

## Phase 10: Multi-Affiliate Routing & End-to-End QA (Complete)

### Multi-Affiliate Conversation Routing
- [x] JWT claims enrichment with affiliateId/programIds from user metadata
- [x] handleGetAll() filtering by affiliate/program query params with JWT defaults
- [x] Conversation router with real routing logic (keyword → program → affiliate mapping)
- [x] Queue health scoping by affiliate (non-admin users see only their affiliate's data)

### Conversation Data Fixes
- [x] Initialize messages array on conversation creation (fix empty chat views)
- [x] Ensure messages sub-action handler creates slice before appending

### SPA Multi-Tenant Updates
- [x] Responder view: pass affiliate/program params in API calls
- [x] Supervisor view: filter users and conversations by affiliate
- [x] Queue view: scope queue health by affiliate
- [x] Chat view: filter transfer responder list by affiliate
- [x] Display program/affiliate context badges in conversation cards

### Seed Data Enrichment
- [x] Add EU-West users (responder + supervisor for aff-003/prog-004)
- [x] Add PARTNER keyword for prog-004

### End-to-End QA Testing
- [x] Playwright multi-agent QA: texters send keywords, verify routing to correct programs
- [x] Cross-affiliate isolation: responders see only their affiliate's conversations
- [x] Supervisor scoping: supervisors see only their affiliate's responders
- [x] Multi-message flow verification with screenshots
- [x] Queue health per-affiliate validation

---

## Phase 11: End-to-End QA & Polish (Complete)

### State Machine Bug Fixes
- [x] Fix instance ID mismatch (double "conv-" prefix between webhooks-api and conversations-api)
- [x] Fix field name normalization (Twilio "Body" to lowercase "body" for contract validation)
- [x] Fix auto-transition state sync (assigned to active not reflected in resource data)
- [x] Fix bridgeToConversation initial state (was hardcoding "queued", now uses "new")

### Playwright E2E Tests for Chat Platform
- [x] Login flow: verify all 8 seed users can log in with correct roles
- [x] Conversation routing: send messages with HELLO/TEEN/WELLNESS/PARTNER keywords, verify routing
- [x] Cross-affiliate isolation: login as aff-001 responder, verify only aff-001 conversations visible
- [x] Supervisor view: verify supervisors see only their affiliate's responders and conversations
- [x] Message flow: send messages both directions, verify real-time updates
- [x] Multi-chat: open multiple conversations simultaneously, verify messages route correctly
- [x] Queue health: verify per-affiliate program stats display correctly
- [x] Transfer flow: transfer conversation between responders, verify handoff
- [x] Escalation flow: test medical/police escalation state transitions
- [x] Screenshot documentation: 21 QA screenshots captured

### UI Improvements
- [x] Accept Conversation banner for queued conversations in chat view
- [x] Tags sidebar auto-refresh after applying tags

### Platform Polish
- [x] Error handling improvements for edge cases discovered during QA
- [ ] Performance profiling of conversation routing under load
- [ ] Documentation updates based on QA findings

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
