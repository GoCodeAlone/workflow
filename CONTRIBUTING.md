# Contributing to Workflow Engine

Thank you for your interest in contributing to the workflow engine. This guide covers everything you need to get started.

## Development Setup

### Prerequisites

- Go 1.26+
- Node.js 18+ (for UI development)
- Git
- `golangci-lint` (install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`)

### Clone and Build

```bash
git clone https://github.com/GoCodeAlone/workflow.git
cd workflow

# Build the server
go build -o server ./cmd/server

# Run tests
go test -race ./...

# Run linter
golangci-lint run

# UI setup (optional)
cd ui
npm install
npm test
```

### Project Structure

```
cmd/server/      Server binary entry point
config/          YAML config structs
module/          48 built-in module implementations
handlers/        5 workflow handler types
dynamic/         Yaegi-based hot-reload system
ai/              AI integration (llm/, copilot/)
plugin/          Plugin registry, SDK, community validator
schema/          JSON Schema generation and validation
middleware/      HTTP middleware (validation, rate limiting)
audit/           Structured audit logging
ui/              React + ReactFlow visual builder
example/         100+ YAML configs and application examples
mock/            Test helpers
docs/            API docs, tutorials, ADRs
```

## How to Contribute

### Reporting Issues

- Search existing issues before creating a new one
- Use a clear, descriptive title
- Include steps to reproduce, expected behavior, and actual behavior
- Include Go version (`go version`), OS, and relevant config

### Submitting Pull Requests

1. **Fork** the repository and create a branch from `main`
2. **Name your branch** descriptively: `feature/add-redis-module`, `fix/state-machine-transition`, `docs/update-api-reference`
3. **Make your changes** following the coding standards below
4. **Write tests** for new functionality (see test coverage targets)
5. **Run the full check suite** before submitting:

```bash
# Go checks
go fmt ./...
golangci-lint run
go test -race ./...

# UI checks (if UI changes)
cd ui
npm run lint
npm test
```

6. **Write a clear PR description** explaining what changed and why
7. **Link related issues** using "Fixes #123" or "Relates to #456"

### PR Review Process

- All PRs require at least one review before merging
- CI must pass (tests, lint, build)
- Maintain or improve test coverage
- Follow the existing code style and conventions

## Coding Standards

### Go Code

- **Formatting**: Always run `go fmt ./...` before committing
- **Linting**: Code must pass `golangci-lint run` with no warnings
- **Testing**: Use the `-race` flag for tests involving concurrency
- **Errors**: Return errors rather than panicking. Wrap errors with context using `fmt.Errorf("action: %w", err)`
- **Naming**: Follow standard Go naming conventions. Exported names should be self-documenting
- **Comments**: Add comments for exported types and non-obvious logic. Don't add comments that restate the code
- **Dependencies**: Minimize external dependencies. Dynamic components must use stdlib only

### Configuration

- Use `app.SetConfigFeeders()` in tests, not global `modular.ConfigFeeders` mutation
- YAML configs in `example/` serve as documentation -- keep them readable and well-commented
- Use environment variable fallbacks for secrets and deployment-specific values

### UI Code (TypeScript/React)

- Follow the existing ESLint configuration
- Use Zustand for state management (not Redux or Context)
- Component tests use Vitest, E2E tests use Playwright
- Prefer functional components with hooks

## Adding a New Module Type

1. Create the module implementation in `module/`:
   - Implement the `modular.Module` interface: `Name()`, `Dependencies()`, `Configure()`
   - Add configuration struct with `yaml:` and `default:` tags

2. Register the module in `engine.go`'s `BuildFromConfig` switch statement

3. Add an example YAML config in `example/` with a companion `.md` file

4. Add the module type to the UI palette in `ui/src/constants/moduleTypes.ts`

5. Write tests covering the module's core functionality

## Adding a New Workflow Handler

1. Implement the `WorkflowHandler` interface in `handlers/`:
   - `CanHandle(workflowType string) bool`
   - `ConfigureWorkflow(engine, config) error`
   - `ExecuteWorkflow(ctx, config, trigger) error`

2. Register with `engine.RegisterWorkflowHandler()` in `cmd/server/main.go`

3. Add tests for configuration and execution paths

## Contributing Plugins

Plugins are dynamic components packaged with a manifest. See the [Building Plugins](docs/tutorials/building-plugins.md) tutorial for details.

### Plugin Submission Checklist

Before submitting a plugin, ensure it passes the 8-point community validation:

1. Valid manifest with all required fields (name, version, author, description)
2. Semver version format (e.g., `1.0.0`)
3. Non-empty, descriptive description
4. Valid plugin name (lowercase alphanumeric with hyphens, 2+ characters)
5. Component compiles and loads successfully in Yaegi
6. All declared dependencies are satisfiable
7. Contract fields have types and descriptions (if contract is declared)
8. License is specified (SPDX identifier preferred)

### Plugin PR Process

1. Create the plugin directory under `plugins/` or submit as an external repository
2. Include `plugin.json` manifest and `component.go` source
3. Include tests (`component_test.go`)
4. Document the plugin's purpose, inputs, outputs, and usage examples
5. Reference the plugin in the PR description

## Test Coverage Targets

| Package | Target |
|---------|--------|
| Root (engine) | 80%+ |
| module/ | 80%+ |
| dynamic/ | 80%+ |
| ai/ packages | 85%+ |
| handlers/ | 80%+ |
| plugin/ | 80%+ |

Run coverage:

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out  # View in browser
```

## Commit Messages

- Use the imperative mood: "Add redis module" not "Added redis module"
- First line: concise summary (50 characters or less ideal)
- Body: explain what and why, not how (the code shows how)
- Reference issues: "Fixes #123" or "Relates to #456"

Example:

```
Add Redis cache module with TTL support

Implements a Redis-backed cache module that supports TTL-based
expiration and connection pooling. Integrates with the existing
cache interface for drop-in replacement of the in-memory cache.

Fixes #42
```

## Getting Help

- Read the [API Reference](docs/API.md) for endpoint documentation
- Browse the [tutorials](docs/tutorials/) for step-by-step guides
- Check the [ADRs](docs/adr/) for architectural context
- Open an issue for questions or discussions

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
