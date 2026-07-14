# Public workflow policy

Public workflow changes are checked by `.github/workflows/public-workflow-policy.yml`.
The `pull_request_target` job executes only SHA-pinned actions and the analyzer,
wrapper, module, and trust manifests from the trusted base checkout. Candidate
data is fetched without credentials from a validated `https://github.com`
repository and exact 40-hex commit, exported with
`git archive --worktree-attributes` so candidate attributes cannot hide files,
and rejected if it contains symlinks. No candidate Git worktree is checked out,
credentials are not persisted, and candidate actions, scripts, Go files,
modules, and trust
manifests are never executed.
The job has only `contents: read`, uses GitHub-hosted runners, and receives no
cloud credentials or OIDC authority.
Every public workflow, regardless of trigger or call graph, rejects every named
repository or environment secret, including the legacy `secrets.GITHUB_TOKEN`
spelling. Only the automatic repository-scoped `github.token` context is
accepted. Known cloud credentials remain categorically forbidden. Job-level
`secrets: inherit`, mapped inherited-secret values, and dynamic secret selectors
are rejected everywhere. Release publication uses only the automatic
repository-scoped GitHub token.

Release integrity separately requires an active repository tag ruleset with
target `tag`, exact include `refs/tags/v*`, no excludes,
creation/update/deletion rules, and exactly one always-on `OrganizationAdmin`
bypass with no actor ID. The verifier trusts this behavior rather than any mutable ruleset name or
repository-local ID. The release job also fetches the fixed `main` ref and
rejects a tag commit that is not already contained in protected `main`.

Registry, IDE, scenario, and Homebrew synchronization is consumer-owned.
Consumers poll releases or run their own scheduled/manual sync. Workflow release
jobs never hold cross-repository dispatch credentials or publish callbacks.

Workflow authority changes use three pull requests. The presence manifest is
the canonical inventory: `present` groups bind a workflow path to its complete
context digest, while `absent` groups are explicit tombstones with no digest.
Command, action, secret, and executable authority is grouped by workflow path,
context digest, and lifecycle state so one workflow cannot consume another
workflow's approved script hash.

1. Add one `staged` trust context group for the future complete workflow digest
   while retaining the current `active` group. The current workflow selects the
   active group; the staged group is tolerated only as transition data.
2. After that trust-only pull request merges, submit the workflow change. The
   trusted base selects the staged group matching the candidate workflow while
   tolerating the old active group.
3. After the workflow merges, submit a trust-only cleanup that removes the old
   group and promotes the new group from `staged` to `active`.

The policy implementation has a separate three-pull-request authority bundle
lifecycle in `.github/public-workflow-authority.json`. A bundle hashes the
wrapper, analyzer source/module/tests, mutation harness and fixtures, and the
operator branch-protection verifier. The authority manifest excludes itself
and excludes the five workflow trust manifests, which are validated separately
as transition data.

1. Retain the realized `active` bundle and add exactly one `staged` bundle for
   the future implementation hashes. The candidate must still realize the
   active bundle.
2. After staging merges, change the implementation files so the candidate
   realizes the already-trusted staged bundle. The manifest remains unchanged.
3. After adoption merges, promote the realized staged bundle to the sole
   active bundle.

The analyzer rejects a bundle that is staged and adopted in one pull request,
old implementation files after adoption or promotion, missing or extra files,
hash mismatches, symlinks, and non-regular authority paths. The manifest uses
strict JSON, exactly one active bundle, at most one staged bundle, and sorted,
unique, repository-confined file rows. The one-time exact bootstrap permits
only one fully realized active bundle.

The same sequence covers all changes:

- Modify: retain the active `present` digest and stage the replacement
  `present` digest with all of its matching authority entries.
- Add: retain an active `absent` tombstone and stage the new `present` digest
  with its authority entries.
- Delete: retain the active `present` digest and stage an `absent` tombstone;
  the cleanup promotes the tombstone and removes the obsolete authority.

Selected executable entries are hash-checked and must be referenced by their
own workflow. Staged executable entries for an unselected future context are
tolerated only during this transition and become mandatory when that context
is selected.

The base manifests intentionally reject workflow additions, removals, or edits
made in the same pull request as their trust changes. Candidate trust manifests
are parsed only as data and must match the stage/adopt/promote transition table;
candidate-new staged rows never authorize that candidate workflow. Candidate
checker, analyzer, test, fixture, or verifier files are likewise inventoried as
data and must match an already-authorized bundle transition. They are never
executed by the trusted-base job. A candidate checker replaced with a no-op and
a same-pull-request live workflow both remain rejected by the base analyzer.

The one-time bootstrap recognizes only pre-policy base commit
`9c364dd4e6dad83808f8a87c1ba990d0132f0372`. If and only if a push reports that
exact `github.event.before` and the trusted checkout lacks the policy files, the
newly merged `main` hash-verified, readonly wrapper self-validates the merged
data. This exception runs only on that exact push; pull-request head data is
never selected as an executable policy root.
Zero, adjacent, unknown, or later missing-policy bases fail closed. If `main`
moves before bootstrap merges, rebase and update this exact SHA through review;
never replace it with a generic missing-policy skip. After bootstrap,
subsequent `.github/workflows` changes use trusted prior-revision policy.
Because that exact base has no public policy workflow or trust manifests, the
initial bootstrap pull request finalizes all active trust atomically. The
required status check is installed immediately after the bootstrap merge; it
is necessarily absent during bootstrap because no base workflow produces it.
This exception does not apply to any later workflow-authority change.

The same stable `Public Workflow Policy / policy` check also runs on pushes to
`main`, comparing `github.event.before` as trusted policy authority with the new
commit as candidate data. Task completion remains contingent on repository
branch protection: changes must use pull requests, require at least one
approval with stale approvals dismissed, require this exact status check from
the GitHub Actions app, block direct pushes, enforce administrators, and permit
no bypass actors. The producer binding is GitHub Actions app slug
`github-actions`, app/integration ID `15368`; a legacy name-only context is not
sufficient.

For classic branch protection, preserve the repository's other required checks
while provisioning this exact producer-bound check. For example:

```bash
repo=GoCodeAlone/workflow
branch=main
context='Public Workflow Policy / policy'
gh api "repos/${repo}/branches/${branch}/protection/required_status_checks" |
  jq --arg context "${context}" --argjson app_id 15368 '
    .strict = true
    | .checks = ([.checks[]? | select(.context != $context)]
      + [{context: $context, app_id: $app_id}])
    | {strict, contexts: (.contexts // []), checks}
  ' |
  gh api --method PATCH \
    "repos/${repo}/branches/${branch}/protection/required_status_checks" \
    --input -
```

For a repository ruleset, the update payload's `required_status_checks` rule
must contain the producer ID as well; submit the full existing ruleset update
payload with rules including:

```json
[
{
  "type": "required_status_checks",
  "parameters": {
    "strict_required_status_checks_policy": true,
    "required_status_checks": [
      {
        "context": "Public Workflow Policy / policy",
        "integration_id": 15368
      }
    ]
  }
},
{"type": "non_fast_forward"},
{"type": "deletion"}
]
```

```bash
gh api --method PUT \
  "repos/GoCodeAlone/workflow/rulesets/RULESET_ID" \
  --input ruleset-update.json
```

Confirm the observed check-run producer, then verify the configured branch or
ruleset:

```bash
gh api repos/GoCodeAlone/workflow/commits/main/check-runs \
  --jq '.check_runs[] | select(.name == "Public Workflow Policy / policy") | {name, app: {slug: .app.slug, id: .app.id}}'

./.github/workflows/scripts/verify-public-workflow-branch-protection.sh GoCodeAlone/workflow main
```

This script is read-only. It verifies classic branch protection or an active
ruleset, requires strict freshness and exact producer ID `15368`, and never
changes repository settings. An applicable ruleset must also contain both
`non_fast_forward` and `deletion` rules. The verifier reads repository metadata
and accepts `~DEFAULT_BRANCH` only when the requested branch equals the
repository's actual `default_branch`; non-default branches require their exact
`refs/heads/<branch>` selector. The same invocation also requires the exact
release-tag ruleset described above. Run this combined check as an
administrator/operator release prerequisite: GitHub intentionally hides
ruleset bypass actors from the read-only `github.token`, so the public policy
workflow cannot prove this privileged governance state.
