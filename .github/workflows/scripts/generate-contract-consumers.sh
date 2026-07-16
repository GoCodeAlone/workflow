#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  generate-contract-consumers.sh (--output FILE | --check FILE) \
    [--consumer OWNER/REPO vSEMVER COMMIT_SHA CHECKOUT]...

Each consumer must identify a clean checkout of a released public repository.
HEAD and the local release tag must equal COMMIT_SHA. The root plugin.json
consumesContracts array is the only contract-selection authority; the first
GoReleaser build supplies the release entrypoint and version ldflags.
USAGE
}

output=""
check=""
declare -a repositories=()
declare -a refs=()
declare -a commits=()
declare -a checkouts=()

while (($# > 0)); do
  case "$1" in
    --output)
      (($# >= 2)) || { usage; exit 2; }
      output="$2"
      shift 2
      ;;
    --check)
      (($# >= 2)) || { usage; exit 2; }
      check="$2"
      shift 2
      ;;
    --consumer)
      (($# >= 5)) || { usage; exit 2; }
      repositories+=("$2")
      refs+=("$3")
      commits+=("$4")
      checkouts+=("$5")
      shift 5
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

if [[ -n "$output" && -n "$check" ]] || [[ -z "$output" && -z "$check" ]]; then
  echo "exactly one of --output or --check is required" >&2
  exit 2
fi

for command in git jq shasum awk grep sed sort uniq ruby; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 2; }
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
records="$tmp_dir/records.jsonl"
: >"$records"

for i in "${!repositories[@]}"; do
  repository="${repositories[$i]}"
  ref="${refs[$i]}"
  commit="${commits[$i]}"
  checkout_input="${checkouts[$i]}"

  if [[ ! "$repository" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$ ]]; then
    echo "invalid public GitHub repository: $repository" >&2
    exit 1
  fi
  if [[ ! "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "consumer ref must be an immutable vSEMVER release tag: $ref" >&2
    exit 1
  fi
  if [[ ! "$commit" =~ ^[0-9a-f]{40}$ ]]; then
    echo "consumer commit must be exactly 40 lowercase hexadecimal characters: $commit" >&2
    exit 1
  fi
  if [[ ! -d "$checkout_input" ]]; then
    echo "checkout not found: $checkout_input" >&2
    exit 1
  fi
  checkout="$(cd "$checkout_input" && pwd -P)"
  checkout_root="$(git -C "$checkout" rev-parse --show-toplevel 2>/dev/null || true)"
  if [[ -z "$checkout_root" || "$(cd "$checkout_root" && pwd -P)" != "$checkout" ]]; then
    echo "consumer checkout must be an exact Git worktree root: $checkout" >&2
    exit 1
  fi

  head_commit="$(git -C "$checkout" rev-parse HEAD 2>/dev/null || true)"
  if [[ "$head_commit" != "$commit" ]]; then
    echo "checkout HEAD $head_commit does not match claimed commit $commit" >&2
    exit 1
  fi
  tag_commit="$(git -C "$checkout" rev-parse "refs/tags/$ref^{commit}" 2>/dev/null || true)"
  if [[ "$tag_commit" != "$commit" ]]; then
    echo "release tag $ref does not resolve to checkout commit $commit" >&2
    exit 1
  fi
  origin="$(git -C "$checkout" config --get remote.origin.url 2>/dev/null || true)"
  case "$origin" in
    "https://github.com/$repository"|"https://github.com/$repository.git"|"git@github.com:$repository.git") ;;
    *)
      echo "checkout origin $origin does not match https://github.com/$repository" >&2
      exit 1
      ;;
  esac

  manifest="$checkout/plugin.json"
  if [[ ! -f "$manifest" ]]; then
    echo "manifest not found: $manifest" >&2
    exit 1
  fi
  release_config_names=()
  for candidate in .goreleaser.yaml .goreleaser.yml; do
    if [[ -f "$checkout/$candidate" ]]; then
      release_config_names+=("$candidate")
    fi
  done
  if ((${#release_config_names[@]} == 0)); then
    echo "GoReleaser config not found: $checkout" >&2
    exit 1
  fi
  if ((${#release_config_names[@]} != 1)); then
    echo "consumer release must contain exactly one GoReleaser config: $checkout" >&2
    exit 1
  fi
  release_config_name="${release_config_names[0]}"
  release_config="$checkout/$release_config_name"
  if [[ -n "$(git -C "$checkout" status --porcelain)" ]]; then
    echo "uncommitted files in checkout: $checkout" >&2
    exit 1
  fi

  if ! jq -e '
      (.name | type == "string" and length > 0) and
      (.version | type == "string" and length > 0) and
      (.consumesContracts | type == "array" and length > 0) and
      (all(.consumesContracts[];
        (.id | type == "string" and length > 0) and
        (.protocol | type == "object") and
        (.protocol.min | type == "number" and floor == . and . >= 1) and
        (.protocol.max | type == "number" and floor == . and . >= 1) and
        (.protocol.min <= .protocol.max))) and
      (([.consumesContracts[].id] | unique | length) == (.consumesContracts | length))
    ' "$manifest" >/dev/null; then
    echo "invalid consumesContracts manifest: $manifest" >&2
    exit 1
  fi

  name="$(jq -r '.name' "$manifest")"
  expected_name="${repository#*/}"
  if [[ "$name" != "$expected_name" ]]; then
    echo "manifest name $name does not match repository $repository" >&2
    exit 1
  fi
  manifest_repository="$(jq -r '.repository // empty' "$manifest")"
  if [[ -n "$manifest_repository" &&
        "$manifest_repository" != "https://github.com/$repository" &&
        "$manifest_repository" != "https://github.com/$repository.git" ]]; then
    echo "manifest repository $manifest_repository does not match $repository" >&2
    exit 1
  fi

  first_build="$tmp_dir/first-build-$i.json"
  ruby -ryaml -rjson - "$release_config" >"$first_build" <<'RUBY'
path = ARGV.fetch(0)
supported = %w[id main binary env goos goarch ldflags].freeze

def fail_build(message)
  warn message
  exit 1
end

begin
  document = Psych.parse_file(path)
  root = document&.root
  fail_build("GoReleaser config must be a YAML mapping: #{path}") unless root.is_a?(Psych::Nodes::Mapping)
  root_key_nodes = root.children.each_slice(2).map(&:first)
  fail_build("GoReleaser config contains a non-scalar root key") unless root_key_nodes.all? { |key| key.is_a?(Psych::Nodes::Scalar) }
  root_keys = root_key_nodes.map(&:value)
  duplicate_root = root_keys.group_by(&:itself).find { |_, values| values.length > 1 }&.first
  fail_build("GoReleaser config repeats root key: #{duplicate_root}") if duplicate_root
  builds_node = root.children.each_slice(2).find { |key, _| key.value == "builds" }&.last
  fail_build("GoReleaser config has no builds array: #{path}") unless builds_node.is_a?(Psych::Nodes::Sequence)
  first_node = builds_node.children.first
  fail_build("GoReleaser config has no first build mapping: #{path}") unless first_node.is_a?(Psych::Nodes::Mapping)
  key_nodes = first_node.children.each_slice(2).map(&:first)
  fail_build("first GoReleaser build contains a non-scalar key") unless key_nodes.all? { |key| key.is_a?(Psych::Nodes::Scalar) }
  keys = key_nodes.map(&:value)
  duplicate = keys.group_by(&:itself).find { |_, values| values.length > 1 }&.first
  fail_build("first GoReleaser build repeats key: #{duplicate}") if duplicate
  unknown = keys.find { |key| !supported.include?(key) }
  fail_build("unsupported first GoReleaser build key: #{unknown}") if unknown

  config = YAML.safe_load(
    File.read(path),
    permitted_classes: [],
    permitted_symbols: [],
    aliases: false,
    filename: path
  )
  first = config.fetch("builds").fetch(0)
  fail_build("first GoReleaser build must resolve to a mapping") unless first.is_a?(Hash)
  fail_build("first GoReleaser build resolves a non-string key") unless first.keys.all? { |key| key.is_a?(String) }
  resolved_unknown = first.keys.find { |key| !supported.include?(key) }
  fail_build("unsupported first GoReleaser build key: #{resolved_unknown}") if resolved_unknown
  puts JSON.generate(first)
rescue Psych::Exception, KeyError, IndexError => error
  fail_build("invalid GoReleaser config #{path}: #{error.message}")
end
RUBY
  if ! jq -e '
      type == "object" and
      (.main | type == "string") and
      (.binary | type == "string" and test("^[A-Za-z0-9._-]+$") and . != "." and . != "..") and
      (.env | type == "array" and length > 0 and all(.[]; type == "string")) and
      (.goos | type == "array" and length > 0 and
        all(.[]; type == "string" and test("^[a-z0-9]+$")) and (unique | length) == length) and
      (.goarch | type == "array" and length > 0 and
        all(.[]; type == "string" and test("^[a-z0-9]+$")) and (unique | length) == length) and
      (.ldflags | type == "array" and length > 0 and all(.[]; type == "string")) and
      ((has("id") | not) or (.id | type == "string" and test("^[A-Za-z0-9._-]+$")))
    ' "$first_build" >/dev/null; then
    echo "first GoReleaser build does not use the reproducible supported schema: $release_config" >&2
    exit 1
  fi

  main_package="$(jq -r '.main' "$first_build")"
  binary_name="$(jq -r '.binary' "$first_build")"
  if [[ ! "$main_package" =~ ^\./[A-Za-z0-9._/-]+$ ||
        "$main_package" == *"/../"* || "$main_package" == *"/.." ]]; then
    echo "first GoReleaser build has invalid main package: $main_package" >&2
    exit 1
  fi
  if [[ ! -d "$checkout/${main_package#./}" ]]; then
    echo "first GoReleaser main package not found: $main_package" >&2
    exit 1
  fi

  jq -r '.env[]' "$first_build" >"$tmp_dir/build-env-$i.txt"
  if [[ ! -s "$tmp_dir/build-env-$i.txt" ]]; then
    echo "first GoReleaser build must declare a reproducible environment: $release_config" >&2
    exit 1
  fi
  while IFS= read -r build_env_entry; do
    case "$build_env_entry" in
      CGO_ENABLED=0|CGO_ENABLED=1|GOPRIVATE=github.com/GoCodeAlone/'*') ;;
      *)
        echo "first GoReleaser build has unsupported environment entry: $build_env_entry" >&2
        exit 1
        ;;
    esac
  done <"$tmp_dir/build-env-$i.txt"
  if sed 's/=.*//' "$tmp_dir/build-env-$i.txt" | sort | uniq -d | grep -q .; then
    echo "first GoReleaser build repeats an environment key: $release_config" >&2
    exit 1
  fi
  build_env="$(jq -Rsc 'split("\n") | map(select(length > 0))' "$tmp_dir/build-env-$i.txt")"

  build_goos="$(jq -c '.goos' "$first_build")"
  build_goarch="$(jq -c '.goarch' "$first_build")"
  if ! jq -e 'index("linux") != null' <<<"$build_goos" >/dev/null; then
    echo "first GoReleaser build must release a linux build: $release_config" >&2
    exit 1
  fi
  if ! jq -e 'index("amd64") != null' <<<"$build_goarch" >/dev/null; then
    echo "first GoReleaser build must release an amd64 build: $release_config" >&2
    exit 1
  fi

  jq -r '.ldflags[]' "$first_build" >"$tmp_dir/release-ldflags-$i.txt"
  if [[ ! -s "$tmp_dir/release-ldflags-$i.txt" ]]; then
    echo "first GoReleaser build has no ldflags: $release_config" >&2
    exit 1
  fi
  while IFS= read -r release_ldflag; do
    if ! grep -Eq '^[A-Za-z0-9._~/:=+{} -]+$' <<<"$release_ldflag"; then
      echo "first GoReleaser build has unsupported ldflags: $release_ldflag" >&2
      exit 1
    fi
    without_versions="$(sed -E 's/\{\{[[:space:]]*\.Version[[:space:]]*\}\}//g' <<<"$release_ldflag")"
    if [[ "$without_versions" == *'{'* || "$without_versions" == *'}'* ]]; then
      echo "first GoReleaser build has unsupported ldflag template: $release_ldflag" >&2
      exit 1
    fi
  done <"$tmp_dir/release-ldflags-$i.txt"
  release_ldflags="$(jq -Rsc 'split("\n") | map(select(length > 0))' "$tmp_dir/release-ldflags-$i.txt")"

  { grep -hoE -- '-X [A-Za-z0-9._~/-]+=\{\{[[:space:]]*\.Version[[:space:]]*\}\}' "$tmp_dir/release-ldflags-$i.txt" || true; } \
    | sed -E 's/^-X ([A-Za-z0-9._~\/-]+)=.*/\1/' \
    | sort -u >"$tmp_dir/version-targets-$i.txt"
  if [[ ! -s "$tmp_dir/version-targets-$i.txt" ]]; then
    echo "first GoReleaser build has no {{.Version}} ldflag: $release_config" >&2
    exit 1
  fi
  version_ldflags="$(jq -Rsc 'split("\n") | map(select(length > 0))' "$tmp_dir/version-targets-$i.txt")"

  version="$(jq -r '.version' "$manifest")"
  manifest_sha="$(shasum -a 256 "$manifest" | awk '{print $1}')"
  release_config_sha="$(shasum -a 256 "$release_config" | awk '{print $1}')"
  contracts="$(jq -cS '[.consumesContracts[] | {id, protocol: {min: .protocol.min, max: .protocol.max}}] | sort_by(.id)' "$manifest")"
  jq -cS -n \
    --arg name "$name" \
    --arg repository "$repository" \
    --arg ref "$ref" \
    --arg commit "$commit" \
    --arg version "$version" \
    --arg manifest_sha "$manifest_sha" \
    --arg release_config "$release_config_name" \
    --arg release_config_sha "$release_config_sha" \
    --arg main_package "$main_package" \
    --arg binary_name "$binary_name" \
    --argjson build_env "$build_env" \
    --argjson build_goos "$build_goos" \
    --argjson build_goarch "$build_goarch" \
    --argjson release_ldflags "$release_ldflags" \
    --argjson version_ldflags "$version_ldflags" \
    --argjson contracts "$contracts" \
    '{
      name: $name,
      repository: $repository,
      ref: $ref,
      commit: $commit,
      manifestVersion: $version,
      manifestSha256: $manifest_sha,
      releaseConfig: $release_config,
      releaseConfigSha256: $release_config_sha,
      mainPackage: $main_package,
      binaryName: $binary_name,
      buildEnv: $build_env,
      buildGoos: $build_goos,
      buildGoarch: $build_goarch,
      releaseLdflags: $release_ldflags,
      versionLdflags: $version_ldflags,
      consumesContracts: $contracts
    }' >>"$records"
done

jq -S -s '{schemaVersion: 1, consumers: sort_by(.name)}' "$records" >"$tmp_dir/generated.json"
if ! jq -e '
    (.consumers | map(.name) | unique | length) == (.consumers | length) and
    (.consumers | map(.repository) | unique | length) == (.consumers | length)
  ' "$tmp_dir/generated.json" >/dev/null; then
  echo "duplicate consumer name or repository" >&2
  exit 1
fi

if [[ -n "$check" ]]; then
  if [[ ! -f "$check" ]] || ! cmp -s "$tmp_dir/generated.json" "$check"; then
    echo "stale generated cache: $check" >&2
    exit 1
  fi
  echo "contract consumer cache is current: $check"
  exit 0
fi

mkdir -p "$(dirname "$output")"
mv "$tmp_dir/generated.json" "$output"
echo "generated contract consumer cache: $output (${#repositories[@]} consumers)"
