# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Copilot SDK comprehensive test coverage with mock-based tests for all client methods
- E2E test expansion: 6 new module types in all spec moduleTypeMaps, 10-category assertions
- Exploratory Playwright usability test suite with screenshot-based visual regression
- Handler test coverage expansion for integration workflow paths (database, webhook, retry)
- Updated ROADMAP.md with Phase 3 tracking

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
