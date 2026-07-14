#!/usr/bin/env bash
set -euo pipefail

repo="${1:?usage: verify-public-workflow-branch-protection.sh OWNER/REPO [BRANCH]}"
branch="${2:-main}"
required_check="Public Workflow Policy / policy"
required_app_id=15368
fixture_mode=false
if [[ "${PUBLIC_WORKFLOW_PROTECTION_FIXTURE_MODE:-}" == "1" ]]; then
  if [[ "${repo}" != "example/repo" || -z "${PUBLIC_WORKFLOW_CLASSIC_JSON_FILE:-}" || -z "${PUBLIC_WORKFLOW_RULESET_JSON_FILE:-}" || -z "${PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE:-}" || -z "${PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE:-}" ]]; then
    echo "fixture mode is restricted to complete example/repo test inputs" >&2
    exit 2
  fi
  fixture_mode=true
fi

if [[ "${fixture_mode}" == true ]]; then
  classic="$(<"${PUBLIC_WORKFLOW_CLASSIC_JSON_FILE}")"
else
  classic="$(gh api "repos/${repo}/branches/${branch}/protection" 2>/dev/null || true)"
fi
branch_verified=false
if jq -e --arg check "${required_check}" --argjson app_id "${required_app_id}" '
  .enforce_admins.enabled == true
  and .required_status_checks.strict == true
  and .required_pull_request_reviews.required_approving_review_count >= 1
  and .required_pull_request_reviews.dismiss_stale_reviews == true
  and ((.required_pull_request_reviews.bypass_pull_request_allowances.users // []) | length == 0)
  and ((.required_pull_request_reviews.bypass_pull_request_allowances.teams // []) | length == 0)
  and ((.required_pull_request_reviews.bypass_pull_request_allowances.apps // []) | length == 0)
  and (any(.required_status_checks.checks[]?; .context == $check and .app_id == $app_id))
  and (.restrictions == null or ((.restrictions.users // []) | length == 0)
       and ((.restrictions.teams // []) | length == 0)
       and ((.restrictions.apps // []) | length == 0))
  and (.allow_force_pushes.enabled // false) == false
  and (.allow_deletions.enabled // false) == false
' <<<"${classic}" >/dev/null 2>&1; then
  branch_verified=true
fi

ruleset_documents=()
tag_ruleset_documents=()
if [[ "${fixture_mode}" == true ]]; then
  ruleset_documents+=("$(<"${PUBLIC_WORKFLOW_RULESET_JSON_FILE}")")
  tag_ruleset_documents+=("$(<"${PUBLIC_WORKFLOW_TAG_RULESET_JSON_FILE}")")
  repository="$(<"${PUBLIC_WORKFLOW_REPOSITORY_JSON_FILE}")"
else
  repository="$(gh api "repos/${repo}")"
  rulesets="$(gh api --paginate "repos/${repo}/rulesets" 2>/dev/null | jq -s 'add // []')"
  while IFS= read -r ruleset_id; do
    ruleset="$(gh api "repos/${repo}/rulesets/${ruleset_id}")"
    ruleset_documents+=("${ruleset}")
    tag_ruleset_documents+=("${ruleset}")
  done < <(jq -r '.[] | select(.enforcement == "active") | .id' <<<"${rulesets}")
fi
default_branch="$(jq -er '.default_branch | select(type == "string" and length > 0)' <<<"${repository}")"
if [[ "${branch_verified}" != true ]]; then
  for ruleset in "${ruleset_documents[@]}"; do
    if jq -e --arg branch "refs/heads/${branch}" --arg default_branch "refs/heads/${default_branch}" --arg check "${required_check}" --argjson app_id "${required_app_id}" '
      .target == "branch"
      and .enforcement == "active"
      and ((.bypass_actors // []) | length == 0)
      and ((.conditions.ref_name.include // []) | any(. == $branch or (. == "~DEFAULT_BRANCH" and $branch == $default_branch)))
      and ((.conditions.ref_name.exclude // []) | length == 0)
      and (any(.rules[]?; .type == "pull_request"
        and .parameters.required_approving_review_count >= 1
        and .parameters.dismiss_stale_reviews_on_push == true))
      and (any(.rules[]?; .type == "required_status_checks"
        and .parameters.strict_required_status_checks_policy == true
        and any(.parameters.required_status_checks[]?; .context == $check and .integration_id == $app_id)))
      and (any(.rules[]?; .type == "non_fast_forward"))
      and (any(.rules[]?; .type == "deletion"))
    ' <<<"${ruleset}" >/dev/null; then
      branch_verified=true
      break
    fi
  done
fi

tag_verified=false
for ruleset in "${tag_ruleset_documents[@]}"; do
  if jq -e '
    .target == "tag"
    and .enforcement == "active"
    and (.conditions.ref_name.include == ["refs/tags/v*"])
    and (.conditions.ref_name.exclude == [])
    and ((.bypass_actors // []) | length == 1)
    and (.bypass_actors[0] | has("actor_id") and .actor_id == null
      and .actor_type == "OrganizationAdmin" and .bypass_mode == "always")
    and (any(.rules[]?; .type == "creation"))
    and (any(.rules[]?; .type == "update"))
    and (any(.rules[]?; .type == "deletion"))
  ' <<<"${ruleset}" >/dev/null; then
    tag_verified=true
    break
  fi
done

if [[ "${branch_verified}" == true && "${tag_verified}" == true ]]; then
  echo "branch and release-tag protection verified for ${repo}:${branch} (${required_check}, GitHub Actions app ${required_app_id})"
  exit 0
fi

echo "repository governance is incomplete for ${repo}:${branch}; require protected branch policy and active refs/tags/v* creation/update/deletion rules with the sole OrganizationAdmin always bypass" >&2
exit 1
