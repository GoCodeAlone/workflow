# 0044. cigen Jenkins/CircleCI renderers omit the legacy docker-build/deploy stages

**Status:** Accepted
**Date:** 2026-05-31
**Decision-makers:** Workflow maintainers, autonomous pipeline
**Related:** `docs/plans/2026-05-31-cigen-jenkins-circleci-design.md`, issue #804

## Context

`cigen` renders a platform-neutral `CIPlan` into CI config. The GitHub Actions
and GitLab CI renderers emit a **plan / per-phase apply / smoke** job set derived
from the CIPlan (secret env wiring, plan-guard, `wfctl migrations up`, smoke).
They deliberately do **not** emit a `docker build`/`docker push`/`wfctl deploy
--image` stage — `CIPlan.Build` exists but no renderer consumes it.

The legacy Jenkins and CircleCI generators in
`workflow-plugin-ci-generator/internal/platforms/` are `text/template` files that
are NOT config-derived: they hardcode `go test ./...`, `go build ./...`, `docker
build/push`, and `wfctl deploy --image $REGISTRY_IMAGE`, ignoring the app's
secrets union, phases, migrations, smoke, and plugin-install needs.

Issue #804 asks to make Jenkins/CircleCI config-derived "like GHA/GitLab in
PR #18" and to retire the templates. This forces a choice about the hardcoded
docker-build/deploy stages the legacy Jenkins/CircleCI templates carried that the
GHA/GitLab renderers never had.

## Decision

The new `cigen.RenderJenkins` / `cigen.RenderCircleCI` renderers emit the **same
logical job set as the GHA/GitLab renderers** — plan / per-phase apply (secret
env + plan-guard + last-phase migrations) / smoke — and **do not** emit a
docker-build/push or `wfctl deploy --image` stage. `CIPlan.Build` remains unused
by all four renderers; no `Analyze` change and no new CIPlan field is introduced.

We reject keeping a docker-build/deploy stage (config-gated on `CIPlan.Build`) in
Jenkins/CircleCI, because that would either (a) make the four renderers diverge
in job set, or (b) require teaching the GHA/GitLab renderers the same stage for
parity — both contradict #804's "mechanical emitters from the existing CIPlan,
no Analyze change" framing and re-introduce non-config-derived, app-shape-guessing
output.

## Consequences

All four platforms now render the **same** config-derived plan from one CIPlan,
so output is consistent and `--from-plan`/`--diff` behave identically across
platforms. This is a **behavior change** for any consumer that relied on the
legacy Jenkins/CircleCI `docker build`/`wfctl deploy` stages: those stages are
removed, surfaced via the plugin minor bump (v0.2.0) and release notes. Building
and pushing an application image is orthogonal to infra CI generation and, if a
user needs it, belongs in app-specific CI the user authors — not in cigen's
infra-deployment output. If config-derived image build/deploy is ever wanted, it
must be added uniformly to all four renderers behind a populated `CIPlan.Build`,
in a separate design.
