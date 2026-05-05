#!/usr/bin/env bash
# File-or-comment dedup helper for the conformance leak scrubber and
# the budget soft-alert. Files a NEW GitHub issue when no open dedup-
# matched issue exists; appends a comment to the existing one when
# it does. The dedup match requires BOTH labels:
#
#   conformance-leak-incident  (or conformance-budget-incident)
#   auto-filed-leak            (or auto-filed-budget)
#
# Operators who file issues manually with only the primary label
# (for human investigation) are NOT appended to — preserving the
# manual postmortem swimlane.
#
# Usage:
#   file-or-comment-leak-issue.sh <count> <details>
#
# Optional environment variables:
#   PRIMARY_LABEL  defaults to "conformance-leak-incident"
#   HELPER_LABEL   defaults to "auto-filed-leak"
#   ISSUE_TITLE    defaults to "Conformance leak: <count> resources scrubbed"
#
# See docs/conformance-runbook.md § "Helper conventions" for the
# label-removal warning and the postmortem-swimlane workaround.

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <count> <details>" >&2
  exit 64  # EX_USAGE
fi

SCRUBBED_COUNT="$1"
SCRUBBED_DETAILS="$2"
PRIMARY_LABEL="${PRIMARY_LABEL:-conformance-leak-incident}"
HELPER_LABEL="${HELPER_LABEL:-auto-filed-leak}"
ISSUE_TITLE="${ISSUE_TITLE:-Conformance leak: ${SCRUBBED_COUNT} resources scrubbed}"

# Match issues that have BOTH labels (operator-filed issues with only
# the primary label are skipped, preserving manual investigation
# context per the runbook's "Helper conventions" section).
EXISTING="$(gh issue list \
  --label "${PRIMARY_LABEL}" \
  --label "${HELPER_LABEL}" \
  --state open \
  --json number \
  --jq '.[0].number // empty')"

NOW="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

if [[ -n "${EXISTING}" ]]; then
  gh issue comment "${EXISTING}" --body "Scrubber run at ${NOW}: scrubbed ${SCRUBBED_COUNT} resources. Details: ${SCRUBBED_DETAILS}"
else
  gh issue create \
    --label "${PRIMARY_LABEL}" \
    --label "${HELPER_LABEL}" \
    --title "${ISSUE_TITLE}" \
    --body "First leak detected at ${NOW}. Scrubbed ${SCRUBBED_COUNT} resources. Details: ${SCRUBBED_DETAILS}

Triage guidance: see [docs/conformance-runbook.md § Operator interventions](../../../docs/conformance-runbook.md). **Do not remove the \`${HELPER_LABEL}\` label** — it is the dedup key. To open a separate postmortem swimlane, file a new issue with ONLY the \`${PRIMARY_LABEL}\` label, link this one, then close this one."
fi
