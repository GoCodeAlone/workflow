# Workflow Engine Roadmap

## Vision
A production-grade, AI-powered workflow orchestration engine with a visual builder UI, dynamic component hot-reload, and comprehensive observability.

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
*Complete*

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

## Phase 3: Quality, Testing & Stability (In Progress)

### Copilot SDK Testing
- [x] Mock-based unit tests for all Copilot client methods
- [x] Tool handler invocation tests with realistic payloads
- [x] Session lifecycle tests (create, send, destroy)
- [x] Error path coverage (CLI not found, session failure, empty response, malformed JSON)
- [x] Integration verification with mock Copilot server

### E2E Test Expansion
- [ ] Update moduleTypeMap in all e2e specs with 6 new module types
- [ ] Update category count assertions (8 -> 10)
- [ ] New category visibility tests (Database, Observability)
- [ ] Drag-and-drop tests for new module types
- [ ] Property panel tests for new module config fields
- [ ] Complex workflow builder: multi-category, 5+ node workflows
- [ ] Screenshot-driven visual regression for all categories
- [ ] Keyboard shortcuts and accessibility testing

### Handler Test Coverage (target: >70%)
- [ ] IntegrationWorkflowHandler: database connector path
- [ ] ExecuteIntegrationWorkflow: retry logic, variable substitution
- [ ] ExecuteWorkflow: multi-step dispatch
- [ ] Service helper edge cases

### Documentation
- [x] ROADMAP.md
- [ ] Update CHANGELOG.md with Phase 2 and Phase 3 entries
- [ ] Update README.md with new module types

### Git Hygiene
- [ ] Commit and push Phase 2 changes
- [ ] Commit and push Phase 3 changes

---

## Phase 4: EventBus Integration (Complete)
*PR #18 merged*

### EventBus Bridge
- [x] EventBusBridge adapter (MessageBroker → EventBus)
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

## Phase 5: AI Server Bootstrap, Test Coverage & E2E Testing (In Progress)

### AI Server Bootstrap (WS1)
- [ ] cmd/server/main.go with HTTP mux and AI handler registration
- [ ] CLI flags for config, address, AI provider configuration
- [ ] Graceful shutdown with signal handling
- [ ] initAIService with conditional Anthropic/Copilot provider registration
- [ ] cmd/server/main_test.go with route verification tests

### Go Test Coverage (WS2)
- [ ] Root package (engine_test.go): 68.6% → ≥80%
- [ ] Module package: 77.1% → ≥80%
- [ ] Dynamic package: 75.4% → ≥80%
- [ ] AI packages: maintain ≥85%

### Playwright E2E Tests (WS3)
- [ ] Shared helpers (helpers.ts) with complete module type map
- [ ] deep-module-coverage.spec.ts: All 30 module types verified
- [ ] deep-complex-workflows.spec.ts: Multi-node workflow tests
- [ ] deep-property-editing.spec.ts: All field types tested
- [ ] deep-keyboard-shortcuts.spec.ts: Shortcut verification
- [ ] deep-ai-panel.spec.ts: AI Copilot panel tests
- [ ] deep-component-browser.spec.ts: Component Browser tests
- [ ] deep-import-export.spec.ts: Complex round-trip tests
- [ ] deep-edge-cases.spec.ts: Edge case coverage
- [ ] deep-accessibility.spec.ts: A11y testing
- [ ] deep-toast-notifications.spec.ts: Toast behavior tests
- [ ] deep-visual-regression.spec.ts: Visual regression baselines

---

## Phase 6: Production Readiness (Planned)

### Workflow Execution Runtime
- [ ] End-to-end workflow execution from YAML config
- [ ] Integration testing with real modular Application
- [ ] Graceful shutdown with in-flight workflow draining
- [ ] Workflow execution history/audit log

### Security
- [ ] Input validation for all API endpoints
- [ ] YAML config schema validation
- [ ] Dynamic component resource limits (CPU, memory, timeout)
- [ ] Authentication for UI and API endpoints

### Performance
- [ ] Interpreter pool sizing and benchmarks
- [ ] Concurrent workflow execution stress tests
- [ ] UI rendering performance with 50+ nodes

### Deployment
- [ ] Docker image with multi-stage build
- [ ] Helm chart for Kubernetes
- [ ] Configuration via environment variables
- [ ] Health check integration with orchestrators

---

## Coverage Targets

| Package | Current | Target | Status |
|---------|---------|--------|--------|
| workflow (root) | 68.6% | 80% | Below target |
| ai | 84.8% | 85% | Near target |
| ai/copilot | 90.3% | 70% | ✓ Exceeded |
| ai/llm | 84.5% | 85% | Near target |
| config | 100% | 100% | ✓ Met |
| dynamic | 75.4% | 80% | Below target |
| handlers | 70.8% | 70% | ✓ Met |
| module | 77.1% | 80% | Below target |
