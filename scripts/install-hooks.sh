#!/usr/bin/env bash
# Installs git hooks from scripts/ into .git/hooks/
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

for hook in "$REPO_ROOT/scripts/pre-push"; do
    name="$(basename "$hook")"
    cp "$hook" "$HOOKS_DIR/$name"
    chmod +x "$HOOKS_DIR/$name"
    echo "Installed $name hook"
done

echo "All hooks installed."
