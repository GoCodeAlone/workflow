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
- [x] Update moduleTypeMap in all e2e specs with 6 new module types
- [x] Update category count assertions (8 -> 10)
- [x] New category visibility tests (Database, Observability)
- [x] Drag-and-drop tests for new module types
- [x] Property panel tests for new module config fields
- [x] Complex workflow builder: multi-category, 5+ node workflows
- [x] Screenshot-driven visual regression for all categories
- [x] Keyboard shortcuts and accessibility testing

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

## Phase 5: AI Server Bootstrap, Test Coverage & E2E Testing (Complete)
*PR #19 merged*

### AI Server Bootstrap (WS1)
- [x] cmd/server/main.go with HTTP mux and AI handler registration
- [x] CLI flags for config, address, AI provider configuration
- [x] Graceful shutdown with signal handling
- [x] initAIService with conditional Anthropic/Copilot provider registration
- [x] cmd/server/main_test.go with route verification tests

### Go Test Coverage (WS2)
- [x] Root package (engine_test.go): 68.6% → ≥80%
- [x] Module package: 77.1% → ≥80%
- [x] Dynamic package: 75.4% → ≥80%
- [x] AI packages: maintain ≥85%

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

## Phase 6: Integration, Realistic Workflows & Documentation (In Progress)

### Bug Fixes
- [ ] Fix API path mismatch (/api/components → /api/dynamic/components)
- [ ] Fix stale moduleTypeMap in e2e specs (add EventBus Bridge)

### Realistic Workflow Example
- [ ] Order Processing Pipeline (10 modules, 5 categories)
- [ ] Integration tests for end-to-end pipeline

### Coverage Improvements
- [ ] cmd/server: 20.3% → ≥70%
- [ ] module: 78.2% → ≥80%

### Copilot SDK Verification
- [ ] Tool handler integration tests
- [ ] Provider selection and fallback tests

### Exploratory E2E Testing
- [ ] Phase 6 exploratory spec with ~33 tests and screenshots

### Documentation
- [ ] README.md rewrite with current project state
- [ ] ROADMAP.md updates

---

## Phase 7: Production Readiness (Planned)

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
| workflow (root) | 97.0% | 80% | ✓ Exceeded |
| ai | 87.6% | 85% | ✓ Exceeded |
| ai/copilot | 90.7% | 70% | ✓ Exceeded |
| ai/llm | 91.2% | 85% | ✓ Exceeded |
| cmd/server | 20.3% | 70% | Below target |
| config | 100% | 100% | ✓ Met |
| dynamic | 85.5% | 80% | ✓ Exceeded |
| handlers | 70.8% | 70% | ✓ Met |
| module | 78.2% | 80% | Below target |
