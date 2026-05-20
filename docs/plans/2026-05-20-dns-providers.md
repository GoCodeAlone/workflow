# DNS providers + DynDNS + scoped secret-set

Caveman SPEC. See `FORMAT.md` for grammar.

## §G — Goal

Extend wfctl IaC surface for ∀ DNS provider, ∀ secret scope. Namecheap +
Hover + DynDNS shipped. Hover login: user+pw+TOTP. Plugin
declares required secrets → wfctl prompts → writes to scoped GH
target (org|repo|env).

## §C — Constraints

```
C1: DNS provider plugin ! implements infra.dns resource type via existing iac.ResourceDriver shape (DO precedent: workflow-plugin-digitalocean/internal/drivers/dns.go)
C2: Namecheap client = github.com/namecheap/go-namecheap-sdk v1.7+
C3: Hover ⊥ official SDK ∴ scraper-style HTTPS client w/ cookie jar (mirror github.com/pjslauta/hover-dyn-dns: POST https://www.hover.com/signin → TOTP challenge → cookie session → DNS CRUD via internal API)
C4: TOTP RFC 6238; Hover seed stored as base32-encoded HOVER_TOTP_SECRET; wfctl never logs the seed
C5: wfctl secrets set --scope ∈ {repo, env, org} ; default = repo (backwards-compat)
C6: GH org secrets ! visibility config (all | selected_repos | private_repos)
C7: GH env secrets ! environment_name flag
C8: wfctl secrets setup --plugin <name> reads plugin manifest required_secrets[] → interactive prompt each → write to chosen scope
C9: infra.dyndns module: poll IP → diff vs current A record → update via DNS driver Update RPC
C10: dyndns ! polling cadence default = 5m; configurable
C11: dyndns IP-detect sources: icanhazip | ifconfig.me | opendns ; multiple sources for redundancy
C12: TOTP code generation in-process; ⊥ external `oathtool` dep
C13: Namecheap auth = (api_user, api_key, client_ip allowlist); wfctl secrets setup writes api_user + api_key
C14: Hover scraper resilient to login-page CSRF token rotation (parse `<input name="_token" value="...">` each login)
C15: DynDNS state machine: detect → diff → update → wait → repeat; on err exponential backoff w/ jitter (max 1h)
C16: ∀ DNS plugin ! pass strict-contracts gRPC boundary (typed proto, no map[string]any)
```

## §I — Interfaces

```
api: GET https://api.namecheap.com/xml.response?Command=namecheap.domains.dns.getHosts → DomainDNSGetHostsResponse XML
api: POST https://api.namecheap.com/xml.response Command=namecheap.domains.dns.setHosts → DomainDNSSetHostsResponse
api: POST https://www.hover.com/signin form: {username, password, _token} → 302 redirect (TOTP page)
api: POST https://www.hover.com/signin/totp form: {code, _token} → 302 (session cookie)
api: GET https://www.hover.com/api/domains/<domain>/dns → JSON {domains: [...]}
api: POST https://www.hover.com/api/dns form: {domain_id, name, type, content, ttl}
api: PUT https://www.hover.com/api/dns/<record_id> form: {content, ttl}
api: DELETE https://www.hover.com/api/dns/<record_id>
cmd: `wfctl secrets set <name> --scope <repo|env|org> [--env <env>] [--visibility <all|selected|private>]`
cmd: `wfctl secrets setup --plugin <plugin-name> [--scope <repo|env|org>]`
cmd: `wfctl secrets setup --provider <namecheap|hover|...>` (alias above)
env: NAMECHEAP_API_USER ! set if iac.dns provider=namecheap
env: NAMECHEAP_API_KEY ! set (sensitive)
env: NAMECHEAP_CLIENT_IP ! set ; whitelisted at api.namecheap.com
env: HOVER_USERNAME ! set if iac.dns provider=hover
env: HOVER_PASSWORD ! set (sensitive)
env: HOVER_TOTP_SECRET ! set (sensitive; base32 seed)
manifest: plugin.json required_secrets[] = [{name, sensitive, description, prompt}]
```

## §V — Invariants

```
V1: ∀ secret write via wfctl ! mask in stdout/stderr
V2: Hover login ! happens iff session cookie expired || ⊥
V3: TOTP code ! regenerated on each login attempt (never cached)
V4: scope=org ! requires admin:org GH PAT scope
V5: scope=env ! requires repo + workflow GH PAT scope
V6: scope=repo (default) ! requires repo GH PAT scope
V7: required_secrets prompt ! masked input via term.ReadPassword when sensitive=true
V8: dyndns ! avoid update RPC if detected IP == current record IP
V9: dyndns ! exponential backoff on consecutive failures; max 1h; reset on success
V10: Hover cookie jar ! persisted across plugin restarts via /var/lib/wfctl/hover-session.json (mode 0600); ⊥ committed
V11: Namecheap client_ip allowlist ! validated against ipify.org on plugin start; warn if mismatch
V12: ∀ DNS provider plugin ! emit `infra.dns` resource shape: {ID, Type:"infra.dns", Outputs:{provider_id, records:[]}}
V13: dyndns module ! emit metrics (gauge dyndns_last_detected_ip, counter dyndns_updates_total{provider})
V14: TOTP seed ! base32-decoded once on plugin Init; ⊥ logged
V15: Hover scraper ! User-Agent header set; otherwise hover may return CAPTCHA
V16: GH org secret ! created via PUT /orgs/{org}/actions/secrets/{name}; encrypted with org public key
V17: GH env secret ! created via PUT /repos/{owner}/{repo}/environments/{env}/secrets/{name}; encrypted with env public key
V18: wfctl secrets set --scope ! short-circuit list-by-name BEFORE create (mirrors DO-Spaces orphan fix patterns from workflow#732)
```

## §T — Tasks

```
id|status|task|cites
T1|.|workflow: extend secrets.GitHubSecretsProvider w/ scope (repo|env|org) constructor + Put switch on scope|C5,C6,C7,V4,V5,V6,V16,V17
T2|.|wfctl secrets set --scope flag + delegation to scoped provider; default repo|C5,V18
T3|.|wfctl secrets setup --plugin <name>: read plugin.json required_secrets[], prompt each (sensitive=masked), write to scope|C8,V1,V7
T4|.|wfctl secrets setup --provider <name>: alias for --plugin (UX sugar)|C8
T5|.|workflow-plugin-namecheap scaffold (new repo): plugin.json + cmd/ + go.mod + GoReleaser + CI|C2,C13,C16
T6|.|namecheap DNSDriver implements interfaces.ResourceDriver for `infra.dns` (Create/Read/Update/Delete/Diff)|C1,C2,V12
T7|.|namecheap required_secrets manifest entry: NAMECHEAP_API_USER, NAMECHEAP_API_KEY, NAMECHEAP_CLIENT_IP|C13
T8|.|namecheap client_ip validation against ipify.org on plugin Start|V11
T9|.|workflow-plugin-hover scaffold (new repo): plugin.json + cmd/ + go.mod + CI|C3,C16
T10|.|hover HTTPS scraper client: login(user, pw, totp) → session cookie jar; List/Get/Create/Update/Delete record|C3,C14,V2,V15
T11|.|hover required_secrets manifest entry: HOVER_USERNAME, HOVER_PASSWORD, HOVER_TOTP_SECRET (sensitive)|C8
T12|.|in-process TOTP impl (RFC 6238 HMAC-SHA1, 30s window, 6 digit) — pure go, ⊥ deps|C4,C12,V3,V14
T13|.|hover session persistence: /var/lib/wfctl/hover-session.json mode 0600; refresh on 401|V10
T14|.|workflow: infra.dyndns module type — config{provider, domain, record_name, poll_interval, detect_via}|C9,C10,C11
T15|.|dyndns module: polling loop, IP detect (multi-source quorum), diff, Update RPC, backoff|C15,V8,V9
T16|.|dyndns metrics: gauge dyndns_last_detected_ip{provider,record}, counter dyndns_updates_total|V13
T17|.|registry manifests: workflow-plugin-namecheap + workflow-plugin-hover added to workflow-registry|workflow#714
T18|.|scenarios: 70-iac-namecheap-dns + 71-iac-hover-dns + 72-iac-dyndns-multiprovider|C1,C3,C9
T19|.|docs: docs/wfctl-secrets-scopes.md w/ examples; docs/iac-dns-providers.md w/ matrix|C5,C8
T20|.|integration test matrix: GH stub server validates org/env/repo PUT paths; secrets set roundtrip|T1,T2
```

## §B — Bugs

```
id|date|cause|fix
```

(empty)
