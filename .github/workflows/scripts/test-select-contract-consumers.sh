#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
generator="${script_dir}/generate-contract-consumers.sh"
selector="${script_dir}/select-contract-consumers.sh"
runner="${script_dir}/run-contract-consumer-shard.sh"
path_map="${repo_root}/.github/contract-path-map.json"
committed_cache="${repo_root}/.github/contract-consumers.json"
workflow="${repo_root}/.github/workflows/cross-plugin-build-test.yml"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

expect_failure() {
  local expected="$1"
  shift
  local output
  if output="$("$@" 2>&1)"; then
    fail "command unexpectedly succeeded: $*"
  fi
  if [[ "$output" != *"$expected"* ]]; then
    fail "failure did not contain '$expected': $output"
  fi
}

write_manifest() {
  local path="$1"
  local name="$2"
  local contract_id="$3"
  local protocol_min="${4:-1}"
  local protocol_max="${5:-1}"
  jq -n \
    --arg name "$name" \
    --arg contract_id "$contract_id" \
    --argjson protocol_min "$protocol_min" \
    --argjson protocol_max "$protocol_max" \
    '{
      name: $name,
      version: "0.0.0",
      author: "contract selector fixture",
      description: "contract selector fixture",
      repository: ("https://github.com/GoCodeAlone/" + $name),
      consumesContracts: [{id: $contract_id, protocol: {min: $protocol_min, max: $protocol_max}}]
    }' >"$path"
}

create_release_checkout() {
  local checkout="$1"
  local name="$2"
  local contract_id="$3"
  local ref="$4"
  local main_package="$5"
  local protocol_min="${6:-1}"
  local protocol_max="${7:-1}"
  local release_mode="${8:-supported}"
  local binary_name="${9:-$name}"
  local module_path="example.com/$name"
  local main_dir="$checkout/${main_package#./}"

  mkdir -p "$main_dir" "$checkout/internal"
  write_manifest "$checkout/plugin.json" "$name" "$contract_id" "$protocol_min" "$protocol_max"
  printf '%s\n' \
    "module $module_path" \
    "" \
    "go 1.26.5" \
    "" \
    "require github.com/GoCodeAlone/workflow v0.0.0" \
    >"$checkout/go.mod"
  printf '%s\n' \
    'package internal' \
    '' \
    'var Version = "0.0.0"' \
    'var ReleaseMode = "dev"' \
    >"$checkout/internal/version.go"
  printf '%s\n' \
    'package main' \
    '' \
    'import (' \
    '  "fmt"' \
    '  "github.com/GoCodeAlone/workflow/config"' \
    "  \"$module_path/internal\"" \
    ')' \
    '' \
    'func main() {' \
    '  protocol := config.ProtocolVersionRange{Min: 1, Max: 1}' \
    '  fmt.Printf("%s:%s:%d", internal.Version, internal.ReleaseMode, protocol.Min)' \
    '}' \
    >"$main_dir/main.go"
  printf '%s\n' \
    '//go:build cgo' \
    '' \
    'package main' \
    '' \
    'var _ = releaseBuildMustDisableCGO' \
    >"$main_dir/cgo_enabled.go"
  printf '%s\n' \
    'version: 2' \
    "project_name: $name" \
    'builds:' \
    "  - id: $name" \
    "    main: $main_package" \
    "    binary: $binary_name" \
    '    env:' \
    '      - CGO_ENABLED=0' \
    '      - GOPRIVATE=github.com/GoCodeAlone/*' \
    '    goos: [linux, darwin]' \
    '    goarch: [amd64, arm64]' \
    '    ldflags:' \
    "      - -s -w -X $module_path/internal.Version={{.Version}} -X $module_path/internal.ReleaseMode=release" \
    >"$checkout/.goreleaser.yaml"
  if [[ "$release_mode" == unsupported-tags ]]; then
    awk '
      /^    ldflags:/ { print "    tags: [integration]" }
      { print }
    ' "$checkout/.goreleaser.yaml" >"$checkout/.goreleaser.yaml.tmp"
    mv "$checkout/.goreleaser.yaml.tmp" "$checkout/.goreleaser.yaml"
  elif [[ "$release_mode" == unsupported-gcflags ]]; then
    awk '
      /^    ldflags:/ { print "    gcflags: [all=-N]" }
      { print }
    ' "$checkout/.goreleaser.yaml" >"$checkout/.goreleaser.yaml.tmp"
    mv "$checkout/.goreleaser.yaml.tmp" "$checkout/.goreleaser.yaml"
  elif [[ "$release_mode" == quoted-unsupported-gcflags ]]; then
    awk '
      /^    ldflags:/ { print "    \"gcflags\": [all=-N]" }
      { print }
    ' "$checkout/.goreleaser.yaml" >"$checkout/.goreleaser.yaml.tmp"
    mv "$checkout/.goreleaser.yaml.tmp" "$checkout/.goreleaser.yaml"
  elif [[ "$release_mode" == unsupported-target ]]; then
    sed 's/goos: \[linux, darwin\]/goos: [plan9]/' \
      "$checkout/.goreleaser.yaml" >"$checkout/.goreleaser.yaml.tmp"
    mv "$checkout/.goreleaser.yaml.tmp" "$checkout/.goreleaser.yaml"
  elif [[ "$release_mode" == duplicate-root-builds ]]; then
    printf '%s\n' \
      'builds:' \
      "  - id: $name-duplicate" \
      "    main: $main_package" \
      "    binary: $name" \
      '    env: [CGO_ENABLED=0]' \
      '    goos: [linux, darwin]' \
      '    goarch: [amd64, arm64]' \
      '    tags: [integration]' \
      '    ldflags:' \
      "      - -s -w -X $module_path/internal.Version={{.Version}}" \
      >>"$checkout/.goreleaser.yaml"
  elif [[ "$release_mode" == dual-release-config ]]; then
    sed "s/binary: $binary_name/binary: yml-$binary_name/" \
      "$checkout/.goreleaser.yaml" >"$checkout/.goreleaser.yml"
  fi

  git -C "$checkout" init --quiet
  git -C "$checkout" config user.name fixture
  git -C "$checkout" config user.email fixture@example.com
  git -C "$checkout" add .
  git -C "$checkout" commit --quiet -m "fixture release"
  git -C "$checkout" tag "$ref"
  git -C "$checkout" remote add origin "https://github.com/GoCodeAlone/${name}.git"
}

create_sdk_release_checkout() {
  local checkout="$1"
  local name="$2"
  local contract_id="$3"
  local ref="$4"
  local main_package="$5"
  local binary_name="$6"
  local module_path="example.com/$name"
  local main_file="$checkout/${main_package#./}/main.go"

  create_release_checkout "$checkout" "$name" "$contract_id" "$ref" \
    "$main_package" 1 1 supported "$binary_name"
  printf '%s\n' \
    'package main' \
    '' \
    'import (' \
    "  \"$module_path/internal\"" \
    '  sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"' \
    ')' \
    '' \
    'type provider struct{}' \
    '' \
    'func (provider) Manifest() sdk.PluginManifest {' \
    "  return sdk.PluginManifest{Name: \"$name\", Version: internal.Version, Author: \"contract selector fixture\", Description: \"contract selector fixture\"}" \
    '}' \
    '' \
    'func main() {' \
    '  sdk.Serve(provider{}, sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))' \
    '}' \
    >"$main_file"
  git -C "$checkout" add "$main_file"
  git -C "$checkout" commit --quiet --amend --no-edit
  git -C "$checkout" tag --force "$ref" >/dev/null
}

assert_json() {
  local file="$1"
  local expression="$2"
  local message="$3"
  jq -e "$expression" "$file" >/dev/null || fail "$message"
}

[[ -x "$generator" ]] || fail "missing executable generator: $generator"
[[ -x "$selector" ]] || fail "missing executable selector: $selector"
[[ -x "$runner" ]] || fail "missing executable consumer runner: $runner"
[[ -f "$path_map" ]] || fail "missing path map: $path_map"
[[ -f "$committed_cache" ]] || fail "missing committed cache: $committed_cache"
grep -Fq 'augment-evidence' "$workflow" || \
  fail "public workflow does not use the production evidence augmenter"
awk '
  /trap record_selection_failure EXIT/ { trapped = NR }
  /pull request SHAs must be exactly/ { validated = NR }
  END { exit !(trapped && validated && trapped < validated) }
' "$workflow" || fail "public workflow does not establish failure evidence before validation"
# shellcheck disable=SC2016 # The workflow condition is matched literally.
grep -Fq '[[ ! -f "$input" || -L "$input" ]]' "$workflow" || \
  fail "public workflow does not reject symlinked artifact inputs"
awk '
  /for input in \.github\/contract-consumers\.json \.github\/contract-path-map\.json/ { guarded = NR }
  /cp \.github\/contract-consumers\.json contract-selection\/contract-consumers\.json/ { copied = NR }
  END { exit !(guarded && copied && guarded < copied) }
' "$workflow" || fail "public workflow validates artifact inputs only after copying them"
awk '
  /cp \.github\/contract-consumers\.json contract-selection\/contract-consumers\.json/ { copied = NR }
  /\.\/\.github\/workflows\/scripts\/select-contract-consumers\.sh/ { selected = NR }
  END { exit !(copied && selected && copied < selected) }
' "$workflow" || fail "public workflow does not preserve selector inputs before invocation"
grep -Fq 'TestContractPathMapRuntimeProtocols' "$selector" || \
  fail "runtime verifier does not run the compiled protocol authority test"
selection_job="$(awk '
  /^  select-contract-consumers:/ { capture = 1 }
  /^  contract-consumer-compatibility:/ { capture = 0 }
  capture { print }
' "$workflow")"
if grep -Eq 'actions/setup-go|(^|[[:space:]])go[[:space:]]+(test|run|build)' <<<"$selection_job"; then
  fail "authoritative selection job executes candidate Go code"
fi
protocol_test_source="$repo_root/plugin/external/sdk/provider_services_protocol_test.go"
if grep -Ein '(^|[^[:alnum:]_])(aws|azure|gcp|digitalocean|doctl)([^[:alnum:]_]|$)' \
    "$protocol_test_source" "$path_map" "$workflow" \
    "$generator" "$selector" "$runner"; then
  fail "provider-specific identifier crossed the contract-consumer boundary"
fi
protocol_test_source_sha256="$(shasum -a 256 "$protocol_test_source" | awk '{print $1}')"
grep -Fq "readonly protocol_test_source_sha256=\"$protocol_test_source_sha256\"" "$selector" || \
  fail "production selector does not bind the compiled runtime protocol test source digest"

generator_sha256="$(shasum -a 256 "$generator" | awk '{print $1}')"
grep -Fq "readonly generator_sha256=\"$generator_sha256\"" "$runner" || \
  fail "consumer runner does not bind the exact generator executable digest"

assert_contract_protocol() {
  local contract_id="$1"
  local source_file="$2"
  local constant="$3"
  local expected
  local actual
  expected="$(jq -r --arg id "$contract_id" '.contractProtocols[$id] // empty' "$path_map")"
  actual="$(awk -v constant="$constant" '
    $1 == constant && $2 == "=" {
      gsub(/"/, "", $3)
      print $3
      exit
    }
  ' "$repo_root/$source_file")"
  [[ "$expected" =~ ^[0-9]+$ && "$actual" == "$expected" ]] || \
    fail "$contract_id path-map protocol $expected does not match SDK constant $actual"
  grep -Eq "ProtocolVersion:[[:space:]]+${constant}," \
    "$repo_root/plugin/external/sdk/provider_services.go" || \
    fail "$constant is not the protocol advertised through ContractRegistry"
}

assert_contract_protocol workflow.provider.credential-issuer \
  plugin/external/sdk/credential_issuer_server.go CredentialIssuerProtocolVersion
assert_contract_protocol workflow.provider.credential-resolver \
  plugin/external/sdk/credential_resolver_server.go CredentialResolverProtocolVersion
assert_contract_protocol workflow.provider.container-registry \
  plugin/external/sdk/container_registry_server.go ContainerRegistryProtocolVersion
assert_contract_protocol workflow.provider.secret-store \
  plugin/external/sdk/secret_store_server.go SecretStoreProtocolVersion

tmp="$(mktemp -d)"
protocol_test_backup="$tmp/provider-services-protocol-test.go"
protocol_skip_test="$repo_root/plugin/external/sdk/zz_protocol_skip_test.go"
protocol_redirect_test="$repo_root/plugin/external/sdk/zz_protocol_redirect_test.go"
protocol_rewrite_test="$repo_root/plugin/external/sdk/zz_protocol_rewrite_test.go"
protocol_cache_rewrite_test="$repo_root/plugin/external/sdk/zz_protocol_cache_rewrite_test.go"
protocol_paths_rewrite_test="$repo_root/plugin/external/sdk/zz_protocol_paths_rewrite_test.go"
path_map_backup="$tmp/contract-path-map-original.json"
selector_backup="$tmp/select-contract-consumers-original.sh"
cleanup() {
  if [[ -f "$protocol_test_backup" ]]; then
    cp "$protocol_test_backup" "$protocol_test_source"
  fi
  if [[ -f "$path_map_backup" ]]; then
    cp "$path_map_backup" "$path_map"
  fi
  if [[ -f "$selector_backup" ]]; then
    cp "$selector_backup" "$selector"
  fi
  rm -f "$protocol_skip_test" "$protocol_redirect_test" "$protocol_rewrite_test" \
    "$protocol_cache_rewrite_test" "$protocol_paths_rewrite_test"
  rm -rf "$tmp"
}
trap cleanup EXIT

printf '%s\n' docs/PLUGIN_DEVELOPMENT.md >"$tmp/no-test-paths.txt"
printf '%s\n' module/new-boundary.go >"$tmp/weakened-rule-paths.txt"

# Evidence collection must never dereference candidate-controlled input
# symlinks into an uploaded artifact or selector authority.
ln -s "$committed_cache" "$tmp/cache-symlink.json"
expect_failure "input must be a regular, non-symlink file" \
  "$selector" \
    --cache "$tmp/cache-symlink.json" \
    --path-map "$path_map" \
    --changed-paths "$tmp/no-test-paths.txt" \
    --output "$tmp/cache-symlink-matrix.json" \
    --evidence "$tmp/cache-symlink-evidence.json"
ln -s "$path_map" "$tmp/path-map-symlink.json"
expect_failure "input must be a regular, non-symlink file" \
  "$selector" \
    --cache "$committed_cache" \
    --path-map "$tmp/path-map-symlink.json" \
    --changed-paths "$tmp/no-test-paths.txt" \
    --output "$tmp/path-map-symlink-matrix.json" \
    --evidence "$tmp/path-map-symlink-evidence.json"

# Compiled runtime parity executes only in the output-free wire-validation job.
"$selector" verify-runtime-protocols --path-map "$path_map"

# The compiled authority source is part of the reviewed executable boundary.
# Any deletion, rename, or edit must fail until the selector digest is reviewed.
cp "$protocol_test_source" "$protocol_test_backup"
printf '%s\n' '// unreviewed protocol authority mutation' >>"$protocol_test_source"
expect_failure "compiled runtime protocol test source digest does not match trusted selector" \
  "$selector" \
    --cache "$committed_cache" \
    --path-map "$path_map" \
    --changed-paths "$tmp/no-test-paths.txt" \
    --output "$tmp/source-mutation-matrix.json" \
    --evidence "$tmp/source-mutation-evidence.json"
cp "$protocol_test_backup" "$protocol_test_source"
rm "$protocol_test_backup"

# A successful package test is not proof that the named authority test ran.
# Rewrite only the requested test name while still delegating to the real Go
# toolchain; the selector must reject Go's successful "no tests to run" result.
real_go="$(command -v go)"
mkdir -p "$tmp/no-test-bin"
# shellcheck disable=SC2016 # These expressions belong to the generated wrapper.
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'args=("$@")' \
  'for index in "${!args[@]}"; do' \
  '  if [[ "${args[$index]}" == "^TestContractPathMapRuntimeProtocols$" ]]; then' \
  '    args[$index]="^TestContractPathMapRuntimeProtocols_DOES_NOT_EXIST$"' \
  '  fi' \
  'done' \
  'exec "$REAL_GO" "${args[@]}"' \
  >"$tmp/no-test-bin/go"
chmod +x "$tmp/no-test-bin/go"
expect_failure "compiled runtime protocol authority test did not pass exactly once" \
  env REAL_GO="$real_go" PATH="$tmp/no-test-bin:$PATH" \
  "$selector" verify-runtime-protocols --path-map "$path_map"

# A candidate TestMain can also exit successfully before running the authority
# test. Requiring its exact JSON pass event closes that package-level bypass.
printf '%s\n' \
  'package sdk' \
  '' \
  'import (' \
  '  "os"' \
  '  "testing"' \
  ')' \
  '' \
  'func TestMain(*testing.M) {' \
  '  os.Exit(0)' \
  '}' \
  >"$protocol_skip_test"
expect_failure "compiled runtime protocol authority test did not pass exactly once" \
  "$selector" verify-runtime-protocols --path-map "$path_map"
rm "$protocol_skip_test"

# Rule semantics share the trusted path-map boundary with protocol values.
jq '.rules = [{pattern: "*", nonContract: true}]' \
  "$path_map" >"$tmp/weakened-rule-path-map.json"
expect_failure "contract path-map digest does not match trusted selector" \
  "$selector" \
    --cache "$committed_cache" \
    --path-map "$tmp/weakened-rule-path-map.json" \
    --changed-paths "$tmp/weakened-rule-paths.txt" \
    --output "$tmp/weakened-rule-matrix.json" \
    --evidence "$tmp/weakened-rule-evidence.json"

# The output-free runtime verifier compares against its parent-owned map and
# ignores any legacy candidate environment redirect.
jq '.contractProtocols["workflow.provider.credential-issuer"] = 2' \
  "$path_map" >"$tmp/redirected-protocol-path-map.json"
path_map_go_literal="$(jq -Rn --arg value "$tmp/redirected-protocol-path-map.json" '$value')"
printf '%s\n' \
  'package sdk' \
  '' \
  'import "os"' \
  '' \
  'func init() {' \
  "  if err := os.Setenv(\"WORKFLOW_CONTRACT_PATH_MAP\", $path_map_go_literal); err != nil {" \
  '    panic(err)' \
  '  }' \
  '}' \
  >"$protocol_redirect_test"
"$selector" verify-runtime-protocols --path-map "$path_map"
rm "$protocol_redirect_test"

# Generator: exact clean release checkouts are authoritative. Input order does
# not affect bytes, alternate GoReleaser entrypoints are preserved, and stale,
# dirty, or missing provenance fails closed.
alpha_checkout="$tmp/workflow-plugin-alpha"
zeta_checkout="$tmp/workflow-plugin-zeta"
create_release_checkout "$alpha_checkout" workflow-plugin-alpha workflow.provider.credential-issuer v1.0.0 ./cmd/workflow-plugin-alpha
create_release_checkout "$zeta_checkout" workflow-plugin-zeta workflow.provider.credential-resolver v1.2.3 ./cmd/plugin 1 1 supported released-zeta
alpha_commit="$(git -C "$alpha_checkout" rev-parse HEAD)"
zeta_commit="$(git -C "$zeta_checkout" rev-parse HEAD)"

"$generator" --output "$tmp/generated.json" \
  --consumer GoCodeAlone/workflow-plugin-zeta v1.2.3 "$zeta_commit" "$zeta_checkout" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"
"$generator" --output "$tmp/generated-reordered.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout" \
  --consumer GoCodeAlone/workflow-plugin-zeta v1.2.3 "$zeta_commit" "$zeta_checkout"
cmp -s "$tmp/generated.json" "$tmp/generated-reordered.json" || fail "generated cache is not canonical byte-for-byte"
assert_json "$tmp/generated.json" '
  (.consumers | map(.name)) == ["workflow-plugin-alpha", "workflow-plugin-zeta"] and
  .consumers[0].mainPackage == "./cmd/workflow-plugin-alpha" and
  .consumers[1].mainPackage == "./cmd/plugin" and
  .consumers[0].binaryName == "workflow-plugin-alpha" and
  .consumers[1].binaryName == "released-zeta" and
  .consumers[0].buildEnv == ["CGO_ENABLED=0", "GOPRIVATE=github.com/GoCodeAlone/*"] and
  .consumers[1].buildEnv == ["CGO_ENABLED=0", "GOPRIVATE=github.com/GoCodeAlone/*"] and
  .consumers[0].buildGoos == ["linux", "darwin"] and
  .consumers[1].buildGoos == ["linux", "darwin"] and
  .consumers[0].buildGoarch == ["amd64", "arm64"] and
  .consumers[1].buildGoarch == ["amd64", "arm64"] and
  .consumers[0].releaseLdflags == ["-s -w -X example.com/workflow-plugin-alpha/internal.Version={{.Version}} -X example.com/workflow-plugin-alpha/internal.ReleaseMode=release"] and
  .consumers[1].releaseLdflags == ["-s -w -X example.com/workflow-plugin-zeta/internal.Version={{.Version}} -X example.com/workflow-plugin-zeta/internal.ReleaseMode=release"] and
  .consumers[0].versionLdflags == ["example.com/workflow-plugin-alpha/internal.Version"] and
  (.consumers | all(.releaseConfig == ".goreleaser.yaml" and (.releaseConfigSha256 | length) == 64))
' "generator did not preserve canonical release build metadata"

dual_config_checkout="$tmp/workflow-plugin-dual-config"
create_release_checkout "$dual_config_checkout" workflow-plugin-dual-config \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 dual-release-config
dual_config_commit="$(git -C "$dual_config_checkout" rev-parse HEAD)"
expect_failure "exactly one GoReleaser config" \
  "$generator" --output "$tmp/dual-config.json" \
    --consumer GoCodeAlone/workflow-plugin-dual-config v1.0.0 \
      "$dual_config_commit" "$dual_config_checkout"

tagged_checkout="$tmp/workflow-plugin-tagged"
create_release_checkout "$tagged_checkout" workflow-plugin-tagged \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 unsupported-tags
tagged_commit="$(git -C "$tagged_checkout" rev-parse HEAD)"
expect_failure "unsupported first GoReleaser build key: tags" \
  "$generator" --output "$tmp/tagged.json" \
    --consumer GoCodeAlone/workflow-plugin-tagged v1.0.0 "$tagged_commit" "$tagged_checkout"

gcflag_checkout="$tmp/workflow-plugin-gcflag"
create_release_checkout "$gcflag_checkout" workflow-plugin-gcflag \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 unsupported-gcflags
gcflag_commit="$(git -C "$gcflag_checkout" rev-parse HEAD)"
expect_failure "unsupported first GoReleaser build key: gcflags" \
  "$generator" --output "$tmp/gcflag.json" \
    --consumer GoCodeAlone/workflow-plugin-gcflag v1.0.0 "$gcflag_commit" "$gcflag_checkout"

quoted_gcflag_checkout="$tmp/workflow-plugin-quoted-gcflag"
create_release_checkout "$quoted_gcflag_checkout" workflow-plugin-quoted-gcflag \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 quoted-unsupported-gcflags
quoted_gcflag_commit="$(git -C "$quoted_gcflag_checkout" rev-parse HEAD)"
expect_failure "unsupported first GoReleaser build key: gcflags" \
  "$generator" --output "$tmp/quoted-gcflag.json" \
    --consumer GoCodeAlone/workflow-plugin-quoted-gcflag v1.0.0 \
      "$quoted_gcflag_commit" "$quoted_gcflag_checkout"

duplicate_builds_checkout="$tmp/workflow-plugin-duplicate-builds"
create_release_checkout "$duplicate_builds_checkout" workflow-plugin-duplicate-builds \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 duplicate-root-builds
duplicate_builds_commit="$(git -C "$duplicate_builds_checkout" rev-parse HEAD)"
expect_failure "GoReleaser config repeats root key: builds" \
  "$generator" --output "$tmp/duplicate-builds.json" \
    --consumer GoCodeAlone/workflow-plugin-duplicate-builds v1.0.0 \
      "$duplicate_builds_commit" "$duplicate_builds_checkout"

unsupported_target_checkout="$tmp/workflow-plugin-unsupported-target"
create_release_checkout "$unsupported_target_checkout" workflow-plugin-unsupported-target \
  workflow.provider.credential-issuer v1.0.0 ./cmd/plugin 1 1 unsupported-target
unsupported_target_commit="$(git -C "$unsupported_target_checkout" rev-parse HEAD)"
expect_failure "must release a linux build" \
  "$generator" --output "$tmp/unsupported-target.json" \
    --consumer GoCodeAlone/workflow-plugin-unsupported-target v1.0.0 \
      "$unsupported_target_commit" "$unsupported_target_checkout"

expect_failure "checkout not found" \
  "$generator" --output "$tmp/missing.json" \
  --consumer GoCodeAlone/workflow-plugin-missing v1.0.0 3333333333333333333333333333333333333333 "$tmp/missing-plugin"
expect_failure "immutable vSEMVER" \
  "$generator" --output "$tmp/prerelease.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0-rc.1 "$alpha_commit" "$alpha_checkout"
expect_failure "checkout HEAD" \
  "$generator" --output "$tmp/wrong-commit.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 0000000000000000000000000000000000000000 "$alpha_checkout"

mv "$alpha_checkout/plugin.json" "$alpha_checkout/plugin.json.missing"
expect_failure "manifest not found" \
  "$generator" --output "$tmp/missing-manifest.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"
mv "$alpha_checkout/plugin.json.missing" "$alpha_checkout/plugin.json"
jq '.description = "dirty"' "$alpha_checkout/plugin.json" >"$tmp/dirty.json"
mv "$tmp/dirty.json" "$alpha_checkout/plugin.json"
expect_failure "uncommitted files in checkout" \
  "$generator" --output "$tmp/dirty.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"
git -C "$alpha_checkout" restore plugin.json
printf '%s\n' 'untracked release input' >"$alpha_checkout/untracked.txt"
expect_failure "uncommitted files in checkout" \
  "$generator" --output "$tmp/dirty-source.json" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"
rm "$alpha_checkout/untracked.txt"

"$generator" --check "$tmp/generated.json" \
  --consumer GoCodeAlone/workflow-plugin-zeta v1.2.3 "$zeta_commit" "$zeta_checkout" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"
jq '.consumers[0].manifestSha256 = ("0" * 64)' "$tmp/generated.json" >"$tmp/stale.json"
expect_failure "stale generated cache" \
  "$generator" --check "$tmp/stale.json" \
  --consumer GoCodeAlone/workflow-plugin-zeta v1.2.3 "$zeta_commit" "$zeta_checkout" \
  --consumer GoCodeAlone/workflow-plugin-alpha v1.0.0 "$alpha_commit" "$alpha_checkout"

# An empty cache is correct until released consumers declare consumesContracts.
"$generator" --check "$committed_cache"

# Exercise the real shard orchestration against hermetic release repositories.
# The fake wfctl is the host dependency seam; it executes the real built plugin
# binary so entrypoint, release-ldflag, and PR-Workflow replacement behavior are
# observable without a live external service.
mkdir -p "$tmp/remotes/GoCodeAlone"
git clone --quiet --bare "$alpha_checkout" "$tmp/remotes/GoCodeAlone/workflow-plugin-alpha.git"
git clone --quiet --bare "$zeta_checkout" "$tmp/remotes/GoCodeAlone/workflow-plugin-zeta.git"
# shellcheck disable=SC2016 # These literals are the generated script body.
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  '[[ "$1" == plugin && "$2" == verify-capabilities && "$3" == --binary ]]' \
  'printf "%s:" "$(basename "$4")" >>"$FAKE_WFCTL_LOG"' \
  '"$4" >>"$FAKE_WFCTL_LOG"' \
  'printf "\n" >>"$FAKE_WFCTL_LOG"' \
  >"$tmp/fake-wfctl"
chmod +x "$tmp/fake-wfctl"
FAKE_WFCTL_LOG="$tmp/loaded.log" \
CONTRACT_CONSUMER_REPOSITORY_BASE="$tmp/remotes" \
  "$runner" \
    --workflow-dir "$repo_root" \
    --wfctl "$tmp/fake-wfctl" \
    --consumers-json "$(jq -c '.consumers' "$tmp/generated.json")"
[[ "$(tr '\n' ',' <"$tmp/loaded.log")" == "workflow-plugin-alpha:1.0.0:release:1,released-zeta:1.2.3:release:1," ]] || \
  fail "consumer runner did not build both release entrypoints against the PR Workflow API"

# Cross the real loader boundary even while the committed consumer cache is
# empty: build one released SDK plugin, then make the production runner invoke
# the real wfctl handshake and capability verifier against that binary.
real_checkout="$tmp/workflow-plugin-real-consumer"
create_sdk_release_checkout "$real_checkout" workflow-plugin-real-consumer \
  workflow.provider.credential-issuer v1.3.0 ./cmd/plugin released-real-consumer
real_commit="$(git -C "$real_checkout" rev-parse HEAD)"
"$generator" --output "$tmp/real-consumer.json" \
  --consumer GoCodeAlone/workflow-plugin-real-consumer v1.3.0 \
    "$real_commit" "$real_checkout"
git clone --quiet --bare "$real_checkout" \
  "$tmp/remotes/GoCodeAlone/workflow-plugin-real-consumer.git"
(
  cd "$repo_root"
  GOWORK=off go build -o "$tmp/real-wfctl" ./cmd/wfctl
)
real_loader_output="$(
  CONTRACT_CONSUMER_REPOSITORY_BASE="$tmp/remotes" \
    "$runner" \
      --workflow-dir "$repo_root" \
      --wfctl "$tmp/real-wfctl" \
      --consumers-json "$(jq -c '.consumers' "$tmp/real-consumer.json")"
)"
[[ "$real_loader_output" == *"OK    workflow-plugin-real-consumer 1.3.0"* &&
   "$real_loader_output" == *"contract consumer shard passed"* ]] || \
  fail "consumer runner did not cross the real wfctl/plugin handshake: $real_loader_output"

# Cache metadata is derived data, not independent authority. A cache-only edit
# must be rejected after the immutable release config is fetched and parsed.
jq -c '[.consumers[0] |
  .releaseLdflags = ["-s -w -X example.com/workflow-plugin-alpha/internal.Version={{.Version}} -X example.com/workflow-plugin-alpha/internal.ReleaseMode=tampered"]
]' "$tmp/generated.json" >"$tmp/tampered-consumer.json"
expect_failure "does not match regenerated release metadata" \
  env \
    FAKE_WFCTL_LOG="$tmp/tampered-loaded.log" \
    CONTRACT_CONSUMER_REPOSITORY_BASE="$tmp/remotes" \
    "$runner" \
      --workflow-dir "$repo_root" \
      --wfctl "$tmp/fake-wfctl" \
      --consumers-json "$(jq -c '.' "$tmp/tampered-consumer.json")"

# Build a 12-consumer cache. Six consume the issuer contract and six the
# resolver contract, which proves exact selection and the >10 sharding rule.
consumer_args=()
for i in $(seq 1 12); do
  name="workflow-plugin-fixture-$(printf '%02d' "$i")"
  contract="workflow.provider.credential-resolver"
  if ((i <= 6)); then
    contract="workflow.provider.credential-issuer"
  fi
  checkout="$tmp/$name"
  create_release_checkout "$checkout" "$name" "$contract" "v1.0.$i" ./cmd/plugin
  commit="$(git -C "$checkout" rev-parse HEAD)"
  consumer_args+=(--consumer "GoCodeAlone/$name" "v1.0.$i" "$commit" "$checkout")
done
"$generator" --output "$tmp/consumers.json" "${consumer_args[@]}"

# Exercise the production changed-path extraction. Git rename detection would
# otherwise report only docs/credential_issuer.md and hide the removed contract
# path, producing an empty selection.
rename_repo="$tmp/rename-repo"
mkdir -p "$rename_repo/plugin/external/proto"
git -C "$rename_repo" init --quiet
git -C "$rename_repo" config user.name fixture
git -C "$rename_repo" config user.email fixture@example.com
printf '%s\n' 'syntax = "proto3";' >"$rename_repo/plugin/external/proto/credential_issuer.proto"
git -C "$rename_repo" add .
git -C "$rename_repo" commit --quiet -m base
rename_base="$(git -C "$rename_repo" rev-parse HEAD)"
mkdir -p "$rename_repo/docs"
git -C "$rename_repo" mv plugin/external/proto/credential_issuer.proto docs/credential_issuer.md
git -C "$rename_repo" commit --quiet -m rename
rename_head="$(git -C "$rename_repo" rev-parse HEAD)"
(
  cd "$rename_repo"
  "$selector" \
    --cache "$tmp/consumers.json" \
    --path-map "$path_map" \
    --base-sha "$rename_base" \
    --head-sha "$rename_head" \
    --changed-paths "$tmp/rename-changed-paths.txt" \
    --output "$tmp/rename-matrix.json" \
    --evidence "$tmp/rename-evidence.json"
)
assert_json "$tmp/rename-matrix.json" '.count == 6' \
  "contract-to-documentation rename did not select affected consumers"
[[ "$(sort <"$tmp/rename-changed-paths.txt")" == $'docs/credential_issuer.md\nplugin/external/proto/credential_issuer.proto' ]] || \
  fail "rename extraction did not preserve both deleted and added paths"

# Candidate additions and updates are rederived by the runner, but removing a
# released consumer must not make an all-consumer decision vacuous.
cache_removal_repo="$tmp/cache-removal-repo"
mkdir -p "$cache_removal_repo/.github"
git -C "$cache_removal_repo" init --quiet
git -C "$cache_removal_repo" config user.name fixture
git -C "$cache_removal_repo" config user.email fixture@example.com
cp "$tmp/generated.json" "$cache_removal_repo/.github/contract-consumers.json"
git -C "$cache_removal_repo" add .github/contract-consumers.json
git -C "$cache_removal_repo" commit --quiet -m base
cache_removal_base="$(git -C "$cache_removal_repo" rev-parse HEAD)"
cp "$committed_cache" "$cache_removal_repo/.github/contract-consumers.json"
git -C "$cache_removal_repo" commit --quiet -am remove
cache_removal_head="$(git -C "$cache_removal_repo" rev-parse HEAD)"
(
  cd "$cache_removal_repo"
  expect_failure "candidate contract consumer cache removes base consumers" \
    "$selector" \
      --cache .github/contract-consumers.json \
      --path-map "$path_map" \
      --base-sha "$cache_removal_base" \
      --head-sha "$cache_removal_head" \
      --changed-paths "$tmp/cache-removal-paths.txt" \
      --output "$tmp/cache-removal-matrix.json" \
      --evidence "$tmp/cache-removal-evidence.json"
)

# Repository identity, not a same-named basename, is the continuity authority.
identity_substitution_repo="$tmp/identity-substitution-repo"
mkdir -p "$identity_substitution_repo/.github"
git -C "$identity_substitution_repo" init --quiet
git -C "$identity_substitution_repo" config user.name fixture
git -C "$identity_substitution_repo" config user.email fixture@example.com
cp "$tmp/generated.json" "$identity_substitution_repo/.github/contract-consumers.json"
git -C "$identity_substitution_repo" add .github/contract-consumers.json
git -C "$identity_substitution_repo" commit --quiet -m base
identity_substitution_base="$(git -C "$identity_substitution_repo" rev-parse HEAD)"
jq '(.consumers[] | select(.name == "workflow-plugin-alpha") | .repository) = "Attacker/workflow-plugin-alpha"' \
  "$tmp/generated.json" >"$identity_substitution_repo/.github/contract-consumers.json"
git -C "$identity_substitution_repo" commit --quiet -am substitute
identity_substitution_head="$(git -C "$identity_substitution_repo" rev-parse HEAD)"
(
  cd "$identity_substitution_repo"
  expect_failure "candidate contract consumer cache removes base consumers: GoCodeAlone/workflow-plugin-alpha" \
    "$selector" \
      --cache .github/contract-consumers.json \
      --path-map "$path_map" \
      --base-sha "$identity_substitution_base" \
      --head-sha "$identity_substitution_head" \
      --changed-paths "$tmp/identity-substitution-paths.txt" \
      --output "$tmp/identity-substitution-matrix.json" \
      --evidence "$tmp/identity-substitution-evidence.json"
)

# Existing repositories cannot silently regress to an older immutable release.
release_regression_repo="$tmp/release-regression-repo"
mkdir -p "$release_regression_repo/.github"
git -C "$release_regression_repo" init --quiet
git -C "$release_regression_repo" config user.name fixture
git -C "$release_regression_repo" config user.email fixture@example.com
cp "$tmp/generated.json" "$release_regression_repo/.github/contract-consumers.json"
git -C "$release_regression_repo" add .github/contract-consumers.json
git -C "$release_regression_repo" commit --quiet -m base
release_regression_base="$(git -C "$release_regression_repo" rev-parse HEAD)"
jq '(.consumers[] | select(.name == "workflow-plugin-zeta") | .ref) = "v1.2.2"' \
  "$tmp/generated.json" >"$release_regression_repo/.github/contract-consumers.json"
git -C "$release_regression_repo" commit --quiet -am regress
release_regression_head="$(git -C "$release_regression_repo" rev-parse HEAD)"
(
  cd "$release_regression_repo"
  expect_failure "candidate contract consumer cache regresses base consumer releases" \
    "$selector" \
      --cache .github/contract-consumers.json \
      --path-map "$path_map" \
      --base-sha "$release_regression_base" \
      --head-sha "$release_regression_head" \
      --changed-paths "$tmp/release-regression-paths.txt" \
      --output "$tmp/release-regression-matrix.json" \
      --evidence "$tmp/release-regression-evidence.json"
)

# An existing immutable tag cannot be rebound to a different commit.
release_mutation_repo="$tmp/release-mutation-repo"
mkdir -p "$release_mutation_repo/.github"
git -C "$release_mutation_repo" init --quiet
git -C "$release_mutation_repo" config user.name fixture
git -C "$release_mutation_repo" config user.email fixture@example.com
cp "$tmp/generated.json" "$release_mutation_repo/.github/contract-consumers.json"
git -C "$release_mutation_repo" add .github/contract-consumers.json
git -C "$release_mutation_repo" commit --quiet -m base
release_mutation_base="$(git -C "$release_mutation_repo" rev-parse HEAD)"
jq '(.consumers[] | select(.name == "workflow-plugin-alpha") | .commit) = "dddddddddddddddddddddddddddddddddddddddd"' \
  "$tmp/generated.json" >"$release_mutation_repo/.github/contract-consumers.json"
git -C "$release_mutation_repo" commit --quiet -am mutate
release_mutation_head="$(git -C "$release_mutation_repo" rev-parse HEAD)"
(
  cd "$release_mutation_repo"
  expect_failure "candidate contract consumer cache mutates immutable base releases" \
    "$selector" \
      --cache .github/contract-consumers.json \
      --path-map "$path_map" \
      --base-sha "$release_mutation_base" \
      --head-sha "$release_mutation_head" \
      --changed-paths "$tmp/release-mutation-paths.txt" \
      --output "$tmp/release-mutation-matrix.json" \
      --evidence "$tmp/release-mutation-evidence.json"
)

# Retirement is an explicit reviewed authority change: the exact repository is
# removed from the cache in the same change that adds it to the digest-bound map.
retirement_repo="$tmp/retirement-repo"
mkdir -p "$retirement_repo/.github"
git -C "$retirement_repo" init --quiet
git -C "$retirement_repo" config user.name fixture
git -C "$retirement_repo" config user.email fixture@example.com
cp "$tmp/generated.json" "$retirement_repo/.github/contract-consumers.json"
git -C "$retirement_repo" add .github/contract-consumers.json
git -C "$retirement_repo" commit --quiet -m base
retirement_base="$(git -C "$retirement_repo" rev-parse HEAD)"
jq 'del(.consumers[] | select(.repository == "GoCodeAlone/workflow-plugin-alpha"))' \
  "$tmp/generated.json" >"$retirement_repo/.github/contract-consumers.json"
git -C "$retirement_repo" commit --quiet -am retire
retirement_head="$(git -C "$retirement_repo" rev-parse HEAD)"
cp -p "$selector" "$selector_backup"
cp "$path_map" "$path_map_backup"
jq '.retiredRepositories = ["GoCodeAlone/workflow-plugin-alpha"]' \
  "$path_map_backup" >"$path_map"
reviewed_path_map_sha256="$(jq -cS '.' "$path_map" | shasum -a 256 | awk '{print $1}')"
awk -v digest="$reviewed_path_map_sha256" '
  /^readonly contract_path_map_sha256=/ {
    print "readonly contract_path_map_sha256=\"" digest "\""
    next
  }
  { print }
' "$selector_backup" >"$selector"
chmod +x "$selector"
(
  cd "$retirement_repo"
  "$selector" \
    --cache .github/contract-consumers.json \
    --path-map "$path_map" \
    --base-sha "$retirement_base" \
    --head-sha "$retirement_head" \
    --changed-paths "$tmp/retirement-paths.txt" \
    --output "$tmp/retirement-matrix.json" \
    --evidence "$tmp/retirement-evidence.json"
)
assert_json "$tmp/retirement-matrix.json" '.count == 1' \
  "reviewed repository retirement did not preserve remaining consumers"
cp "$selector_backup" "$selector"
cp "$path_map_backup" "$path_map"
rm "$selector_backup" "$path_map_backup"

run_selector() {
  local changed_path="$1"
  printf '%s\n' "$changed_path" >"$tmp/changed-paths.txt"
  "$selector" \
    --cache "$tmp/consumers.json" \
    --path-map "$path_map" \
    --changed-paths "$tmp/changed-paths.txt" \
    --output "$tmp/matrix.json" \
    --evidence "$tmp/evidence.json"
}

# Authoritative selection must never execute candidate Go. This init would
# rewrite the reviewed map and leave a marker if the selector regressed.
cp "$path_map" "$path_map_backup"
weakened_path_map_literal="$(jq -Rs '.' <"$tmp/weakened-rule-path-map.json")"
path_map_literal="$(jq -Rn --arg value "$path_map" '$value')"
candidate_go_marker_literal="$(jq -Rn --arg value "$tmp/candidate-go-ran" '$value')"
printf '%s\n' \
  'package sdk' \
  '' \
  'import "os"' \
  '' \
  'func init() {' \
  "  if err := os.WriteFile($path_map_literal, []byte($weakened_path_map_literal), 0o644); err != nil {" \
  '    panic(err)' \
  '  }' \
  "  if err := os.WriteFile($candidate_go_marker_literal, []byte(\"ran\\n\"), 0o644); err != nil {" \
  '    panic(err)' \
  '  }' \
  '}' \
  >"$protocol_rewrite_test"
run_selector module/post-check-rewrite.go
[[ ! -e "$tmp/candidate-go-ran" ]] || \
  fail "authoritative selection executed candidate Go initialization"
cp "$path_map_backup" "$path_map"
rm "$path_map_backup" "$protocol_rewrite_test"
assert_json "$tmp/matrix.json" '.count == 12' \
  "post-check path-map rewrite changed the frozen selection rules"
assert_json "$tmp/evidence.json" '.selectAll == true and .selectedCount == 12' \
  "post-check path-map rewrite weakened fail-closed selection"

# A second candidate init attempts to rewrite the validated cache.
cp "$tmp/consumers.json" "$tmp/consumers-before-rewrite.json"
consumer_cache_literal="$(jq -Rn --arg value "$tmp/consumers.json" '$value')"
printf '%s\n' \
  'package sdk' \
  '' \
  'import "os"' \
  '' \
  'func init() {' \
  "  if err := os.WriteFile($consumer_cache_literal, []byte(\"{\\\"schemaVersion\\\":1,\\\"consumers\\\":[]}\\n\"), 0o644); err != nil {" \
  '    panic(err)' \
  '  }' \
  '}' \
  >"$protocol_cache_rewrite_test"
run_selector module/post-check-cache-rewrite.go
cp "$tmp/consumers-before-rewrite.json" "$tmp/consumers.json"
rm "$protocol_cache_rewrite_test"
assert_json "$tmp/matrix.json" '.count == 12' \
  "post-check cache rewrite changed the frozen consumer set"
assert_json "$tmp/evidence.json" '.selectAll == true and .selectedCount == 12' \
  "post-check cache rewrite weakened fail-closed selection"

# A third candidate init attempts to rewrite the Git-derived changed paths.
changed_paths_literal="$(jq -Rn --arg value "$tmp/changed-paths.txt" '$value')"
printf '%s\n' \
  'package sdk' \
  '' \
  'import "os"' \
  '' \
  'func init() {' \
  "  if err := os.WriteFile($changed_paths_literal, []byte(\"docs/PLUGIN_DEVELOPMENT.md\\n\"), 0o644); err != nil {" \
  '    panic(err)' \
  '  }' \
  '}' \
  >"$protocol_paths_rewrite_test"
run_selector module/post-check-paths-rewrite.go
rm "$protocol_paths_rewrite_test"
assert_json "$tmp/matrix.json" '.count == 12' \
  "post-check changed-path rewrite changed the frozen selection"
assert_json "$tmp/evidence.json" '
  .selectAll == true and .selectedCount == 12 and
  .changedPaths == ["module/post-check-paths-rewrite.go"]
' "post-check changed-path rewrite changed the frozen evidence"

run_selector plugin/external/proto/credential_issuer.proto
assert_json "$tmp/matrix.json" '.count == 6 and (.include | length) == 1 and (.include[0].consumers | length) == 6' \
  "known contract path did not select only matching consumers"
assert_json "$tmp/evidence.json" '.fallbackAll == false and .selectedCount == 6' \
  "known contract path incorrectly used fallback"

# Selection must bind the path map to the protocol actually advertised by the
# runtime SDK, even when a changed path would otherwise select no consumers.
jq '.contractProtocols["workflow.provider.credential-issuer"] = 2' \
  "$path_map" >"$tmp/stale-protocol-path-map.json"
printf '%s\n' docs/PLUGIN_DEVELOPMENT.md >"$tmp/stale-protocol-paths.txt"
expect_failure "contract path-map digest" \
  "$selector" \
    --cache "$tmp/consumers.json" \
    --path-map "$tmp/stale-protocol-path-map.json" \
    --changed-paths "$tmp/stale-protocol-paths.txt" \
    --output "$tmp/stale-protocol-matrix.json" \
    --evidence "$tmp/stale-protocol-evidence.json"

# An affected release whose inclusive range excludes Workflow's current
# contract protocol is incompatible; it must fail rather than disappear from
# selection or pass on source compilation alone.
protocol2_checkout="$tmp/workflow-plugin-protocol2"
create_release_checkout "$protocol2_checkout" workflow-plugin-protocol2 \
  workflow.provider.credential-issuer v2.0.0 ./cmd/plugin 2 2
protocol2_commit="$(git -C "$protocol2_checkout" rev-parse HEAD)"
"$generator" --output "$tmp/protocol2-consumer.json" \
  --consumer GoCodeAlone/workflow-plugin-protocol2 v2.0.0 "$protocol2_commit" "$protocol2_checkout"
jq '. + {contractProtocols: {
  "workflow.provider.credential-issuer": 1,
  "workflow.provider.credential-resolver": 1,
  "workflow.provider.container-registry": 1,
  "workflow.provider.secret-store": 1
}}' "$path_map" >"$tmp/path-map-with-protocols.json"
printf '%s\n' plugin/external/proto/credential_issuer.proto >"$tmp/protocol2-paths.txt"
expect_failure "does not support Workflow protocol 1" \
  "$selector" \
    --cache "$tmp/protocol2-consumer.json" \
    --path-map "$tmp/path-map-with-protocols.json" \
    --changed-paths "$tmp/protocol2-paths.txt" \
    --output "$tmp/protocol2-matrix.json" \
    --evidence "$tmp/protocol2-evidence.json"
assert_json "$tmp/protocol2-evidence.json" '
  .selectedConsumers == ["workflow-plugin-protocol2"] and
  (.incompatibleConsumers | length) == 1 and
  .incompatibleConsumers[0].contract == "workflow.provider.credential-issuer"
' "incompatible selection did not preserve rich selector evidence"

# The exact production augmenter preserves rich selector detail while adding
# the workflow failure envelope, and covers failures before path extraction.
"$selector" augment-evidence \
  --evidence "$tmp/protocol2-evidence.json" \
  --status failed \
  --selector-exit-code 1 \
  --base-sha 1111111111111111111111111111111111111111 \
  --head-sha 2222222222222222222222222222222222222222 \
  --changed-paths "$tmp/protocol2-paths.txt"
assert_json "$tmp/protocol2-evidence.json" '
  .status == "failed" and .selectorExitCode == 1 and
  .baseSha == "1111111111111111111111111111111111111111" and
  .headSha == "2222222222222222222222222222222222222222" and
  .selectedConsumers == ["workflow-plugin-protocol2"] and
  (.incompatibleConsumers | length) == 1
' "production evidence augmentation discarded rich selector evidence"
"$selector" augment-evidence \
  --evidence "$tmp/early-failure-evidence.json" \
  --status failed \
  --selector-exit-code 2 \
  --base-sha invalid-base \
  --head-sha invalid-head \
  --changed-paths "$tmp/missing-changed-paths.txt"
assert_json "$tmp/early-failure-evidence.json" '
  .schemaVersion == 1 and .status == "failed" and
  .selectorExitCode == 2 and .baseSha == "invalid-base" and
  .headSha == "invalid-head" and .changedPaths == []
' "production evidence augmentation did not cover an early validation failure"

run_selector docs/PLUGIN_DEVELOPMENT.md
assert_json "$tmp/matrix.json" '.count == 0 and .include == []' \
  "known non-contract path selected consumers"
assert_json "$tmp/evidence.json" '.fallbackAll == false and .selectedCount == 0' \
  "known non-contract path incorrectly used fallback"

run_selector module/new-boundary.go
assert_json "$tmp/matrix.json" '.count == 12 and (.include | map(.consumers | length)) == [10, 2]' \
  "unknown path did not fail closed to <=10-consumer shards"
assert_json "$tmp/evidence.json" '.fallbackAll == true and .selectedCount == 12' \
  "unknown path did not record all-consumer fallback"

run_selector .github/workflows/scripts/release-parser.sh
assert_json "$tmp/matrix.json" '.count == 12' \
  "unknown GitHub execution helper did not select every consumer"
assert_json "$tmp/evidence.json" '.selectAll == true and .selectedCount == 12' \
  "unknown GitHub execution helper was treated as non-contract"

for changed_path in \
  plugin/sdk/manifest.go \
  plugin/external/proto/plugin.proto \
  plugin/external/manager.go \
  .github/README.md \
  .github/workflows/scripts/run-contract-consumer-shard.sh; do
  run_selector "$changed_path"
  assert_json "$tmp/matrix.json" '.count == 12 and (.include | map(.consumers | length)) == [10, 2]' \
    "$changed_path did not select all consumers"
done

# The public workflow itself must remain provider-neutral and credential-free.
grep -q 'max-parallel: 4' "$workflow" || fail "workflow does not cap consumer parallelism at four"
grep -q 'cancel-in-progress: true' "$workflow" || fail "workflow does not cancel superseded runs"
grep -q 'run-contract-consumer-shard.sh' "$workflow" || fail "workflow does not execute the tested consumer runner"
grep -Eq '^[[:space:]]+\./\.github/workflows/scripts/select-contract-consumers\.sh' "$workflow" || \
  fail "workflow does not invoke the selector through a hash-bound literal path"
grep -Eq '^[[:space:]]+\./\.github/workflows/scripts/run-contract-consumer-shard\.sh' "$workflow" || \
  fail "workflow does not invoke the consumer runner through a policy-reviewable literal path"
grep -Eq '^[[:space:]]+\./\.github/workflows/scripts/test-select-contract-consumers\.sh' "$workflow" || \
  fail "workflow does not run the contract-consumer regression through a hash-bound literal path"
# shellcheck disable=SC2016 # The forbidden expression is matched literally.
if grep -Fq '"$workflow_dir/.github/workflows/scripts/run-contract-consumer-shard.sh"' "$workflow"; then
  fail "workflow dynamically constructs the consumer runner command path"
fi
grep -q 'BASE_REPOSITORY' "$workflow" || fail "workflow does not fetch the base SHA from the base repository"
grep -q 'github.event.pull_request.head.repo.full_name' "$workflow" || fail "workflow does not check out fork pull-request heads"
grep -q 'fetch-depth: 0' "$workflow" || fail "workflow cannot compute an accurate pull-request merge base"
grep -Fq -- "--base-sha \"\$BASE_SHA\"" "$workflow" || \
  fail "workflow does not delegate base-aware path extraction to the selector"
grep -Fq -- "--head-sha \"\$HEAD_SHA\"" "$workflow" || \
  fail "workflow does not delegate head-aware path extraction to the selector"
# shellcheck disable=SC2016 # The expression is intentionally matched literally.
if grep -q -- '--depth=1 base "$BASE_SHA"' "$workflow"; then
  fail "workflow shallow-fetches the base and cannot compute a reliable three-dot diff"
fi
grep -q 'contract-selection-evidence' "$workflow" || fail "workflow does not upload selection evidence"
grep -q 'runs-on: ubuntu-latest' "$workflow" || fail "workflow is not GitHub-hosted"
if grep -Eq 'self-hosted|id-token:|secrets\.|workflow-plugin-(aws|gcp|azure|digitalocean)' "$workflow"; then
  fail "workflow contains provider-specific, secret, OIDC, or self-hosted authority"
fi

echo "PASS: generated contract consumers, provenance, loader, selection, sharding, and public workflow boundary"
