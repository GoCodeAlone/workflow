#!/usr/bin/env bash
set -euo pipefail

wrapper_source="${BASH_SOURCE[0]}"
if [[ -L "${wrapper_source}" ]]; then
  echo "policy wrapper path must not be a symlink" >&2
  exit 1
fi
# Resolve ancestor-directory aliases physically; all later confinement checks
# and repository paths use only this canonical directory.
wrapper_dir="$(cd -- "$(dirname -- "${wrapper_source}")" && pwd -P)"
if [[ "${wrapper_source##*/}" != "check-public-workflow-policy.sh" ]]; then
  echo "policy wrapper must use its canonical filename" >&2
  exit 1
fi
case "${wrapper_dir}" in
  */scripts) ;;
  *)
    echo "policy wrapper must reside in scripts" >&2
    exit 1
    ;;
esac
repo_root="${wrapper_dir%/scripts}"
for required_dir in \
  "${repo_root}" \
  "${repo_root}/.github" \
  "${repo_root}/.github/workflows" \
  "${repo_root}/scripts"; do
  if [[ ! -d "${required_dir}" || -L "${required_dir}" ]]; then
    echo "policy wrapper repository path is not a canonical directory" >&2
    exit 1
  fi
done
policytool="${repo_root}/.github/workflows/policytool"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    LC_ALL=C shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "no SHA-256 implementation available" >&2
    exit 1
  fi
}

verify_policytool_layout() {
  while IFS= read -r policytool_path; do
    relative_path="${policytool_path#"${policytool}/"}"
    if [[ -d "${policytool_path}" && ! -L "${policytool_path}" ]]; then
      continue
    fi
    if [[ ! -f "${policytool_path}" || -L "${policytool_path}" ]]; then
      echo "policytool path must be a regular non-symlink file: ${relative_path}" >&2
      exit 1
    fi
  done < <(find "${policytool}" -mindepth 1 -print | LC_ALL=C sort)
}

verify_policytool_file() {
  local relative_path="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "${policytool}/${relative_path}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "policytool hash mismatch for ${relative_path}" >&2
    exit 1
  fi
}

verify_policytool() {
  verify_policytool_layout
  verify_policytool_file main.go 14dfbc962052c523912b248ee3acfdf32d4fa4bdf882a465b482e07cac0e3ad7
  verify_policytool_file main_test.go 811390919697081fc474b28bc899967ef7520b9c617aee4ebe38ba8ba092096f
  verify_policytool_file go.mod b29b886c829ddf6f83b795df62b92093a6c06d1c5a0ff6842dc36ae352094790
  verify_policytool_file go.sum 790ef858e5aeed12269a69e764ac69c02c3877678b0e7d9384ad3728b6e09f6c
}

cd "${policytool}"
verify_policytool
env GOWORK=off GOFLAGS=-mod=readonly go mod download
verify_policytool
exec env GOWORK=off GOFLAGS=-mod=readonly go run ./main.go --repo "${repo_root}" "$@"
