#!/usr/bin/env bash
# GitHub expression literals below are mutation data, never shell expansion.
# shellcheck disable=SC2016
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
checker_binary="${repo_root}/scripts/check-public-workflow-policy.sh"
fixtures="${repo_root}/scripts/fixtures/public-workflow-policy"
policytool="${repo_root}/.github/workflows/policytool"
harness_source="${BASH_SOURCE[0]}"

repo_switch_line="$(grep -n '^repo_root="${archive_repo}"$' "${harness_source}" | cut -d: -f1 || true)"
integrity_target_line="$(grep -n '^integrity_extra=' "${harness_source}" | cut -d: -f1 || true)"
repo_local_tmp_pattern='mktemp -d "${repo_'
repo_local_tmp_pattern+='root}/'
if [[ -z "${repo_switch_line}" || -z "${integrity_target_line}" ||
  "${repo_switch_line}" -ge "${integrity_target_line}" ]] ||
  grep -Fq -- "${repo_local_tmp_pattern}" "${harness_source}"; then
  echo "policy mutation harness does not confine writes to an external archive copy" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    LC_ALL=C shasum -a 256 "$1" | awk '{print $1}'
  fi
}

production_repo_root="$(cd "${repo_root}" && pwd -P)"
production_status_before="$(git -C "${production_repo_root}" status --porcelain=v1 --untracked-files=all)"
production_snapshot() {
  local root="$1"
  (
    cd "${root}"
    if command -v sha256sum >/dev/null 2>&1; then
      git ls-files -z --cached --others --exclude-standard |
        LC_ALL=C sort -z |
        COPYFILE_DISABLE=1 LC_ALL=C tar --null --no-recursion -T - -cf - |
        sha256sum | awk '{print $1}'
    else
      git ls-files -z --cached --others --exclude-standard |
        LC_ALL=C sort -z |
        COPYFILE_DISABLE=1 LC_ALL=C tar --null --no-recursion -T - -cf - |
        LC_ALL=C shasum -a 256 | awk '{print $1}'
    fi
  )
}
production_snapshot_before="$(production_snapshot "${production_repo_root}")"
sandbox_root=""
cleanup() {
  local prior_status=$?
  trap - EXIT
  if [[ -n "${sandbox_root}" ]]; then
    rm -rf "${sandbox_root}"
  fi
  local production_status_after
  local production_snapshot_after
  production_status_after="$(git -C "${production_repo_root}" status --porcelain=v1 --untracked-files=all)"
  production_snapshot_after="$(production_snapshot "${production_repo_root}")"
  if [[ "${production_status_after}" != "${production_status_before}" ||
    "${production_snapshot_after}" != "${production_snapshot_before}" ]]; then
    echo "policy mutation harness changed the production worktree" >&2
    diff -u <(printf '%s\n' "${production_snapshot_before}") <(printf '%s\n' "${production_snapshot_after}") >&2 || true
    exit 1
  fi
  exit "${prior_status}"
}
trap cleanup EXIT

# BEGIN root-module-graph-check
root_module_graph="$(GOWORK=off GOFLAGS=-mod=readonly go list -m all)"
if grep -Eq '^mvdan\.cc/sh/v3 ' <<<"${root_module_graph}"; then
  echo "policy parser dependency leaked into the Workflow module graph" >&2
  exit 1
fi
# END root-module-graph-check
if [[ "$(cd "${policytool}" && GOWORK=off GOFLAGS=-mod=readonly go list -m -f '{{.Version}}' mvdan.cc/sh/v3)" != "v3.13.1" ]]; then
  echo "policy tool must pin mvdan.cc/sh/v3 v3.13.1" >&2
  exit 1
fi
(cd "${policytool}" && GOWORK=off GOFLAGS=-mod=readonly go test ./...)

governance_workflow="${repo_root}/.github/workflows/public-workflow-policy.yml"
protection_verifier="${repo_root}/.github/workflows/scripts/verify-public-workflow-branch-protection.sh"
osv_workflow="${repo_root}/.github/workflows/osv-scanner.yml"
dependency_workflow="${repo_root}/.github/workflows/dependency-update.yml"
benchmark_workflow="${repo_root}/.github/workflows/benchmark.yml"
prerelease_workflow="${repo_root}/.github/workflows/pre-release.yml"
if [[ "$(grep -Fc -- 'uses: actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5' "${governance_workflow}")" -ne 1 ]]; then
  echo "public policy must check out only the trusted base" >&2
  exit 1
fi
for required_candidate_fetch in \
  'Fetch candidate as inert data' \
  'CANDIDATE_REPOSITORY:' \
  'CANDIDATE_SHA:' \
  'GIT_ASKPASS: /bin/false' \
  'GIT_CONFIG_GLOBAL: /dev/null' \
  'GIT_CONFIG_NOSYSTEM: 1' \
  'GIT_TERMINAL_PROMPT: 0' \
  'origin="https://github.com/${CANDIDATE_REPOSITORY}.git"' \
  'credential.helper=' \
  'core.askPass=/bin/false' \
  'credential.interactive=never' \
  'http.https://github.com/.extraheader=' \
  'git -C candidate-source archive --worktree-attributes --format=tar FETCH_HEAD' \
  'find candidate -type l -print -quit'; do
  grep -Fq -- "${required_candidate_fetch}" "${governance_workflow}"
done
if rg -n -- 'Check out candidate|path: candidate|persist-credentials: true|git (checkout|switch|worktree)' "${governance_workflow}"; then
  echo "candidate policy input can enter an executable checkout" >&2
  exit 1
fi
grep -Fq -- "github.event.before" "${governance_workflow}"
grep -Fq -- "This one-time push executes newly merged main, never PR-head data." "${governance_workflow}"
grep -Fq -- "9c364dd4e6dad83808f8a87c1ba990d0132f0372" "${governance_workflow}"
grep -Fq -- '--bootstrap-authority' "${governance_workflow}"
if grep -Fq -- "9c364dd4e6dad83808f8a87c1ba990d0132f0371" "${governance_workflow}" || \
  grep -Fq -- "github.event.before != '0000000000000000000000000000000000000000'" "${governance_workflow}"; then
  echo "bootstrap workflow permits a non-exact prior SHA" >&2
  exit 1
fi
require_text() {
  local label="$1"
  local expected="$2"
  local file="$3"
  if ! grep -Fq -- "${expected}" "${file}"; then
    echo "missing ${label}: ${expected}" >&2
    exit 1
  fi
}
grep -Fq -- "branches: [main]" "${governance_workflow}"
require_text "pull_request_target edited trigger" \
  "types: [opened, synchronize, reopened, ready_for_review, edited]" "${governance_workflow}"
require_text "immutable benchstat install" \
  "go install golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d" "${benchmark_workflow}"
require_text "snapshot release JSON query" \
  'snapshot_tags="$(gh release list --limit 50 --json tagName --jq' "${prerelease_workflow}"
require_text "snapshot release tag filter" \
  'select(.tagName | startswith("snapshot-"))' "${prerelease_workflow}"
require_text "empty snapshot list handling" '[[ -z "$tag" ]] && continue' "${prerelease_workflow}"
require_text "propagating snapshot deletion" 'gh release delete "$tag" --yes --cleanup-tag' "${prerelease_workflow}"
if grep -Fq -- 'gh release delete "$tag" --yes --cleanup-tag || true' "${prerelease_workflow}"; then
  echo "snapshot cleanup still swallows release deletion failures" >&2
  exit 1
fi
grep -Fq -- 'required_check="Public Workflow Policy / policy"' "${protection_verifier}"
grep -Fq -- 'required_approving_review_count >= 1' "${protection_verifier}"
grep -Fq -- 'dismiss_stale_reviews' "${protection_verifier}"
grep -Fq -- 'bypass_pull_request_allowances.users' "${protection_verifier}"
grep -Fq -- 'conditions.ref_name.exclude' "${protection_verifier}"
grep -Fq -- 'required_status_checks.strict == true' "${protection_verifier}"
grep -Fq -- 'strict_required_status_checks_policy == true' "${protection_verifier}"
grep -Fq -- 'refs/tags/v*' "${protection_verifier}"
grep -Fq -- '.type == "creation"' "${protection_verifier}"
grep -Fq -- '.type == "update"' "${protection_verifier}"
grep -Fq -- '.type == "deletion"' "${protection_verifier}"
grep -Fq -- '.actor_type == "OrganizationAdmin"' "${protection_verifier}"
grep -Fq -- 'git fetch --no-tags origin refs/heads/main:refs/remotes/origin/main' "${repo_root}/.github/workflows/release.yml"
grep -Fq -- 'git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main' "${repo_root}/.github/workflows/release.yml"
if rg -n --max-depth 1 --glob '*.yml' --glob '*.yaml' \
  'repo_dispatch_token|notify-workflow-registry|peter-evans/repository-dispatch|secrets\.|secrets\[' \
  "${repo_root}/.github/workflows" || \
  rg -n -- 'repo_dispatch_token|notify-workflow-registry|peter-evans/repository-dispatch' \
    "${repo_root}/.github/public-workflow-secret-allowlist.json" \
    "${repo_root}/.github/public-workflow-action-allowlist.json"; then
  echo "public workflows retain named-secret or publisher-side registry authority" >&2
  exit 1
fi
if rg -n --max-depth 1 --glob '*.yml' --glob '*.yaml' \
  '^[ ]{4}uses:[[:space:]]' "${repo_root}/.github/workflows"; then
  echo "public workflows retain a forbidden reusable-workflow job" >&2
  exit 1
fi
jq -e 'length == 0' "${repo_root}/.github/public-workflow-secret-allowlist.json" >/dev/null
authority_manifest="${repo_root}/.github/public-workflow-authority.json"
jq -e '
  .version == 1 and
  ([.bundles[] | select(.state == "active")] | length) == 1 and
  ([.bundles[] | select(.state == "staged")] | length) <= 1 and
  all(.bundles[]; (.files | length) > 0)
' "${authority_manifest}" >/dev/null
if [[ "$(rg -o -- 'npm ci' "${repo_root}/.github/workflows"/*.yml | wc -l | tr -d ' ')" != \
  "$(rg -o -- 'NODE_AUTH_TOKEN: \$\{\{ github.token \}\}' "${repo_root}/.github/workflows"/*.yml | wc -l | tr -d ' ')" ]]; then
  echo "every npm ci step must receive the automatic GitHub Packages token" >&2
  exit 1
fi
if rg -F -- 'verify-public-workflow-branch-protection.sh' "${governance_workflow}"; then
  echo "public policy workflow cannot inspect privileged repository governance" >&2
  exit 1
fi
osv_image='docker://ghcr.io/google/osv-scanner-action@sha256:48406c58197201fe55e56615ad9d414f85063da320e204d0b0ed460fb3908dba'
if [[ "$(grep -Fc -- "uses: ${osv_image}" "${osv_workflow}")" -ne 5 ]] ||
  [[ "$(grep -Fc -- 'entrypoint: /root/osv-reporter' "${osv_workflow}")" -ne 2 ]] ||
  [[ "$(grep -Fc -- 'security-events: write' "${osv_workflow}")" -ne 2 ]] ||
  [[ "$(grep -Fc -- 'github/codeql-action/upload-sarif@8aad20d150bbac5944a9f9d289da16a4b0d87c1e' "${osv_workflow}")" -ne 2 ]]; then
  echo "OSV workflow lost digest-pinned scanner/reporter or job-scoped SARIF behavior" >&2
  exit 1
fi
for differential_arg in '--old=old-results.json' '--new=new-results.json' '--output-files=sarif:results.sarif,gh-annotations:#stderr' '--fail-on-vuln=true'; do
  grep -Fq -- "${differential_arg}" "${osv_workflow}"
done
for baseline_selector in 'github.event.pull_request.base.sha' 'github.event.merge_group.base_sha'; do
  grep -Fq -- "${baseline_selector}" "${osv_workflow}"
done
if grep -Fq -- 'GITHUB_BASE_REF' "${osv_workflow}"; then
  echo "OSV merge-group baseline still depends on pull-request-only GITHUB_BASE_REF" >&2
  exit 1
fi
if [[ "$(grep -Ec -- '^[[:space:]]+contents:[[:space:]]+write[[:space:]]*$' "${dependency_workflow}")" -ne 1 ]] ||
  [[ "$(grep -Ec -- '^[[:space:]]+pull-requests:[[:space:]]+write[[:space:]]*$' "${dependency_workflow}")" -ne 1 ]]; then
  echo "dependency updater lacks job-scoped branch and pull-request authority" >&2
  exit 1
fi
grep -Fq -- 'Workflow authority changes use three pull requests' "${repo_root}/docs/public-workflow-policy.md"
grep -Fq -- 'separate three-pull-request authority bundle' "${repo_root}/docs/public-workflow-policy.md"

for forbidden_path in \
  .github/workflows/conformance-smoke.yml \
  .github/workflows/conformance-budget-check.yml \
  .github/workflows/conformance-leak-scrubber.yml \
  .github/conformance/cleanup.yaml \
  .github/workflows/scripts/file-or-comment-leak-issue.sh \
  docs/conformance-runbook.md \
  .github/workflows/test-dispatch.yml \
  .github/workflows/create-release.yml; do
  if [[ -e "${repo_root}/${forbidden_path}" ]]; then
    echo "forbidden public-provider authority remains: ${forbidden_path}" >&2
    exit 1
  fi
  jq -e --arg path "${forbidden_path}" \
    'any(.[]; .path == $path and .state == "active" and .presence == "absent" and (.contextSHA256 | not))' \
    "${repo_root}/.github/public-workflow-presence-allowlist.json" >/dev/null
done

temp_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
case "${temp_parent}" in
  "${production_repo_root}"|"${production_repo_root}"/*)
    echo "TMPDIR must be outside the production repository" >&2
    exit 1
    ;;
esac
sandbox_root="$(mktemp -d "${temp_parent%/}/workflow-policy-test.XXXXXX")"
archive_copy_root="${sandbox_root}/archive"
tmp_dir="${sandbox_root}/tmp"
mkdir -p "${archive_copy_root}" "${tmp_dir}"
if [[ "$(cd "${sandbox_root}" && pwd -P)" == "${production_repo_root}"/* ]]; then
  echo "policy mutation sandbox resolved inside the production repository" >&2
  exit 1
fi

archive_repo="${archive_copy_root}/repo"
mkdir -p "${archive_repo}"
archive_paths="${sandbox_root}/archive-paths"
: >"${archive_paths}"
while IFS= read -r -d '' tracked_path; do
  if [[ -e "${production_repo_root}/${tracked_path}" ]]; then
    printf '%s\0' "${tracked_path}" >>"${archive_paths}"
  fi
done < <(git -C "${production_repo_root}" ls-files -z --cached --others --exclude-standard)
tar -C "${production_repo_root}" --null -T "${archive_paths}" -cf - | tar -C "${archive_repo}" -xf -
test ! -e "${archive_repo}/.git"
if git -C "${archive_repo}" rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "archive regression fixture remains inside an enclosing Git worktree" >&2
  exit 1
fi

repo_root="${archive_repo}"
checker_binary="${repo_root}/scripts/check-public-workflow-policy.sh"
fixtures="${repo_root}/scripts/fixtures/public-workflow-policy"
policytool="${repo_root}/.github/workflows/policytool"
integrity_extra="${policytool}/extra_linux.go"
integrity_vendor="${policytool}/vendor"
integrity_symlink="${policytool}/extra-link.go"
mutation_backup="${tmp_dir}/workflow-backup.yml"
export TMPDIR="${tmp_dir}"
fixture_executables="${tmp_dir}/fixture-executables.json"
printf '[]\n' >"${fixture_executables}"
fixture_commands="${tmp_dir}/fixture-commands.json"
printf '[]\n' >"${fixture_commands}"
fixture_actions="${tmp_dir}/fixture-actions.json"
printf '[]\n' >"${fixture_actions}"
empty_allowlist="${tmp_dir}/empty-allowlist.json"
printf '[]\n' >"${empty_allowlist}"
archive_checker="${archive_repo}/scripts/check-public-workflow-policy.sh"
(cd "${archive_repo}" && ./scripts/check-public-workflow-policy.sh --scan-root "${archive_repo}") >/dev/null

archive_repo_alias="${archive_copy_root}/repo-alias"
ln -s "${archive_repo}" "${archive_repo_alias}"
(cd "${archive_repo_alias}" && ./scripts/check-public-workflow-policy.sh --scan-root "${archive_repo_alias}") >/dev/null

archive_checker_link="${archive_copy_root}/linked-checker.sh"
ln -s "${archive_checker}" "${archive_checker_link}"
set +e
archive_link_output="$(cd "${archive_repo}" && "${archive_checker_link}" --scan-root "${archive_repo}" 2>&1)"
archive_link_status=$?
set -e
if [[ "${archive_link_status}" -eq 0 ]] || ! grep -Fq -- "policy wrapper path must not be a symlink" <<<"${archive_link_output}"; then
  echo "policy wrapper accepted a symlinked entry path" >&2
  printf '%s\n' "${archive_link_output}" >&2
  exit 1
fi

mkdir -p "${archive_copy_root}/misplaced"
misplaced_checker="${archive_copy_root}/misplaced/check-public-workflow-policy.sh"
cp "${archive_checker}" "${misplaced_checker}"
set +e
misplaced_output="$(cd "${archive_repo}" && "${misplaced_checker}" --scan-root "${archive_repo}" 2>&1)"
misplaced_status=$?
set -e
if [[ "${misplaced_status}" -eq 0 ]] || ! grep -Fq -- "policy wrapper must reside in scripts" <<<"${misplaced_output}"; then
  echo "policy wrapper accepted an invalid repository layout" >&2
  printf '%s\n' "${misplaced_output}" >&2
  exit 1
fi

module_graph_probe="${tmp_dir}/module-graph-probe.sh"
awk '
  $0 == "# BEGIN root-module-graph-check" { in_probe=1; next }
  $0 == "# END root-module-graph-check" { exit }
  in_probe { print }
' "$0" >"${module_graph_probe}"
fake_go_bin="${tmp_dir}/fake-go-bin"
mkdir -p "${fake_go_bin}"
cat >"${fake_go_bin}/go" <<'BASH'
#!/usr/bin/env bash
exit 42
BASH
chmod +x "${fake_go_bin}/go"
set +e
PATH="${fake_go_bin}:$PATH" bash -euo pipefail "${module_graph_probe}" >/dev/null 2>&1
module_graph_failure_status=$?
set -e
if [[ "${module_graph_failure_status}" -ne 42 ]]; then
  echo "root module graph command failure did not fail closed" >&2
  exit 1
fi

candidate_fetch_script="${tmp_dir}/fetch-candidate.sh"
awk '
  $0 == "      - name: Fetch candidate as inert data" { in_step=1; next }
  in_step && $0 == "        run: |" { in_run=1; next }
  in_run && /^      - name:/ { exit }
  in_run { sub(/^          /, ""); print }
' "${governance_workflow}" >"${candidate_fetch_script}"
fake_bin="${tmp_dir}/fake-bin"
fake_archive="${tmp_dir}/fake-archive"
mkdir -p "${fake_bin}" "${fake_archive}/.github/workflows"
printf 'name: inert candidate\n' >"${fake_archive}/.github/workflows/inert.yml"
cat >"${fake_bin}/git" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
[[ "${GIT_ASKPASS}" == /bin/false ]]
[[ "${GIT_CONFIG_GLOBAL}" == /dev/null ]]
[[ "${GIT_CONFIG_NOSYSTEM}" == 1 ]]
[[ "${GIT_TERMINAL_PROMPT}" == 0 ]]
printf '%s\n' "$*" >>"${FAKE_GIT_LOG}"
case " $* " in
  *" init --quiet "*) ;;
  *" fetch "*)
    [[ "$*" == *"https://github.com/example/repo.git ${CANDIDATE_SHA}"* ]]
    ;;
  *" rev-parse "*) printf '%s\n' "${FAKE_FETCHED_SHA:-${CANDIDATE_SHA}}" ;;
  *" archive "*)
    if [[ -n "${FAKE_REAL_ARCHIVE_REPO:-}" ]]; then
      archive_attributes=()
      if [[ " $* " == *" --worktree-attributes "* ]]; then
        archive_attributes+=(--worktree-attributes)
      fi
      /usr/bin/git -C "${FAKE_REAL_ARCHIVE_REPO}" archive "${archive_attributes[@]}" --format=tar HEAD
    else
      /usr/bin/tar -cf - -C "${FAKE_ARCHIVE_ROOT}" .
    fi
    ;;
  *) echo "unexpected fake git invocation: $*" >&2; exit 2 ;;
esac
BASH
chmod +x "${fake_bin}/git"

run_candidate_fetch() {
  local workdir="$1"
  local repository="$2"
  local sha="$3"
  local archive_root="$4"
  mkdir -p "${workdir}"
  (
    cd "${workdir}"
    PATH="${fake_bin}:$PATH" \
      CANDIDATE_REPOSITORY="${repository}" \
      CANDIDATE_SHA="${sha}" \
      GIT_ASKPASS=/bin/false \
      GIT_CONFIG_GLOBAL=/dev/null \
      GIT_CONFIG_NOSYSTEM=1 \
      GIT_TERMINAL_PROMPT=0 \
      FAKE_GIT_LOG="${workdir}/git.log" \
      FAKE_ARCHIVE_ROOT="${archive_root}" \
      bash -euo pipefail "${candidate_fetch_script}"
  )
}

candidate_sha=0123456789012345678901234567890123456789
candidate_fetch_good="${tmp_dir}/candidate-fetch-good"
run_candidate_fetch "${candidate_fetch_good}" example/repo "${candidate_sha}" "${fake_archive}"
test -f "${candidate_fetch_good}/candidate/.github/workflows/inert.yml"
grep -Fq -- "credential.helper=" "${candidate_fetch_good}/git.log"
grep -Fq -- "core.askPass=/bin/false" "${candidate_fetch_good}/git.log"
grep -Fq -- "credential.interactive=never" "${candidate_fetch_good}/git.log"
grep -Fq -- "http.https://github.com/.extraheader=" "${candidate_fetch_good}/git.log"
grep -Fq -- "https://github.com/example/repo.git ${candidate_sha}" "${candidate_fetch_good}/git.log"

for invalid_candidate in \
  'bad-repository|example/repo?token=leak|0123456789012345678901234567890123456789' \
  'short-sha|example/repo|0123456789'; do
  IFS='|' read -r label repository sha <<<"${invalid_candidate}"
  if run_candidate_fetch "${tmp_dir}/candidate-${label}" "${repository}" "${sha}" "${fake_archive}" >/dev/null 2>&1; then
    echo "candidate fetch accepted ${label}" >&2
    exit 1
  fi
done

mismatched_candidate_sha=ffffffffffffffffffffffffffffffffffffffff
candidate_fetch_mismatch="${tmp_dir}/candidate-fetch-mismatch"
if FAKE_FETCHED_SHA="${mismatched_candidate_sha}" \
  run_candidate_fetch "${candidate_fetch_mismatch}" example/repo "${candidate_sha}" "${fake_archive}" >/dev/null 2>&1; then
  echo "candidate fetch accepted a mismatched fetched commit" >&2
  exit 1
fi
if grep -Fq -- " archive " "${candidate_fetch_mismatch}/git.log"; then
  echo "candidate fetch archived data before verifying commit identity" >&2
  exit 1
fi

fake_symlink_archive="${tmp_dir}/fake-symlink-archive"
mkdir -p "${fake_symlink_archive}/.github/workflows"
ln -s ../../outside "${fake_symlink_archive}/.github/workflows/escape.yml"
if run_candidate_fetch "${tmp_dir}/candidate-symlink" example/repo "${candidate_sha}" "${fake_symlink_archive}" >/dev/null 2>&1; then
  echo "candidate fetch accepted a symlink path escape" >&2
  exit 1
fi

real_attribute_repo="${tmp_dir}/real-attribute-repo"
mkdir -p "${real_attribute_repo}/.github/workflows"
git -C "${real_attribute_repo}" init --quiet
git -C "${real_attribute_repo}" config user.name policy-test
git -C "${real_attribute_repo}" config user.email policy-test@example.invalid
git -C "${real_attribute_repo}" config commit.gpgsign false
printf '.github/workflows/hidden.yml export-ignore\n' >"${real_attribute_repo}/.gitattributes"
printf 'name: must remain visible\n' >"${real_attribute_repo}/.github/workflows/hidden.yml"
git -C "${real_attribute_repo}" add .
git -C "${real_attribute_repo}" commit --quiet -m fixture
rm -rf "${real_attribute_repo}/.gitattributes" "${real_attribute_repo}/.github"
candidate_fetch_attributes="${tmp_dir}/candidate-fetch-attributes"
FAKE_REAL_ARCHIVE_REPO="${real_attribute_repo}" \
  run_candidate_fetch "${candidate_fetch_attributes}" example/repo "${candidate_sha}" "${fake_archive}"
test -f "${candidate_fetch_attributes}/candidate/.github/workflows/hidden.yml"

bootstrap_selector="${tmp_dir}/select-policy-root.sh"
awk '
  $0 == "      - name: Select exact trusted policy root" { in_step=1; next }
  in_step && $0 == "        run: |" { in_run=1; next }
  in_run && /^      - name:/ { exit }
  in_run { sub(/^          /, ""); print }
' "${governance_workflow}" >"${bootstrap_selector}"
bootstrap_root="${tmp_dir}/bootstrap"
mkdir -p "${bootstrap_root}/candidate/scripts"
printf '#!/usr/bin/env bash\n' >"${bootstrap_root}/candidate/scripts/check-public-workflow-policy.sh"
chmod +x "${bootstrap_root}/candidate/scripts/check-public-workflow-policy.sh"
bootstrap_output="${tmp_dir}/bootstrap-output"
(cd "${bootstrap_root}" && BEFORE_SHA=9c364dd4e6dad83808f8a87c1ba990d0132f0372 EVENT_NAME=push GITHUB_OUTPUT="${bootstrap_output}" bash "${bootstrap_selector}")
grep -Fxq -- 'root=candidate' "${bootstrap_output}"
for rejected_before in \
  9c364dd4e6dad83808f8a87c1ba990d0132f0371 \
  0000000000000000000000000000000000000000 \
  ffffffffffffffffffffffffffffffffffffffff; do
  set +e
  (cd "${bootstrap_root}" && BEFORE_SHA="${rejected_before}" EVENT_NAME=push GITHUB_OUTPUT="${bootstrap_output}" bash "${bootstrap_selector}") >/dev/null 2>&1
  bootstrap_status=$?
  set -e
  if [[ "${bootstrap_status}" -eq 0 ]]; then
    echo "bootstrap accepted non-exact prior SHA ${rejected_before}" >&2
    exit 1
  fi
done
set +e
(cd "${bootstrap_root}" && BEFORE_SHA=9c364dd4e6dad83808f8a87c1ba990d0132f0372 EVENT_NAME=pull_request_target GITHUB_OUTPUT="${bootstrap_output}" bash "${bootstrap_selector}") >/dev/null 2>&1
bootstrap_pr_status=$?
set -e
if [[ "${bootstrap_pr_status}" -eq 0 ]]; then
  echo "bootstrap accepted exact SHA outside the push event" >&2
  exit 1
fi
mkdir -p "${bootstrap_root}/trusted/scripts"
printf '#!/usr/bin/env bash\n' >"${bootstrap_root}/trusted/scripts/check-public-workflow-policy.sh"
chmod +x "${bootstrap_root}/trusted/scripts/check-public-workflow-policy.sh"
: >"${bootstrap_output}"
(cd "${bootstrap_root}" && BEFORE_SHA=ffffffffffffffffffffffffffffffffffffffff EVENT_NAME=pull_request_target GITHUB_OUTPUT="${bootstrap_output}" bash "${bootstrap_selector}")
grep -Fxq -- 'root=trusted' "${bootstrap_output}"

classic_protection="${tmp_dir}/classic-protection.json"
ruleset_protection="${tmp_dir}/ruleset-protection.json"
invalid_protection="${tmp_dir}/invalid-protection.json"
repository_metadata="${tmp_dir}/repository-metadata.json"
tag_protection="${tmp_dir}/tag-protection.json"
printf '{}\n' >"${invalid_protection}"
printf '{"default_branch":"main"}\n' >"${repository_metadata}"
cat >"${classic_protection}" <<'JSON'
{
  "enforce_admins":{"enabled":true},
  "required_status_checks":{
    "strict":true,
    "contexts":["Public Workflow Policy / policy"],
    "checks":[{"context":"Public Workflow Policy / policy","app_id":15368}]
  },
  "required_pull_request_reviews":{
    "required_approving_review_count":1,
    "dismiss_stale_reviews":true,
    "bypass_pull_request_allowances":{"users":[],"teams":[],"apps":[]}
  },
  "restrictions":null,
  "allow_force_pushes":{"enabled":false},
  "allow_deletions":{"enabled":false}
}
JSON
cat >"${ruleset_protection}" <<'JSON'
{
  "target":"branch",
  "enforcement":"active",
  "bypass_actors":[],
  "conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},
  "rules":[
    {"type":"pull_request","parameters":{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":true}},
    {"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"Public Workflow Policy / policy","integration_id":15368}]}},
    {"type":"non_fast_forward"},
    {"type":"deletion"}
  ]
}
JSON
cat >"${tag_protection}" <<'JSON'
{
  "id":18817055,
  "name":"Protect release tags",
  "target":"tag",
  "enforcement":"active",
  "bypass_actors":[{"actor_id":null,"actor_type":"OrganizationAdmin","bypass_mode":"always"}],
  "conditions":{"ref_name":{"include":["refs/tags/v*"],"exclude":[]}},
  "rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"}]
}
JSON
PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE=1 PUBLIC_WORKFLOW_CLASSIC_JSON_FILE="${classic_protection}" PUBLIC_WORKFLOW_RULESET_JSON_FILE="${invalid_protection}" PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE="${repository_metadata}" PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE="${tag_protection}" \
  "${protection_verifier}" example/repo main >/dev/null
PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE=1 PUBLIC_WORKFLOW_CLASSIC_JSON_FILE="${invalid_protection}" PUBLIC_WORKFLOW_RULESET_JSON_FILE="${ruleset_protection}" PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE="${repository_metadata}" PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE="${tag_protection}" \
  "${protection_verifier}" example/repo main >/dev/null
ruleset_explicit="${ruleset_protection}.explicit"
jq '.conditions.ref_name.include=["refs/heads/release"]' "${ruleset_protection}" >"${ruleset_explicit}"
PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE=1 PUBLIC_WORKFLOW_CLASSIC_JSON_FILE="${invalid_protection}" PUBLIC_WORKFLOW_RULESET_JSON_FILE="${ruleset_explicit}" PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE="${repository_metadata}" PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE="${tag_protection}" \
  "${protection_verifier}" example/repo release >/dev/null

assert_protection_rejected() {
  local kind="$1"
  local fixture="$2"
  local branch="${3:-main}"
  local classic_file="${invalid_protection}"
  local ruleset_file="${invalid_protection}"
  if [[ "${kind}" == classic ]]; then
    classic_file="${fixture}"
  else
    ruleset_file="${fixture}"
  fi
  set +e
  PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE=1 PUBLIC_WORKFLOW_CLASSIC_JSON_FILE="${classic_file}" PUBLIC_WORKFLOW_RULESET_JSON_FILE="${ruleset_file}" PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE="${repository_metadata}" PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE="${tag_protection}" \
    "${protection_verifier}" example/repo "${branch}" >/dev/null 2>&1
  local status=$?
  set -e
  if [[ "${status}" -eq 0 ]]; then
    echo "${kind} protection accepted an invalid producer/freshness fixture: ${fixture}" >&2
    exit 1
  fi
}
jq 'del(.required_status_checks.strict)' "${classic_protection}" >"${classic_protection}.missing"
jq '.required_status_checks.strict=false' "${classic_protection}" >"${classic_protection}.false"
assert_protection_rejected classic "${classic_protection}.missing"
assert_protection_rejected classic "${classic_protection}.false"
jq 'del(.required_status_checks.checks[0].app_id)' "${classic_protection}" >"${classic_protection}.producer-missing"
jq '.required_status_checks.checks[0].app_id=99999' "${classic_protection}" >"${classic_protection}.producer-wrong"
jq '.required_status_checks.checks[0].app_id=null' "${classic_protection}" >"${classic_protection}.producer-null"
jq 'del(.required_status_checks.checks)' "${classic_protection}" >"${classic_protection}.legacy-context-only"
assert_protection_rejected classic "${classic_protection}.producer-missing"
assert_protection_rejected classic "${classic_protection}.producer-wrong"
assert_protection_rejected classic "${classic_protection}.producer-null"
assert_protection_rejected classic "${classic_protection}.legacy-context-only"
jq 'del(.rules[1].parameters.strict_required_status_checks_policy)' "${ruleset_protection}" >"${ruleset_protection}.missing"
jq '.rules[1].parameters.strict_required_status_checks_policy=false' "${ruleset_protection}" >"${ruleset_protection}.false"
assert_protection_rejected ruleset "${ruleset_protection}.missing"
assert_protection_rejected ruleset "${ruleset_protection}.false"
jq 'del(.rules[1].parameters.required_status_checks[0].integration_id)' "${ruleset_protection}" >"${ruleset_protection}.producer-missing"
jq '.rules[1].parameters.required_status_checks[0].integration_id=99999' "${ruleset_protection}" >"${ruleset_protection}.producer-wrong"
jq '.rules[1].parameters.required_status_checks[0].integration_id=null' "${ruleset_protection}" >"${ruleset_protection}.producer-null"
jq '.rules[1].parameters.required_status_checks=[] | .rules[1].parameters.contexts=["Public Workflow Policy / policy"]' "${ruleset_protection}" >"${ruleset_protection}.legacy-context-only"
assert_protection_rejected ruleset "${ruleset_protection}.producer-missing"
assert_protection_rejected ruleset "${ruleset_protection}.producer-wrong"
assert_protection_rejected ruleset "${ruleset_protection}.producer-null"
assert_protection_rejected ruleset "${ruleset_protection}.legacy-context-only"
jq '.rules |= map(select(.type != "non_fast_forward"))' "${ruleset_protection}" >"${ruleset_protection}.missing-non-fast-forward"
jq '.rules |= map(select(.type != "deletion"))' "${ruleset_protection}" >"${ruleset_protection}.missing-deletion"
assert_protection_rejected ruleset "${ruleset_protection}.missing-non-fast-forward"
assert_protection_rejected ruleset "${ruleset_protection}.missing-deletion"
assert_protection_rejected ruleset "${ruleset_protection}" release

assert_tag_protection_rejected() {
  local fixture="$1"
  set +e
  PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE=1 PUBLIC_WORKFLOW_CLASSIC_JSON_FILE="${classic_protection}" PUBLIC_WORKFLOW_RULESET_JSON_FILE="${invalid_protection}" PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE="${repository_metadata}" PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE="${fixture}" \
    "${protection_verifier}" example/repo main >/dev/null 2>&1
  local status=$?
  set -e
  if [[ "${status}" -eq 0 ]]; then
    echo "tag protection accepted invalid fixture: ${fixture}" >&2
    exit 1
  fi
}
for rule_type in creation update deletion; do
  jq --arg type "${rule_type}" '.rules |= map(select(.type != $type))' "${tag_protection}" >"${tag_protection}.missing-${rule_type}"
  assert_tag_protection_rejected "${tag_protection}.missing-${rule_type}"
done
jq '.conditions.ref_name.include=["refs/tags/*"]' "${tag_protection}" >"${tag_protection}.broader-include"
jq '.conditions.ref_name.include += ["refs/tags/*"]' "${tag_protection}" >"${tag_protection}.extra-include"
jq '.conditions.ref_name.exclude=["refs/tags/v0.*"]' "${tag_protection}" >"${tag_protection}.exclude"
jq '.bypass_actors=[]' "${tag_protection}" >"${tag_protection}.missing-bypass"
jq '.bypass_actors += [{"actor_id":1,"actor_type":"RepositoryRole","bypass_mode":"always"}]' "${tag_protection}" >"${tag_protection}.extra-bypass"
jq 'del(.bypass_actors[0].actor_id)' "${tag_protection}" >"${tag_protection}.missing-bypass-actor-id"
jq '.bypass_actors[0].actor_id=1' "${tag_protection}" >"${tag_protection}.wrong-bypass-actor-id"
jq '.bypass_actors[0].actor_type="Team"' "${tag_protection}" >"${tag_protection}.wrong-bypass-actor-type"
jq '.bypass_actors[0].bypass_mode="pull_request"' "${tag_protection}" >"${tag_protection}.wrong-bypass-mode"
jq '.target="branch"' "${tag_protection}" >"${tag_protection}.wrong-target"
jq '.enforcement="evaluate"' "${tag_protection}" >"${tag_protection}.wrong-enforcement"
for invalid_tag_fixture in broader-include extra-include exclude missing-bypass extra-bypass missing-bypass-actor-id wrong-bypass-actor-id wrong-bypass-actor-type wrong-bypass-mode wrong-target wrong-enforcement; do
  assert_tag_protection_rejected "${tag_protection}.${invalid_tag_fixture}"
done

lifecycle_root="${tmp_dir}/lifecycle"
mkdir -p "${lifecycle_root}/.github/workflows"
mkdir -p "${lifecycle_root}/scripts"
lifecycle_workflow="${lifecycle_root}/.github/workflows/lifecycle.yml"
lifecycle_transition="${tmp_dir}/lifecycle-transition.json"
lifecycle_presence="${tmp_dir}/lifecycle-presence.json"
cat >"${lifecycle_presence}" <<'JSON'
[
  {"path":".github/workflows/lifecycle.yml","contextSHA256":"520dfbf6fb88a8734c2e19b80fa0fdca65769a172442709a0bbac0d2695d7ca5","state":"active","presence":"present"},
  {"path":".github/workflows/lifecycle.yml","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"staged","presence":"present"}
]
JSON
cat >"${lifecycle_transition}" <<'JSON'
[
  {"path":".github/workflows/lifecycle.yml","command":"echo","statementSHA256":"819b561be4b01d042acf9c152963504db679c1f35863be463a27d0b1f829fce2","contextSHA256":"520dfbf6fb88a8734c2e19b80fa0fdca65769a172442709a0bbac0d2695d7ca5","state":"active","rationale":"Current lifecycle context."},
  {"path":".github/workflows/lifecycle.yml","command":"lifecycle.sh","statementSHA256":"dba5e9682987ecf0db39babf5824d3bdea717b091db48e154c082bced59f6b79","contextSHA256":"520dfbf6fb88a8734c2e19b80fa0fdca65769a172442709a0bbac0d2695d7ca5","state":"active","rationale":"Current lifecycle executable."},
  {"path":".github/workflows/lifecycle.yml","command":"echo","statementSHA256":"fe696343d9c54236742da9a5f73af7180c94578dca254d9099440c71775da76a","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"staged","rationale":"Future lifecycle context."},
  {"path":".github/workflows/lifecycle.yml","command":"lifecycle.sh","statementSHA256":"dba5e9682987ecf0db39babf5824d3bdea717b091db48e154c082bced59f6b79","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"staged","rationale":"Future lifecycle executable."}
]
JSON
lifecycle_executables="${tmp_dir}/lifecycle-executables.json"
cat >"${lifecycle_executables}" <<'JSON'
[
  {"path":"scripts/lifecycle.sh","workflowPath":".github/workflows/lifecycle.yml","contextSHA256":"520dfbf6fb88a8734c2e19b80fa0fdca65769a172442709a0bbac0d2695d7ca5","state":"active","sha256":"2e1f5a51dcffcd76df111e383338b6d7e68d8b01ab088636ea87758a05b0e084","rationale":"Current script hash."},
  {"path":"scripts/lifecycle.sh","workflowPath":".github/workflows/lifecycle.yml","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"staged","sha256":"cacb7804eaa7158147c9216632414e3ec06c3bd463d7a72f2a9aa7b6b06290e0","rationale":"Future script hash."}
]
JSON
printf '#!/usr/bin/env bash\necho old\n' >"${lifecycle_root}/scripts/lifecycle.sh"
cp "${fixtures}/lifecycle-old.yml" "${lifecycle_workflow}"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}" --command-allowlist "${lifecycle_transition}" --action-allowlist "${empty_allowlist}"
printf '#!/usr/bin/env bash\necho future\n' >"${lifecycle_root}/scripts/lifecycle.sh"
cp "${fixtures}/lifecycle-future.yml" "${lifecycle_workflow}"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}" --command-allowlist "${lifecycle_transition}" --action-allowlist "${empty_allowlist}"
lifecycle_cleanup="${tmp_dir}/lifecycle-cleanup.json"
jq 'map(select(.state=="staged") | .state="active")' "${lifecycle_transition}" >"${lifecycle_cleanup}"
jq '[.[1] | .state="active"]' "${lifecycle_presence}" >"${lifecycle_presence}.cleanup"
jq '[.[1] | .state="active"]' "${lifecycle_executables}" >"${lifecycle_executables}.cleanup"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_presence}.cleanup" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.cleanup" --command-allowlist "${lifecycle_cleanup}" --action-allowlist "${empty_allowlist}"

# Workflow additions and deletions use the same trusted three-phase lifecycle.
# An absent tombstone authorizes zero matching workflow files without weakening
# the default failure for an undeclared empty workflow set.
lifecycle_add_presence="${tmp_dir}/lifecycle-add-presence.json"
cat >"${lifecycle_add_presence}" <<'JSON'
[
  {"path":".github/workflows/lifecycle.yml","state":"active","presence":"absent"},
  {"path":".github/workflows/lifecycle.yml","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"staged","presence":"present"}
]
JSON
jq '[.[] | select(.state=="staged")]' "${lifecycle_transition}" >"${lifecycle_transition}.add"
jq '[.[] | select(.state=="staged")]' "${lifecycle_executables}" >"${lifecycle_executables}.add"
rm -f "${lifecycle_workflow}" "${lifecycle_root}/scripts/lifecycle.sh"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_add_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.add" --command-allowlist "${lifecycle_transition}.add" --action-allowlist "${empty_allowlist}"
printf '#!/usr/bin/env bash\necho future\n' >"${lifecycle_root}/scripts/lifecycle.sh"
cp "${fixtures}/lifecycle-future.yml" "${lifecycle_workflow}"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_add_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.add" --command-allowlist "${lifecycle_transition}.add" --action-allowlist "${empty_allowlist}"
jq '[.[1] | .state="active"]' "${lifecycle_add_presence}" >"${lifecycle_add_presence}.cleanup"
jq 'map(.state="active")' "${lifecycle_transition}.add" >"${lifecycle_transition}.add-cleanup"
jq 'map(.state="active")' "${lifecycle_executables}.add" >"${lifecycle_executables}.add-cleanup"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_add_presence}.cleanup" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.add-cleanup" --command-allowlist "${lifecycle_transition}.add-cleanup" --action-allowlist "${empty_allowlist}"

lifecycle_delete_presence="${tmp_dir}/lifecycle-delete-presence.json"
cat >"${lifecycle_delete_presence}" <<'JSON'
[
  {"path":".github/workflows/lifecycle.yml","contextSHA256":"c985f3c24ee6b14675094e63cb9c0fd27a4f05ebab6297770b309229057724cc","state":"active","presence":"present"},
  {"path":".github/workflows/lifecycle.yml","state":"staged","presence":"absent"}
]
JSON
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_delete_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.add-cleanup" --command-allowlist "${lifecycle_transition}.add-cleanup" --action-allowlist "${empty_allowlist}"
rm -f "${lifecycle_workflow}" "${lifecycle_root}/scripts/lifecycle.sh"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_delete_presence}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}.add-cleanup" --command-allowlist "${lifecycle_transition}.add-cleanup" --action-allowlist "${empty_allowlist}"
jq '[.[1] | .state="active"]' "${lifecycle_delete_presence}" >"${lifecycle_delete_presence}.cleanup"
"${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${lifecycle_delete_presence}.cleanup" --allowlist "${empty_allowlist}" --executable-allowlist "${empty_allowlist}" --command-allowlist "${empty_allowlist}" --action-allowlist "${empty_allowlist}"

set +e
undeclared_empty_output="$("${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${empty_allowlist}" --allowlist "${empty_allowlist}" --executable-allowlist "${empty_allowlist}" --command-allowlist "${empty_allowlist}" --action-allowlist "${empty_allowlist}" 2>&1)"
undeclared_empty_status=$?
set -e
if [[ "${undeclared_empty_status}" -eq 0 ]] || ! grep -Fq -- "no public workflow files found and no trusted absence is declared" <<<"${undeclared_empty_output}"; then
  echo "empty workflow set passed without a trusted absence declaration" >&2
  printf '%s\n' "${undeclared_empty_output}" >&2
  exit 1
fi
invalid_tombstone="${tmp_dir}/invalid-tombstone.json"
printf '[{"path":"../../outside.yml","state":"active","presence":"absent"}]\n' >"${invalid_tombstone}"
set +e
invalid_tombstone_output="$("${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${invalid_tombstone}" --allowlist "${empty_allowlist}" --executable-allowlist "${empty_allowlist}" --command-allowlist "${empty_allowlist}" --action-allowlist "${empty_allowlist}" 2>&1)"
invalid_tombstone_status=$?
set -e
if [[ "${invalid_tombstone_status}" -eq 0 ]] || ! grep -Fq -- "invalid trust group for ../../outside.yml" <<<"${invalid_tombstone_output}"; then
  echo "malformed tombstone authorized an empty workflow inventory" >&2
  printf '%s\n' "${invalid_tombstone_output}" >&2
  exit 1
fi

assert_lifecycle_invalid() {
  local manifest="$1"
  local expected="$2"
  set +e
  local output
  output="$("${checker_binary}" --scan-root "${lifecycle_root}" --presence-allowlist "${manifest}" --allowlist "${empty_allowlist}" --executable-allowlist "${lifecycle_executables}" --command-allowlist "${lifecycle_transition}" --action-allowlist "${empty_allowlist}" 2>&1)"
  local status=$?
  set -e
  if [[ "${status}" -eq 0 ]] || ! grep -Fq -- "${expected}" <<<"${output}"; then
    echo "invalid lifecycle manifest was accepted: ${expected}" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
}
lifecycle_invalid="${tmp_dir}/lifecycle-invalid.json"
# Restore the future fixture used by the invalid-manifest cases below.
printf '#!/usr/bin/env bash\necho future\n' >"${lifecycle_root}/scripts/lifecycle.sh"
cp "${fixtures}/lifecycle-future.yml" "${lifecycle_workflow}"
jq '.[0].state="pending" | [.[0]]' "${lifecycle_presence}" >"${lifecycle_invalid}"
assert_lifecycle_invalid "${lifecycle_invalid}" "invalid trust group"
jq '.[0].contextSHA256="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" | [.[0]]' "${lifecycle_presence}" >"${lifecycle_invalid}"
assert_lifecycle_invalid "${lifecycle_invalid}" "no trust group matches workflow"
jq '.[0] as $active | .[1] as $staged | [$active, $staged, ($staged | .contextSHA256="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")]' "${lifecycle_presence}" >"${lifecycle_invalid}"
assert_lifecycle_invalid "${lifecycle_invalid}" "multiple staged trust groups"
jq '.[0] as $active | [$active, ($active | .state="staged")]' "${lifecycle_presence}" >"${lifecycle_invalid}"
assert_lifecycle_invalid "${lifecycle_invalid}" "mixed trust group state"
checker="${tmp_dir}/check-public-workflow-policy.sh"
cat >"${checker}" <<EOF
#!/usr/bin/env bash
exec "${checker_binary}" --presence-allowlist "${empty_allowlist}" --executable-allowlist "${fixture_executables}" --command-allowlist "${fixture_commands}" --action-allowlist "${fixture_actions}" "\$@"
EOF
chmod +x "${checker}"

printf 'package policytool\n' >"${integrity_extra}"
set +e
extra_go_output="$("${checker_binary}" 2>&1)"
extra_go_status=$?
set -e
rm -f "${integrity_extra}"
if [[ "${extra_go_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority change does not match" <<<"${extra_go_output}"; then
  echo "extra Go source bypassed policytool integrity" >&2
  printf '%s\n' "${extra_go_output}" >&2
  exit 1
fi

mkdir -p "${integrity_vendor}/mvdan.cc/sh/v3/syntax"
printf 'package syntax\n' >"${integrity_vendor}/mvdan.cc/sh/v3/syntax/override.go"
set +e
vendor_output="$("${checker_binary}" 2>&1)"
vendor_status=$?
set -e
rm -rf "${integrity_vendor}"
if [[ "${vendor_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority change does not match" <<<"${vendor_output}"; then
  echo "vendor override bypassed policytool integrity" >&2
  printf '%s\n' "${vendor_output}" >&2
  exit 1
fi

ln -s main.go "${integrity_symlink}"
set +e
symlink_output="$("${checker_binary}" 2>&1)"
symlink_status=$?
set -e
rm -f "${integrity_symlink}"
if [[ "${symlink_status}" -eq 0 ]] || ! grep -Fq -- \
  "policytool path must be a regular non-symlink file: extra-link.go" <<<"${symlink_output}"; then
  echo "policytool symlink bypassed integrity" >&2
  printf '%s\n' "${symlink_output}" >&2
  exit 1
fi

if ! grep -Fq -- 'exec env GOWORK=off GOFLAGS=-mod=readonly go run ./main.go' "${checker_binary}"; then
  echo "policytool wrapper does not execute the fixed source file" >&2
  exit 1
fi
if ! grep -Fq -- 'env GOWORK=off GOFLAGS=-mod=readonly go mod download' "${checker_binary}"; then
  echo "policytool wrapper does not prepare dependencies read-only" >&2
  exit 1
fi
if grep -Fq -- '-mod=mod' "${checker_binary}"; then
  echo "policytool wrapper permits module mutation" >&2
  exit 1
fi
policytool_hashes_before="$(git hash-object \
  "${policytool}/main.go" \
  "${policytool}/main_test.go" \
  "${policytool}/go.mod" \
  "${policytool}/go.sum" | tr '\n' ' ')"
"${checker_binary}"
ci_secret="${tmp_dir}/ci-secret.json"
ci_executable="${tmp_dir}/ci-executable.json"
ci_command="${tmp_dir}/ci-command.json"
ci_action="${tmp_dir}/ci-action.json"
ci_presence="${tmp_dir}/ci-presence.json"
jq '[.[] | select(.path == ".github/workflows/ci.yml")]' \
  "${repo_root}/.github/public-workflow-secret-allowlist.json" >"${ci_secret}"
jq '[.[] | select(.workflowPath == ".github/workflows/ci.yml")]' \
  "${repo_root}/.github/public-workflow-executable-allowlist.json" >"${ci_executable}"
jq '[.[] | select(.path == ".github/workflows/ci.yml")]' \
  "${repo_root}/.github/public-workflow-command-allowlist.json" >"${ci_command}"
jq '[.[] | select(.path == ".github/workflows/ci.yml")]' \
  "${repo_root}/.github/public-workflow-action-allowlist.json" >"${ci_action}"
jq '[.[] | select(.path == ".github/workflows/ci.yml")]' \
  "${repo_root}/.github/public-workflow-presence-allowlist.json" >"${ci_presence}"
ci_policy_args=(
  --allowlist "${ci_secret}"
  --executable-allowlist "${ci_executable}"
  --command-allowlist "${ci_command}"
  --action-allowlist "${ci_action}"
  --presence-allowlist "${ci_presence}"
)
(cd "${policytool}" && \
  "${checker_binary}" "${ci_policy_args[@]}" ".github/workflows/ci.yml")
policytool_hashes_after="$(git hash-object \
  "${policytool}/main.go" \
  "${policytool}/main_test.go" \
  "${policytool}/go.mod" \
  "${policytool}/go.sum" | tr '\n' ' ')"
if [[ "${policytool_hashes_before}" != "${policytool_hashes_after}" ]]; then
  echo "read-only policytool preparation changed trusted files" >&2
  exit 1
fi

trailing_secret="${tmp_dir}/trailing-secret.json"
trailing_executable="${tmp_dir}/trailing-executable.json"
trailing_command="${tmp_dir}/trailing-command.json"
trailing_action="${tmp_dir}/trailing-action.json"
trailing_presence="${tmp_dir}/trailing-presence.json"
printf '[] {}\n' >"${trailing_secret}"
printf '[] garbage\n' >"${trailing_executable}"
printf '[] {}\n' >"${trailing_command}"
printf '[] garbage\n' >"${trailing_action}"
printf '[] {}\n' >"${trailing_presence}"
for trust_input in secret executable command action presence; do
  args=()
  case "${trust_input}" in
    secret) args=(--allowlist "${trailing_secret}") ;;
    executable) args=(--executable-allowlist "${trailing_executable}") ;;
    command) args=(--command-allowlist "${trailing_command}") ;;
    action) args=(--action-allowlist "${trailing_action}") ;;
    presence) args=(--presence-allowlist "${trailing_presence}") ;;
  esac
  set +e
  trailing_output="$("${checker_binary}" "${args[@]}" 2>&1)"
  trailing_status=$?
  set -e
  if [[ "${trailing_status}" -eq 0 ]] || ! grep -Fq -- "unexpected trailing JSON" <<<"${trailing_output}"; then
    echo "${trust_input} allowlist accepted trailing JSON" >&2
    printf '%s\n' "${trailing_output}" >&2
    exit 1
  fi
done

null_allowlist="${tmp_dir}/null-allowlist.json"
printf 'null\n' >"${null_allowlist}"
set +e
null_output="$("${checker_binary}" --allowlist "${null_allowlist}" 2>&1)"
null_status=$?
set -e
if [[ "${null_status}" -eq 0 ]] || ! grep -Fq -- "top-level JSON array must not be null" <<<"${null_output}"; then
  echo "secret allowlist accepted JSON null" >&2
  printf '%s\n' "${null_output}" >&2
  exit 1
fi

candidate_root="${tmp_dir}/candidate"
mkdir -p "${candidate_root}"
cp -R "${repo_root}/.github" "${candidate_root}/.github"
cp -R "${repo_root}/scripts" "${candidate_root}/scripts"
"${checker_binary}" --scan-root "${candidate_root}"

# Candidate trust manifests are parsed only as transition data. Replacing one
# cannot weaken the trusted-base workflow authority.
candidate_presence="${candidate_root}/.github/public-workflow-presence-allowlist.json"
printf '[]\n' >"${candidate_presence}"
set +e
candidate_trust_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_trust_status=$?
set -e
if [[ "${candidate_trust_status}" -eq 0 ]] || ! grep -Fq -- \
  "trust manifest change does not match" <<<"${candidate_trust_output}"; then
  echo "candidate trust-manifest replacement bypassed transition validation" >&2
  printf '%s\n' "${candidate_trust_output}" >&2
  exit 1
fi
cp "${repo_root}/.github/public-workflow-presence-allowlist.json" "${candidate_presence}"

# Candidate analyzer and harness files are inventoried as data and require an
# authorized bundle lifecycle before they can become trusted implementation.
printf 'this is not Go source\n' >"${candidate_root}/.github/workflows/policytool/main.go"
set +e
candidate_analyzer_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_analyzer_status=$?
set -e
if [[ "${candidate_analyzer_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority change does not match" <<<"${candidate_analyzer_output}"; then
  echo "candidate analyzer mutation bypassed authority lifecycle" >&2
  printf '%s\n' "${candidate_analyzer_output}" >&2
  exit 1
fi
cp "${repo_root}/.github/workflows/policytool/main.go" \
  "${candidate_root}/.github/workflows/policytool/main.go"

candidate_checker="${candidate_root}/scripts/check-public-workflow-policy.sh"
printf '#!/usr/bin/env bash\nexit 0\n' >"${candidate_checker}"
set +e
candidate_checker_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_checker_status=$?
set -e
if [[ "${candidate_checker_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority change does not match" <<<"${candidate_checker_output}"; then
  echo "candidate CI checker mutation bypassed authority lifecycle" >&2
  printf '%s\n' "${candidate_checker_output}" >&2
  exit 1
fi
cp "${repo_root}/scripts/check-public-workflow-policy.sh" "${candidate_checker}"

candidate_policy_test="${candidate_root}/scripts/test-check-public-workflow-policy.sh"
printf '#!/usr/bin/env bash\nexit 0\n' >"${candidate_policy_test}"
set +e
candidate_test_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_test_status=$?
set -e
if [[ "${candidate_test_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority change does not match" <<<"${candidate_test_output}"; then
  echo "candidate CI policy-test mutation bypassed authority lifecycle" >&2
  printf '%s\n' "${candidate_test_output}" >&2
  exit 1
fi
cp "${repo_root}/scripts/test-check-public-workflow-policy.sh" "${candidate_policy_test}"

# Authority implementation changes use stage, adopt, then promote. The future
# bundle changes an operator-only verifier so the trusted wrapper remains
# executable throughout the lifecycle proof.
authority_path=".github/workflows/scripts/verify-public-workflow-branch-protection.sh"
candidate_authority="${candidate_root}/.github/public-workflow-authority.json"
candidate_verifier="${candidate_root}/${authority_path}"
archive_authority="${archive_repo}/.github/public-workflow-authority.json"
archive_verifier="${archive_repo}/${authority_path}"
active_authority="${tmp_dir}/authority-active.json"
active_verifier="${tmp_dir}/authority-active-verifier.sh"
staged_authority="${tmp_dir}/authority-staged.json"
future_verifier="${tmp_dir}/authority-future-verifier.sh"
cp "${candidate_authority}" "${active_authority}"
cp "${candidate_verifier}" "${active_verifier}"
cp "${candidate_verifier}" "${future_verifier}"
printf '\n# authority lifecycle fixture\n' >>"${future_verifier}"
future_verifier_sha="$(sha256_file "${future_verifier}")"
jq --arg path "${authority_path}" --arg sha "${future_verifier_sha}" '
  .bundles += [{
    state: "staged",
    files: (.bundles[0].files | map(if .path == $path then .sha256 = $sha else . end))
  }]
' "${active_authority}" >"${staged_authority}"

# Stage-only retains the active implementation.
cp "${staged_authority}" "${candidate_authority}"
"${checker_binary}" --scan-root "${candidate_root}" >/dev/null

# The same pull request cannot both introduce and realize the staged bundle.
cp "${future_verifier}" "${candidate_verifier}"
set +e
same_pr_authority_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
same_pr_authority_status=$?
set -e
if [[ "${same_pr_authority_status}" -eq 0 ]] || ! grep -Fq -- \
  "authority bundle cannot be staged and adopted in the same pull request" <<<"${same_pr_authority_output}"; then
  echo "same-PR authority stage and adoption was not explicitly rejected" >&2
  printf '%s\n' "${same_pr_authority_output}" >&2
  exit 1
fi

# Once staging is trusted in the base, adoption realizes the staged bundle.
cp "${staged_authority}" "${archive_authority}"
"${archive_checker}" --scan-root "${candidate_root}" >/dev/null

# Once the base realizes the staged bundle, promotion makes it solely active.
cp "${future_verifier}" "${archive_verifier}"
jq '.bundles = [(.bundles[] | select(.state == "staged") | .state = "active")]' \
  "${staged_authority}" >"${candidate_authority}"
"${archive_checker}" --scan-root "${candidate_root}" >/dev/null

cp "${active_authority}" "${candidate_authority}"
cp "${active_verifier}" "${candidate_verifier}"
cp "${active_authority}" "${archive_authority}"
cp "${active_verifier}" "${archive_verifier}"

(cd "${policytool}" && \
  "${checker_binary}" --scan-root "${candidate_root}" \
    "${ci_policy_args[@]}" ".github/workflows/ci.yml")

printf 'name: Outside candidate root\n' >"${tmp_dir}/outside.yml"
set +e
relative_outside_output="$(cd "${policytool}" && \
  "${checker_binary}" --scan-root "${candidate_root}" \
    "${ci_policy_args[@]}" "../outside.yml" 2>&1)"
relative_outside_status=$?
set -e
if [[ "${relative_outside_status}" -eq 0 ]] || ! grep -Fq -- \
  "workflow path ../outside.yml is outside repository" <<<"${relative_outside_output}"; then
  echo "relative workflow path escaped alternate scan root" >&2
  printf '%s\n' "${relative_outside_output}" >&2
  exit 1
fi

cat >"${candidate_root}/.github/workflows/candidate-live.yml" <<'YAML'
name: Candidate live cloud workflow
on:
  pull_request:
defaults:
  run:
    shell: bash
jobs:
  live:
    runs-on: ubuntu-latest
    steps:
      - run: doctl compute droplet list
YAML
set +e
candidate_live_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_live_status=$?
set -e
if [[ "${candidate_live_status}" -eq 0 ]] || ! grep -Fq -- \
  "executable provider CLI doctl" <<<"${candidate_live_output}"; then
  echo "same-PR policy and live workflow weakening bypassed trusted base enforcement" >&2
  printf '%s\n' "${candidate_live_output}" >&2
  exit 1
fi
rm "${candidate_root}/.github/workflows/candidate-live.yml"

cat >"${candidate_root}/.github/workflows/candidate-service.yml" <<'YAML'
name: Candidate service execution
on: pull_request_target
permissions:
  contents: write
defaults:
  run:
    shell: bash
jobs:
  service:
    runs-on: ubuntu-latest
    services:
      attacker:
        image: ghcr.io/example/attacker:latest
    steps:
      - run: '(( 1 ))'
YAML
set +e
candidate_service_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_service_status=$?
set -e
if [[ "${candidate_service_status}" -eq 0 ]] || ! grep -Fq -- \
  "declares forbidden job services" <<<"${candidate_service_output}"; then
  echo "candidate service workflow bypassed trusted base enforcement" >&2
  printf '%s\n' "${candidate_service_output}" >&2
  exit 1
fi
rm "${candidate_root}/.github/workflows/candidate-service.yml"

cat >"${candidate_root}/.github/workflows/candidate-arithmetic.yml" <<'YAML'
name: Candidate arithmetic execution
on: pull_request_target
permissions:
  contents: write
defaults:
  run:
    shell: bash
jobs:
  arithmetic:
    runs-on: ubuntu-latest
    env:
      X: ${{ github.event.pull_request.title }}
    steps:
      - run: '(( X ))'
YAML
set +e
candidate_arithmetic_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_arithmetic_status=$?
set -e
if [[ "${candidate_arithmetic_status}" -eq 0 ]] || ! grep -Fq -- \
  "executes unreviewed exact shell statement" <<<"${candidate_arithmetic_output}"; then
  echo "candidate arithmetic workflow bypassed trusted base enforcement" >&2
  printf '%s\n' "${candidate_arithmetic_output}" >&2
  exit 1
fi
rm "${candidate_root}/.github/workflows/candidate-arithmetic.yml"

for inherited_shape in scalar mapping; do
  candidate_inherit="${candidate_root}/.github/workflows/candidate-inherit.yml"
  if [[ "${inherited_shape}" == scalar ]]; then
    inherited_yaml='    secrets: inherit'
    inherited_expected='inherits all job secrets'
  else
    inherited_yaml=$'    secrets:\n      token: inherit'
    inherited_expected='maps inherited secret token'
  fi
  cat >"${candidate_inherit}" <<YAML
name: Candidate inherited secrets
on: pull_request_target
jobs:
  inherited:
    uses: acme/platform/.github/workflows/reuse.yml@0123456789012345678901234567890123456789
${inherited_yaml}
YAML
  set +e
  candidate_inherit_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
  candidate_inherit_status=$?
  set -e
  if [[ "${candidate_inherit_status}" -eq 0 ]] || ! grep -Fq -- "${inherited_expected}" <<<"${candidate_inherit_output}"; then
    echo "candidate ${inherited_shape} inherited-secret shape bypassed policy" >&2
    printf '%s\n' "${candidate_inherit_output}" >&2
    exit 1
  fi
  rm "${candidate_inherit}"
done

candidate_workflow_call="${candidate_root}/.github/workflows/candidate-workflow-call.yml"
cat >"${candidate_workflow_call}" <<'YAML'
name: Candidate reusable environment secret
on:
  workflow_call:
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    env:
      DEPLOY_TOKEN: ${{ secrets.RELEASES_TOKEN }}
    steps:
      - run: echo reusable
YAML
set +e
candidate_workflow_call_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
candidate_workflow_call_status=$?
set -e
if [[ "${candidate_workflow_call_status}" -eq 0 ]] || ! grep -Fq -- \
  "public workflow references forbidden repository secret RELEASES_TOKEN" <<<"${candidate_workflow_call_output}"; then
  echo "workflow_call environment secret bypassed public workflow policy" >&2
  printf '%s\n' "${candidate_workflow_call_output}" >&2
  exit 1
fi
rm "${candidate_workflow_call}"

for push_selector in branches tags; do
  candidate_push_secret="${candidate_root}/.github/workflows/candidate-${push_selector}-secret.yml"
  cat >"${candidate_push_secret}" <<YAML
name: Candidate ${push_selector} secret
on:
  push:
    ${push_selector}:
      - main
jobs:
  publish:
    runs-on: ubuntu-latest
    env:
      PUBLISH_TOKEN: \${{ secrets.RELEASES_TOKEN }}
    steps:
      - run: echo publish
YAML
  set +e
  candidate_push_output="$("${checker_binary}" --scan-root "${candidate_root}" 2>&1)"
  candidate_push_status=$?
  set -e
  if [[ "${candidate_push_status}" -eq 0 ]] || ! grep -Fq -- \
    "public workflow references forbidden repository secret RELEASES_TOKEN" <<<"${candidate_push_output}"; then
    echo "${push_selector} push repository secret bypassed public workflow policy" >&2
    printf '%s\n' "${candidate_push_output}" >&2
    exit 1
  fi
  rm "${candidate_push_secret}"
done

assert_exact_mutation_rejected() {
  local label="$1"
  local workflow="$2"
  local sed_expression="$3"
  local expected="$4"
  mutated_workflow="${workflow}"
  cp "${workflow}" "${mutation_backup}"
  sed -i.bak -e "${sed_expression}" "${workflow}"
  rm -f "${workflow}.bak"
  set +e
  local output
  output="$("${checker_binary}" 2>&1)"
  local status=$?
  set -e
  cp "${mutation_backup}" "${workflow}"
  mutated_workflow=""
  if [[ "${status}" -eq 0 ]] || ! grep -Fq -- "${expected}" <<<"${output}"; then
    echo "exact workflow mutation was accepted: ${label}" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
}

if rg -n --glob '*.yml' --glob '*.yaml' \
  'secrets\.|RELEASES_TOKEN|GOPRIVATE|x-access-token|repo_dispatch_token|repository-dispatch' \
  "${repo_root}/.github/workflows"; then
  echo "pull_request-capable workflow retains repository-secret authority" >&2
  exit 1
fi
assert_exact_mutation_rejected \
  "Go test trailing target" \
  "${repo_root}/.github/workflows/ci.yml" \
  's|go test -v -race -coverprofile=coverage.out ./...|go test -v -race -coverprofile=coverage.out ./... ./config/...|' \
  "unreviewed exact statement containing go"
assert_exact_mutation_rejected \
  "GitHub release arguments" \
  "${repo_root}/.github/workflows/release.yml" \
  's/--draft=false/--draft=true/' \
  "unreviewed exact statement containing gh"
assert_exact_mutation_rejected \
  "candidate fixed GitHub origin" \
  "${repo_root}/.github/workflows/public-workflow-policy.yml" \
  's|https://github.com/\${CANDIDATE_REPOSITORY}.git|https://example.com/\${CANDIDATE_REPOSITORY}.git|' \
  "uses unreviewed exact action actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5"
assert_exact_mutation_rejected \
  "candidate credential isolation" \
  "${repo_root}/.github/workflows/public-workflow-policy.yml" \
  '/-c credential.helper=/d' \
  "uses unreviewed exact action actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5"
assert_exact_mutation_rejected \
  "candidate symlink rejection" \
  "${repo_root}/.github/workflows/public-workflow-policy.yml" \
  's/find candidate -type l/find candidate -type f/' \
  "uses unreviewed exact action actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5"
assert_exact_mutation_rejected \
  "release ancestry command removed" \
  "${repo_root}/.github/workflows/release.yml" \
  '/git merge-base --is-ancestor/d' \
  "uses unreviewed exact action actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10"
assert_exact_mutation_rejected \
  "release ancestry ref changed" \
  "${repo_root}/.github/workflows/release.yml" \
  's|refs/remotes/origin/main|refs/remotes/origin/release|g' \
  "uses unreviewed exact action actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10"
assert_exact_mutation_rejected \
  "dynamic trailing expression" \
  "${repo_root}/.github/workflows/ci.yml" \
  's|go test -v -race -coverprofile=coverage.out ./...|go test -v -race -coverprofile=coverage.out ./... "${{ vars.EXTRA_TARGET }}"|' \
  "unreviewed exact statement containing go"
assert_exact_mutation_rejected \
  "workflow inherited safe environment" \
  "${repo_root}/.github/workflows/ci.yml" \
  's/WFCTL_DIFFCACHE: ":memory:"/WFCTL_DIFFCACHE: ":disk:"/' \
  "uses unreviewed exact action actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10"
assert_exact_mutation_rejected \
  "approved local script working-directory" \
  "${repo_root}/.github/workflows/ci.yml" \
  $'s/        run: |/        working-directory: \/tmp\\\n        run: |/g' \
  "declares forbidden working-directory"
assert_exact_mutation_rejected \
  "release trigger" \
  "${repo_root}/.github/workflows/release.yml" \
  's/^  push:/  pull_request_target:/' \
  "uses unreviewed exact action actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10"
assert_exact_mutation_rejected \
  "job container" \
  "${repo_root}/.github/workflows/ci.yml" \
  $'s/    runs-on: ubuntu-latest/    runs-on: ubuntu-latest\\\n    container: ghcr.io\/example\/attacker:latest/g' \
  "uses unreviewed exact action actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10"
assert_exact_mutation_rejected \
  "standalone builtin" \
  "${repo_root}/.github/workflows/ci.yml" \
  $'s/        run: |/        run: |\\\n          set -x/g' \
  "unreviewed exact statement containing set"
assert_exact_mutation_rejected \
  "pull request repository secret" \
  "${repo_root}/.github/workflows/ci.yml" \
  's/WFCTL_DIFFCACHE: ":memory:"/WFCTL_DIFFCACHE: ":memory:"\n  PRIVATE_TOKEN: ${{ secrets.RELEASES_TOKEN }}/' \
  "public workflow references forbidden repository secret RELEASES_TOKEN"
assert_exact_mutation_rejected \
  "pull request target repository secret" \
  "${repo_root}/.github/workflows/ci.yml" \
  's/pull_request:/pull_request_target:/; s/WFCTL_DIFFCACHE: ":memory:"/WFCTL_DIFFCACHE: ":memory:"\n  PRIVATE_TOKEN: ${{ secrets.RELEASES_TOKEN }}/' \
  "public workflow references forbidden repository secret RELEASES_TOKEN"
assert_exact_mutation_rejected \
  "tag push repository secret" \
  "${repo_root}/.github/workflows/release.yml" \
  's/TAG_NAME: \${{ github.ref_name }}/TAG_NAME: ${{ github.ref_name }}\n  PRIVATE_TOKEN: ${{ secrets.RELEASES_TOKEN }}/' \
  "public workflow references forbidden repository secret RELEASES_TOKEN"
assert_exact_mutation_rejected \
  "push cloud secret" \
  "${repo_root}/.github/workflows/release.yml" \
  's/TAG_NAME: \${{ github.ref_name }}/TAG_NAME: ${{ github.ref_name }}\n  CLOUD_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}/' \
  "references known cloud secret DIGITALOCEAN_TOKEN"
assert_exact_mutation_rejected \
  "release tag shell interpolation" \
  "${repo_root}/.github/workflows/release.yml" \
  's/${{ env.TAG_NAME }}/${{ github.ref_name }}/' \
  "executes unreviewed exact statement containing gh"

for action_mutation in \
  '${{ vars.ACTION_REF }}' \
  'actions/checkout@main' \
  'actions/checkout@v4' \
  'actions/checkout@0123456789012345678901234567890123456789'; do
  expected="uses unreviewed exact action ${action_mutation}"
  if [[ "${action_mutation}" == '${{ vars.ACTION_REF }}' ]]; then
    expected="uses forbidden dynamic action ${action_mutation}"
  fi
  assert_exact_mutation_rejected \
    "action reference ${action_mutation}" \
    "${repo_root}/.github/workflows/ci.yml" \
    "s/actions\\/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10/${action_mutation//\//\\/}/g" \
    "${expected}"
done

assert_exact_mutation_rejected \
  "upload-artifact path" \
  "${repo_root}/.github/workflows/ci.yml" \
  's|path: ui/dist/|path: ui/other-dist/|' \
  "uses unreviewed exact action actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
assert_exact_mutation_rejected \
  "GitHub release environment" \
  "${repo_root}/.github/workflows/release.yml" \
  's/GH_TOKEN: \${{ github.token }}/GH_TOKEN: ${{ secrets.RELEASES_TOKEN }}/' \
  "public workflow references forbidden repository secret RELEASES_TOKEN"
assert_exact_mutation_rejected \
  "mutable benchstat version" \
  "${repo_root}/.github/workflows/benchmark.yml" \
  's|benchstat@v0.0.0-20260709024250-82a0b07e230d|benchstat@latest|' \
  'executes categorically forbidden command go'

pass_allowlist="${tmp_dir}/pass-allowlist.json"
pass_presence="${tmp_dir}/pass-presence.json"
pass_scan_root="${tmp_dir}/pass-scan-root"
pass_fixture_dir="${pass_scan_root}/.github/workflows/fixtures/public-workflow-policy"
mkdir -p "${pass_fixture_dir}" "${pass_scan_root}/scripts"
cp "${checker_binary}" "${pass_scan_root}/scripts/check-public-workflow-policy.sh"
cp \
  "${fixtures}/pass.yml" \
  "${fixtures}/pass-negative-guard.yml" \
  "${fixtures}/pass-expression-and-deny-guard.yml" \
  "${fixtures}/reject.yml" \
  "${pass_fixture_dir}/"
pass_workflow="${pass_fixture_dir}/pass.yml"
pass_negative_guard_workflow="${pass_fixture_dir}/pass-negative-guard.yml"
pass_expression_guard_workflow="${pass_fixture_dir}/pass-expression-and-deny-guard.yml"
reject_workflow="${pass_fixture_dir}/reject.yml"
for required_staged_path in \
  "${pass_workflow}" \
  "${pass_negative_guard_workflow}" \
  "${pass_expression_guard_workflow}" \
  "${reject_workflow}" \
  "${pass_scan_root}/scripts/check-public-workflow-policy.sh"; do
  if [[ ! -f "${required_staged_path}" || -L "${required_staged_path}" ]]; then
    echo "missing staged policy fixture dependency: ${required_staged_path}" >&2
    exit 1
  fi
done
cat >"${pass_presence}" <<'JSON'
[
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state":"active","presence":"present"},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","state":"active","presence":"present"},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-expression-and-deny-guard.yml","contextSHA256":"8efbd606ce583197947daf7d1c19795490827a2a2b5f6258adc9c0e5f19b70eb","state":"active","presence":"present"}
]
JSON
printf '[]\n' >"${pass_allowlist}"

pass_commands="${tmp_dir}/pass-commands.json"
cat >"${pass_commands}" <<'JSON'
[
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass.yml",
    "command": "go",
    "statementSHA256": "5384574a39b2103666734bbe92565841174832d0b8865a6d5f521eb663438c51",
    "contextSHA256": "304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b",
    "state": "active", "rationale": "Run the exact credential-free integration test fixture."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass.yml",
    "command": "go",
    "statementSHA256": "1bb497e3e13a1105cf24e3359fa3ef75de08b66ff8a2839cd7f9ea97824d9eb3",
    "contextSHA256": "304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b",
    "state": "active", "rationale": "Run the exact credential-free default Go test fixture."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass.yml",
    "command": "gh",
    "statementSHA256": "0a111d913d8601e23bc6e43fa1b4b6a5fa65c44c342a26c23f10bf8fa119827a",
    "contextSHA256": "304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b",
    "state": "active", "rationale": "Exercise the exact GitHub release upload fixture."
  },
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","command":"echo","statementSHA256":"552ab348c73a453fe78c6df7a1b2cf0c8381dc11a20908a96a643105c8abfdc7","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact rejection-guard echo statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","command":"exit","statementSHA256":"552ab348c73a453fe78c6df7a1b2cf0c8381dc11a20908a96a643105c8abfdc7","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact rejection-guard exit statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","command":"rg","statementSHA256":"552ab348c73a453fe78c6df7a1b2cf0c8381dc11a20908a96a643105c8abfdc7","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact rejection-guard search statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","command":"$assignment","statementSHA256":"df3893e5269970fcf8bb076be5b5f849eec4a7237b311ab4129af31b78271a09","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact safe standalone assignment fixture."},
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass-expression-and-deny-guard.yml",
    "command": "go",
    "statementSHA256": "1bb497e3e13a1105cf24e3359fa3ef75de08b66ff8a2839cd7f9ea97824d9eb3",
    "contextSHA256": "8efbd606ce583197947daf7d1c19795490827a2a2b5f6258adc9c0e5f19b70eb",
    "state": "active", "rationale": "Run the exact credential-free Go test fixture."
  },
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-expression-and-deny-guard.yml","command":"echo","statementSHA256":"0d3a22321340ce7d2928c4f9b228a94c4ae2e00f5cd9482b48ed0d7b0f2aceab","contextSHA256":"8efbd606ce583197947daf7d1c19795490827a2a2b5f6258adc9c0e5f19b70eb","state": "active", "rationale":"Exact expression-guard echo statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-expression-and-deny-guard.yml","command":"exit","statementSHA256":"0d3a22321340ce7d2928c4f9b228a94c4ae2e00f5cd9482b48ed0d7b0f2aceab","contextSHA256":"8efbd606ce583197947daf7d1c19795490827a2a2b5f6258adc9c0e5f19b70eb","state": "active", "rationale":"Exact expression-guard exit statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-expression-and-deny-guard.yml","command":"rg","statementSHA256":"0d3a22321340ce7d2928c4f9b228a94c4ae2e00f5cd9482b48ed0d7b0f2aceab","contextSHA256":"8efbd606ce583197947daf7d1c19795490827a2a2b5f6258adc9c0e5f19b70eb","state": "active", "rationale":"Exact expression-guard search statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","command":"echo","statementSHA256":"16e6dfba0f3777c91b890a6aa03e083595250d63a6cc015886c4092caab0b07a","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","state": "active", "rationale":"Exact negative-guard echo statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","command":"exit","statementSHA256":"16e6dfba0f3777c91b890a6aa03e083595250d63a6cc015886c4092caab0b07a","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","state": "active", "rationale":"Exact negative-guard exit statement."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","command":"rg","statementSHA256":"16e6dfba0f3777c91b890a6aa03e083595250d63a6cc015886c4092caab0b07a","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","state": "active", "rationale":"Exact negative-guard search statement."}
]
JSON

pass_actions="${tmp_dir}/pass-actions.json"
cat >"${pass_actions}" <<'JSON'
[
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","uses":"actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5","nodeSHA256":"72a9f885834e7e7cfc170d24954ed15b9c11a222760a6b919510993561321f03","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact immutable fixture action node."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","uses":"actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff","nodeSHA256":"4097668e631432c1244a4dd4d9557c50491bf42112c9ba85e62d3fcf637047d1","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact immutable fixture action node."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","uses":"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02","nodeSHA256":"0d8a6b42c70a0f3275850fe9d63cfb646c0a9e0d7f6ea1c8b45171a1076060b7","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact immutable fixture action node."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","uses":"GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956","nodeSHA256":"93dce32c457545dd0624d77c36ec255298b321308974a9cf046e67691e5dd745","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact immutable fixture action node."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass.yml","uses":"goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94","nodeSHA256":"e755472a8b992588d44f5bed60d0ebdf304a0854661bd6b598ca4f6bceafa4b9","contextSHA256":"304ae504129bf47e8740e781cb93d32228a86ff9447cd1836f4ad73df920260b","state": "active", "rationale":"Exact immutable fixture action node."}
]
JSON

"${checker}" \
  --scan-root "${pass_scan_root}" \
  --presence-allowlist "${pass_presence}" \
  --allowlist "${pass_allowlist}" \
  --executable-allowlist "${pass_allowlist}" \
  --command-allowlist "${pass_commands}" \
  --action-allowlist "${pass_actions}" \
  "${pass_workflow}" \
  "${pass_negative_guard_workflow}" \
  "${pass_expression_guard_workflow}"

mutated_workflow="${pass_workflow}"
cp "${mutated_workflow}" "${mutation_backup}"
sed -i.bak 's/SAFE_VALUE=one/SAFE_VALUE=two/' "${mutated_workflow}"
rm -f "${mutated_workflow}.bak"
set +e
assignment_mutation_output="$("${checker}" \
  --scan-root "${pass_scan_root}" \
  --presence-allowlist "${pass_presence}" \
  --allowlist "${pass_allowlist}" \
  --executable-allowlist "${pass_allowlist}" \
  --command-allowlist "${pass_commands}" \
  --action-allowlist "${pass_actions}" \
  "${pass_workflow}" \
  "${pass_negative_guard_workflow}" \
  "${pass_expression_guard_workflow}" 2>&1)"
assignment_mutation_status=$?
set -e
cp "${mutation_backup}" "${mutated_workflow}"
mutated_workflow=""
if [[ "${assignment_mutation_status}" -eq 0 ]] || ! grep -Fq -- \
  "executes unreviewed standalone assignment" <<<"${assignment_mutation_output}"; then
  echo "safe assignment digest mutation was accepted" >&2
  printf '%s\n' "${assignment_mutation_output}" >&2
  exit 1
fi

set +e
yaml_structure_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-yaml-structure.yml" 2>&1)"
yaml_structure_status=$?
set -e
if [[ "${yaml_structure_status}" -eq 0 ]]; then
  echo "expected YAML aliases and duplicate keys to fail policy" >&2
  exit 1
fi
for expected in \
  "contains forbidden YAML alias" \
  "contains duplicate mapping key runs-on" \
  "contains duplicate mapping key run" \
  "contains duplicate mapping key uses" \
  "contains duplicate mapping key VALUE"; do
  if ! grep -Fq -- "${expected}" <<<"${yaml_structure_output}"; then
    echo "missing YAML structure diagnostic: ${expected}" >&2
    printf '%s\n' "${yaml_structure_output}" >&2
    exit 1
  fi
done

statement_secret_allowlist="${tmp_dir}/statement-secret-allowlist.json"
cat >"${statement_secret_allowlist}" <<'JSON'
[
  {
    "path": "scripts/fixtures/public-workflow-policy/reject-statement-authority.yml",
    "secret": "RELEASES_TOKEN",
    "state": "active", "rationale": "Mutation fixture proves exact statements reject secret output independently of secret-name review."
  }
]
JSON
set +e
statement_output="$("${checker}" \
  --allowlist "${statement_secret_allowlist}" \
  "${fixtures}/reject-statement-authority.yml" 2>&1)"
statement_status=$?
set -e
if [[ "${statement_status}" -eq 0 ]]; then
  echo "expected statement authority mutations to fail policy" >&2
  exit 1
fi
for expected in \
  "unreviewed exact statement containing go" \
  "assigns forbidden execution environment variable PATH" \
  "assigns forbidden execution environment variable BASH_ENV" \
  "assigns forbidden execution environment variable ENV" \
  "assigns forbidden execution environment variable SHELLOPTS" \
  "assigns forbidden execution environment variable LD_PRELOAD" \
  "assigns forbidden execution environment variable DYLD_INSERT_LIBRARIES" \
  "assigns forbidden execution environment variable BASH_FUNC_wrapper%% through env" \
  "redirects to forbidden GitHub command file GITHUB_ENV" \
  "redirects to forbidden GitHub command file GITHUB_PATH" \
  "unreviewed exact statement containing echo" \
  "unreviewed exact statement containing printf" \
  "uses forbidden dynamic command execution"; do
  if ! grep -Fq -- "${expected}" <<<"${statement_output}"; then
    echo "missing statement authority diagnostic: ${expected}" >&2
    printf '%s\n' "${statement_output}" >&2
    exit 1
  fi
done

set +e
execution_context_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-execution-context.yml" 2>&1)"
execution_context_status=$?
set -e
if [[ "${execution_context_status}" -eq 0 ]]; then
  echo "expected working-directory and inherited environment overrides to fail policy" >&2
  exit 1
fi
for expected in \
  "declares forbidden defaults.run.working-directory" \
  "declares forbidden working-directory" \
  "execution-affecting environment variable CC" \
  "execution-affecting environment variable GIT_CONFIG_GLOBAL" \
  "execution-affecting environment variable HOME" \
  "execution-affecting environment variable IFS" \
  "execution-affecting environment variable GOFLAGS" \
  "execution-affecting environment variable BASH_FUNC_WORKFLOW%%" \
  "execution-affecting environment variable BASH_FUNC_JOB%%" \
  "execution-affecting environment variable BASH_FUNC_STEP%%"; do
  if ! grep -Fq -- "${expected}" <<<"${execution_context_output}"; then
    echo "missing execution context diagnostic: ${expected}" >&2
    printf '%s\n' "${execution_context_output}" >&2
    exit 1
  fi
done

reject_allowlist="${tmp_dir}/reject-allowlist.json"
reject_presence="${tmp_dir}/reject-presence.json"
printf '[{"path":".github/workflows/fixtures/public-workflow-policy/reject.yml","contextSHA256":"e604ecf5a5ac14b51f40aa220e0aa14aeef76f9ddf297b6b69002d155a1f1715","state":"active","presence":"present"}]\n' >"${reject_presence}"
cat >"${reject_allowlist}" <<'JSON'
[
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/reject.yml",
    "contextSHA256": "e604ecf5a5ac14b51f40aa220e0aa14aeef76f9ddf297b6b69002d155a1f1715",
    "secret": "DIGITALOCEAN_TOKEN",
    "state": "active", "rationale": "A rationale must never make a known cloud credential acceptable."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/reject.yml",
    "contextSHA256": "e604ecf5a5ac14b51f40aa220e0aa14aeef76f9ddf297b6b69002d155a1f1715",
    "secret": "DEPLOY_AUTH",
    "state": "active", "rationale": "An alias must never hide provider authority from the policy."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/reject.yml",
    "contextSHA256": "e604ecf5a5ac14b51f40aa220e0aa14aeef76f9ddf297b6b69002d155a1f1715",
    "secret": "STALE_TOKEN",
    "state": "active", "rationale": "This deliberately stale exception proves fail-closed validation."
  }
]
JSON

set +e
reject_output="$("${checker}" \
  --scan-root "${pass_scan_root}" \
  --presence-allowlist "${reject_presence}" \
  --allowlist "${reject_allowlist}" \
  "${reject_workflow}" 2>&1)"
reject_status=$?
set -e

if [[ "${reject_status}" -eq 0 ]]; then
  echo "expected forbidden workflow fixture to fail policy" >&2
  exit 1
fi

for expected in \
  "self-hosted runner" \
  "id-token: write" \
  "known cloud secret DIGITALOCEAN_TOKEN" \
  "public workflow references forbidden repository secret DEPLOY_AUTH" \
  "provider authority with secret DEPLOY_AUTH" \
  "executable provider CLI doctl" \
  "fixed provider API api.digitalocean.com" \
  "provider SDK marker with provider authority" \
  "integration tag with provider authority" \
  "named live test" \
  "manual provider-authority job" \
  "scheduled provider-authority job" \
  "public workflow references forbidden repository secret UNREVIEWED_TOKEN" \
  "stale allowlist entry STALE_TOKEN"; do
  if ! grep -Fq -- "${expected}" <<<"${reject_output}"; then
    echo "missing expected policy diagnostic: ${expected}" >&2
    printf '%s\n' "${reject_output}" >&2
    exit 1
  fi
done

executable_escape="${pass_scan_root}/executable-escape.sh"
ln -s /dev/null "${executable_escape}"
executable_escape_rel="${executable_escape#"${pass_scan_root}/"}"
invalid_executables="${tmp_dir}/invalid-executables.json"
negative_presence="${tmp_dir}/negative-presence.json"
printf '[{"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","state":"active","presence":"present"}]\n' >"${negative_presence}"
cat >"${invalid_executables}" <<EOF
[
  {"path":"scripts/check-public-workflow-policy.sh","workflowPath":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","sha256":"0000000000000000000000000000000000000000000000000000000000000000","state":"active","rationale":"Hash mismatch and stale-entry mutation fixture."},
  {"path":"../escape.sh","workflowPath":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","sha256":"0000000000000000000000000000000000000000000000000000000000000000","state":"active","rationale":"Traversal mutation fixture."},
  {"path":"${executable_escape_rel}","workflowPath":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","contextSHA256":"83ba6760400ca021cbcfcf1df10cbf6bee795312632b1853da4167a1080a25f1","sha256":"0000000000000000000000000000000000000000000000000000000000000000","state":"active","rationale":"Symlink escape mutation fixture."}
]
EOF
set +e
executable_integrity_output="$("${checker}" \
  --scan-root "${pass_scan_root}" \
  --presence-allowlist "${negative_presence}" \
  --allowlist "${empty_allowlist}" \
  --executable-allowlist "${invalid_executables}" \
  "${pass_negative_guard_workflow}" 2>&1)"
executable_integrity_status=$?
set -e
for expected in \
  "executable hash mismatch for scripts/check-public-workflow-policy.sh" \
  "stale executable allowlist entry scripts/check-public-workflow-policy.sh" \
  "executable allowlist path ../escape.sh escapes the repository" \
  "resolves outside repository"; do
  if [[ "${executable_integrity_status}" -eq 0 ]] || ! grep -Fq -- "${expected}" <<<"${executable_integrity_output}"; then
    echo "missing expected executable integrity diagnostic: ${expected}" >&2
    printf '%s\n' "${executable_integrity_output}" >&2
    exit 1
  fi
done

set +e
permissions_output="$(TMPDIR="${tmp_dir}" "${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-permissions.yml" 2>&1)"
permissions_status=$?
set -e

if [[ "${permissions_status}" -eq 0 ]]; then
  echo "expected unsafe permission shapes to fail policy" >&2
  exit 1
fi
# The GitHub expression below is an intentionally literal expected diagnostic.
# shellcheck disable=SC2016
for expected in \
  "workflow scripts/fixtures/public-workflow-policy/reject-permissions.yml uses forbidden permissions: write-all" \
  "job job-write-all uses forbidden permissions: write-all" \
  'job dynamic-permissions uses forbidden dynamic permissions selector ${{ vars.PERMISSIONS }}' \
  "job malformed-permissions uses unsupported permissions shape"; do
  if ! grep -Fq -- "${expected}" <<<"${permissions_output}"; then
    echo "missing expected permissions diagnostic: ${expected}" >&2
    printf '%s\n' "${permissions_output}" >&2
    exit 1
  fi
done

set +e
implicit_permissions_output="$(TMPDIR="${tmp_dir}" "${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-implicit-permissions.yml" 2>&1)"
implicit_permissions_status=$?
set -e
if [[ "${implicit_permissions_status}" -eq 0 ]] ||
  ! grep -Fq -- "job implicit-defaults does not declare permissions and workflow has no explicit permissions to inherit" <<<"${implicit_permissions_output}"; then
  echo "implicit repository-default job permissions passed policy" >&2
  printf '%s\n' "${implicit_permissions_output}" >&2
  exit 1
fi

set +e
dynamic_secrets_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-dynamic-secrets.yml" 2>&1)"
dynamic_secrets_status=$?
set -e

if [[ "${dynamic_secrets_status}" -eq 0 ]]; then
  echo "expected dynamic credential selectors to fail policy" >&2
  exit 1
fi
for expected in \
  "dynamic secret selector secrets[vars.NAME]" \
  "dynamic secret selector secrets[format('{0}_TOKEN', vars.PROVIDER)]" \
  "dynamic secret selector secrets[vars.DIGITALOCEAN_TOKEN]" \
  "dynamic secret selector secrets.*" \
  "whole secrets context" \
  "known cloud credential variable reference DIGITALOCEAN_TOKEN"; do
  if ! grep -Fq -- "${expected}" <<<"${dynamic_secrets_output}"; then
    echo "missing expected dynamic credential diagnostic: ${expected}" >&2
    printf '%s\n' "${dynamic_secrets_output}" >&2
    exit 1
  fi
done

set +e
env_indirection_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-env-indirection.yml" 2>&1)"
env_indirection_status=$?
set -e

if [[ "${env_indirection_status}" -eq 0 ]]; then
  echo "expected provider environment and dynamic command indirection to fail policy" >&2
  exit 1
fi
for expected in \
  "environment value contains provider CLI doctl" \
  "environment value contains provider API api.digitalocean.com" \
  "environment value contains provider SDK marker" \
  "dynamic command execution" \
  "executes categorically forbidden command eval"; do
  if ! grep -Fq -- "${expected}" <<<"${env_indirection_output}"; then
    echo "missing expected environment indirection diagnostic: ${expected}" >&2
    printf '%s\n' "${env_indirection_output}" >&2
    exit 1
  fi
done

eval_exact_presence="${tmp_dir}/eval-exact-presence.json"
eval_exact_commands="${tmp_dir}/eval-exact-commands.json"
eval_exact_root="${tmp_dir}/eval-exact-root"
mkdir -p "${eval_exact_root}/.github/workflows"
cp "${fixtures}/reject-env-indirection.yml" "${eval_exact_root}/.github/workflows/eval.yml"
cat >"${eval_exact_presence}" <<'JSON'
[
  {
    "path": ".github/workflows/eval.yml",
    "contextSHA256": "01d7192e6358dc28b4a69606b4034e5e9beaa0cf74b1a5587d87eba4b92c4946",
    "state": "active", "presence": "present"
  }
]
JSON
cat >"${eval_exact_commands}" <<'JSON'
[
  {
    "path": ".github/workflows/eval.yml",
    "command": "eval",
    "statementSHA256": "386106e2283c16c542972bf7332921a2bf316fd46e3bba0cb0f8638bb68117cf",
    "contextSHA256": "01d7192e6358dc28b4a69606b4034e5e9beaa0cf74b1a5587d87eba4b92c4946",
    "state": "active", "rationale": "Mutation: exact eval row must remain categorically unusable."
  }
]
JSON
set +e
eval_exact_output="$("${checker_binary}" \
  --scan-root "${eval_exact_root}" \
  --presence-allowlist "${eval_exact_presence}" \
  --allowlist "${empty_allowlist}" \
  --executable-allowlist "${empty_allowlist}" \
  --command-allowlist "${eval_exact_commands}" \
  --action-allowlist "${empty_allowlist}" 2>&1)"
eval_exact_status=$?
set -e
if [[ "${eval_exact_status}" -eq 0 ]] ||
  ! grep -Fq -- "executes categorically forbidden command eval" <<<"${eval_exact_output}" ||
  ! grep -Fq -- "provider-capable command eval is categorically unallowlistable" <<<"${eval_exact_output}"; then
  echo "exact command authority authorized eval dynamic execution" >&2
  printf '%s\n' "${eval_exact_output}" >&2
  exit 1
fi

builtin_exact_presence="${tmp_dir}/builtin-exact-presence.json"
builtin_exact_commands="${tmp_dir}/builtin-exact-commands.json"
builtin_exact_root="${tmp_dir}/builtin-exact-root"
mkdir -p "${builtin_exact_root}/.github/workflows"
cp "${fixtures}/reject-builtin-wrapper.yml" "${builtin_exact_root}/.github/workflows/builtin.yml"
cat >"${builtin_exact_presence}" <<'JSON'
[
  {
    "path": ".github/workflows/builtin.yml",
    "contextSHA256": "8a698faf1dcaeb62936d8a6ec2e0f8e035c145b2a3b57715fed9d7830179f330",
    "state": "active", "presence": "present"
  }
]
JSON
cat >"${builtin_exact_commands}" <<'JSON'
[
  {
    "path": ".github/workflows/builtin.yml",
    "command": "builtin",
    "statementSHA256": "678180a888bd9f84f7702545a4b1c886e6cb80b87320674314acf3b6dfa45cfa",
    "contextSHA256": "8a698faf1dcaeb62936d8a6ec2e0f8e035c145b2a3b57715fed9d7830179f330",
    "state": "active", "rationale": "Mutation: exact builtin eval row must remain categorically unusable."
  },
  {
    "path": ".github/workflows/builtin.yml",
    "command": "builtin",
    "statementSHA256": "c8b6ac1ebcc696243f7aa9ff405a3c7969f37d6a2657bca692646f68a3f06dcd",
    "contextSHA256": "8a698faf1dcaeb62936d8a6ec2e0f8e035c145b2a3b57715fed9d7830179f330",
    "state": "active", "rationale": "Mutation: nested command builtin eval row must remain categorically unusable."
  }
]
JSON
set +e
builtin_exact_output="$("${checker_binary}" \
  --scan-root "${builtin_exact_root}" \
  --presence-allowlist "${builtin_exact_presence}" \
  --allowlist "${empty_allowlist}" \
  --executable-allowlist "${empty_allowlist}" \
  --command-allowlist "${builtin_exact_commands}" \
  --action-allowlist "${empty_allowlist}" 2>&1)"
builtin_exact_status=$?
set -e
if [[ "${builtin_exact_status}" -eq 0 ]] ||
  ! grep -Fq -- "executes categorically forbidden command builtin" <<<"${builtin_exact_output}" ||
  ! grep -Fq -- "provider-capable command builtin is categorically unallowlistable" <<<"${builtin_exact_output}"; then
  echo "exact command authority authorized builtin execution wrapper" >&2
  printf '%s\n' "${builtin_exact_output}" >&2
  exit 1
fi

set +e
script_execution_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-script-execution.yml" 2>&1)"
script_execution_status=$?
set -e

if [[ "${script_execution_status}" -eq 0 ]]; then
  echo "expected unreviewed committed script execution to fail policy" >&2
  exit 1
fi
for expected in \
  "unallowlisted executable script ./scripts/unreviewed.sh" \
  "workflow executable path ../escape.sh is outside repository"; do
  if ! grep -Fq -- "${expected}" <<<"${script_execution_output}"; then
    echo "missing expected executable script diagnostic: ${expected}" >&2
    printf '%s\n' "${script_execution_output}" >&2
    exit 1
  fi
done

uses_allowlist="${tmp_dir}/uses-allowlist.json"
cat >"${uses_allowlist}" <<'JSON'
[
  {
    "path": "scripts/fixtures/public-workflow-policy/reject-provider-uses.yml",
    "secret": "DEPLOY_AUTH",
    "state": "active", "rationale": "An opaque alias must not hide authority granted to a provider action."
  }
]
JSON
set +e
uses_output="$("${checker}" \
  --allowlist "${uses_allowlist}" \
  "${fixtures}/reject-provider-uses.yml" 2>&1)"
uses_status=$?
set -e

if [[ "${uses_status}" -eq 0 ]]; then
  echo "expected provider action and reusable workflow references to fail policy" >&2
  exit 1
fi
for expected in \
  "uses unreviewed exact action digitalocean/action-doctl@v2" \
  "job provider-reusable-workflow uses forbidden reusable workflow job" \
  "uses unreviewed exact reusable workflow digitalocean/platform/.github/workflows/live-deploy.yml@main" \
  "public workflow references forbidden repository secret DEPLOY_AUTH" \
  "provider authority with secret DEPLOY_AUTH"; do
  if ! grep -Fq -- "${expected}" <<<"${uses_output}"; then
    echo "missing expected provider uses diagnostic: ${expected}" >&2
    printf '%s\n' "${uses_output}" >&2
    exit 1
  fi
done

set +e
unreviewed_uses_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-unreviewed-uses.yml" 2>&1)"
unreviewed_uses_status=$?
set -e

if [[ "${unreviewed_uses_status}" -eq 0 ]]; then
  echo "expected unreviewed action references to fail policy" >&2
  exit 1
fi
for expected in \
  "uses unreviewed exact action octocat/unknown-action@v1" \
  "uses unreviewed exact action ./.github/actions/not-reviewed" \
  "uses unreviewed exact action docker://alpine:3.20" \
  "uses unreviewed exact action digitalocean/experimental-deploy@v1" \
  'uses forbidden dynamic action ${{ vars.ACTION_REF }}' \
  "uses unreviewed exact action actions/checkout@v5" \
  "uses unreviewed exact action actions/setup-go@main" \
  "uses unreviewed exact action actions/upload-artifact@0123456789012345678901234567890123456789" \
  "job unknown-reusable-workflow uses forbidden reusable workflow job" \
  "uses unreviewed exact reusable workflow acme/platform/.github/workflows/deploy.yml@main"; do
  if ! grep -Fq -- "${expected}" <<<"${unreviewed_uses_output}"; then
    echo "missing expected unreviewed uses diagnostic: ${expected}" >&2
    printf '%s\n' "${unreviewed_uses_output}" >&2
    exit 1
  fi
done

set +e
guard_suffix_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-guard-suffix.yml" 2>&1)"
guard_suffix_status=$?
set -e

if [[ "${guard_suffix_status}" -eq 0 ]]; then
  echo "expected executable suffix after negative guard to fail policy" >&2
  exit 1
fi
if ! grep -Fq -- "executable provider CLI doctl" <<<"${guard_suffix_output}"; then
  echo "missing provider CLI diagnostic after negative guard suffix" >&2
  printf '%s\n' "${guard_suffix_output}" >&2
  exit 1
fi
if ! grep -Fq -- "job command-substitution executes forbidden provider authority: executable provider CLI doctl" <<<"${guard_suffix_output}"; then
  echo "command substitution inside a negative guard bypassed provider CLI detection" >&2
  printf '%s\n' "${guard_suffix_output}" >&2
  exit 1
fi

set +e
command_analysis_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-command-analysis.yml" 2>&1)"
command_analysis_status=$?
set -e

if [[ "${command_analysis_status}" -eq 0 ]]; then
  echo "expected shell command analysis bypasses to fail policy" >&2
  exit 1
fi
for job in \
  assignment-prefix \
  command-wrapper \
  exec-wrapper \
  env-wrapper \
  subshell \
  group \
  command-substitution; do
  if ! grep -Fq -- "job ${job} invokes unallowlisted executable script ./scripts/live.sh" <<<"${command_analysis_output}"; then
    echo "missing AST command diagnostic for ${job}" >&2
    printf '%s\n' "${command_analysis_output}" >&2
    exit 1
  fi
done
for job in dynamic-path wrapped-dynamic-command sudo-wrapper; do
  if ! grep -Fq -- "job ${job} uses forbidden dynamic command execution" <<<"${command_analysis_output}"; then
    echo "missing dynamic command diagnostic for ${job}" >&2
    printf '%s\n' "${command_analysis_output}" >&2
    exit 1
  fi
done

set +e
shell_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-shell.yml" 2>&1)"
shell_status=$?
set -e

if [[ "${shell_status}" -eq 0 ]]; then
  echo "expected custom shells and shell parse failures to fail policy" >&2
  exit 1
fi
# The GitHub expression below is an intentionally literal expected diagnostic.
# shellcheck disable=SC2016
for expected in \
  "job custom-shell uses forbidden custom shell python" \
  'job dynamic-shell uses forbidden dynamic shell ${{ vars.SHELL }}' \
  "job invalid-shell-program shell parse failed"; do
  if ! grep -Fq -- "${expected}" <<<"${shell_output}"; then
    echo "missing shell policy diagnostic: ${expected}" >&2
    printf '%s\n' "${shell_output}" >&2
    exit 1
  fi
done

set +e
deny_execution_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-deny-pattern-execution.yml" 2>&1)"
deny_execution_status=$?
set -e

if [[ "${deny_execution_status}" -eq 0 ]] || ! grep -Fq -- \
  "job execute-deny-value uses provider deny pattern outside a pure rejection guard" \
  <<<"${deny_execution_output}"; then
  echo "provider deny pattern escaped its parser-proven guard" >&2
  printf '%s\n' "${deny_execution_output}" >&2
  exit 1
fi

set +e
shell_inheritance_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-shell-inheritance.yml" 2>&1)"
shell_inheritance_status=$?
missing_shell_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-missing-shell.yml" 2>&1)"
missing_shell_status=$?
unsafe_commands="${tmp_dir}/unsafe-commands.json"
cat >"${unsafe_commands}" <<'JSON'
[
  {
    "path": "scripts/fixtures/public-workflow-policy/reject-unsafe-programs.yml",
    "command": "go",
    "statementSHA256": "0000000000000000000000000000000000000000000000000000000000000000",
    "contextSHA256": "0000000000000000000000000000000000000000000000000000000000000000",
    "state": "active", "rationale": "Mutation: prove an allowlisted command with the wrong subcommand remains rejected."
  }
]
JSON
unsafe_program_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  --command-allowlist "${unsafe_commands}" \
  "${fixtures}/reject-unsafe-programs.yml" 2>&1)"
unsafe_program_status=$?
set -e

if [[ "${shell_inheritance_status}" -eq 0 || "${missing_shell_status}" -eq 0 ]]; then
  echo "expected inherited, overridden, and missing shells to fail policy" >&2
  exit 1
fi
# The GitHub expression below is an intentionally literal expected diagnostic.
# shellcheck disable=SC2016
for expected in \
  'workflow scripts/fixtures/public-workflow-policy/reject-shell-inheritance.yml uses forbidden dynamic shell ${{ vars.DEFAULT_SHELL }}' \
  "job job-python uses forbidden custom shell python" \
  "job job-pwsh uses forbidden custom shell pwsh" \
  "job job-pwsh executes unreviewed exact statement containing invoke-restmethod" \
  "job job-custom uses forbidden custom shell fish" \
  "job step-override uses forbidden custom shell powershell"; do
  if ! grep -Fq -- "${expected}" <<<"${shell_inheritance_output}"; then
    echo "missing effective shell diagnostic: ${expected}" >&2
    printf '%s\n' "${shell_inheritance_output}" >&2
    exit 1
  fi
done
if ! grep -Fq -- "job implicit-platform-default does not declare an explicit Bash shell" <<<"${missing_shell_output}"; then
  echo "missing implicit platform shell diagnostic" >&2
  printf '%s\n' "${missing_shell_output}" >&2
  exit 1
fi

if [[ "${unsafe_program_status}" -eq 0 ]]; then
  echo "expected provider-capable and dynamic programs to fail policy" >&2
  exit 1
fi
for expected in \
  "job curl-endpoint executes categorically forbidden command curl" \
  "job wget-endpoint executes categorically forbidden command wget" \
  "job http-client executes categorically forbidden command http" \
  "job python-code executes categorically forbidden command python" \
  "job node-code executes categorically forbidden command node" \
  "job dynamic-interpreter-script executes categorically forbidden command python" \
  "job powershell-endpoint executes categorically forbidden command pwsh" \
  "job go-run executes categorically forbidden command go with argv [\"run\"" \
  "job npx-exec executes categorically forbidden command npx" \
  "job npm-exec executes categorically forbidden command npm" \
  "job docker-run executes categorically forbidden command docker" \
  "job docker-login executes categorically forbidden command docker" \
  "job docker-push executes categorically forbidden command docker" \
  "job terraform-plan executes categorically forbidden command terraform" \
  "job tofu-plan executes categorically forbidden command tofu" \
  "job pulumi-preview executes categorically forbidden command pulumi" \
  "job kubectl-get executes categorically forbidden command kubectl" \
  "job helm-list executes categorically forbidden command helm" \
  "job ansible-playbook executes categorically forbidden command ansible" \
  "job rclone-list executes categorically forbidden command rclone" \
  "job s3cmd-list executes categorically forbidden command s3cmd" \
  "job mc-list executes categorically forbidden command mc" \
  "job unknown-executable executes unreviewed exact statement containing mystery-tool" \
  "job sudo-wrapper uses forbidden dynamic command execution"; do
  if ! grep -Fq -- "${expected}" <<<"${unsafe_program_output}"; then
    echo "missing unsafe program diagnostic: ${expected}" >&2
    printf '%s\n' "${unsafe_program_output}" >&2
    exit 1
  fi
done

invalid_commands="${tmp_dir}/invalid-commands.json"
cat >"${invalid_commands}" <<'JSON'
[
  {
    "path": "/tmp/absolute.yml",
    "command": "go",
    "statementSHA256": "1111111111111111111111111111111111111111111111111111111111111111",
    "contextSHA256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "state": "active", "rationale": "Absolute paths must not grant command authority."
  },
  {
    "path": "../traversal.yml",
    "command": "go",
    "statementSHA256": "2222222222222222222222222222222222222222222222222222222222222222",
    "contextSHA256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "state": "active", "rationale": "Traversal must not grant command authority."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml",
    "command": "terraform",
    "statementSHA256": "3333333333333333333333333333333333333333333333333333333333333333",
    "contextSHA256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "state": "active", "rationale": "Known provider commands are forbidden even with a rationale."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml",
    "command": "go",
    "statementSHA256": "4444444444444444444444444444444444444444444444444444444444444444",
    "contextSHA256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "state": "active", "rationale": "Deliberately stale command capability mutation."
  },
  {
    "path": ".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml",
    "command": "go",
    "statementSHA256": "4444444444444444444444444444444444444444444444444444444444444444",
    "contextSHA256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "state": "active", "rationale": "Deliberate duplicate command capability mutation."
  }
]
JSON
set +e
invalid_commands_output="$("${checker}" \
  --scan-root "${pass_scan_root}" \
  --allowlist "${empty_allowlist}" \
  --command-allowlist "${invalid_commands}" \
  "${pass_negative_guard_workflow}" 2>&1)"
invalid_commands_status=$?
set -e
if [[ "${invalid_commands_status}" -eq 0 ]]; then
  echo "expected invalid command allowlist entries to fail policy" >&2
  exit 1
fi
if grep -Fq -- \
  "invalid command allowlist entry for .github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml" \
  <<<"${invalid_commands_output}"; then
  echo "semantic invalid-command probes did not reach command classification" >&2
  printf '%s\n' "${invalid_commands_output}" >&2
  exit 1
fi
for expected in \
  "command allowlist path /tmp/absolute.yml must be repository-relative" \
  "command allowlist path ../traversal.yml escapes the repository" \
  "provider-capable command terraform is categorically unallowlistable" \
  "duplicate command allowlist entry go sha256:4444444444444444444444444444444444444444444444444444444444444444" \
  "no trust group matches workflow"; do
  if ! grep -Fq -- "${expected}" <<<"${invalid_commands_output}"; then
    echo "missing invalid command allowlist diagnostic: ${expected}" >&2
    printf '%s\n' "${invalid_commands_output}" >&2
    exit 1
  fi
done

invalid_actions="${tmp_dir}/invalid-actions.json"
cat >"${invalid_actions}" <<'JSON'
[
  {"path":"/tmp/absolute.yml","uses":"actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5","nodeSHA256":"1111111111111111111111111111111111111111111111111111111111111111","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Absolute paths must not grant action authority."},
  {"path":"../traversal.yml","uses":"actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5","nodeSHA256":"2222222222222222222222222222222222222222222222222222222222222222","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Traversal must not grant action authority."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"digitalocean/action-doctl@0123456789012345678901234567890123456789","nodeSHA256":"3333333333333333333333333333333333333333333333333333333333333333","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Provider actions remain categorically forbidden."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"${{ vars.ACTION_REF }}","nodeSHA256":"4444444444444444444444444444444444444444444444444444444444444444","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Dynamic action references remain forbidden."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"actions/checkout@v4","nodeSHA256":"6666666666666666666666666666666666666666666666666666666666666666","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Mutable action tags remain forbidden in trust policy."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"actions/checkout@main","nodeSHA256":"7777777777777777777777777777777777777777777777777777777777777777","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Mutable action branches remain forbidden in trust policy."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5","nodeSHA256":"5555555555555555555555555555555555555555555555555555555555555555","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Deliberately stale exact action mutation."},
  {"path":".github/workflows/fixtures/public-workflow-policy/pass-negative-guard.yml","uses":"actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5","nodeSHA256":"5555555555555555555555555555555555555555555555555555555555555555","contextSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state": "active", "rationale":"Deliberate duplicate exact action mutation."}
]
JSON
set +e
invalid_actions_output="$("${checker}" \
  --scan-root "${pass_scan_root}" \
  --allowlist "${empty_allowlist}" \
  --action-allowlist "${invalid_actions}" \
  "${pass_negative_guard_workflow}" 2>&1)"
invalid_actions_status=$?
set -e
if [[ "${invalid_actions_status}" -eq 0 ]]; then
  echo "expected invalid action allowlist entries to fail policy" >&2
  exit 1
fi
if [[ "$(grep -Fc -- "invalid action allowlist entry" <<<"${invalid_actions_output}")" -lt 3 ]]; then
  echo "dynamic, tag, and branch action allowlist entries were not all rejected" >&2
  printf '%s\n' "${invalid_actions_output}" >&2
  exit 1
fi
for expected in \
  "action allowlist path /tmp/absolute.yml must be repository-relative" \
  "action allowlist path ../traversal.yml escapes the repository" \
  "provider action digitalocean/action-doctl@0123456789012345678901234567890123456789 is categorically unallowlistable" \
  "invalid action allowlist entry" \
  "duplicate action allowlist entry actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5" \
  "no trust group matches workflow"; do
  if ! grep -Fq -- "${expected}" <<<"${invalid_actions_output}"; then
    echo "missing invalid action allowlist diagnostic: ${expected}" >&2
    printf '%s\n' "${invalid_actions_output}" >&2
    exit 1
  fi
done

invalid_paths_allowlist="${tmp_dir}/invalid-paths-allowlist.json"
cat >"${invalid_paths_allowlist}" <<'JSON'
[
  {
    "path": "/tmp/absolute-workflow.yml",
    "secret": "PACKAGE_TOKEN",
    "state": "active", "rationale": "Absolute paths must never be accepted as workflow policy exceptions."
  },
  {
    "path": "../traversal-workflow.yml",
    "secret": "RELEASES_TOKEN",
    "state": "active", "rationale": "Parent traversal must never escape exact repository-relative matching."
  }
]
JSON
set +e
invalid_allowlist_output="$("${checker}" \
  --allowlist "${invalid_paths_allowlist}" \
  "${fixtures}/pass-negative-guard.yml" 2>&1)"
invalid_allowlist_status=$?
set -e

if [[ "${invalid_allowlist_status}" -eq 0 ]]; then
  echo "expected unconfined allowlist paths to fail policy" >&2
  exit 1
fi
for expected in \
  "allowlist path /tmp/absolute-workflow.yml must be repository-relative" \
  "allowlist path ../traversal-workflow.yml escapes the repository"; do
  if ! grep -Fq -- "${expected}" <<<"${invalid_allowlist_output}"; then
    echo "missing expected allowlist confinement diagnostic: ${expected}" >&2
    printf '%s\n' "${invalid_allowlist_output}" >&2
    exit 1
  fi
done

escape_scan_root="${tmp_dir}/symlink-root"
mkdir -p "${escape_scan_root}"
escape_workflow="${escape_scan_root}/symlink-escape.yml"
ln -s /dev/null "${escape_workflow}"
set +e
outside_output="$("${checker}" \
  --scan-root "${escape_scan_root}" \
  --allowlist "${empty_allowlist}" \
  /dev/null \
  "${escape_workflow}" 2>&1)"
outside_status=$?
set -e

if [[ "${outside_status}" -eq 0 ]]; then
  echo "expected outside and symlink-escaping workflow paths to fail policy" >&2
  exit 1
fi
for expected in \
  "workflow path /dev/null is outside repository" \
  "resolves outside repository"; do
  if ! grep -Fq -- "${expected}" <<<"${outside_output}"; then
    echo "missing expected workflow confinement diagnostic: ${expected}" >&2
    printf '%s\n' "${outside_output}" >&2
    exit 1
  fi
done

secret_syntax_allowlist="${tmp_dir}/secret-syntax-allowlist.json"
cat >"${secret_syntax_allowlist}" <<'JSON'
[
  {
    "path": "scripts/fixtures/public-workflow-policy/reject-secret-syntax.yml",
    "secret": "DEPLOY_AUTH",
    "state": "active", "rationale": "A global opaque alias must remain visible to provider-authority analysis."
  },
  {
    "path": "scripts/fixtures/public-workflow-policy/reject-secret-syntax.yml",
    "secret": "DIGITALOCEAN_TOKEN",
    "state": "active", "rationale": "Known provider credentials remain forbidden regardless of syntax or rationale."
  }
]
JSON
set +e
secret_syntax_output="$("${checker}" \
  --allowlist "${secret_syntax_allowlist}" \
  "${fixtures}/reject-secret-syntax.yml" 2>&1)"
secret_syntax_status=$?
set -e

if [[ "${secret_syntax_status}" -eq 0 ]]; then
  echo "expected bracket and global secret references to fail policy" >&2
  exit 1
fi
for expected in \
  "workflow scripts/fixtures/public-workflow-policy/reject-secret-syntax.yml references known cloud secret DIGITALOCEAN_TOKEN" \
  "public workflow references forbidden repository secret DEPLOY_AUTH" \
  "provider authority with secret DEPLOY_AUTH" \
  "public workflow references forbidden repository secret UNREVIEWED_TOKEN"; do
  if ! grep -Fq -- "${expected}" <<<"${secret_syntax_output}"; then
    echo "missing expected secret syntax diagnostic: ${expected}" >&2
    printf '%s\n' "${secret_syntax_output}" >&2
    exit 1
  fi
done
if grep -Fq -- "stale allowlist entry" <<<"${secret_syntax_output}"; then
  echo "bracket secret references were not matched to their exact allowlist entries" >&2
  printf '%s\n' "${secret_syntax_output}" >&2
  exit 1
fi

set +e
legacy_github_token_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-legacy-github-token.yml" 2>&1)"
legacy_github_token_status=$?
set -e
if [[ "${legacy_github_token_status}" -eq 0 ]] || ! grep -Fq -- \
  "public workflow references forbidden repository secret GITHUB_TOKEN" \
  <<<"${legacy_github_token_output}"; then
  echo "legacy secrets.GITHUB_TOKEN selector bypassed public workflow policy" >&2
  printf '%s\n' "${legacy_github_token_output}" >&2
  exit 1
fi

set +e
runner_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-runners.yml" 2>&1)"
runner_status=$?
set -e

if [[ "${runner_status}" -eq 0 ]]; then
  echo "expected non-GitHub-hosted runner selectors to fail policy" >&2
  exit 1
fi
# The GitHub expression below is an intentionally literal expected diagnostic.
# shellcheck disable=SC2016
for expected in \
  "runner selector private-linux is not recognized as GitHub-hosted" \
  "runner selector linux is not recognized as GitHub-hosted" \
  'forbidden dynamic runner selector ${{ vars.RUNNER_LABEL }}'; do
  if ! grep -Fq -- "${expected}" <<<"${runner_output}"; then
    echo "missing expected runner policy diagnostic: ${expected}" >&2
    printf '%s\n' "${runner_output}" >&2
    exit 1
  fi
done

set +e
global_output="$("${checker}" \
  --allowlist "${empty_allowlist}" \
  "${fixtures}/reject-global.yml" 2>&1)"
global_status=$?
set -e

if [[ "${global_status}" -eq 0 ]]; then
  echo "expected global policy markers to fail even when job permissions override them" >&2
  exit 1
fi
for expected in \
  "id-token: write" \
  "known cloud credential variable AWS_ACCESS_KEY_ID" \
  "known cloud secret SPACES_ACCESS_KEY_ID" \
  "known cloud secret SPACES_SECRET_ACCESS_KEY" \
  "known cloud secret DIGITALOCEAN_SPACES_ACCESS_KEY_ID" \
  "known cloud secret DIGITALOCEAN_SPACES_SECRET_ACCESS_KEY" \
  "known cloud secret DO_SPACES_ACCESS_KEY_ID" \
  "known cloud secret DO_SPACES_SECRET_ACCESS_KEY"; do
  if ! grep -Fq -- "${expected}" <<<"${global_output}"; then
    echo "missing expected global policy diagnostic: ${expected}" >&2
    printf '%s\n' "${global_output}" >&2
    exit 1
  fi
done

echo "public workflow policy fixtures passed"
