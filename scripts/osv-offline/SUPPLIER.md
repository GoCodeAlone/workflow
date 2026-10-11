# Hosted supplier preparation

`prepare-supplier.py` prepares public scanner suppliers on the existing approved
Ubuntu 24.04 GitHub-hosted Linux/amd64 runner. Source review and actual serial
rebuilding are separate requirements. It does not execute a compiled scanner,
reporter or govulncheck, preload project dependencies, acquire advisory archives,
or admit installation. OSV's upstream contribution
instructions were inspected; this is local Workflow supplier preparation, with
no upstream submission or maintainer action.

Execution requires `--execute`, repository `GoCodeAlone/workflow`, job
`osv-production-proof`, event `workflow_dispatch`, and branch
`refs/heads/prep/osv-prerequisites-20261011-r1`. The receipt binds the actual
repository, checkout SHA, run ID, attempt and job; the fixed dispatch contract
is recorded separately. Output must be an empty mode-0700 runner-owned directory
named `RUNNER_TEMP/osv-supplier-XXXXXXXXXX` (ten alphanumeric suffix characters).

The setup/preparation phase must establish and admit the **existing standard**
`/home/runner/go`, `/home/runner/go/pkg/mod` and
`/home/runner/.cache/go-build` directories and the installed canonical Go1.27.2
executable. The supplier never creates another GOPATH, module cache or build
cache. A missing directory refuses before downloads. No clone, npm operation,
private proxy, VCS fallback, auto toolchain, install hook or candidate source is
used. Cache contents are updated only by the fixed public Go supplier phase.

The reported GOROOT must equal the canonical installed executable's SDK root.
An existing writable root is tightened through its opened directory descriptor
only when it belongs to the current runner UID: group and other write bits are
removed. The receipt records original/final modes and conserved inode/owner.
Foreign-owned writable roots, symlinks and special modes refuse. Compiler files,
cache permissions and other SDK directories are preserved.

The scanner source is `github.com/google/osv-scanner/v2@v2.6.0`, authenticated by
the reviewed ZIP/go.mod h1s and exact origin metadata
`e840a6e8adb14b7777c78e26cfbf6e2abc1d1fc6`. Every module ZIP and proxy go.mod is
independently checked using Go's [Hash1 format](https://github.com/golang/mod/blob/v0.41.0/sumdb/dirhash/hash.go).
Missing origin metadata, checksum mismatch, unsafe ZIP entry, cache path escape,
unknown source directive or missing pinned checksum fails closed. Only the
reviewed go.mod/go.sum changes are applied: Go1.27.0 to Go1.27.2, x/net0.58.0 to
0.60.0, and its two authenticated checksum rows. Original checksum rows remain.
Exact upstream and patched manifest SHA256 values are enforced in source.

Only the scanner's authenticated 206 direct/indirect module requirements are
downloaded, in fixed batches of 16, through `https://proxy.golang.org` and
`sum.golang.org`. Go's official public blob redirect is part of that existing
supplier profile. The closed child environment contains no GitHub token,
credential, proxy variable, private module pattern or compiler hook. Requests
contain supplier module paths/versions and checksum protocol quantities only.
Project preload remains the caller's separate approved phase.

Before the first Go command, an owned temporary XDG configuration records
telemetry `off`; actual `go env GOTELEMETRY` must report `off`, and that mode file
is conserved at closeout. This follows the
[Go1.27.2 telemetry reader](https://github.com/golang/go/blob/go1.27.2/src/cmd/vendor/golang.org/x/telemetry/internal/telemetry/dir.go).
It does not modify the runner's normal configuration or create a private cache.
Network admission remains the trusted hosted supplier boundary; environment
flags are not represented as an OS firewall.

Compilation is serial and offline (`GOPROXY=off`, `GOSUMDB=off`), using
`-trimpath -buildvcs=false -p=2`, CGO disabled, Linux/amd64/v1,
GOMAXPROCS=2 and GOMEMLIMIT=768MiB. Scanner and reporter use `-mod=readonly` from
the conserved source export. Govulncheck is installed only at the authenticated
version `golang.org/x/vuln/cmd/govulncheck@v1.6.0` into the owned `binaries`
subtree. No built binary runs; `go version -m` reads build metadata.
Go's [supported buildvcs=false flag](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)
omits the unrelated historical workspace VCS stamp. New hashes require actual
hosted rebuilding and admission; equality to the old pins is never claimed.

Finite source bounds are 1,800 seconds total, 180 seconds per download batch,
600 seconds per build, 4 MiB per child stream, 500 MiB per module ZIP/decoded
tree, 64 MiB per member, 20,000 files per module and 4 GiB aggregate supplier
source bytes. Successful and failed native child exits, serial timings and raw
bounded stdout/stderr are retained. These source limits do not admit workflow
timing or any production resource policy.

The caller invokes the reviewed script with explicit recorded paths, for example
after those standard directories and the installed Go path have been admitted:

```sh
python3 -I -S -B -X pycache_prefix="$RUNNER_TEMP/osv-supplier-pycache-absent" \
  scripts/osv-offline/prepare-supplier.py --execute \
  --output "$OSV_SUPPLIER_OUTPUT" --go "$OSV_INSTALLED_GO" \
  --gopath /home/runner/go --gomodcache /home/runner/go/pkg/mod \
  --gocache /home/runner/.cache/go-build
```

`supplier-delivery-proof.json` always has `accepted:false` and
`installation_admitted:false`. `complete:true`/`supplied:true` require all three
actual builds, metadata checks, source/cache/binary/toolchain conservation and
final deadline checks. It includes the three `binary_pins`. Publication uses a
pending file and an atomic rename, followed by a deadline veto; failure removes
the positive receipt and writes an incomplete diagnostic receipt. Any I/O
failure that also prevents invalidation remains a failed actual supplier step.
The caller must require that exact actual step's success, fresh receipt,
source/dispatch bindings and separately admitted pins. A provisional receipt
alone grants no authority. Genuine vulnerability and baseline acceptance gates
remain unchanged.

The focused controls use synthetic public-module ZIPs, a fake Go command adapter,
temporary files and two small Python children. Hosted/platform/compiler gates
are explicitly substituted in orchestration tests; no actual Go, network,
Docker, namespace or privileged operation is performed:

```sh
python3 -I -S -B -X pycache_prefix=/tmp/osv-supplier-controls-absent \
  scripts/osv-offline/test-prepare-supplier.py
```
