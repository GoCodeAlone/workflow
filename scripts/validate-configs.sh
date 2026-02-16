#!/usr/bin/env bash
# validate-configs.sh — CI pipeline script for validating workflow configs.
# Exit code 0 = all configs valid, 1 = one or more invalid.
#
# Usage:
#   ./scripts/validate-configs.sh              # validate example/ + admin/
#   ./scripts/validate-configs.sh --strict     # strict mode (no empty modules)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Build wfctl if not already built
WFCTL="$ROOT_DIR/wfctl"
if [ ! -f "$WFCTL" ] || [ "$ROOT_DIR/cmd/wfctl/validate.go" -nt "$WFCTL" ]; then
    echo "Building wfctl..."
    (cd "$ROOT_DIR" && go build -o wfctl ./cmd/wfctl)
fi

EXTRA_FLAGS="${*}"

echo "Validating all workflow configs..."
echo

"$WFCTL" validate --dir "$ROOT_DIR/example/" "$ROOT_DIR/admin/config.yaml" $EXTRA_FLAGS
