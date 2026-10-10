#!/usr/bin/env bash
set -euo pipefail

launcher_source="${BASH_SOURCE[0]}"
if [[ -L "${launcher_source}" ]]; then
  echo "adoption guard launcher must not be a symlink" >&2
  exit 1
fi
if [[ "${launcher_source##*/}" != "run.sh" ]]; then
  echo "adoption guard launcher must use its canonical filename" >&2
  exit 1
fi
launcher_dir="$(cd -- "$(dirname -- "${launcher_source}")" && pwd -P)"
if [[ "${launcher_dir##*/}" != "adoptionguard" || ! -d "${launcher_dir}" || -L "${launcher_dir}" ]]; then
  echo "adoption guard launcher must reside in a real adoptionguard directory" >&2
  exit 1
fi
policytool_dir="${launcher_dir%/adoptionguard}"
if [[ "${policytool_dir}" == "${launcher_dir}" || "${policytool_dir##*/}" != "policytool" ||
  ! -d "${policytool_dir}" || -L "${policytool_dir}" ]]; then
  echo "adoption guard launcher could not locate the trusted policytool directory" >&2
  exit 1
fi
case "${policytool_dir}" in
  */.github/workflows/policytool) ;;
  *)
    echo "adoption guard launcher is outside the trusted policytool path" >&2
    exit 1
    ;;
esac

cd -- "${policytool_dir}"
exec env GOWORK=off GOFLAGS=-mod=readonly go run ./adoptionguard "$@"
