# Offline scanner prerequisite preparation

Installation and scanner execution remain on HOLD. The bootstrap manifest keeps
all five admission receipts unbound and `permanent_install_authorized: false`.
The existing scanner profile, finding policy and automatic OSV workflow are
unchanged.

`run-systemd-proof.py` prepares one prerequisite witness on the existing
`ubuntu-latest` hosted path. It requires a separately authorized manual dispatch
on `prep/osv-prerequisites-20261011-r1` with `osv_privileged_proof: true`; the input
defaults to false. Publishing these sources does not authorize that dispatch.
The first proof executes fixed probes only and cannot admit installation or run
the scanner or candidate code.

Before submission, the launcher checks the exact checkout and dispatch identity,
authenticated temporary bubblewrap, existing namespace eligibility, canonical
tools, systemd 255 manager defaults, unit lookup metadata and the complete
ancestor controller sets. It repeats the read-only preflight immediately before
submission. Unknown, denied, changed or conflicting state stops the proof;
there is no policy or alternate-host fallback. Configuration contents are not
read. Both the launcher and unit manager bind stdin to the verified canonical
null device before any child execution.

The sole new transient unit runs as the actual nonzero runner UID/GID in
`system.slice`, with an empty delegated root, a `supervisor` leaf and a private
worker sibling. Both root and worker retain memory 2 GiB, swap 0 and 256 tasks.
The root runtime is 150 seconds, startup 15 seconds and stop 5 seconds; the root
command is enclosed by a 180-second timeout with a 10-second kill grace. The
whole launcher has a separate 240-second orchestration timeout, and the job
remains eight minutes. IO and legacy BlockIO accounting are explicitly disabled;
only memory and pids may be activated inside the new empty owned root. Both
ancestor controller sets must already provide memory and pids and remain
unchanged.

Cleanup binds the captured unit invocation, control group, main PID and owner
before stopping that exact unit. Missing or replaced attestation refuses cleanup
and success. The unit's independent runtime bound and hosted VM teardown remain
in force. Receipts are provisional: actual supplier and proof step outcomes must
both succeed, and receipt closure past the deadline vetoes the result.

Run the offline regressions without executing systemd, sudo, namespaces or a
scanner:

```sh
python3 -I -S -B -X pycache_prefix=/tmp/osv-cgroup-test-cache-absent scripts/osv-offline/test-osv-cgroup.py
python3 -I -S -B -X pycache_prefix=/tmp/osv-launcher-test-cache-absent scripts/osv-offline/test-run-systemd-proof.py
python3 -I -S -B -X pycache_prefix=/tmp/osv-runtime-test-cache-absent scripts/osv-offline/test-prove-runtime.py
python3 -I -S -B -X pycache_prefix=/tmp/osv-supplier-test-cache-absent scripts/osv-offline/test-supply-bubblewrap.py
```
