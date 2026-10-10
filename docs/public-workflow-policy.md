# Public workflow policy

Public workflow changes are checked by `.github/workflows/public-workflow-policy.yml`.
On the normal trusted-scan path, the `pull_request_target` job executes only
SHA-pinned actions and the analyzer, wrapper, module, and trust manifests from
the trusted base checkout. Candidate data is fetched without credentials from
a validated `https://github.com` repository and exact 40-hex commit, exported
with `git archive --worktree-attributes` so candidate attributes cannot hide
files, and rejected if it contains symlinks. No candidate Git worktree is
checked out, credentials are not persisted, and candidate actions or code
outside the exact exception below are never executed.
The `pull_request_target` and `push` triggers are filtered to `main`, and
candidate fetch rechecks the runtime event name, base ref, and full event ref
before any policy toolchain or scanner runs. Pull requests targeting other
branches do not trigger this workflow; a mismatched runtime context exits
before policy setup.

Outside the one-time historical bootstrap described below, the sole exact
pre-staged adoption exception is fail-closed: candidate wrapper, analyzer, and
module code may execute only after the already-active trusted adoption guard
authorizes the exact active-to-staged authority delta and complete wrapper
digest replacement, plus complete mutation-harness digest replacement when
that fixed harness changes in the staged bundle. If that guard fails, candidate
policy code remains inert.
The workflow reaches that guard only through the executable, hash-inventoried
trusted launcher at
`.github/workflows/policytool/adoptionguard/run.sh`; the launcher locates its
own trusted policytool directory and forces `GOWORK=off` and
`GOFLAGS=-mod=readonly`. Candidate code never authorizes itself.
Both trusted and authorized-candidate setup steps pin Go 1.27.1 with caching
disabled. The isolated policy module retains its Go 1.26.5 minimum; that module
floor is not the compiler selected by the workflow.
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

The public workflow has a fail-closed adoption bridge for the second step. It
always runs the trusted default-branch scanner first. A successful trusted scan
ends policy evaluation without touching candidate policy code. Only when that
scan fails may the already-active, hash-inventoried adoption guard inspect the
two inert repository trees. The guard requires byte-identical authority
manifests, a realized active base inventory, a realized staged candidate
inventory, an unchanged active public workflow and guard, unchanged trust
inputs, and an exact tree delta limited to active-to-staged authority files plus
the executable allowlist. Every existing policy-wrapper row must retain its
key, order, cardinality, state, context, and rationale while changing exactly from the
active wrapper digest to the staged wrapper digest.
Only `scripts/test-check-public-workflow-policy.sh` may also change executable
pins, and only when its active and staged bundle digests differ. That change
requires at least one existing harness row and updates every existing active
and unused staged harness row exactly from the active digest to the staged
digest, preserving order, membership, cardinality, and metadata. An unchanged
harness forbids harness pin updates; every other executable digest remains
unchanged. This does not authorize harness execution during candidate scanning.
Every shared base/candidate path must also retain its executable-mode identity,
even when its content digest is staged to change. The directly invoked policy
wrapper must be executable in both trees; content-plus-mode drift, mode-only
drift, and a non-executable wrapper all fail closed.

The guard emits its candidate-policy authorization output only after all checks
pass. The pinned candidate `actions/setup-go` step and candidate wrapper are
conditional on that output. A malformed manifest, partial or mixed realization,
symlink, unrelated file, workflow/trust-input edit, executable metadata change,
secret, OIDC, actor, self-hosted runner, provider credential, or live-provider
command fails without emitting candidate invocation authority. The original
pre-policy-base bootstrap remains the sole exception because no trusted guard
exists at that historical base.

This bridge does not abandon or replace an already-staged authority bundle.
Once staged, those exact bytes must be adopted and promoted. A later governed
policy correction will define the narrower case for correcting an unused
staged executable digest: that transition is inert until its own
stage/adopt/promote sequence completes, may not change membership or metadata,
and cannot execute candidate policy while only staged. Until then, a bad staged
digest remains a failure that must complete the reviewed correction sequence;
it is not an implicit permission to rewrite staged authority.

The analyzer rejects a bundle that is staged and adopted in one pull request,
old implementation files after adoption or promotion, missing or extra files,
hash mismatches, symlinks, and non-regular authority paths. The manifest uses
strict JSON, exactly one active bundle, at most one staged bundle, and sorted,
unique, repository-confined file rows. The one-time exact bootstrap permits
only one fully realized active bundle.

The initial bridge preserves the original mutation-harness bytes and executable
mode. Harness lifecycle isolation and explicit fixture event-context repairs
belong to a later separately staged, adopted, and promoted policy correction;
the bridge does not install those repairs. Its actual-chain tests retain the
immutable historical 35-file bootstrap inventory, then exercise the installed
38-file guard with a benign harness byte change and complete real executable
row replacements. They run the real original wrapper, trusted guard, and
authorized candidate self-scan for both pull-request and push contexts, without
executing the mutation harness or requiring Git history.

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
data and must match an already-authorized bundle transition. The sole
pre-staged adoption exception permits the candidate wrapper, analyzer, and
module to execute only after the active trusted guard authorizes the exact
active-to-staged authority delta and complete wrapper digest replacement, plus
complete fixed-harness digest replacement only when its staged digest changes.
Candidate tests, fixtures, the branch-protection verifier, and all unauthorized
candidate code remain unconditionally inert. Candidate code never authorizes
itself. A candidate checker replaced with a no-op and a same-pull-request live
workflow both remain rejected by the base analyzer.

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
