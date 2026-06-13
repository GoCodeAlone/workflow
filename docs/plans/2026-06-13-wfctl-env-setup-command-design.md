# wfctl env setup Command Design

## Summary

`wfctl secrets setup --manifest` now configures a mixed set of environment
inputs: encrypted secrets, non-secret provider variables, provider environment
targets, and optional config name mappings. The behavior is correct, but the
command name is misleading. This design promotes the unified flow to
`wfctl env setup` while keeping existing `wfctl secrets setup` and
`wfctl vars setup` entry points as compatibility aliases.

## Global Design Guidance

Sources: `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`,
`docs/plans/2026-06-12-unified-env-setup-design.md`.

| guidance | design response |
|---|---|
| New wfctl commands update `cmd/wfctl/`, docs, and tests. | Add `wfctl env` dispatch, command help, tests, and `docs/WFCTL.md` updates. |
| Core keeps bootstrap-critical CLI behavior; provider-specific behavior stays behind providers. | Reuse the existing manifest setup engine and provider contracts; do not add provider APIs in this change. |
| Keep command behavior compatible unless intentionally migrated. | Existing `secrets setup` and `vars setup` commands continue to work and route into the same implementation. |

## Architecture

Add `wfctl env` as the primary command group for environment input setup:

- `wfctl env setup` invokes the existing manifest-backed unified setup path.
- `wfctl env status` is out of scope for this slice; the setup table already
  shows current status as part of setup.
- `wfctl secrets setup` remains valid. When it uses manifest-backed setup, help
  and optional notices should say that "secrets setup" is now also available as
  `wfctl env setup`; do not use the phrase "manifest setup" in user-facing
  migration text.
- `wfctl vars setup` remains valid and should point users at
  `wfctl env setup --kind var` or equivalent once kind filtering exists.

The unified setup model must keep the secret/variable distinction visible:

- rows include a kind label, at minimum `secret` or `var`;
- secret prompts stay masked and values are never printed;
- variable prompts are not treated as encrypted values, but values are still not
  echoed into logs by default;
- status checks and writes continue to use mapped storage names.

## CLI Shape

Primary examples:

```sh
wfctl env setup --manifest wfctl.yaml --config 'infra/*.yaml,deploy.yaml'
wfctl env setup --manifest wfctl.yaml --kind secret
wfctl env setup --manifest wfctl.yaml --kind var
wfctl env setup --manifest wfctl.yaml --name-map NAMECHEAP_API_KEY=GCA_NC_API_KEY --write-config
```

Compatibility examples:

```sh
wfctl secrets setup --manifest wfctl.yaml --config 'infra/*.yaml'
wfctl vars setup --plugin workflow-plugin-cloudflare --from-env
```

Compatibility commands should not be removed before Workflow 1.0. The new help
surface should clearly describe `env` as "environment input setup" rather than
only provider-side environments such as GitHub Actions Environments.

Compatibility aliases should avoid noisy runtime warnings by default so existing
CI does not change output unexpectedly. Help text and docs can advertise the new
primary command; a future deprecation warning can be added only with an explicit
migration policy.

## Security Review

This is a command-surface change over an existing secret/variable engine. The
main security risk is blurring encrypted secrets and visible variables. The UI,
help text, JSON/status output, and tests must keep `kind` explicit. Compatibility
aliases must not downgrade masking or leak values. GitHub org visibility remains
`private` by default from the previous unified setup change.

## Infrastructure Impact

No new cloud resources or provider APIs are introduced. Running the command can
still create or update provider secrets, variables, and provider environments
through existing behavior. Release impact is limited to the `wfctl` binary and
docs.

## Multi-Component Validation

Validation should exercise:

- `wfctl env -h` and `wfctl env setup -h`;
- representative `wfctl env setup --manifest ... --from-env --non-interactive`
  using local test fixtures;
- compatibility `wfctl secrets setup --manifest ...` still reaching the same
  behavior;
- compatibility `wfctl vars setup ...` still reaching variable setup;
- docs examples reflecting the primary command.

## Assumptions

- Users understand `env` as "application environment setup" when help text says
  so explicitly.
- The existing manifest-backed setup engine is the correct implementation
  center; this change should not fork setup behavior.
- Keeping aliases avoids breaking existing automation while allowing docs and
  recommendations to move to `wfctl env setup`.

## Rollback

Revert the `wfctl env` command registration, docs, and tests. Existing
`wfctl secrets setup` and `wfctl vars setup` behavior remains the fallback
surface because the underlying setup engine is not removed.
