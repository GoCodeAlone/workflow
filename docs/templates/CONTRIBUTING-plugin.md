# Contributing to workflow-plugin-<name>

This plugin is part of the [GoCodeAlone/workflow](https://github.com/GoCodeAlone/workflow) ecosystem.

## Before contributing

Read the [upstream CONTRIBUTING.md](https://github.com/GoCodeAlone/workflow/blob/main/CONTRIBUTING.md) for general conventions, signing, and review expectations.

## Local development

If you have a `go.work` file in a parent directory (common when working on
multiple GoCodeAlone repos side-by-side), use `GOWORK=off` so the plugin
builds against its own pinned `go.mod` rather than the workspace.

```sh
git clone https://github.com/GoCodeAlone/workflow-plugin-<name>.git
cd workflow-plugin-<name>
GOWORK=off go build ./...
GOWORK=off go test ./...
```

## Pull requests

- One feature or bugfix per PR.
- Update CHANGELOG.md with a Keep-a-Changelog entry.
- Add tests covering new behavior.
- Run `GOWORK=off go vet ./...` before pushing.

## Reporting issues

See the issue templates under `.github/ISSUE_TEMPLATE/`.
