# Workflow Assistant release smoke compatibility

## Evidence and goal

The official v0.87.0 Darwin arm64 asset matches its published checksum and
GitHub digest, embeds clean source `4b2c11cc` and MCP SDK v1.3.0, and passes the
five-version wire matrix. The existing package smoke completes its fixture
checks but rejects stderr: Sonic 1.15.2 prints its exact public Go 1.27
`encoding/json` fallback warning at startup. The helper currently allows only
the exact legacy plugin-deprecation notice.

Recognize and report that known diagnostic without weakening unknown-stderr,
release-version, input-privacy or acceptance boundaries. This is a separate
package-maintenance follow-up; the completed two-PR/six-task MCP plan is unchanged.

## Chosen behavior

- Classify complete stderr lines by exact equality with the existing deprecation
  notice or the observed Sonic fallback notice. Reject every other line,
  including prefix/suffix mutations, injected extra lines and blank lines.
- Return symbolic diagnostic status; never include raw stderr in smoke reports.
  Preserve existing empty/deprecation-only statuses and distinguish Sonic-only
  and combined known diagnostics.
- Keep the strict release-version guard and mismatch handling unchanged.
  Source pseudo-versions and snapshots remain unknown.
- Publish package patch 0.1.1 with the same three skills and direct MCP manifest.
  Document verified release pairings and the nonfatal diagnostic explicitly.
- Retain runtime, desktop, binary-health and input-equivalence unknowns.

## Rejected alternatives

Blanket stderr suppression can hide sensitive or unexpected output. Relabeling a
source binary would fabricate release evidence. Upgrading Sonic changes core
runtime behavior and is unnecessary for this bounded maintainer-client fix.
The published engine tag, release assets, workflows and permissions are unchanged.

## Verification

Use RED/GREEN tests for exact known lines and adversarial variants. Run standalone
race/vet checks, then the actual unchanged v0.86.2 and official v0.87.0 assets.
Check wrong-version/source-version failures, safe reports and preserved unknowns.
Record the smoke client's source separately from the official binary's source.
Independent spec/quality review, public-data audit and ordinary draft-PR CI follow.

## Design backport — 2026-10-20

The genuine-child negative test disproved the assumption that existing cleanup
already rejected nonzero exit: `cmd.Wait()` errors were discarded. A child that
emitted the known warning and exited 2 returned no cleanup error (observed RED).
Retain its wait error and report bounded `check_failed` after reaping; protocol
and fixture failures keep their earlier precedence. Forced termination also
fails while cleanup still reaps the child and reader. Independent review found
this necessary to enforce the chosen successful-shutdown boundary within Task 1's
same helper/test files. The one-PR/one-task scope manifest and lock are unchanged.
Spec review also identified a child that handles forced SIGTERM with exit 0;
its genuine-child regression failed before correction. Record required signal
escalation as `check_failed` regardless of final exit status, while retaining
bounded cleanup and timeout precedence.
