#!/usr/bin/env bash
# Exact container candidate: permanent replacement authorized; actual admission decisions remain unbound.
set -euo pipefail
umask 077
case "${1-}" in
  prepare-scheduled|prepare-differential|scan-scheduled|scan-baseline|scan-candidate|report-scheduled|report-differential|gate-scheduled|gate-differential) ;;
  *) echo "invalid offline scanner phase" >&2; exit 2 ;;
esac
if [[ $# != 1 || -L "${BASH_SOURCE[0]}" ]]; then
  echo "offline bootstrap requires its canonical regular file and one phase" >&2
  exit 2
fi
bootstrap_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
case "$bootstrap_dir" in */scripts/osv-offline) ;; *) exit 2 ;; esac
for helper in run.py bootstrap-manifest.json; do
  if [[ ! -f "$bootstrap_dir/$helper" || -L "$bootstrap_dir/$helper" ]]; then
    echo "reviewed offline bootstrap input is missing" >&2
    exit 2
  fi
done
runner_sha256='1bf192a8283396234c9514e0fd66939b72fe95d38e806c070f3fd4a5299eb996'
manifest_sha256='b898780384a2d424b42b61e715d02d596e57ca829cb27c41ce94c4ed2fdd684e'
if [[ ! "$runner_sha256" =~ ^[0-9a-f]{64}$ || ! "$manifest_sha256" =~ ^[0-9a-f]{64}$ ]]; then
  echo "offline scanner exact source hashes are unresolved" >&2
  exit 2
fi
actual_runner="$(/usr/bin/sha256sum "$bootstrap_dir/run.py")"
actual_manifest="$(/usr/bin/sha256sum "$bootstrap_dir/bootstrap-manifest.json")"
if [[ "${actual_runner%% *}" != "$runner_sha256" || "${actual_manifest%% *}" != "$manifest_sha256" ]]; then
  echo "reviewed offline bootstrap input hash mismatch" >&2
  exit 2
fi
# Admission checks occur before Python can read any cached helper bytecode.
if [[ ! -d "${RUNNER_TEMP-}" || -L "${RUNNER_TEMP-}" ]]; then
  echo "trusted runner temporary parent is unavailable" >&2
  exit 2
fi
python_state="$(/usr/bin/mktemp -d -- "${RUNNER_TEMP%/}/osv-python-XXXXXXXXXX")"
/usr/bin/chmod 700 "$python_state"
if [[ -L "$python_state" || ! -d "$python_state" ||
      "$(/usr/bin/stat -c '%u' "$python_state")" != "$UID" ||
      "$(/usr/bin/stat -c '%a' "$python_state")" != 700 ]]; then
  echo "fresh Python state is not exclusively owned" >&2
  exit 2
fi
pycache_prefix="$python_state/pycache"
if [[ -e "$pycache_prefix" || -L "$pycache_prefix" ]]; then
  echo "fresh Python cache prefix must be absent" >&2
  exit 2
fi
# -I removes environment/cwd/user-site import authority; -S excludes site startup.
# -B blocks writes, while the fresh absent -X prefix prevents old .pyc reads.
# run.py must hash transitive helper source before importing it and own cleanup.
# No credentials or repository source are forwarded to public advisory services.
exec /usr/bin/python3 -I -S -B -X "pycache_prefix=$pycache_prefix" "$bootstrap_dir/run.py" "$1"
