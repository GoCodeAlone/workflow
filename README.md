# Workflow Engine

A configurable workflow engine based on GoCodeAlone's Modular library, allowing applications to be built entirely from configuration.

## Overview

This workflow engine lets you create applications by chaining together modular components based on YAML configuration files. You can configure the same codebase to operate as:

- An API server with authentication middleware
- An event processing system
- A bidirectional chat system with triage capabilities

All without changing code - just by modifying configuration files.

## Architecture

The workflow engine is built on these core concepts:

- **Modules**: Self-contained components that provide specific functionality
- **Workflows**: Configurations that chain modules together to create application behavior
- **Handlers**: Components that understand how to interpret and configure specific workflow types

### Module Types

The engine supports several types of modules:

- **HTTP Server**: Handles HTTP requests
- **HTTP Router**: Routes HTTP requests to handlers
- **HTTP Handlers**: Processes HTTP requests and generates responses
- **Authentication Middleware**: Validates requests before they reach handlers
- **Message Broker**: Facilitates message-based communication
- **Message Handlers**: Processes messages from specific topics
- **UI Server**: Provides a web-based interface for workflow management

## Configuration

Applications are defined via YAML configuration files. Here's a basic example:

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

### Advanced Configuration

You can create more complex applications with authentication and messaging:

```yaml
modules:
  - name: api-http-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
  - name: auth-middleware
    type: http.middleware.auth
  - name: users-api
    type: api.handler
    config:
      resourceName: "users"
  - name: message-broker
    type: messaging.broker

workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: users-api
        middlewares:
          - auth-middleware
  messaging:
    subscriptions:
      - topic: user-events
        handler: user-event-handler
```

## Example Applications

The repository includes several example applications:

1. **API Server**: A RESTful API with protected endpoints
2. **Event Processor**: A message-based event processing system
3. **SMS Chat**: A bidirectional chat system with triage workflow

## Usage

Run any example by specifying the configuration file:

```
go run example/main.go -config example/api-server-config.yaml
```

## Extending

To extend the workflow engine with custom modules:

1. Implement the appropriate interfaces in the `module` package
2. Register your module type in the `BuildFromConfig` method of the engine
3. Create a configuration file that uses your new module type

## Testing

Run the tests to verify the workflow engine functionality:

```
go test ./...
```