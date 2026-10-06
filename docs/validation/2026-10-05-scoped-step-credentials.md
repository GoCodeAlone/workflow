# Scoped Step Credentials Validation

Date: 2026-10-05, source verification freeze06:41 UTC. Status: local source
verification complete; PR publication/CI are subsequent gates recorded in PR
metadata and the workspace checkpoint. No release, deployment or live-cloud claim.

## Source And Provenance

Branch: `fix/scoped-step-credentials-20261005`.
Baseline: `c5d53caa78226d0b079fb5ac3b244e19bd99d83c`.
Task1: `ba4ec152f26584c5347ee7cedc64900f3cd6d3f0`.
Task2: `e39801ee4bef3bf6b730da1a67daca65477ac44e`.
Task3 / Task4 starting HEAD: `be8ba74826f6b5c18be40bd477bb102cbfed59fe`.
Task4 source correction removes only the unused free `configTransformHook`;
the instance method and registered hook are unchanged.

Receipts: implementer handoffs and corresponding reviews at
`/private/tmp/scoped-step-credentials-task{1,2,3}{,-review}-20261005.md`.
Rows below attribute prior executions to their owners, not to this doc writer.
Task1 review SHIP-IT; Task2 Important unknown-mode finding closed with an explicit
private allowlist (cycle2 SHIP-IT); Task3 SHIP-IT, no Important/Critical findings.

## Behavioral Ledger

| Owner / run | Exact outcome | What it proves |
| --- | --- | --- |
| T1 baseline ownership RED and module-global inverse RED | exit1, both ownership assertions fail | A/B source replacement and transform-to-Init snapshot divergence detected through actual application Init |
| T1 preflight inverse RED | exit1, template alias accepted assertion | Authorization must precede reference expansion |
| T1 validator RED | exit1, missing literal-validator assertion | Every bound root field needs immutable-reference validation |
| T1 restored required race GREEN | exit0; module2.554s, configprovider1.770s, interfaces2.704s (no selected tests) | Original source/validator paths restored |
| T1 expanded race GREEN | exit0; 57 test/subtest PASS lines, 0 FAIL, 0 selected skips | Exact/foreign targets, unknown fields/scopes/sources, malformed/missing/empty refs, duplicate/ambiguous grants, mutation/cancellation, private frozen source, service duplication, env snapshot and plugin reuse controls |
| T2 initial behavioral RED | exit1, 8 subtest failures | 3 missing carriers; 4 endpoint/global-fallback cases; Current.config spoof |
| T2 independent carrier / evaluator inverses | each exit1; 3 carrier / 5 endpoint-spoof-missing-key failures | Private carrier and same-app evaluation are independently necessary; both restored GREEN |
| T2 final required / expanded race before review correction | exit0; 185 / 205 test events PASS, 0 FAIL, 0 SKIP; 3 / 4 package PASS | Includes nested/escaped/marker-equal echoes, safe warnings, exact ordinary payloads, map immutability, nil/Stop/error paths and existing typed controls |
| T2 review correction, lead | 4 behavioral RED subtests, then restored expanded race GREEN | Numeric99/-99 reject at factory and Execute before lookup/Create/Execute; no-grant routing unchanged |
| T3 native first GREEN, session26046 | exit0, 6 cases, 24 authorized POSTs | Fresh SDK subprocesses through full host and nested YAML |
| T3 binding inverse, session2558 | exit1, 6 behavioral failures, 0 skips, 0 HTTP requests | Removing only binding produces genuine missing-carrier failure |
| T3 evaluator inverse, session72090 | exit1, 4 wrong-endpoint failures; 2 pure-expansion controls PASS | Removing only scoped evaluator sends first-built token to wrong endpoint; rejected POST, no authorized payload |
| T3 restored native, session77470 | exit0, package33.016s, 6 cases, 24 exact POSTs, 0 skips | Both build orders, interleaving and pure/composed Go/Expr URLs; A=2/B=2 per case |
| T3 actual released065, session10990 | exit0, package2.856s, 2 cases, 0 skips; granted1/no-grant0 requests | Real consumer403 Stop versus original unresolved-ref Stop |
| T3 lead fresh combined race, session94320 | exit0, package34.440s; native6/24 POSTs, actual0652/granted1/no-grant0 | Independent local acceptance; all26 PIDs reaped, IPC closed, listeners gone |
| Lead fresh no-body-logging guard | PASS (lead-supplied receipt) | Existing guard retained; not a new logging policy or manager retrofit |
| Lead full lint before T4, session45314 | RED: exactly unused free wrapper; untouched baseline full lint exit0 | Known introduced finding; post-removal full lint still Pending |
| T4 focused configprovider, session85478 | `go test ./plugins/configprovider -count=1 -timeout=90s`, exit0, package0.594s | Wrapper-only removal retains package behavior; gofmt listing empty and diff check exit0 |

T1/T2 session IDs were not supplied in their implementer receipts; none are
invented here. T2 totals count parent/subtest events, not assertions. Its 15
no-grant matrix cases cover five modes (nil, UNSPECIFIED, LEGACY_STRUCT,
PROTO_WITH_LEGACY_STRUCT, STRICT_PROTO) across nil app, no service and foreign
binding. Granted typed/unknown modes reject; absent/empty grants, ordinary and
zero-value/global evaluators, unrelated plugins and normal plugin reuse remain
controls. Credentialed reuse fails; separate credentialed apps retain their own
single frozen snapshot, including declared-env mutation before Init.

Preliminary native session22557 was a sandbox listener denial, not behavioral RED.
Released sessions20706/40296 failed fixture provider-image/URL configuration, not
host RED; corrected fixture then passed session62627 and final10990. Task3 focused
lint sessions61052/99263 and vet91905 exited0; these are not full-repository gates.

## Runtime Boundary And Hashes

Actual local runtime: **Darwin/arm64 only**. Mandatory native test always builds
the committed SDK fixture and uses modular/StdEngine, real manager discovery/load,
BuildFromConfig/Start, parent -> response-child -> URL-only capture-child,
ExecutePipelineContext, Stop/Shutdown. Six cases make 24 genuine loopback POSTs;
72 unrelated native invocations receive no carrier. Workload JSON and returned
context/errors/logs are checked without publishing token/header/environment data.

Released065 is separately opted in with `WORKFLOW_CAPTURE_V0165_PLUGIN_DIR`,
hash-checked before spawn with disk/runtime name/version/type checks; no download,
source rebuild or PATH fallback. Granted result is exactly
`POST /v1/tasks: got status 403`, Stop=true, one authorized request. Withdrawn
grant yields `config ref "product_capture.compute_token" not resolved`, Stop=true,
zero requests; the following continuation marker is absent in both cases.
Loopback Compute returning403 is the substituted dependency. **No task was
accepted, successful capture/live Amazon access or signed provider proof was
established**, and no worker/browser/artifact/proof activity followed.

| SHA256 subject | Recorded digest |
| --- | --- |
| Unchanged released Darwin/arm64065 binary | `fbce8a8169aa740002e165fa3263e7322155ffa3d2da11e6bd44a507ddace775` |
| Native fixture binary in independent inverses and restored GREEN | `374b5c4e4bdde24c475258646a469bef08b2c248e1e900b41e7f6db2fb528dcb` |
| `engine_step_credentials_test.go` | `f84995a26920365c64a72ab15a52699d1bdf034baf5f9a0e421312a8851e3f8d` |
| `plugin/external/testdata/step-credentials-plugin/main.go` | `2b1cf873f8b57b2674643166f6f3ebe38e38c73fbb5f7fad3907e5e0767947c2` |

Author restored/inverse runs each reaped24/24 children and closed12/12 loopback
listeners; final065 reaped2/2 and closed2/2 listeners. All recorded author sessions
finished. Lead fresh combined cleanup is separately recorded above. No manager,
SDK/proto, CI, dependency pin or native-process policy change is included.

Optional065 without an external asset path is **SKIPPED / NOT EXECUTED**, never
065 PASS and never substituted by the mandatory native fixture. Hosted CI had not
run at this source freeze. Linux/Windows runtime and other released-asset platforms
are unverified. Nonblocking fixture limitation: the cached065 asset and test
TempDir must share a filesystem because setup hard-links the verified binary;
a cross-filesystem path fails setup (EXDEV), not credential acceptance.
Native plugins remain trusted and can retain values; legacy global and inherited
environment access is not retroactively sandboxed. Detection rejects known-value
echoes rather than rewriting legitimate payloads; arbitrary malicious encodings
are not a containment guarantee.

## Final Lead Verification

Ran sequentially with the offline local Go1.26.5 toolchain, `GOWORK=off`,
`GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`,
`GOFLAGS=-mod=readonly`. Tests removed live DO/AWS/GitHub/Compute credential
variables and set `WFCTL_E2E_DNS_IMPORT=0`; only dummy values, local SDK processes
and owned loopback HTTP were used. Both full/final-focused tests explicitly set
`WORKFLOW_CAPTURE_V0165_PLUGIN_DIR` to the verified cached Darwin/arm64 asset.
Counts below are JSON test/subtest events, not assertions.

Lead checkpoint: uncached full-race session21054 started around06:27; native
already PASS, 151 packages with0 failures reported, CLI still running. JSON:
`/private/tmp/scoped-step-credentials-full-tests-20261005.jsonl`. This is an
in-progress lead receipt, **not a completed full-race PASS**. Task4 did not
duplicate this run, full lint or review.

| Gate | Status |
| --- | --- |
| `go test -race ./... -count=1 -timeout=20m -json` | session21054 exit0:152 package PASS,19 no-test packages,13327 test/subtest PASS,0 FAIL,58 SKIP; CLI540.343s |
| Final post-cleanup focused race, exact command below | session18056 exit0:7 package PASS,255 test/subtest PASS,0 FAIL,0 SKIP; native6/24 exact POSTs, released0652/granted1 versus ungranted0 |
| `go vet ./...` | session99655 exit0 |
| `go build ./...` | session24220 exit0 |
| `golangci-lint run --timeout=10m` | session26960 exit0,0 issues; introduced wrapper finding removed |
| `golangci-lint run --timeout=10m --new-from-rev=c5d53caa78226d0b079fb5ac3b244e19bd99d83c` | session13233 exit0,0 issues |
| `golangci-lint run --timeout=10m . ./plugin/external/testdata/step-credentials-plugin` | session58299 exit0,0 issues; includes normally wildcard-excluded fixture |
| Formatting/diff/scope audit | Changed Go files `gofmt -l` exit0/empty; `git diff --check` exit0; strict four-task/one-PR lock check PASS |
| Final independent source review | SHIP-IT; unchanged Task1-3 hashes and exact dead-helper/doc delta checked, no Important/Critical findings; no cosmetic review loop |
| Publication/CI | Not executed at this source freeze; exactly one source PR is required, no merge/release/deploy authority |

```sh
go test -race . ./module ./plugins/configprovider ./interfaces ./plugin/external ./pipeline ./secrets \
  -run 'Test(EngineScopedCredentials|StepCredential|ConfigProvider|ConfigTransform|GlobalConfigRegistry|RemoteStep|ScopedConfigLookup|TemplateEngine|ExprEngine|NoBodyLoggingInterceptor|ExternalPluginAdapter|CreateTypedConfigRequest|Redactor)' \
  -count=1 -timeout=180s -json
```

Full race began after the final runtime changes and Task3 commit; the only later
source edit removed the unused unexported wrapper. Fresh post-cleanup focused
race/vet/build/full lint above validate that final delta. No behavior, test,
SDK/proto/manager/provider, dependency/release pin or CI changes followed.
Final native/065 cleanup assertions passed; native stdout contained zero dummy
canary matches. No owned verification sessions or fixture runtimes remain.
The lint executable was built with Go1.26.5 from the already cached
`github.com/golangci/golangci-lint/v2@v2.12.0` module; its version reports `devel`,
not an official release binary. Untouched c5d53caa full lint also exited0.

### Full-Suite Skips

These are not acceptance PASS, and no skip was added or broadened by this change.
Mandatory native and the explicitly supplied actual065 tests both passed locally.

| Package | Skipped events | Existing reason/boundary |
| --- | --- | --- |
| Root examples | 29 | No-module configs or declared unsupported/missing example services/modules/plugins |
| `cmd/wfctl` | 6 | Two DO-plugin-dependent plan rows lack that plugin; old-plan downgrade baseline unconfigured; two live Docker tests not opted in; plan-index regeneration not requested |
| `iac/conformance` | 3 | Intentional skip-report controls and optional ProviderValidator absence |
| `module` | 5 | Three Docker CLI unavailable; two disposable PostgreSQL URLs absent |
| `pkg/k8s/imageloader` | 1 | Docker unavailable on the verification PATH |
| `sandbox` | 2 | Docker CLI unavailable on that PATH |
| `secrets` | 6 | Vault executable absent |
| `store` | 6 | `PG_URL` absent |

No live Docker, DO provisioning/API acceptance, PostgreSQL or Vault integration
is inferred from those skipped rows. Generated local JSON receipts are retained
under `/private/tmp/scoped-step-credentials-{full-tests,final-focused}-20261005.jsonl`;
raw transcripts, environment values, bearer headers and consumer bodies are not
copied into public evidence.

Operator grant/lifecycle/rollback guidance:
[Private Step Credentials](../dsl-reference.md#private-step-credentials).
Rollback withdraws grants and rebuilds (or reverts the single source PR), then
reruns no-grant/missing-ref controls. No token-in-input/metadata/env workaround;
callback, staging, browser and successful provider-proof acceptance stay separate.
