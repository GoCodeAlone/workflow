#!/usr/bin/env bash
set -euo pipefail

readonly protocol_test_source_sha256="1cd4fc033d7707c52e35a89a6076b6e6e84a794221ea7f22a7c5da5bfccebd1b"
readonly contract_path_map_sha256="191551ae08a824fdf7257ad85f0117841d37f4c02548750b077e5c0f54fc3f85"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "$script_dir/../../.." && pwd -P)"

usage() {
  cat >&2 <<'USAGE'
Usage: select-contract-consumers.sh \
  --cache FILE --path-map FILE --changed-paths FILE \
  [--base-sha SHA --head-sha SHA] \
  --output MATRIX_JSON --evidence EVIDENCE_JSON
USAGE
}

augment_evidence_usage() {
  cat >&2 <<'USAGE'
Usage: select-contract-consumers.sh augment-evidence \
  --evidence EVIDENCE_JSON --status STATUS --selector-exit-code CODE \
  --base-sha SHA --head-sha SHA --changed-paths FILE
USAGE
}

verify_runtime_protocols_usage() {
  cat >&2 <<'USAGE'
Usage: select-contract-consumers.sh verify-runtime-protocols --path-map FILE
USAGE
}

augment_evidence() {
  local evidence=""
  local status=""
  local selector_exit_code=""
  local base_sha=""
  local head_sha=""
  local changed_paths=""
  local base_sha_set=false
  local head_sha_set=false
  while (($# > 0)); do
    case "$1" in
      --evidence|--status|--selector-exit-code|--base-sha|--head-sha|--changed-paths)
        (($# >= 2)) || { augment_evidence_usage; return 2; }
        case "$1" in
          --evidence) evidence="$2" ;;
          --status) status="$2" ;;
          --selector-exit-code) selector_exit_code="$2" ;;
          --base-sha) base_sha="$2"; base_sha_set=true ;;
          --head-sha) head_sha="$2"; head_sha_set=true ;;
          --changed-paths) changed_paths="$2" ;;
        esac
        shift 2
        ;;
      -h|--help)
        augment_evidence_usage
        return 0
        ;;
      *)
        echo "unknown augment-evidence argument: $1" >&2
        augment_evidence_usage
        return 2
        ;;
    esac
  done
  if [[ -z "$evidence" || -z "$status" || -z "$selector_exit_code" ||
        -z "$changed_paths" || "$base_sha_set" != true || "$head_sha_set" != true ]]; then
    augment_evidence_usage
    return 2
  fi
  if [[ "$status" != failed && "$status" != succeeded ]]; then
    echo "--status must be failed or succeeded" >&2
    return 2
  fi
  if [[ ! "$selector_exit_code" =~ ^[0-9]+$ ]] || ((selector_exit_code > 255)); then
    echo "--selector-exit-code must be an integer from 0 through 255" >&2
    return 2
  fi
  if [[ "$changed_paths" == "$evidence" ]]; then
    echo "--changed-paths must not overwrite --evidence" >&2
    return 2
  fi

  local changed_paths_json='[]'
  if [[ -f "$changed_paths" ]]; then
    changed_paths_json="$(jq -Rsc 'split("\n") | map(select(length > 0))' "$changed_paths")"
  fi
  local existing='{}'
  if [[ -f "$evidence" ]] && jq -e '
      type == "object" and .schemaVersion == 1 and
      ((.changedPaths // []) | type == "array" and all(.[]; type == "string"))
    ' "$evidence" >/dev/null 2>&1; then
    existing="$(jq -cS '.' "$evidence")"
  fi

  mkdir -p "$(dirname "$evidence")"
  local temporary_evidence
  temporary_evidence="$(mktemp "${evidence}.tmp.XXXXXX")"
  if ! jq -S -n \
      --argjson existing "$existing" \
      --arg status "$status" \
      --argjson selector_exit_code "$selector_exit_code" \
      --arg base_sha "$base_sha" \
      --arg head_sha "$head_sha" \
      --argjson changed_paths "$changed_paths_json" '
        $existing + {
          schemaVersion: 1,
          status: $status,
          selectorExitCode: $selector_exit_code,
          baseSha: $base_sha,
          headSha: $head_sha,
          changedPaths: ($existing.changedPaths // $changed_paths)
        }
      ' >"$temporary_evidence"; then
    rm -f "$temporary_evidence"
    return 1
  fi
  mv "$temporary_evidence" "$evidence"
}

verify_runtime_protocols() {
  local path_map=""
  while (($# > 0)); do
    case "$1" in
      --path-map)
        (($# >= 2)) || { verify_runtime_protocols_usage; return 2; }
        path_map="$2"
        shift 2
        ;;
      -h|--help)
        verify_runtime_protocols_usage
        return 0
        ;;
      *)
        echo "unknown verify-runtime-protocols argument: $1" >&2
        verify_runtime_protocols_usage
        return 2
        ;;
    esac
  done
  if [[ -z "$path_map" ]]; then
    verify_runtime_protocols_usage
    return 2
  fi
  if [[ ! -f "$path_map" || -L "$path_map" ]]; then
    echo "path map must be a regular, non-symlink file: $path_map" >&2
    return 1
  fi

  local canonical_path_map
  canonical_path_map="$(jq -cS '.' "$path_map")" || return 1
  local actual_contract_path_map_sha256
  actual_contract_path_map_sha256="$(
    printf '%s\n' "$canonical_path_map" | shasum -a 256 | awk '{print $1}'
  )"
  if [[ "$actual_contract_path_map_sha256" != "$contract_path_map_sha256" ]]; then
    echo "contract path-map digest does not match trusted selector" >&2
    return 1
  fi

  local protocol_test_source="$repo_root/plugin/external/sdk/provider_services_protocol_test.go"
  if [[ ! -f "$protocol_test_source" || -L "$protocol_test_source" ]]; then
    echo "compiled runtime protocol test source must be a regular, non-symlink file: $protocol_test_source" >&2
    return 1
  fi
  local actual_protocol_test_source_sha256
  actual_protocol_test_source_sha256="$(shasum -a 256 "$protocol_test_source" | awk '{print $1}')"
  if [[ "$actual_protocol_test_source_sha256" != "$protocol_test_source_sha256" ]]; then
    echo "compiled runtime protocol test source digest does not match trusted selector" >&2
    return 1
  fi

  local protocol_test_output
  if ! protocol_test_output="$(
      cd "$repo_root"
      env -u GITHUB_OUTPUT -u GITHUB_ENV -u GITHUB_PATH -u GITHUB_STEP_SUMMARY \
        -u GITHUB_WORKSPACE -u RUNNER_TEMP GOWORK=off \
        go test -json -v ./plugin/external/sdk \
          -run '^TestContractPathMapRuntimeProtocols$' -count=1 2>&1
    )"; then
    echo "compiled runtime protocol authority test failed" >&2
    printf '%s\n' "$protocol_test_output" >&2
    return 1
  fi
  local protocol_test_pass_count
  if ! protocol_test_pass_count="$(
      jq -s '[.[] |
        select(.Action == "pass" and .Test == "TestContractPathMapRuntimeProtocols")
      ] | length' <<<"$protocol_test_output" 2>/dev/null
    )" || [[ "$protocol_test_pass_count" != 1 ]]; then
    echo "compiled runtime protocol authority test did not pass exactly once" >&2
    printf '%s\n' "$protocol_test_output" >&2
    return 1
  fi

  local protocol_markers
  protocol_markers="$(jq -cS -s '[.[] |
      select(
        .Action == "output" and
        .Test == "TestContractPathMapRuntimeProtocols" and
        (.Output | contains("WORKFLOW_CONTRACT_PROTOCOLS="))
      ) |
      .Output |
      capture("WORKFLOW_CONTRACT_PROTOCOLS=(?<protocols>\\{[^\\n]+\\})").protocols
    ]' <<<"$protocol_test_output")"
  if [[ "$(jq 'length' <<<"$protocol_markers")" -ne 1 ]]; then
    echo "compiled runtime protocol authority did not emit exactly one protocol record" >&2
    printf '%s\n' "$protocol_test_output" >&2
    return 1
  fi
  local runtime_protocols
  runtime_protocols="$(jq -er '.[0] | fromjson | tojson' <<<"$protocol_markers")" || {
    echo "compiled runtime protocol authority emitted invalid protocol JSON" >&2
    return 1
  }
  runtime_protocols="$(jq -cS '.' <<<"$runtime_protocols")"
  local expected_protocols
  expected_protocols="$(jq -cS '.contractProtocols' <<<"$canonical_path_map")"
  if [[ "$runtime_protocols" != "$expected_protocols" ]]; then
    echo "compiled runtime protocol authority does not match contract path map" >&2
    return 1
  fi
  echo "compiled runtime protocol authority matches contract path map"
}

if [[ "${1:-}" == augment-evidence ]]; then
  shift
  augment_evidence "$@"
  exit $?
fi
if [[ "${1:-}" == verify-runtime-protocols ]]; then
  shift
  verify_runtime_protocols "$@"
  exit $?
fi

cache=""
path_map=""
changed_paths=""
output=""
evidence=""
base_sha=""
head_sha=""
while (($# > 0)); do
  case "$1" in
    --cache|--path-map|--changed-paths|--output|--evidence|--base-sha|--head-sha)
      (($# >= 2)) || { usage; exit 2; }
      case "$1" in
        --cache) cache="$2" ;;
        --path-map) path_map="$2" ;;
        --changed-paths) changed_paths="$2" ;;
        --output) output="$2" ;;
        --evidence) evidence="$2" ;;
        --base-sha) base_sha="$2" ;;
        --head-sha) head_sha="$2" ;;
      esac
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

for required in cache path_map changed_paths output evidence; do
  eval "value=\${$required}"
  if [[ -z "$value" ]]; then
    echo "--${required//_/-} is required" >&2
    exit 2
  fi
done
for input in "$cache" "$path_map"; do
  if [[ ! -f "$input" || -L "$input" ]]; then
    echo "input must be a regular, non-symlink file: $input" >&2
    exit 1
  fi
done
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
base_cache=""
if [[ -n "$base_sha" || -n "$head_sha" ]]; then
  if [[ ! "$base_sha" =~ ^[0-9a-f]{40}$ || ! "$head_sha" =~ ^[0-9a-f]{40}$ ]]; then
    echo "--base-sha and --head-sha must both be exact lowercase 40-character commit SHAs" >&2
    exit 2
  fi
  command -v git >/dev/null || { echo "git is required" >&2; exit 2; }
  if [[ "$changed_paths" == "$cache" || "$changed_paths" == "$path_map" ||
        "$changed_paths" == "$output" || "$changed_paths" == "$evidence" ]]; then
    echo "--changed-paths output must not overwrite another selector file" >&2
    exit 2
  fi
  if [[ -L "$changed_paths" ]]; then
    echo "--changed-paths must not be a symlink" >&2
    exit 2
  fi
  mkdir -p "$(dirname "$changed_paths")"
  git diff --no-renames --name-only --diff-filter=ACDMRTUXB \
    "$base_sha...$head_sha" >"$changed_paths"
  base_cache="$tmp_dir/base-contract-consumers.json"
  if git cat-file -e "$base_sha:.github/contract-consumers.json" 2>/dev/null; then
    git show "$base_sha:.github/contract-consumers.json" >"$base_cache"
  else
    jq -S -n '{schemaVersion: 1, consumers: []}' >"$base_cache"
  fi
elif [[ ! -f "$changed_paths" || -L "$changed_paths" ]]; then
  echo "changed-path input must be a regular, non-symlink file: $changed_paths" >&2
  exit 1
fi
canonical_changed_paths="$(<"$changed_paths")"

if ! jq -e '
    .schemaVersion == 1 and
    (.consumers | type == "array") and
    (all(.consumers[];
      (.name | type == "string" and test("^[a-z0-9][a-z0-9-]*$")) and
      (.repository | type == "string" and test("^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")) and
      (.name == (.repository | split("/")[1])) and
      (.ref | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) and
      (.commit | type == "string" and test("^[0-9a-f]{40}$")) and
      (.manifestVersion | type == "string" and length > 0) and
      (.manifestSha256 | type == "string" and test("^[0-9a-f]{64}$")) and
      (.releaseConfig == ".goreleaser.yaml" or .releaseConfig == ".goreleaser.yml") and
      (.releaseConfigSha256 | type == "string" and test("^[0-9a-f]{64}$")) and
      (.mainPackage | type == "string" and test("^\\./[A-Za-z0-9._/-]+$") and (contains("/../") | not) and (endswith("/..") | not)) and
      (.binaryName | type == "string" and test("^[A-Za-z0-9._-]+$") and . != "." and . != "..") and
      (.buildEnv | type == "array" and length > 0 and
        all(.[]; . == "CGO_ENABLED=0" or . == "CGO_ENABLED=1" or . == "GOPRIVATE=github.com/GoCodeAlone/*") and
        (map(split("=")[0]) | unique | length) == length) and
      (.buildGoos | type == "array" and length > 0 and
        all(.[]; type == "string" and test("^[a-z0-9]+$")) and
        (index("linux") != null) and (unique | length) == length) and
      (.buildGoarch | type == "array" and length > 0 and
        all(.[]; type == "string" and test("^[a-z0-9]+$")) and
        (index("amd64") != null) and (unique | length) == length) and
      (.releaseLdflags | type == "array" and length > 0 and
        all(.[]; type == "string" and length > 0)) and
      (.versionLdflags | type == "array" and length > 0 and
        all(.[]; type == "string" and test("^[A-Za-z0-9._~/-]+$")) and (unique | length) == length) and
      (.consumesContracts | type == "array" and length > 0) and
      (all(.consumesContracts[];
        (.id | type == "string" and length > 0) and
        (.protocol.min | type == "number" and floor == . and . >= 1) and
        (.protocol.max | type == "number" and floor == . and . >= 1) and
        (.protocol.min <= .protocol.max))) and
      (([.consumesContracts[].id] | unique | length) == (.consumesContracts | length)) and
      ([.consumesContracts[].id] == ([.consumesContracts[].id] | sort)))) and
    ((.consumers | map(.name) | unique | length) == (.consumers | length)) and
    ((.consumers | map(.repository) | unique | length) == (.consumers | length)) and
    ((.consumers | map(.name)) == (.consumers | map(.name) | sort))
  ' "$cache" >/dev/null; then
  echo "invalid contract consumer cache: $cache" >&2
  exit 1
fi
canonical_cache="$(jq -cS '.' "$cache")"

if ! jq -e '
    .schemaVersion == 1 and
    (.retiredRepositories | type == "array" and
      all(.[]; type == "string" and test("^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")) and
      (unique | length) == length) and
    (.contractProtocols | type == "object" and length > 0) and
    (.contractProtocols as $protocols |
      all($protocols | to_entries[];
        (.key | type == "string" and length > 0) and
        (.value | type == "number" and floor == . and . >= 1))) and
    (.rules | type == "array" and length > 0) and
    (.contractProtocols as $protocols |
      all(.rules[];
        (.pattern | type == "string" and length > 0 and test("^[A-Za-z0-9._/*?+-]+$")) and
        (([has("contracts"), (.selectAll == true), (.nonContract == true)] | map(select(.)) | length) == 1) and
        (if has("contracts") then
          (.contracts | type == "array" and length > 0 and
            all(.[]; type == "string" and length > 0 and $protocols[.] != null) and
            (unique | length) == length)
         else true end))) and
    ((.rules | map(.pattern) | unique | length) == (.rules | length))
  ' "$path_map" >/dev/null; then
  echo "invalid contract path map: $path_map" >&2
  exit 1
fi

canonical_path_map="$(jq -cS '.' "$path_map")"
actual_contract_path_map_sha256="$(
  printf '%s\n' "$canonical_path_map" | shasum -a 256 | awk '{print $1}'
)"
if [[ "$actual_contract_path_map_sha256" != "$contract_path_map_sha256" ]]; then
  echo "contract path-map digest does not match trusted selector" >&2
  exit 1
fi

if [[ -n "$base_cache" ]]; then
  if ! jq -e '
      .schemaVersion == 1 and
      (.consumers | type == "array") and
      (all(.consumers[];
        (.name | type == "string" and test("^[a-z0-9][a-z0-9-]*$")) and
        (.repository | type == "string" and test("^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")) and
        (.name == (.repository | split("/")[1])) and
        (.ref | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) and
        (.commit | type == "string" and test("^[0-9a-f]{40}$")))) and
      ((.consumers | map(.name) | unique | length) == (.consumers | length)) and
      ((.consumers | map(.repository) | unique | length) == (.consumers | length))
    ' "$base_cache" >/dev/null; then
    echo "invalid base contract consumer cache: $base_cache" >&2
    exit 1
  fi
  canonical_base_cache="$(jq -cS '.' "$base_cache")"
  continuity="$(jq -cS -n \
    --argjson base "$canonical_base_cache" \
    --argjson candidate "$canonical_cache" \
    --argjson path_map "$canonical_path_map" '
      def semver: ltrimstr("v") | split(".") | map(tonumber);
      ([$base.consumers[].repository] - [$candidate.consumers[].repository] | unique | sort) as $removed |
      {
        unauthorizedRemovals: ($removed - $path_map.retiredRepositories),
        retiredStillPresent: [
          $candidate.consumers[].repository as $repository |
          select($path_map.retiredRepositories | index($repository) != null) |
          $repository
        ] | unique | sort,
        releaseRegressions: [
          $base.consumers[] as $existing |
          $candidate.consumers[] |
          select(.repository == $existing.repository) |
          select((.ref | semver) < ($existing.ref | semver)) |
          {repository, baseRef: $existing.ref, candidateRef: .ref}
        ],
        releaseMutations: [
          $base.consumers[] as $existing |
          $candidate.consumers[] |
          select(.repository == $existing.repository and .ref == $existing.ref) |
          select(.commit != $existing.commit) |
          {
            repository,
            ref,
            baseCommit: $existing.commit,
            candidateCommit: .commit
          }
        ]
      }
    ')"
  if [[ "$(jq '.unauthorizedRemovals | length' <<<"$continuity")" -ne 0 ]]; then
    echo "candidate contract consumer cache removes base consumers: $(jq -r '.unauthorizedRemovals | join(", ")' <<<"$continuity")" >&2
    exit 1
  fi
  if [[ "$(jq '.retiredStillPresent | length' <<<"$continuity")" -ne 0 ]]; then
    echo "candidate contract consumer cache retains retired repositories: $(jq -r '.retiredStillPresent | join(", ")' <<<"$continuity")" >&2
    exit 1
  fi
  if [[ "$(jq '.releaseRegressions | length' <<<"$continuity")" -ne 0 ]]; then
    echo "candidate contract consumer cache regresses base consumer releases: $(jq -c '.releaseRegressions' <<<"$continuity")" >&2
    exit 1
  fi
  if [[ "$(jq '.releaseMutations | length' <<<"$continuity")" -ne 0 ]]; then
    echo "candidate contract consumer cache mutates immutable base releases: $(jq -c '.releaseMutations' <<<"$continuity")" >&2
    exit 1
  fi
fi

protocol_test_source="$repo_root/plugin/external/sdk/provider_services_protocol_test.go"
if [[ ! -f "$protocol_test_source" || -L "$protocol_test_source" ]]; then
  echo "compiled runtime protocol test source must be a regular, non-symlink file: $protocol_test_source" >&2
  exit 1
fi
actual_protocol_test_source_sha256="$(shasum -a 256 "$protocol_test_source" | awk '{print $1}')"
if [[ "$actual_protocol_test_source_sha256" != "$protocol_test_source_sha256" ]]; then
  echo "compiled runtime protocol test source digest does not match trusted selector" >&2
  exit 1
fi

unknown_protocol_contracts="$(jq -cS -n \
  --argjson cache "$canonical_cache" \
  --argjson path_map "$canonical_path_map" '
    [$cache.consumers[].consumesContracts[].id] | unique |
    map(select($path_map.contractProtocols[.] == null))
  ')"
if [[ "$(jq 'length' <<<"$unknown_protocol_contracts")" -ne 0 ]]; then
  echo "contract consumer cache contains contracts with no Workflow protocol: $(jq -r 'join(", ")' <<<"$unknown_protocol_contracts")" >&2
  exit 1
fi

: >"$tmp_dir/contracts.txt"
: >"$tmp_dir/decisions.jsonl"
: >"$tmp_dir/paths.txt"

select_all=false
fallback_all=false
while IFS= read -r changed_path || [[ -n "$changed_path" ]]; do
  [[ -n "$changed_path" ]] || continue
  if [[ "$changed_path" == /* || "$changed_path" == ../* || "$changed_path" == */../* ]]; then
    echo "invalid changed path: $changed_path" >&2
    exit 1
  fi
  printf '%s\n' "$changed_path" >>"$tmp_dir/paths.txt"
  matched=false
  matched_pattern=""
  mode=""
  decision_contracts='[]'
  while IFS= read -r rule; do
    pattern="$(jq -r '.pattern' <<<"$rule")"
    # shellcheck disable=SC2053 # The validated RHS is intentionally a glob.
    if [[ "$changed_path" == $pattern ]]; then
      matched=true
      matched_pattern="$pattern"
      if [[ "$(jq -r '.selectAll // false' <<<"$rule")" == true ]]; then
        select_all=true
        mode="all-consumers"
      elif [[ "$(jq -r '.nonContract // false' <<<"$rule")" == true ]]; then
        mode="non-contract"
      else
        mode="contracts"
        decision_contracts="$(jq -cS '.contracts | sort' <<<"$rule")"
        jq -r '.contracts[]' <<<"$rule" >>"$tmp_dir/contracts.txt"
      fi
      break
    fi
  done < <(jq -c '.rules[]' <<<"$canonical_path_map")

  if [[ "$matched" == false ]]; then
    select_all=true
    fallback_all=true
    mode="fallback-all"
  fi
  jq -cS -n \
    --arg path "$changed_path" \
    --arg pattern "$matched_pattern" \
    --arg mode "$mode" \
    --argjson contracts "$decision_contracts" \
    '{path: $path, pattern: $pattern, mode: $mode, contracts: $contracts}' \
    >>"$tmp_dir/decisions.jsonl"
done <<<"$canonical_changed_paths"

if [[ ! -s "$tmp_dir/paths.txt" ]]; then
  select_all=true
  fallback_all=true
  jq -cS -n '{path: "", pattern: "", mode: "fallback-all", contracts: []}' \
    >>"$tmp_dir/decisions.jsonl"
fi

contract_ids="$(jq -Rsc 'split("\n") | map(select(length > 0)) | unique | sort' "$tmp_dir/contracts.txt")"
if [[ "$select_all" == true ]]; then
  selected="$(jq -cS '.consumers' <<<"$canonical_cache")"
else
  selected="$(jq -cS --argjson ids "$contract_ids" '
    [.consumers[] |
      select(any(.consumesContracts[]; .id as $id | $ids | index($id)))]
  ' <<<"$canonical_cache")"
fi

incompatible_consumers="$(jq -cS \
  --argjson ids "$contract_ids" \
  --argjson select_all "$select_all" \
  --argjson path_map "$canonical_path_map" '
    [.[] as $consumer |
      $consumer.consumesContracts[] as $contract |
      select($select_all or ($ids | index($contract.id))) |
      ($path_map.contractProtocols[$contract.id]) as $workflow_protocol |
      select($workflow_protocol < $contract.protocol.min or
        $workflow_protocol > $contract.protocol.max) |
      {
        consumer: $consumer.name,
        contract: $contract.id,
        min: $contract.protocol.min,
        max: $contract.protocol.max,
        workflowProtocol: $workflow_protocol
      }
    ]
  ' <<<"$selected")"

jq -S -n --argjson consumers "$selected" '
  ($consumers | length) as $count |
  {
    count: $count,
    include: [
      range(0; $count; 10) as $start |
      {
        shard: (($start / 10 | floor) + 1),
        consumers: $consumers[$start:($start + 10)]
      }
    ]
  }
' >"$tmp_dir/matrix.json"

paths_json="$(jq -Rsc 'split("\n") | map(select(length > 0))' "$tmp_dir/paths.txt")"
decisions_json="$(jq -s '.' "$tmp_dir/decisions.jsonl")"
selected_names="$(jq -cS '[.[].name]' <<<"$selected")"
selected_count="$(jq 'length' <<<"$selected")"
jq -S -n \
  --argjson changed_paths "$paths_json" \
  --argjson decisions "$decisions_json" \
  --argjson contracts "$contract_ids" \
  --argjson selected_names "$selected_names" \
  --argjson selected_count "$selected_count" \
  --argjson incompatible_consumers "$incompatible_consumers" \
  --argjson select_all "$select_all" \
  --argjson fallback_all "$fallback_all" \
  '{
    schemaVersion: 1,
    changedPaths: $changed_paths,
    decisions: $decisions,
    contracts: $contracts,
    selectAll: $select_all,
    fallbackAll: $fallback_all,
    selectedCount: $selected_count,
    selectedConsumers: $selected_names,
    incompatibleConsumers: $incompatible_consumers
  }' >"$tmp_dir/evidence.json"

mkdir -p "$(dirname "$output")" "$(dirname "$evidence")"
mv "$tmp_dir/matrix.json" "$output"
mv "$tmp_dir/evidence.json" "$evidence"
if [[ "$(jq 'length' <<<"$incompatible_consumers")" -ne 0 ]]; then
  jq -r '.[] |
    "\(.consumer) consumes \(.contract) at protocol \(.min)..\(.max) and does not support Workflow protocol \(.workflowProtocol)"' \
    <<<"$incompatible_consumers" >&2
  exit 1
fi
echo "selected $selected_count contract consumer(s)"
