# 0045. Keep CI generation core-first with plugin consumption

Date: 2026-06-01
Status: Accepted

## Context

`wfctl ci plan` derives a platform-neutral `cigen.CIPlan` from workflow config.
`wfctl ci generate` renders first-class CI files for GitHub Actions, GitLab CI,
Jenkins, and CircleCI from that plan. The `workflow-plugin-ci-generator` plugin
also imports `cigen` and exposes generation as `step.ci_generate`.

There are platform plugins such as `workflow-plugin-github` and
`workflow-plugin-gitlab`, plus `workflow-plugin-infra` for abstract `infra.*`
module declarations. These plugins own runtime integration and provider
capabilities, not CI file rendering.

## Decision

Keep `CIPlan`, analysis, and the current first-class renderers in core for now.
Keep `workflow-plugin-ci-generator` as a plugin consumer of the same core
package.

Do not move the GitHub Actions or GitLab renderers into the GitHub/GitLab
provider plugins in this cleanup. That would make CI bootstrapping depend on
installing a plugin before generating the CI file that often installs plugins.
It would also require a new external `ci.renderer` contract that does not exist
today.

## Guidance

- Add new first-class renderer behavior in `cigen` when it must work from
  `wfctl` without prior plugin installation.
- Keep generated CI files delegating operational work to `wfctl`; CI platforms
  should remain schedulers and runners, not duplicated Workflow DSLs.
- Use `workflow-plugin-ci-generator` when CI generation is needed inside a
  workflow pipeline step.
- Consider a future `ci.renderer` plugin contract only when there is a clear
  need for third-party renderers that should not ship in core. That design must
  specify renderer discovery, versioning, offline bootstrap behavior, and
  fallback semantics.

## Consequences

Core keeps a small amount of platform-specific YAML rendering, but bootstrap UX
stays simple and deterministic. Provider plugins remain focused on runtime API
integration, IaC, SCM actions, and platform operations.
