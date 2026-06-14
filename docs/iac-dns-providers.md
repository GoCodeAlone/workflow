# IaC DNS providers

The `infra.dns` resource type is implemented by multiple plugins.
Each provider has different capabilities and auth requirements.

## Provider matrix

| Provider | Plugin | Auth | CIDR allowlist | Bulk ops | Status |
|----------|--------|------|----------------|----------|--------|
| DigitalOcean | [workflow-plugin-digitalocean](https://github.com/GoCodeAlone/workflow-plugin-digitalocean) | API token | n/a | yes (idempotent record list) | verified |
| Namecheap | [workflow-plugin-namecheap](https://github.com/GoCodeAlone/workflow-plugin-namecheap) | API user + key + single client IP | NO — single-IP only | yes (`setHosts` is full-replace) | experimental |
| Hover | [workflow-plugin-hover](https://github.com/GoCodeAlone/workflow-plugin-hover) | username + password + TOTP (browser-flow) | n/a | per-record (no batch API) | experimental |

## Configuration shape (all providers)

```yaml
modules:
  - name: <provider-instance-name>
    type: iac.provider.<digitalocean|namecheap|hover>
    config:
      # provider-specific auth keys; see each plugin's README
      ...

  - name: iac-state
    type: iac.state
    config:
      backend: <memory|spaces|gcs|azureblob|postgres>

resources:
  - name: <zone-id>
    type: infra.dns
    config:
      provider: <provider-instance-name>
      domain: example.com
      records:
        - type: A
          name: '@'           # apex, or subdomain
          data: 203.0.113.10
          ttl: 1800
        - type: CNAME
          name: www
          data: example.com.
          ttl: 1800
        - type: MX
          name: '@'
          data: mail.example.com.
          mx: 10              # MX priority
          ttl: 3600
        - type: TXT
          name: '_acme-challenge'
          data: "abc123"
          ttl: 60
```

## Per-provider notes

### DigitalOcean

- Best for the GoCodeAlone reference stack (App Platform droplets
  resolved by name; built-in DNS).
- Token must have full read+write scope.

### Namecheap

**Allowlist gotcha:** every IP that hits `api.namecheap.com` must be
explicitly whitelisted at Profile → Tools → Namecheap API Access.
Namecheap does NOT support CIDR. CI runners with rotating outbound
IPs need either:

1. A NAT gateway with a static egress IP.
2. A bastion host that proxies the API call.

The plugin's Config.Validate refuses CIDR strings outright so the
failure is detected at boot, not at apply time.

`setHosts` is a full-replace API: the plugin reads existing records
first and merges only the diff. Concurrent applies against the same
zone can lose writes — serialize with `wfctl infra apply`'s
single-pass guarantee.

### Hover

Hover has no official API. The plugin mimics the browser-side auth
used by [pjslauta/hover-dyn-dns](https://github.com/pjslauta/hover-dyn-dns):

1. GET `/signin` → parse CSRF `_token`.
2. POST `/signin` with username + password + token.
3. GET `/signin/totp` → fresh `_token`.
4. POST `/signin/totp` with RFC 6238 code + token.
5. Subsequent `/api/dns/...` requests carry the session cookie.

**TOTP**: provide the base32 seed (shown when you enabled 2FA in
Hover). Codes are generated in-process via pure-Go HMAC-SHA1.

**Failure modes**:

- Hover may serve a CAPTCHA challenge on suspicious logins. The
  plugin doesn't solve CAPTCHAs; log in manually from the same IP
  once to seed trust, OR use a static egress IP.
- Hover's signin HTML can change. The plugin fails loud with
  `CSRF token not found at /signin` when the regex stops matching.
- No batch API — record edits are per-call. Don't use Hover for
  zones with > ~50 records.

## Dynamic DNS

For dynamic IPs (home labs, mobile workstations), pair any DNS
provider with the `infra.dyndns` module:

```yaml
- name: home-dns
  type: infra.dyndns
  config:
    provider: namecheap        # any iac.provider.* module name
    domain: gocodealone.tech
    record_name: home
    poll_interval: 5m
    detect_via: [icanhazip, ifconfig.me, ipify]
    quorum: 2                  # 2-of-3 must agree before update fires
```

The daemon:

1. Polls each detector in parallel.
2. Requires `quorum` of them to return the same IP.
3. Compares to last-known IP.
4. Calls the provider's `UpdateRecord` on change.
5. Exponential backoff on consecutive failures (capped at 1h).

Per-record `detect_via` lets you trade redundancy for fewer
outbound calls; private LANs without internet access can supply
a custom detector via the plugin SDK.

## Ownership policy

DNS ownership is enforced through the cross-provider `wfctl dns-policy`
surface, not through per-record `_dns-managed-by` TXT records.

`wfctl dns-policy` stores a zone-level TXT policy at:

```text
_workflow-dns-policy.<zone>
```

Each policy entry declares an owner, optional record-name patterns, optional
record types, and at most one default owner. During `wfctl infra apply`,
`infra.dns` actions pass through a pre-dispatch gate when `WORKFLOW_DNS_OWNER`
is set. The gate reads the policy through the active provider's
`ResourceDriver("infra.dns")` and denies changes where the caller's owner is
not delegated for the `(record name, record type)` tuple.

Common operations:

```sh
wfctl dns-policy show --config infra.yaml --provider do-prod --zone example.com

wfctl dns-policy set \
  --config infra.yaml \
  --provider do-prod \
  --zone example.com \
  --owner sre \
  --patterns 'www,api,_acme-challenge,_acme-challenge.*' \
  --types 'A,CNAME,TXT'

wfctl dns-policy transfer-ownership \
  --config infra.yaml \
  --provider do-prod \
  --zone example.com \
  --name api \
  --new-owner platform

WORKFLOW_DNS_OWNER=platform wfctl infra apply --config infra.yaml --env prod
```

Operational rules:

- Missing policy fails closed when `WORKFLOW_DNS_OWNER` is set and an
  `infra.dns` action is checked.
- Missing `WORKFLOW_DNS_OWNER` logs a warning and skips DNS policy enforcement
  for compatibility with older applies.
- `SOA` and `NS` are protected unless explicitly delegated by type.
- The policy TXT is preserved by DNS record sanitizers so ownership metadata is
  visible in audits.

The older `_dns-managed-by.<domain>` idea is intentionally superseded. A single
zone-level policy is easier to audit, supports multi-owner delegation, and
avoids scattering ownership records across every managed DNS name.

## Secret and variable management

Each provider declares credentials in plugin.json `required_secrets[]` and
non-secret operational values in `required_config[]`. `wfctl secrets setup`
handles both when run from a `wfctl.yaml` manifest: secret inputs are written to
provider secrets and non-secret inputs are written to provider variables.

```sh
wfctl secrets setup --manifest wfctl.yaml \
  --config 'infra/*.yaml,deploy.yaml' \
  --plugin-dir data/plugins \
  --scope org --org GoCodeAlone --from-env
```

Cloudflare account IDs, Namecheap API users, and Namecheap API client IPs are
operational configuration, not secrets. They should be written as provider
variables, while API keys, tokens, passwords, and TOTP seeds remain provider
secrets. Plugin-only setup can still be split explicitly:

```sh
wfctl secrets setup --plugin workflow-plugin-namecheap \
  --scope org --org GoCodeAlone

NAMECHEAP_API_USER=alice NAMECHEAP_CLIENT_IP=203.0.113.10 wfctl vars setup \
  --plugin workflow-plugin-namecheap --from-env
```

Use `--name-map` when a repo or organization stores provider values under local
names rather than the plugin's logical contract names. Status checks, writes,
and `--from-env` lookup use the stored name first. Pair it with
`--write-config` when config files should be rewritten to the stored names:

```sh
GCA_NC_API_KEY=... GCA_NC_API_USER=... wfctl secrets setup \
  --manifest wfctl.yaml --config 'infra/*.yaml' \
  --scope org --org GoCodeAlone \
  --name-map NAMECHEAP_API_KEY=GCA_NC_API_KEY \
  --name-map NAMECHEAP_API_USER=GCA_NC_API_USER \
  --write-config --from-env
```

See `docs/wfctl-secrets-scopes.md` for the scope flag matrix.

## Provider plan

The full DNS provider plan (Namecheap + Hover + dyndns + scoped
secret-set) is tracked in `docs/plans/2026-05-20-dns-providers.md`
(workflow#735) — caveman SPEC format with 20 tasks, 16 constraints,
18 invariants.

## Domain Intent Compiler

Use `wfctl dns intent compile` when a domain migration spans hosted DNS and a
registrar delegation provider. The command reads a domain intent file plus one
or more `wfctl infra import-all --format portfolio` DNS catalog exports, then
emits ordinary `infra.dns` and `infra.dns_delegation` resources plus a JSON
report.

```sh
wfctl dns intent compile \
  --intent domains.json \
  --portfolio 'zones/*.portfolio.json' \
  --domain example.com \
  --output infra/domain-reconcile.generated.wfctl.yaml \
  --report reports/domain-reconcile-report.json
```

Intent file shape:

```json
{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "nameserver_cutover": true,
      "records_policy": "preserve_authoritative",
      "expected_current_nameservers": ["ns1.hover.com", "ns2.hover.com"]
    }
  }
}
```

Supported first increment:

- `dns_host: cloudflare` creates or updates `infra.dns` resources from the
  selected portfolio records and Cloudflare-assigned nameservers.
- `registrar: hover` with `nameserver_cutover: true` creates
  `infra.dns_delegation` resources targeting the Cloudflare nameservers.
- `records_policy: preserve_authoritative` chooses the current authoritative
  source when available.
- `records_policy: preserve_cloudflare` keeps the existing Cloudflare snapshot.
- `records_policy: discard_parked` emits an empty managed Cloudflare zone only
  when Hover records match the known parking pattern, unless
  `allow_discard_nonparked` is explicitly set.

Unsupported provider pairs and unsafe discard requests are reported as blockers
and cause the command to exit non-zero. Provider plugins still own the actual
apply behavior; the compiler only removes repository-local glue for producing
the concrete IaC resources and report.

For CI and operator workflows that want wfctl to drive the first lifecycle
steps, use `wfctl dns intent reconcile`:

```sh
wfctl dns intent reconcile \
  --intent domains.json \
  --portfolio 'zones/*.portfolio.json' \
  --domain example.com \
  --plugin-dir data/plugins \
  --mode plan
```

`reconcile` compiles the intent, validates the generated infra-only config with
the correct no-entry-points setting, and runs `wfctl infra plan`. With
`--mode apply --auto-approve`, it runs `wfctl infra apply` after the plan. The
command preserves the generated config, report, optional bundle, and plan JSON
paths so CI can upload them as artifacts. Provider-specific preflight and
post-apply verification still belong in the consuming workflow until wfctl grows
first-class provider-aware verification.
