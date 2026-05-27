#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: resolve-gh-run-for-ref.sh --workflow <name-or-file> --commit <sha> --event <event> --branch <branch> --created-after <timestamp> [--repo <owner/repo>]

Find exactly one GitHub Actions run matching the requested workflow, head SHA,
event, branch, and created-at lower bound. Prints the run database ID.
USAGE
}

workflow=""
commit=""
event=""
branch=""
created_after=""
repo=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --workflow)
      workflow="${2:-}"
      shift 2
      ;;
    --commit)
      commit="${2:-}"
      shift 2
      ;;
    --event)
      event="${2:-}"
      shift 2
      ;;
    --branch)
      branch="${2:-}"
      shift 2
      ;;
    --created-after)
      created_after="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

for required in workflow commit event branch created_after; do
  if [[ -z "${!required}" ]]; then
    echo "missing required argument: --${required//_/-}" >&2
    usage
    exit 2
  fi
done

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 127
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 127
fi

gh_args=(run list --workflow "$workflow" --limit 100 --json databaseId,headSha,event,headBranch,createdAt,url)
if [[ -n "$repo" ]]; then
  gh_args+=(--repo "$repo")
fi

matches="$(
  gh "${gh_args[@]}" |
    jq -r \
      --arg commit "$commit" \
      --arg event "$event" \
      --arg branch "$branch" \
      --arg created_after "$created_after" \
      '.[] | select(
        .headSha == $commit and
        .event == $event and
        .headBranch == $branch and
        .createdAt >= $created_after
      ) | "\(.databaseId)\t\(.createdAt)\t\(.url)"'
)"

count="$(printf '%s\n' "$matches" | sed '/^$/d' | wc -l | tr -d ' ')"
if [[ "$count" != "1" ]]; then
  echo "expected exactly one matching workflow run, found ${count}" >&2
  if [[ -n "$matches" ]]; then
    printf '%s\n' "$matches" >&2
  fi
  exit 1
fi

printf '%s\n' "$matches" | cut -f1
