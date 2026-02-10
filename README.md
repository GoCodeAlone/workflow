# Workflow Engine

A configurable, AI-powered workflow orchestration engine built on [CrisisTextLine/modular](https://github.com/CrisisTextLine/modular) v1.11.11, featuring a visual builder UI, dynamic component hot-reload, and comprehensive observability.

## Overview

This workflow engine lets you create applications by chaining together modular components based on YAML configuration files. You can configure the same codebase to operate as:

- An API server with authentication middleware
- An event processing system with state machine workflows
- A message-driven pipeline with metrics and health monitoring
- An AI-assisted workflow builder with visual drag-and-drop UI

All without changing code - just by modifying configuration files.

## Features

### Visual Workflow Builder
- ReactFlow-based drag-and-drop UI
- 30 module types across 10 categories
- Real-time YAML import/export with round-trip fidelity
- Undo/redo, validation, and property editing

### AI-Powered Generation
- **Anthropic Claude** direct API integration with tool use
- **GitHub Copilot SDK** integration for development workflows
- Automatic workflow generation from natural language descriptions
- Component suggestion and validation

### Dynamic Component Hot-Reload
- Yaegi interpreter for runtime Go component loading
- File watcher for automatic hot-reload
- Sandboxed execution with stdlib-only imports
- Component registry with lifecycle management

### EventBus Integration
- Native EventBus bridge for message broker compatibility
- Workflow lifecycle events (started, completed, failed)
- Event-driven triggers and subscriptions

### Observability
- Prometheus metrics collection
- Health check endpoints (/health, /ready, /live)
- Request ID propagation (X-Request-ID)

## Requirements

- Go 1.25 or later
- Node.js 18+ (for UI development)

## Module Types

The engine supports 30 module types across 10 categories:

| Category | Module Types |
|----------|-------------|
| HTTP | http.server, http.router, http.handler, http.proxy, api.handler, chimux.router |
| Middleware | http.middleware.auth, http.middleware.logging, http.middleware.ratelimit, http.middleware.cors, http.middleware.requestid |
| Messaging | messaging.broker, messaging.handler, messaging.broker.eventbus |
| State Machine | statemachine.engine, state.tracker, state.connector |
| Events | eventlogger.modular |
| Integration | httpclient.modular, data.transformer, webhook.sender |
| Scheduling | scheduler.modular |
| Infrastructure | auth.modular, eventbus.modular, cache.modular, database.modular, jsonschema.modular |
| Database | database.workflow |
| Observability | metrics.collector, health.checker |

## Quick Start

### Run the Server

```bash
go run ./cmd/server -config example/order-processing-pipeline.yaml
```

### Development UI

```bash
cd ui && npm install && npm run dev
```

### Configuration

Applications are defined via YAML configuration files:

```yaml
modules:
  - name: http-server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
  - name: hello-handler
    type: http.handler

workflows:
  http:
    routes:
      - method: GET
        path: /hello
        handler: hello-handler
```

## Example Applications

The `example/` directory includes several configurations:

- **Order Processing Pipeline**: 10-module workflow with HTTP, state machine, messaging, and observability
- **API Server**: RESTful API with protected endpoints
- **State Machine Workflow**: Order lifecycle with state transitions
- **Event Processor**: Message-based event processing
- **Data Pipeline**: Data transformation and webhook delivery

## Testing

```bash
# Go tests
go test ./...

# UI component tests
cd ui && npm test

# E2E Playwright tests
cd ui && npx playwright test
```

## Architecture

The engine is built on these core concepts:

- **Modules**: Self-contained components providing specific functionality
- **Workflows**: YAML configurations chaining modules together
- **Handlers**: Components interpreting and configuring workflow types (HTTP, Messaging, State Machine, Scheduler, Integration)
- **Dynamic Components**: Runtime-loaded Go modules via Yaegi interpreter
- **AI Generation**: Natural language to workflow YAML via LLM APIs
