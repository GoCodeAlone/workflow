# 0062. Align scaffold and version test boundaries

**Status:** Accepted
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0059; source Tasks1/2/S4/S6, PR1022.

## Context

The Go1.27.1 cold Linux run rejected old init assertions that still require
optional observability plugins. Its linked-version test also combined a
dependency's stderr compatibility warning with the correct final version line.
The focused channel probe confirms the existing CLI handler sends both to
stderr, not stdout.
Neither failure justifies changing the approved self-contained defaults or
suppressing diagnostics. The source manifest remains three tasks/one PR.

## Decision

Name cmd/wfctl/init_test.go as a Task2 owner. Check that default generated
configs omit optional external module types; retain file/module/UI assertions.
In the already-owned main_test.go, assert the exact final linked-version line
on its existing stderr writer and empty stdout, retaining earlier stderr in
failure diagnostics and successful test logs. Reproduce
both failures before correction. No CLI output or dependency change.

## Consequences

Tests follow the actual product boundaries instead of conflating an optional
plugin with bootstrap support or diagnostics with the final version line. Keep the warning
visible. Rerun focused and final cold gates; the failed cold checkpoint is not
acceptance. No task, PR, timeout, protected-file or production behavior change.
