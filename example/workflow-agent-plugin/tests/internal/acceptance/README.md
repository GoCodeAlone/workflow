# Offline acceptance support

This Go-only maintainer support runs through the standalone suite and the
repository's existing `TestWorkflowAgentPluginPackage` native CI wrapper. It adds
no Python dependency, host installer, OAuth entry point or workflow permissions.
From the package's tests directory:

```sh
GOWORK=off go test -race ./...
GOWORK=off go run ./cmd/acceptance-audit path/to/sanitized-evidence.json
```

The auditor reads retained, already sanitized app-server JSON records, deduplicates
matching start/completion events and flags forbidden names or missing/unsafe
required arguments. Failed attempts still count. It emits identifiers/categories
and a source digest without YAML or tool output. Use sanitized case/tool names;
the auditor does not redact arbitrary names. Exit 1 means a violation or
conflicting event, exit 2 means unavailable input/output, and exit 0 means only
that this limited lint found none. Every report keeps clean_acceptance:false.
The fixed synthetic canary check is not general secret detection. It does not
validate YAML, authorize or prevent calls, prove model obedience or certify GUI.

`SupportingFilePreflight` takes a controller-owned RPC client, disposable run
root, reviewed relative-file digest manifest and a trusted host-specific byte
reader argv prefix. A POSIX controller can explicitly supply `/bin/cat`; other
hosts require their own reviewed exact-byte reader. Nothing is selected from
untrusted config/tool text and no shell interpolation is used. The helper checks
the three exact discovered skill name/path bindings and reviewed installed bytes,
then uses threadless command/exec with an explicit readOnly policy and
networkAccess:false to read the shared contract and main skills. Registration
alone cannot pass. Reviewed supporting files must be regular files of at most
32 KiB; local hashing is bounded to the same limit. The caller must stop before authentication/model turns when
Passed is false. RPC implementations must honor the supplied context/timeouts.

The helper creates no client, login, process, sandbox profile or provider
connection. It cannot replace whole-controller/MCP containment or enforce the
user's cumulative model-turn authorization/quota; those belong to the separately
reviewed driver. The Mac-specific driver/broker and real session records remain
outside this portable package. Their stricter aggregate checks, cumulative cap
and group-context guards are preserved locally.

The testdata policy fixture is synthetic: it retains observed public-test tool
names and explicit argument flags, with inert YAML, synthetic identifiers and
synthetic completion statuses. It has 75 attempts, including 11 forbidden-name
and 11 additional argument-contract violations. It is not an original model
transcript or engine output. Fake RPC clients and public byte fixtures test
failure handling, not host/model acceptance.

## Current acceptance limit

The real isolated Codex 0.163.0-alpha.11.1 host exposed the configured 14 names,
but the owned contract read failed before cat with exit71/sandbox_apply_EPERM.
The driver wrapped the whole app-server in an outer macOS Seatbelt sandbox, then
Codex attempted its own read-only command sandbox. This is a harness launch
composition failure; it does not establish that ordinary Codex read-only commands
fail on every Mac. Removing the outer policy broadens controller/MCP access and
is not a fix in this change. The Go read adapter has deterministic fake-client
coverage; no additional real host attempt was made after that failed prerequisite.

Acceptance needs a supported separately contained disposable executor with no
user-data/profile/keychain shares, reviewed equivalent isolation and passing
no-auth canaries. Named command permission profiles alone do not establish
equivalent whole-controller containment. No sandbox disabling, external-sandbox
substitution, trust change or fresh sign-in is proposed here.

The prior model run completed via HTTPS fallback after WebSocket UnknownIssuer
errors. Patched model behavior, implicit skill selection, native call-time tool
rejection, WebSocket health and GUI remain unverified. One targeted turn cannot
certify the full matrix or all three skills. Source package 0.1.2 separates this
patch from the preserved 0.1.1 package; no new engine release is involved.
