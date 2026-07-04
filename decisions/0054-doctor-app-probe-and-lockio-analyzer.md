# 0054. Add `wfctl doctor app` probe and `lint/lockio` analyzer

**Status:** Proposed — pending GoCodeAlone maintainer sign-off (see Scope note below)
**Date:** 2026-07-04
**Decision-makers:** codingsloth@pm.me (directed), Claude (Fable 5)
**Related:** `decisions/0052-top-level-wfctl-doctor.md`, `GoCodeAlone/workflow-compute` `docs/plans/2026-07-04-durable-mutation-lifecycle-design.md` ("Upstream Guards" U1/U2)

## Context

A workflow-compute staging incident traced to a lock held across a slow store
write, starving unrelated requests. Diagnosing it cost hours largely because
two very different failure shapes looked identical from the outside: an edge
proxy's own HTML error page (the request never reached the app) versus the
app's own JSON structured error (the request reached the app, which was
busy/contended). The same incident's fix relied on an in-repo `go/ast` checker
that enforces "every lock acquisition has a release on every return path,"
including a goroutine-opaque traversal bug found and fixed during review (a
release call fired only inside an unawaited `go func(){}()` must not count as
covering the return path).

Both pieces are generic problems, not specific to workflow-compute:

1. Distinguishing a platform-edge failure from an app-origin failure by
   content-type/body-shape applies to any deployed HTTP app behind any edge
   proxy.
2. "No lock held across store I/O," and the specific goroutine-traversal bug
   class, applies to any Go codebase using a begin/release critical-section
   idiom, not just this one app's method names.

## Decision

Add two independent pieces to this repo:

- **`wfctl doctor app <url>`** (`cmd/wfctl/doctor_app.go`): a new subcommand
  under the existing `doctor` command. It runs N sequential + M
  lightly-concurrent GET requests against `<url><health-path>` and reports
  per-request latency (p50/p99), failure-origin classification
  (platform-edge HTML vs. app-origin JSON, using content-type/body-shape
  heuristics only — no provider-specific logic), and health flip-flop rate.
  Flags follow existing `doctor` conventions: `--health-path`, `--probes`,
  `--concurrency`, `--timeout`, `--format text|json`, `--strict`.
- **`lint/lockio`** (`lint/lockio/`): a new top-level package containing a
  `go/analysis`-style checker (`lockio.NewAnalyzer`) parameterized by
  `Config{AcquireMethods, ReleaseMethods, RestrictedIOMethods,
  PermanentIOCallers, PermanentReturnPathFunctions}` — all plain method-name
  sets, no hardcoded vocabulary. It ports the workflow-compute checker's two
  violation classes and the goroutine-opaque traversal fix, with a lower-level
  `FindViolations` function for consumers who want to build their own
  shrink-only allowlist test on top of it, plus the `analysis.Analyzer`
  wiring for consumers who want to plug it into `golangci-lint`, `go vet`, or
  a standalone checker binary.

## Scope note (flagged for maintainer sign-off)

ADR 0052 scoped `wfctl doctor` explicitly to checkout-only diagnostics
("must avoid network access unless explicitly requested"). `doctor app` is a
new, explicitly-invoked (`doctor app <url>`, not a flag on the base command)
online mode that goes beyond that remit: it makes real HTTP requests to an
operator-supplied URL. This is a deliberate scope expansion, not an oversight,
and is called out here for maintainer review rather than assumed.

Separately, `lint/` is a new top-level directory. `docs/REPO_LAYOUT.md`'s Main
Roots table did not have a home for reusable static-analysis tooling before
this change; `lint/lockio` is the first package there. The path and package
name are a proposal (`lint/lockio` per the workflow-compute design doc), not a
fixed requirement — happy to relocate under maintainer guidance if a different
location fits the repo's conventions better.

## Consequences

Operators get a single command to distinguish "the edge is broken" from "the
app is broken" during an incident instead of manually inspecting response
bodies. Any host app on this framework can adopt `lint/lockio` for its own
lock/lease-across-I/O invariant instead of hand-rolling the checker, as
workflow-compute did. workflow-compute's own copy of this checker
(`internal/server/mutation_lifecycle_guard_test.go`) is tracked to swap to
this package in a follow-up once released, per its own FOLLOWUPS expiry row.

Rollback is simple: remove `cmd/wfctl/doctor_app.go`, the `app` dispatch
branch in `cmd/wfctl/doctor.go`, the `lint/` directory, and the associated
docs/tests; no other command or package depends on either addition.
