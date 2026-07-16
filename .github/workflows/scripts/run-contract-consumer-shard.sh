#!/usr/bin/env bash
set -euo pipefail

# This script is hash-bound by the public-workflow policy. Binding the helper
# digest here makes every transitive PR executable part of the same review
# boundary: a generator edit necessarily requires a reviewed runner edit.
readonly generator_sha256="7fd9c1b5b6f20ca3bec9b1a76594666627bd291232af25451b332637be41bde7"

usage() {
  cat >&2 <<'USAGE'
Usage: run-contract-consumer-shard.sh \
  --workflow-dir DIR --wfctl BINARY --consumers-json JSON

Fetches each immutable public release, verifies its generated-cache metadata,
compiles it against DIR, and exercises its real binary through wfctl.
USAGE
}

workflow_dir=""
wfctl=""
consumers_json=""
while (($# > 0)); do
  case "$1" in
    --workflow-dir)
      (($# >= 2)) || { usage; exit 2; }
      workflow_dir="$2"
      shift 2
      ;;
    --wfctl)
      (($# >= 2)) || { usage; exit 2; }
      wfctl="$2"
      shift 2
      ;;
    --consumers-json)
      (($# >= 2)) || { usage; exit 2; }
      consumers_json="$2"
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

[[ -d "$workflow_dir" && -f "$workflow_dir/go.mod" ]] || { echo "Workflow checkout not found: $workflow_dir" >&2; exit 1; }
[[ -x "$wfctl" ]] || { echo "wfctl binary is not executable: $wfctl" >&2; exit 1; }
for command in git go jq awk; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 2; }
done
if ! command -v sha256sum >/dev/null && ! command -v shasum >/dev/null; then
  echo "sha256sum or shasum is required" >&2
  exit 2
fi

if ! jq -e '
    type == "array" and length > 0 and
    (all(.[];
      (.name | type == "string" and test("^[a-z0-9][a-z0-9-]*$")) and
      (.repository | type == "string" and test("^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")) and
      (.ref | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) and
      (.commit | type == "string" and test("^[0-9a-f]{40}$")) and
      (.manifestSha256 | type == "string" and test("^[0-9a-f]{64}$")) and
      (.releaseConfig == ".goreleaser.yaml" or .releaseConfig == ".goreleaser.yml") and
      (.releaseConfigSha256 | type == "string" and test("^[0-9a-f]{64}$")) and
      (.mainPackage | type == "string" and test("^\\./[A-Za-z0-9._/-]+$")) and
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
        all(.[]; type == "string" and test("^[A-Za-z0-9._~/-]+$"))) and
      (.consumesContracts | type == "array" and length > 0)))
  ' <<<"$consumers_json" >/dev/null; then
  echo "invalid contract consumer shard JSON" >&2
  exit 1
fi

repository_base="${CONTRACT_CONSUMER_REPOSITORY_BASE:-https://github.com}"
repository_base="${repository_base%/}"
if [[ "$repository_base" != "https://github.com" && ! -d "$repository_base" ]]; then
  echo "test repository base is not a local directory: $repository_base" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
workflow_dir="$(cd "$workflow_dir" && pwd -P)"
wfctl="$(cd "$(dirname "$wfctl")" && pwd -P)/$(basename "$wfctl")"
generator="$workflow_dir/.github/workflows/scripts/generate-contract-consumers.sh"
[[ -x "$generator" ]] || { echo "contract consumer generator is not executable: $generator" >&2; exit 1; }
actual_generator_sha256="$(sha256_file "$generator")"
if [[ "$actual_generator_sha256" != "$generator_sha256" ]]; then
  echo "contract consumer generator digest does not match the reviewed runner binding" >&2
  exit 1
fi
target_goos="$(go env GOOS)"
target_goarch="$(go env GOARCH)"

while IFS= read -r consumer; do
  name="$(jq -r '.name' <<<"$consumer")"
  repository="$(jq -r '.repository' <<<"$consumer")"
  ref="$(jq -r '.ref' <<<"$consumer")"
  expected_commit="$(jq -r '.commit' <<<"$consumer")"
  expected_manifest_sha="$(jq -r '.manifestSha256' <<<"$consumer")"
  release_config="$(jq -r '.releaseConfig' <<<"$consumer")"
  expected_release_config_sha="$(jq -r '.releaseConfigSha256' <<<"$consumer")"
  main_package="$(jq -r '.mainPackage' <<<"$consumer")"
  binary_name="$(jq -r '.binaryName' <<<"$consumer")"
  build_env=()
  while IFS= read -r build_env_entry; do
    build_env+=("$build_env_entry")
  done < <(jq -r '.buildEnv[]' <<<"$consumer")
  if ! jq -e --arg target "$target_goos" '.buildGoos | index($target) != null' \
      <<<"$consumer" >/dev/null; then
    echo "$repository release does not ship runner GOOS $target_goos" >&2
    exit 1
  fi
  if ! jq -e --arg target "$target_goarch" '.buildGoarch | index($target) != null' \
      <<<"$consumer" >/dev/null; then
    echo "$repository release does not ship runner GOARCH $target_goarch" >&2
    exit 1
  fi
  consumer_dir="$tmp_dir/$name"

  if [[ "$main_package" == *"/../"* || "$main_package" == *"/.." ]]; then
    echo "$repository has an unsafe generated main package" >&2
    exit 1
  fi

  mkdir "$consumer_dir"
  git -C "$consumer_dir" init --quiet
  git -C "$consumer_dir" remote add origin "${repository_base}/${repository}.git"
  git -C "$consumer_dir" \
    -c credential.helper= \
    -c core.askPass=/bin/false \
    -c credential.interactive=never \
    -c http.https://github.com/.extraheader= \
    fetch --no-tags --depth=1 origin "refs/tags/$ref:refs/tags/$ref"
  actual_commit="$(git -C "$consumer_dir" rev-parse "refs/tags/$ref^{commit}")"
  if [[ "$actual_commit" != "$expected_commit" ]]; then
    echo "$repository $ref resolved to $actual_commit, expected $expected_commit" >&2
    exit 1
  fi
  git -C "$consumer_dir" checkout --quiet --detach "$actual_commit"

  actual_manifest_sha="$(sha256_file "$consumer_dir/plugin.json")"
  if [[ "$actual_manifest_sha" != "$expected_manifest_sha" ]]; then
    echo "$repository $ref plugin.json does not match the generated cache" >&2
    exit 1
  fi
  actual_release_config_sha="$(sha256_file "$consumer_dir/$release_config")"
  if [[ "$actual_release_config_sha" != "$expected_release_config_sha" ]]; then
    echo "$repository $ref $release_config does not match the generated cache" >&2
    exit 1
  fi
  if [[ ! -d "$consumer_dir/${main_package#./}" ]]; then
    echo "$repository does not expose generated entrypoint $main_package" >&2
    exit 1
  fi

  # The cache is derived selection data. Recreate its complete record from the
  # fetched immutable release before trusting any cached build metadata.
  git -C "$consumer_dir" remote set-url origin "https://github.com/${repository}.git"
  regenerated="$tmp_dir/$name-regenerated.json"
  "$generator" --output "$regenerated" \
    --consumer "$repository" "$ref" "$expected_commit" "$consumer_dir" \
    >/dev/null
  cached_consumer="$(jq -cS '.' <<<"$consumer")"
  regenerated_consumer="$(jq -cS '.consumers[0]' "$regenerated")"
  if [[ "$cached_consumer" != "$regenerated_consumer" ]]; then
    echo "$repository $ref cached consumer record does not match regenerated release metadata" >&2
    exit 1
  fi

  (
    cd "$consumer_dir"
    env "${build_env[@]}" GOWORK=off go mod edit \
      -replace github.com/GoCodeAlone/workflow="$workflow_dir"
    env "${build_env[@]}" GOWORK=off go mod tidy
    env "${build_env[@]}" GOOS="$target_goos" GOARCH="$target_goarch" \
      GOWORK=off go build ./...

    release_version="${ref#v}"
    release_ldflags="$(jq -r --arg version "$release_version" '
      [.releaseLdflags[] |
        gsub("\\{\\{[[:space:]]*\\.Version[[:space:]]*\\}\\}"; $version)] |
      join(" ")
    ' <<<"$consumer")"
    binary_dir="$tmp_dir/bin/$name"
    mkdir -p "$binary_dir"
    binary_path="$binary_dir/$binary_name"
    env "${build_env[@]}" GOOS="$target_goos" GOARCH="$target_goarch" \
      GOWORK=off go build -ldflags="$release_ldflags" \
      -o "$binary_path" "$main_package"
    "$wfctl" plugin verify-capabilities \
      --binary "$binary_path" .
  )
done < <(jq -c '.[]' <<<"$consumers_json")

echo "contract consumer shard passed"
