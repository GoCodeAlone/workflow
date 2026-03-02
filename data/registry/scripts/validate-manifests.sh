#!/usr/bin/env bash
# validate-manifests.sh — validates every plugins/*/manifest.json against
# schema/registry-schema.json using ajv-cli (JSON Schema draft 2020-12).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCHEMA="$REPO_ROOT/schema/registry-schema.json"
PLUGINS_DIR="$REPO_ROOT/plugins"

errors=0

# Use the globally installed ajv if available, otherwise fall back to npx
AJV="${AJV:-$(command -v ajv 2>/dev/null || echo "npx --yes ajv-cli")}"

for manifest in "$PLUGINS_DIR"/*/manifest.json; do
  if ! $AJV validate --spec=draft2020 -s "$SCHEMA" -d "$manifest"; then
    errors=$((errors + 1))
  fi
done

if [[ $errors -gt 0 ]]; then
  echo ""
  echo "Manifest validation failed: $errors invalid manifest(s)."
  exit 1
else
  echo "All plugin manifests are valid."
fi
