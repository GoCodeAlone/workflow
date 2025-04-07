# Workflow Engine Documentation

## Overview

The Workflow Engine is a modular Go-based system designed to orchestrate and manage various types of workflows through configuration. It provides a flexible framework for defining, executing, and monitoring workflows across different domains including HTTP services, messaging systems, and scheduled jobs.

## Supported Functionality

### Core Components

1. **Workflow Engine**
   - Central orchestrator for managing workflow execution
   - Configuration-driven workflow setup
   - Support for different workflow handlers
   - Lifecycle management (start/stop)

2. **Service Registry**
   - Module and service registration
   - Dependency management and injection
   - Service discovery

### Modules

1. **HTTP Module**
   - HTTP Server: Configurable web server for handling HTTP requests
   - HTTP Router: Request routing with path and method matching
   - HTTP Handlers: Processing for HTTP requests with configurable responses
   - API Handlers: RESTful API implementation
   - Middleware: Support for authentication and request processing

2. **Messaging Module**
   - In-memory message broker implementation
   - Message publishing and subscription
   - Topic-based messaging
   - Message handlers for processing incoming messages

3. **Scheduler Module**
   - Scheduled job execution
   - Cron-like scheduling
   - One-time and recurring tasks

### Configuration

The workflow engine is configured through YAML files that define:
- Modules to be loaded
- Module configurations
- Workflow definitions
- Service dependencies

## Usage Examples

The `example/` directory contains various configuration examples:

1. **API Gateway**: Configures an HTTP server that routes requests to backend services
2. **API Server**: Sets up a RESTful API server
3. **Data Pipeline**: Demonstrates data processing workflows
4. **Dependency Injection**: Shows how services can be injected and used across modules
5. **Event Processing**: Configuration for event-driven workflows
6. **Multi-workflow**: Running multiple workflows in parallel
7. **Scheduled Jobs**: Using the scheduler for recurring tasks
8. **SMS Chat**: Messaging-based workflow example

## Configuration Format

```yaml
name: "Example Workflow"
description: "Example workflow configuration"

modules:
  - name: "http-server"
    type: "http.server"
    config:
      address: ":8080"
  
  - name: "http-router"
    type: "http.router"
    config:
      routes:
        - path: "/api/v1/users"
          method: "GET"
          handler: "user-handler"

workflows:
  - name: "main-workflow"
    type: "http"
    config:
      endpoints:
        - path: "/health"
          method: "GET"
          response:
            statusCode: 200
            body: '{"status": "ok"}'
```

## Current Limitations

1. **Workflow Orchestration**
   - Limited support for complex workflow sequencing
   - No built-in error handling and retry mechanisms
   - Lack of workflow state persistence
   - No visual workflow representation

2. **Scalability**
   - Single instance execution
   - No distributed workflow support
   - Limited to in-memory message broker

3. **Monitoring and Observability**
   - Basic logging only
   - No metrics collection
   - No tracing support
   - Limited visibility into workflow execution

4. **Development Experience**
   - No workflow testing framework
   - Limited documentation and examples
   - No web-based workflow designer

## Planned Improvements

1. **Workflow Orchestration Enhancements**
   - Advanced workflow sequencing with branching and conditions
   - Workflow state persistence
   - Error handling with customizable retry strategies
   - Sub-workflow support

2. **Scalability and Performance**
   - Distributed workflow execution
   - External message broker support (Kafka, RabbitMQ)
   - Horizontal scaling capabilities

3. **Monitoring and Management**
   - Metrics collection and reporting
   - Distributed tracing integration
   - Workflow monitoring dashboard
   - Execution history and audit logs

4. **Developer Experience**
   - Comprehensive testing framework
   - Interactive workflow designer
   - More extensive example library
   - Improved documentation