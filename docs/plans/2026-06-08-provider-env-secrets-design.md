# Provider Environment Secrets Design

## Summary

Workflow currently treats GitHub environment secrets as a provider target named
`github:env`, but the CLI does not manage the environment lifecycle. The
environment name may already be declared in YAML through top-level
`environments:`, `ci.deploy.environments`, `secretStores.<name>.config.environment`,
or `platform.environment`. Those declarations should be treated as desired state:
they answer which environments exist from Workflow's point of view, while the
provider implementation determines whether the provider-side object exists and
how to create or validate it.

This design introduces provider-neutral contracts for secret targets that depend
on environments. Core keeps portable YAML discovery and CLI orchestration.
Provider-specific lifecycle stays behind provider implementations, starting with
GitHub Actions environments in the current in-core provider and leaving the
external `workflow-plugin-github` migration as the next provider-owned step.

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`

| guidance | design response |
|---|---|
| Core keeps shared contracts; external plugins own provider integrations. | Add a small core contract and keep GitHub-specific API details inside the GitHub provider. |
| Update docs/tests with CLI/config behavior changes. | Add tests for YAML environment discovery, GitHub env ensure behavior, and docs for env preflight. |
| Use focused tests first, then broaden. | Target `secrets`, `config`, and `cmd/wfctl` tests before full lint/package verification. |

## Architecture

Add `secrets.EnvironmentManager` as an optional provider capability:

- `ListEnvironments(ctx) ([]ProviderEnvironment, error)`
- `EnsureEnvironment(ctx, name string) (ProviderEnvironment, error)`
- `ValidateEnvironment(ctx, name string) (ProviderEnvironment, error)`

Add provider-owned `ProviderEnvironment` metadata with safe, non-secret fields
such as provider, name, label, exists, and source.

Add config helpers in core to derive desired Workflow environments from:

- top-level `environments`
- `ci.deploy.environments`
- `platform.environment`
- `secretStores[*].config.environment`

`wfctl secrets setup --manifest` will use these derived names when building
GitHub environment targets. If a selected target is environment-scoped and the
provider implements `EnvironmentManager`, the setup path validates or ensures the
environment before writing secrets. Non-interactive mode validates and errors on
missing provider environments; interactive mode can create them.

## Security Review

Secret values remain masked and are never logged. Environment preflight uses the
same provider credentials already required to set secrets. GitHub environment
creation is non-destructive but still a remote provider mutation, so it is only
auto-created in interactive setup or when explicitly requested by a future
non-interactive flag. The first implementation does not edit environment
protection rules, reviewers, or branch policies.

## Infrastructure Impact

GitHub environment creation may create repository-scoped GitHub Environment
objects. No cloud resources, DNS, databases, or migrations are changed. Provider
environment deletion is out of scope.

## Multi-Component Validation

Tests exercise:

- YAML environment discovery from config.
- GitHub provider environment endpoints through an HTTP test server.
- `wfctl secrets setup --manifest` target construction from YAML-declared
  environments.
- preflight behavior for missing environment names and provider errors.

## Assumptions

- YAML-declared environments are desired state but not proof of provider-side
  existence.
- GitHub environment creation is safe enough for interactive setup when the user
  selected that environment target.
- Provider plugins can adopt the new contract without changing the portable YAML
  schema again.
- Some providers map "environment" to namespace/path/region rather than a first
  class provider object.

## Rollback

Revert the provider-contract and CLI preflight commits. Existing repo/org secret
setup remains compatible because the new environment methods are optional and
non-environment secret targets bypass them.

## Deferred

- Move the GitHub implementation from core into `workflow-plugin-github`.
- Add environment protection policy management.
- Add provider environment deletion/destruction.
- Add non-interactive `--ensure-environments` after the interactive behavior has
  landed safely.
