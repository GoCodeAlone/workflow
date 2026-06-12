# Unified Environment Setup Design

## Summary

Workflow now has separate setup paths for sensitive secrets and non-sensitive
provider variables. That split is technically correct but operationally awkward:
DNS and infrastructure plugins often need both, and users should not have to run
two setup commands or know which values belong in GitHub Actions secrets versus
GitHub Actions variables.

This design introduces a unified environment setup model for plugin and app
configuration inputs. Each input is still typed as secret or variable, but one
setup flow can discover, status-check, prompt, and write both kinds. The existing
`wfctl secrets setup` and `wfctl vars setup` commands remain as compatibility
wrappers while the unified model becomes the internal path.

## Global Design Guidance

Sources: `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`,
`docs/plans/2026-06-08-provider-env-secrets-design.md`.

| guidance | design response |
|---|---|
| Core keeps shared contracts; provider-specific behavior stays behind provider implementations. | Keep the shared input model in `cmd/wfctl` and `secrets`; keep GitHub visibility and variable APIs in the GitHub provider. |
| Update docs/tests with CLI/config behavior changes. | Add focused wfctl tests, provider-default tests, and docs for unified setup, vars, and name mapping. |
| Prefer existing libraries and repo patterns. | Reuse `gopkg.in/yaml.v3`, existing manifest discovery, `VariableProvider`, `SecretsProvider`, and table prompt flows. |
| Use clean worktrees for broad work. | Implement in `.worktrees/unified-env-setup` from `origin/main`. |

## Architecture

Add a unified setup input model:

- logical name: the plugin/app contract name, such as `NAMECHEAP_API_KEY`;
- storage name: the provider key to read/write, such as `GCA_NC_API_KEY`;
- kind: `secret` or `var`;
- sensitivity: prompt masking behavior;
- source list: plugin manifest and config files that introduced the input;
- allowed targets: provider + scope policy from plugin manifests;
- store hint: existing app config store routing where present.

`required_secrets[]` remains the canonical plugin manifest field for sensitive
inputs. `required_config[]` remains the canonical plugin manifest field for
non-sensitive inputs; docs may call these "required vars" but the schema should
not grow a second canonical `required_vars[]` field in this change. App config
metadata continues to use `secrets.entries`, `vars.entries`,
`variables.entries`, and `config.provider` schema entries.

The unified path discovers both kinds, presents one status matrix, and writes
each selected input through the provider interface that matches its kind:

- secrets use `SecretsProvider.Set` and metadata checks;
- variables use `secrets.VariableProvider.SetVariable` and `CheckVariable`.

`wfctl secrets setup --manifest` becomes the first unified entry point because
it already discovers plugins and app config references. `wfctl vars setup` keeps
working and reuses the same name-mapping helpers for non-secret-only flows.

## Name Mapping

Users may store an input under a custom provider key with
`--name-map LOGICAL=STORED`. For example:

```sh
wfctl secrets setup --manifest wfctl.yaml --name-map NAMECHEAP_API_KEY=GCA_NC_API_KEY
```

The setup path must apply mapping before provider status checks and writes. This
prevents the race where `wfctl` checks `NAMECHEAP_API_KEY`, sees it missing,
then writes `GCA_NC_API_KEY` anyway.

When `--write-config` is supplied, `wfctl` updates matching `${LOGICAL}`
references in the selected config files to `${STORED}` after provider writes
succeed. YAML editing uses `yaml.v3` node traversal and only changes scalar env
references. If no references match, the command reports that no config rewrite
was needed. Config rewriting is explicit so setup cannot silently churn YAML.

## Provider Defaults

Provider-owned defaults must be least-privilege:

- GitHub org-scope secrets and variables default to `private` visibility, not
  `all`.
- GitHub still accepts explicit `--visibility all|private|selected`.
- `selected` remains rejected unless selected repository IDs are supplied by a
  supported command path; exposing selected-repo selection is out of this PR.
- Vault, AWS Secrets Manager, file, keychain, and process env providers do not
  have a broad organization visibility concept. This change documents that their
  access defaults come from provider configuration and IAM/token/file ACLs.

The GitHub provider remains the only provider in this repo that implements
non-secret variables today. Other providers keep returning unsupported for vars
until their own provider implementations add variable support.

## Downstream Plugin Classification

Infrastructure/DNS provider plugins should declare non-sensitive operational
values as `required_config[]` and sensitive credentials as `required_secrets[]`.

Initial cascade:

- Namecheap: `NAMECHEAP_API_KEY` secret; `NAMECHEAP_API_USER` and
  `NAMECHEAP_CLIENT_IP` vars.
- Cloudflare: `CLOUDFLARE_API_TOKEN` secret; `CLOUDFLARE_ACCOUNT_ID` var.
- Hover: `HOVER_PASSWORD` and `HOVER_TOTP_SECRET` secrets;
  `HOVER_USERNAME` var.

The workflow core PR ships the shared setup behavior first. After it is merged
and released, the cascade continues with separate plugin PRs and releases for
the affected provider plugins, then app config/docs updates where those plugins
are consumed. This split keeps the core CLI rollback independent from plugin
manifest releases while still treating the downstream cascade as part of the
overall objective.

## Security Review

Secret values stay masked and never appear in status tables, logs, audit rows,
or config rewrites. Non-secret variables are prompted unmasked but should not be
printed with values. Mapping affects names only, never values. GitHub org
default visibility changes from broad `all` to `private`, reducing blast radius.

## Infrastructure Impact

This changes provider metadata writes only. GitHub org-level setup writes may
create or update organization secrets/variables with `private` visibility by
default. No cloud resources, DNS records, databases, or migrations are changed.
Downstream plugin releases update manifests and documentation.

## Multi-Component Validation

Validation covers:

- unified discovery of plugin secrets and required config;
- secret versus variable provider calls;
- mapped names used for status checks and writes;
- YAML env reference rewrite when `--write-config` is explicit;
- GitHub org visibility default for secrets and variables;
- downstream plugin manifest validation and representative `wfctl` discovery.
- follow-up plugin/app PR verification against released workflow core before
  claiming the ecosystem update complete.

## Assumptions

- Provider APIs identify secrets and variables by name only; renaming means
  write-new-name, not server-side rename.
- Existing apps can continue using old env names until they opt into
  `--name-map --write-config`.
- `required_config[]` is sufficiently established to keep as the canonical
  manifest field for non-secret variables.
- GitHub `private` visibility is the best least-privilege org default supported
  by the current API.

## Rollback

Revert the workflow core PR to restore split setup behavior and prior GitHub
visibility defaults. Revert downstream plugin manifest PRs if a released plugin
expects the old all-secret classification. Custom name mapping is opt-in and
only mutates app YAML when `--write-config` is supplied, so rollback for a
rewritten app config is a normal git revert.

## Deferred

- A first-class `wfctl env setup` command name. The compatibility command path
  can ship first through `wfctl secrets setup --manifest`.
- GitHub selected-repository picker for org visibility.
- Variable support for GitLab, Vault, AWS, and other provider plugins that can
  support a non-secret provider variable concept.
- Deprecating `wfctl vars setup`; keep it until Workflow 1.0 migration policy is
  clearer.
