#!/usr/bin/env bash
# validate-templates.sh — checks that every plugin listed in templates/*/plugins_required
# has a matching manifest in plugins/*/manifest.json with the same "name" field.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLUGINS_DIR="$REPO_ROOT/plugins"
TEMPLATES_DIR="$REPO_ROOT/templates"

errors=0

# Build a lookup set of known plugin names from manifests
declare -A known_plugins
for manifest in "$PLUGINS_DIR"/*/manifest.json; do
  plugin_name=$(grep -oP '"name"\s*:\s*"\K[^"]+' "$manifest" | head -1)
  if [[ -n "$plugin_name" ]]; then
    known_plugins["$plugin_name"]=1
  fi
done

echo "Known plugins: ${!known_plugins[*]}"
echo ""

# For each template, extract plugins_required entries and validate them
for template in "$TEMPLATES_DIR"/*.yaml; do
  template_name=$(basename "$template")
  in_plugins_required=0
  while IFS= read -r line; do
    # Detect the plugins_required block
    if [[ "$line" =~ ^plugins_required: ]]; then
      in_plugins_required=1
      continue
    fi
    # Exit the block on a new top-level key
    if [[ $in_plugins_required -eq 1 ]] && [[ "$line" =~ ^[a-z] ]]; then
      in_plugins_required=0
    fi
    # Process list items in the block
    if [[ $in_plugins_required -eq 1 ]] && [[ "$line" =~ ^[[:space:]]*-[[:space:]]+(.+)$ ]]; then
      plugin_ref="${BASH_REMATCH[1]}"
      if [[ -z "${known_plugins[$plugin_ref]+_}" ]]; then
        echo "ERROR: $template_name references unknown plugin '$plugin_ref'"
        errors=$((errors + 1))
      fi
    fi
  done < "$template"
done

if [[ $errors -gt 0 ]]; then
  echo ""
  echo "Validation failed: $errors unresolved plugin reference(s)."
  exit 1
else
  echo "All template plugin references are valid."
fi
