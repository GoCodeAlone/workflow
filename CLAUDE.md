# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Configuration-driven workflow orchestration engine built in Go on the [modular](https://github.com/CrisisTextLine/modular) framework. Turns YAML configs into running applications — API servers, event pipelines, state machines, and more.

## Build & Run

```sh
# Build the server
go build -o server ./cmd/server
./server -config example/api-server-config.yaml

# Build the CLI
go build -o wfctl ./cmd/wfctl
./wfctl validate example/api-server-config.yaml

# Run tests
go test ./...
go test -race ./...

# UI development
cd ui && npm install && npm run dev

# Lint
go fmt ./...
golangci-lint run
```

## Project Structure

- `cmd/server/` — Server binary entry point
- `cmd/wfctl/` — CLI tool (validate, inspect, deploy, api extract, etc.)
- `config/` — YAML config structs
- `module/` — All module and pipeline step implementations
- `handlers/` — Workflow handler types (HTTP, Messaging, StateMachine, Scheduler, Integration)
- `plugins/` — Built-in engine plugins (auth, storage, pipeline-steps, etc.)
- `plugin/` — Plugin SDK (EnginePlugin interface, factories, hooks)
- `schema/` — JSON Schema generation for config validation
- `dynamic/` — Yaegi hot-reload system
- `ai/` — AI integration (Anthropic Claude, GitHub Copilot)
- `ui/` — React + ReactFlow visual builder (Vite, TypeScript)
- `example/` — Example YAML configs with companion .md docs
- `deploy/` — Deployment configs (Docker, Kubernetes/Helm, OpenTofu/AWS)
- `docs/` — Documentation (tutorials, ADRs, API reference)

## Key Conventions

- Module implementations live in `module/` with the naming pattern `module/<type>.go`
- Pipeline steps follow `module/pipeline_step_<name>.go`
- Template functions are registered in `module/pipeline_template.go` via `templateFuncMap()`
- Module factories are registered in `engine.go` `BuildFromConfig()`
- Plugin step/module factories are registered in `plugins/<name>/plugin.go`
- wfctl commands each have their own file in `cmd/wfctl/<command>.go`
- Tests follow standard Go conventions: `*_test.go` alongside source files

## Documentation Maintenance

**When adding new functionality, update the corresponding documentation:**

| Change | Update |
|--------|--------|
| New module type | `DOCUMENTATION.md` module table, `engine.go` factory registration |
| New pipeline step | `DOCUMENTATION.md` step table, register in plugin or engine |
| New template function | `DOCUMENTATION.md` template functions section, add to `templateFuncMap()` |
| New wfctl command | `docs/WFCTL.md` command reference, update `main.go` usage() |
| New trigger type | `DOCUMENTATION.md` trigger types section |
| Config format change | `DOCUMENTATION.md` configuration section |

## Common Tasks

### Adding a New Module Type
1. Create `module/<type>.go` implementing `modular.Module`
2. Register factory in `engine.go` `BuildFromConfig()` or in a plugin's `ModuleFactories()`
3. Add schema in the plugin's `ModuleSchemas()` or inline
4. Add example YAML in `example/`
5. Update `DOCUMENTATION.md`

### Adding a New Pipeline Step
1. Create `module/pipeline_step_<name>.go` implementing the step interface
2. Register in `plugins/pipelinesteps/plugin.go` `StepFactories()`
3. Update `DOCUMENTATION.md`

### Adding a Template Function
1. Add to `templateFuncMap()` in `module/pipeline_template.go`
2. Add tests in `module/pipeline_template_test.go`
3. Update `DOCUMENTATION.md` template functions section

## Links

- [Full Documentation](DOCUMENTATION.md)
- [CLI Reference](docs/WFCTL.md)
- [Deployment Guide](deploy/README.md)
- [Tutorials](docs/tutorials/)
- [Go Package Docs](https://pkg.go.dev/github.com/GoCodeAlone/workflow)
