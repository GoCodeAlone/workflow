# GitHub Copilot Instructions for Workflow Engine

This is a configurable workflow engine built on GoCodeAlone's Modular library that allows creating applications entirely from YAML configuration files. Please follow these guidelines when contributing to this project.

## Project Overview

The Workflow Engine enables building applications by chaining together modular components via configuration. The same codebase can operate as:
- API servers with authentication middleware
- Event processing systems
- Message-based communication systems
- Scheduled job processors
- State machines

All functionality is configured through YAML files without code changes.

## Code Standards

### Go Development
- **Formatting**: All Go code must be formatted with `gofmt`. Run `go fmt ./...` before committing
- **Linting**: Use `golangci-lint run` to check code quality
- **Testing**: Run `go test ./...` for unit tests, `go test -v ./...` for verbose output
- **Module Dependencies**: The project relies on `github.com/CrisisTextLine/modular` (the fork, not the original) - follow its patterns

### Required Before Each Commit
- Format Go code with `gofmt`
- Run `golangci-lint run` and fix any linting issues
- Ensure all tests pass
- Update documentation when adding new module types or workflow handlers
- Test configuration files in `/example` directory

## Architecture Overview

### Core Components

#### Engine (`engine.go`)
- **StdEngine**: Main workflow execution engine
- **WorkflowHandler**: Interface for handling different workflow types
- **ModuleFactory**: Factory functions for creating modules from configuration
- **TriggerRegistry**: Manages workflow triggers (HTTP, schedule, events)

#### Modules (`/module`)
- **HTTP Components**: Server, router, handlers, middleware
- **Messaging**: Brokers, message handlers, pub/sub
- **Scheduling**: Cron-style job scheduling
- **State Management**: State machines and connectors
- **Event Processing**: Event triggers and processors

#### Handlers (`/handlers`)
- **HTTP Handler**: Manages HTTP workflows and routing
- **Events Handler**: Processes event-driven workflows
- **Integration Handler**: Manages service integrations
- **Scheduler Handler**: Handles scheduled workflows
- **State Machine Handler**: Manages state transitions

#### Configuration (`/config`)
- **WorkflowConfig**: Main configuration structure for YAML files
- Supports module definitions, workflow configurations, and trigger setups

### Module Types

The engine supports these built-in module types:

#### HTTP Modules
- `http.server`: HTTP server with configurable address
- `http.router`: Request routing
- `http.handler`: Request processing
- `http.middleware.auth`: Authentication middleware
- `http.trigger`: HTTP-based workflow triggers

#### Messaging Modules
- `messaging.broker`: Message broker (memory-based)
- `messaging.handler`: Message processing
- `event.processor`: Event processing
- `event.trigger`: Event-based triggers

#### Scheduling Modules
- `scheduler`: Cron-style job scheduler
- `schedule.trigger`: Time-based triggers

#### State Management
- `state.machine`: State machine workflows
- `state.connector`: State connectors

## Development Guidelines

### Adding New Module Types

1. **Create Module Implementation**:
   ```go
   // In /module directory
   type YourModule struct {
       name string
       // module-specific fields
   }
   
   func (m *YourModule) Name() string { return m.name }
   func (m *YourModule) Dependencies() []string { return []string{} }
   func (m *YourModule) Configure(app modular.Application) error {
       // Configuration logic
   }
   ```

2. **Register in Engine**:
   ```go
   // In engine.go BuildFromConfig method
   case "your.module.type":
       mod = module.NewYourModule(modCfg.Name, modCfg.Config)
   ```

3. **Add Factory Function** (if needed):
   ```go
   engine.AddModuleType("your.module.type", func(name string, config map[string]interface{}) modular.Module {
       return module.NewYourModule(name, config)
   })
   ```

### Creating Workflow Handlers

1. **Implement WorkflowHandler Interface**:
   ```go
   type YourHandler struct{}
   
   func (h *YourHandler) CanHandle(workflowType string) bool {
       return workflowType == "your-workflow"
   }
   
   func (h *YourHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
       // Setup workflow from config
   }
   
   func (h *YourHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
       // Execute workflow logic
   }
   ```

2. **Register Handler**:
   ```go
   engine.RegisterWorkflowHandler(&YourHandler{})
   ```

### Configuration Best Practices

1. **YAML Structure**: Follow the standard module/workflow pattern:
   ```yaml
   modules:
     - name: module-name
       type: module.type
       config:
         key: value
   
   workflows:
     workflow-type:
       # workflow-specific configuration
   ```

2. **Module Naming**: Use descriptive names that indicate purpose
3. **Dependencies**: Ensure modules list their dependencies correctly
4. **Testing**: Create example configurations in `/example` directory

### HTTP Workflow Patterns

1. **Route Definition**:
   ```yaml
   workflows:
     http:
       routes:
         - method: GET
           path: /api/resource
           handler: resource-handler
           middlewares:
             - auth-middleware
   ```

2. **Handler Configuration**:
   ```yaml
   modules:
     - name: resource-handler
       type: http.handler
       config:
         contentType: "application/json"
         response: '{"status": "ok"}'
   ```

### Event-Driven Patterns

1. **Event Processing**:
   ```yaml
   workflows:
     events:
       processors:
         - event: user.created
           handler: user-processor
   ```

2. **Message Subscriptions**:
   ```yaml
   workflows:
     messaging:
       subscriptions:
         - topic: user-events
           handler: user-event-handler
   ```

### Testing Guidelines

1. **Unit Tests**: Test individual modules and handlers
2. **Integration Tests**: Test complete workflow configurations
3. **Configuration Tests**: Validate example YAML files
4. **Mock Dependencies**: Use test helpers in `/mock` directory

### Running Examples

Execute example configurations:
```bash
go run example/main.go -config example/api-server-config.yaml
go run example/main.go -config example/event-processor-config.yaml
go run example/main.go -config example/sms-chat-config.yaml
```

### Debugging and Logging

- Use the modular logger interface: `logger.Debug()`, `logger.Info()`, `logger.Error()`
- Enable verbose logging during development
- Log module registration and workflow configuration steps

## Project Structure

```
/
├── engine.go              # Main workflow engine
├── engine_test.go         # Engine tests
├── /config               # Configuration structures
├── /module               # Module implementations
├── /handlers             # Workflow handlers
├── /example              # Example configurations and runner
├── /mock                 # Test mocks and helpers
└── go.mod                # Go module definition
```

## Common Patterns

### Lifecycle Management
- Modules implementing `StartStopModule` interface for proper cleanup
- Context cancellation for graceful shutdowns
- Resource cleanup in `Stop()` methods

### Dependency Injection
- Use modular.Application for service registration
- Declare dependencies in module `Dependencies()` method
- Resolve dependencies in `Configure()` method

### Error Handling
- Return meaningful error messages from module operations
- Use context for cancellation and timeouts
- Log errors at appropriate levels

This workflow engine leverages the power of configuration-driven development, allowing rapid application prototyping and deployment through YAML files.
