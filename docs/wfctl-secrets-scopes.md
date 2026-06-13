# wfctl secrets — GitHub scope reference

`wfctl secrets set`, `wfctl secrets setup --plugin`, and the unified
`wfctl env setup` flow write to GitHub Actions destinations. Secret inputs use
GitHub Actions Secrets. Non-secret config inputs use GitHub Actions Variables
when the selected provider target supports variables. Each GitHub scope requires
a different PAT scope and exposes different visibility controls.

| Scope | URL prefix | PAT scopes | Visibility flags |
|-------|------------|-----------|------------------|
| `repo` (default) | `/repos/{owner}/{repo}/actions/secrets/{name}` | `repo` | — |
| `env` | `/repos/{owner}/{repo}/environments/{env}/secrets/{name}` | `repo`, `workflow` | — |
| `org` | `/orgs/{org}/actions/secrets/{name}` | `admin:org` | `--visibility {all,selected,private}` |

## Default (repo) scope

Backwards-compatible. Reads the repo from `secrets.config.repo` in
`app.yaml`:

```sh
wfctl secrets set MY_TOKEN --value "abc123"
# → PUT /repos/GoCodeAlone/example/actions/secrets/MY_TOKEN
```

Pipes are honored:

```sh
echo -n "abc123" | wfctl secrets set MY_TOKEN
```

If stdin is a TTY and neither `--value` nor `--from-file` is set,
the value is read with `term.ReadPassword` (masked).

## Environment scope

Writes to a repo's GitHub Actions environment. Requires the env to
already exist (create it once in the repo's Settings → Environments
panel).

```sh
wfctl secrets set STRIPE_KEY \
  --scope env --env production \
  --value "sk_live_..."
# → PUT /repos/GoCodeAlone/example/environments/production/secrets/STRIPE_KEY
```

Repo is still resolved from `app.yaml`'s `secrets.config.repo`.

## Organization scope

Writes a secret that any selected repo can pull. Bypasses `app.yaml`
since org secrets are out-of-band of repo config. The PAT in
`$GITHUB_TOKEN` (or `--token-env`) MUST carry `admin:org`.
The CLI default visibility is `private`, matching GitHub's private/internal
repository intent instead of exposing new organization secrets to all repos.

```sh
# Private and internal repos in the org can pull this secret.
wfctl secrets set SHARED_API \
  --scope org --org GoCodeAlone \
  --visibility private \
  --value "$(openssl rand -hex 32)"

# All repos in the org can pull this secret.
wfctl secrets set BROAD_API \
  --scope org --org GoCodeAlone \
  --visibility all \
  --value "..."

# Only the listed repo IDs can pull. (selected_repository_ids
# are populated programmatically via a follow-up; CLI accepts
# them in a future flag.)
wfctl secrets set CI_SECRET \
  --scope org --org GoCodeAlone \
  --visibility selected \
  --value "..."
```

## Plugin-driven setup

If you're configuring a plugin that declares `required_secrets[]` in its
`plugin.json` (workflow-plugin-namecheap, workflow-plugin-hover, etc.), use the
interactive setup flow:

```sh
wfctl secrets setup --plugin workflow-plugin-hover \
  --scope org --org GoCodeAlone --visibility private
```

Plugin-only setup:

1. Reads `plugin.json` from the installed plugin directory. The directory may be
   the full plugin name, the normalized provider name, or
   `workflow-plugin-<provider>`.
2. Iterates `required_secrets[]`.
3. Prompts for each (masked iff `sensitive: true`).
4. Writes each to the chosen GH scope.

Plugins can also declare non-secret setup values in `required_config[]`.
`wfctl vars setup --plugin <name>` writes only those variable entries. A value
marked `sensitive: true` is a plugin manifest bug and must be moved to
`required_secrets[]`.

Applications can use the same variable provider path for non-secret
`config.provider` schema values. Run `wfctl vars setup --config app.yaml` to
scan env-backed schema entries where `sensitive: false`; sensitive entries are
left for the app's secret setup flow.

`wfctl env setup` can discover both plugin secrets and plugin variables from
`wfctl.yaml`, `.wfctl-lock.yaml`, and installed plugin manifests:

```sh
wfctl env setup --manifest wfctl.yaml \
  --config 'infra/*.yaml,deploy.yaml' \
  --scope org --org GoCodeAlone --from-env
```

Environment input setup treats `required_secrets[]` and sensitive config refs as
provider secrets, and `required_config[]` plus non-sensitive config refs as
provider variables. This lets one review/setup flow configure values like
`NAMECHEAP_API_KEY` and `NAMECHEAP_API_USER` together while still storing the
key as a secret and the user/client ID/account ID as variables.

Use `--name-map LOGICAL=STORED` to store a logical workflow input under a
provider-specific name:

```sh
GCA_NC_API_KEY=... GCA_NC_API_USER=... wfctl env setup \
  --manifest wfctl.yaml \
  --name-map NAMECHEAP_API_KEY=GCA_NC_API_KEY \
  --name-map NAMECHEAP_API_USER=GCA_NC_API_USER \
  --write-config --from-env
```

With a name mapping, status checks and writes use the stored provider name.
Value lookup also checks the stored name first, then the logical name. When
`--write-config` is set, matching `${LOGICAL}` references in the scanned config
files are rewritten to `${STORED}` after setup succeeds.

When `--scope` is omitted and stdin is interactive, `wfctl env setup` uses
configured `secretStores` when present; otherwise it offers concrete GitHub
targets discovered from repo/org/env settings. The first prompt is a compact
matrix with one row per input, a `Kind` column (`secret` or `var`), and one
column per target:

| mark | meaning |
|------|---------|
| `○` | unset |
| `✓` | already set |
| `!` | inaccessible or check failed |
| `?` | unconfigured |

GitHub columns are only GitHub destinations: `github:repo`, `github:env`, and
`github:org`. Local `.env`, file, keychain, Vault, and AWS stores appear as
their own provider targets. Use `--verbose` when you need the source config
file, plugin name, or full target label in the prompt rows.

`wfctl secrets setup --manifest ...` remains supported as a compatibility path
for secrets setup, and it reaches the same environment input engine. Prefer
`wfctl env setup` for mixed secret and non-secret setup.

Pipe a value list to skip the prompt loop in CI:

```sh
printf 'alice\nhunter2\nJBSWY3DPEHPK3PXP\n' | \
  wfctl secrets setup --plugin workflow-plugin-hover \
  --scope org --org GoCodeAlone
```

## PAT scope cheat sheet

| Token use | Required scopes |
|-----------|-----------------|
| Repo secrets | `repo` (or fine-grained `Actions:write` + `Secrets:write`) |
| Env secrets | `repo` + `workflow` |
| Org secrets | `admin:org` (classic PAT) — fine-grained PATs cannot manage org secrets as of GH API v2022-11-28 |

## Troubleshooting

- **`HTTP 403: Resource not accessible by integration`** — missing
  PAT scope. Most often `admin:org` for `--scope=org`.
- **`HTTP 422: secret value cannot be empty`** — the prompted value
  was empty. The setup flow skips empty values; check terminal
  echo settings.
- **`improperly encrypted secret`** — local clock skew vs GitHub or
  a truncated public key. Re-run; the encryption nonce is per-call.
- **Org secret missing from a repo's Actions environment** — check
  visibility. `selected` requires the repo ID to be in
  `selected_repository_ids`; CLI accepts the list via a follow-up
  flag.
