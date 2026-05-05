#!/usr/bin/env bash
# pre-commit-gofmt.sh — abort commit if any staged .go files are not gofmt-clean.
# Install: ln -sf ../../scripts/pre-commit-gofmt.sh .git/hooks/pre-commit

set -euo pipefail

bad=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' | xargs -r gofmt -l)
if [[ -n "$bad" ]]; then
  echo "gofmt: the following files need formatting (run: gofmt -w <file>):"
  echo "$bad" | sed 's/^/  /'
  exit 1
fi
