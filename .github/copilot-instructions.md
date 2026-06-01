# Workflow Engine - Copilot Instructions

Configuration-driven workflow orchestration engine in Go. Keep this file short;
the detailed agent guide is `docs/AGENT_GUIDE.md`, and layout rules are in
`docs/REPO_LAYOUT.md`.

## Must Follow

- Use `GOWORK=off` for Go commands in this multi-repo workspace.
- Prefer make targets when available: `make build-wfctl`, `make build-examples`,
  `make test-configs`.
- Keep examples in `example/`; do not add root `examples/`, `test-*`, or scratch
  app directories.
- Update docs and tests with behavior, CLI, config, or layout changes.

## Common Commands

```sh
GOWORK=off go test ./...
GOWORK=off go test ./cmd/wfctl -run TestName -count=1
GOWORK=off golangci-lint run
cd ui && npm test -- --run
```
