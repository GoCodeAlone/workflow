# CLAUDE.md

Short repo-local guidance for Claude Code. Detailed guidance lives in
`docs/AGENT_GUIDE.md`; layout rules live in `docs/REPO_LAYOUT.md`.

## Must Follow

- Use `GOWORK=off` for Go commands in this multi-repo workspace.
- Prefer focused tests first, then broaden based on risk.
- Keep committed examples under `example/`; do not add root `examples/`,
  `test-*`, or scratch app directories.
- Do not revert unrelated user changes.
- Update docs with behavior, CLI, config, or layout changes.

## Useful Commands

```sh
GOWORK=off go test ./...
GOWORK=off golangci-lint run
make build-wfctl
make build-examples
make test-configs
```

## Links

- `docs/AGENT_GUIDE.md`
- `docs/REPO_LAYOUT.md`
- `docs/WFCTL.md`
- `DOCUMENTATION.md`
