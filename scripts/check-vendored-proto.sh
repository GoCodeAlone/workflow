#!/usr/bin/env bash
# check-vendored-proto.sh — assert iac/admin/testdata/infra.proto is in sync
# with the upstream GoCodeAlone/workflow-plugin-infra proto descriptor.
#
# Exit 0: vendored copy matches upstream message set.
# Exit 1: drift detected or environment error.
#
# How it works:
#   1. Reads the "Source version: <tag>" comment from the vendored file header
#      to know which upstream tag to fetch.
#   2. Fetches the upstream proto from GitHub at that tag via the raw API
#      (no local checkout required — CI-safe).
#   3. Extracts the set of `message *Config { ... }` names from both files.
#   4. Diffs the two sets. Any addition or removal fails the check.
#
# Refresh procedure: `make vendor-infra-proto` (see Makefile target).
# The vendored file header must then be updated: update "Source version:" to
# the new upstream tag.
#
# Usage: bash scripts/check-vendored-proto.sh [--vendored PATH] [--tag TAG]
#   --vendored PATH  Override the vendored proto path
#                    (default: iac/admin/testdata/infra.proto)
#   --tag TAG        Override the upstream tag to fetch
#                    (default: read from vendored file header)
set -euo pipefail

VENDORED_PROTO="${VENDORED_PROTO:-iac/admin/testdata/infra.proto}"
UPSTREAM_REPO="GoCodeAlone/workflow-plugin-infra"
UPSTREAM_PATH="internal/contracts/infra.proto"

# Parse flags.
while [[ $# -gt 0 ]]; do
  case "$1" in
    --vendored) VENDORED_PROTO="$2"; shift 2 ;;
    --tag)      OVERRIDE_TAG="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# Resolve script root (works whether called from repo root or scripts/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENDORED_PROTO="$REPO_ROOT/$VENDORED_PROTO"

if [[ ! -f "$VENDORED_PROTO" ]]; then
  echo "ERROR: vendored proto not found: $VENDORED_PROTO" >&2
  exit 1
fi

# Extract the source tag from the vendored file header.
# Expected line: "// Source version: v1.0.0 (sourced 2026-05-27)"
if [[ -n "${OVERRIDE_TAG:-}" ]]; then
  UPSTREAM_TAG="$OVERRIDE_TAG"
else
  UPSTREAM_TAG=$(grep -m1 '// Source version:' "$VENDORED_PROTO" | \
    sed 's|// Source version: \([^ ]*\).*|\1|')
  if [[ -z "$UPSTREAM_TAG" ]]; then
    echo "ERROR: cannot read '// Source version: <tag>' from $VENDORED_PROTO" >&2
    echo "       Update the header or pass --tag <tag>" >&2
    exit 1
  fi
fi

echo "Checking vendored proto against upstream $UPSTREAM_REPO @ $UPSTREAM_TAG ..."

# Fetch upstream proto.
UPSTREAM_URL="https://raw.githubusercontent.com/$UPSTREAM_REPO/$UPSTREAM_TAG/$UPSTREAM_PATH"
UPSTREAM_TMP=$(mktemp /tmp/infra-proto-upstream.XXXXXX.proto)
trap 'rm -f "$UPSTREAM_TMP"' EXIT

if ! curl -fsSL "$UPSTREAM_URL" -o "$UPSTREAM_TMP"; then
  echo "ERROR: failed to fetch $UPSTREAM_URL" >&2
  echo "       Check connectivity and that tag '$UPSTREAM_TAG' exists in $UPSTREAM_REPO" >&2
  exit 1
fi

# Extract *Config message names from a proto file (line-by-line regex).
# Scope is intentionally limited to `*Config` messages — these are the
# typed resource configs that catalog_proto_parity_test.go asserts have
# catalog entries. Non-Config messages (service RPCs, generic types) are
# out of scope for the parity test and are therefore excluded here too.
extract_config_messages() {
  grep -oE '^[[:space:]]*message[[:space:]]+([A-Za-z0-9_]+Config)[[:space:]]*\{' "$1" \
    | sed 's|.*message[[:space:]]\+\([A-Za-z0-9_]*Config\)[[:space:]]*{.*|\1|' \
    | sort
}

VENDORED_MSGS=$(extract_config_messages "$VENDORED_PROTO")
UPSTREAM_MSGS=$(extract_config_messages "$UPSTREAM_TMP")

if [[ "$VENDORED_MSGS" == "$UPSTREAM_MSGS" ]]; then
  echo "OK: vendored infra.proto message set is in sync with upstream $UPSTREAM_TAG."
  exit 0
fi

# Compute diff for the error message.
DIFF=$(diff <(echo "$VENDORED_MSGS") <(echo "$UPSTREAM_MSGS") || true)
ADDED=$(echo "$DIFF" | grep '^>' | sed 's/^> /  + /' || true)
REMOVED=$(echo "$DIFF" | grep '^<' | sed 's/^< /  - /' || true)

echo "ERROR: vendored infra.proto is stale!" >&2
echo "" >&2
if [[ -n "$REMOVED" ]]; then
  echo "Messages in vendored but NOT in upstream (upstream removed them):" >&2
  echo "$REMOVED" >&2
fi
if [[ -n "$ADDED" ]]; then
  echo "Messages in upstream but NOT in vendored (upstream added them):" >&2
  echo "$ADDED" >&2
fi
echo "" >&2
echo "To refresh, run:" >&2
echo "  make vendor-infra-proto" >&2
echo "" >&2
echo "Then update '// Source version:' in $VENDORED_PROTO to match the new upstream tag." >&2
exit 1
