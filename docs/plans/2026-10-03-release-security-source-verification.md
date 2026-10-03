# Source Security Verification Checkpoint

2026-10-03; source-only boundary, not scanner rollout or release promotion.

## Go And Host Evidence

- Exact Go1.26.5 baseline govulncheck called findings retained; patched native
  Go1.26.8 has zero called findings. Unchanged isolated policytool at exact
  Go1.26.5 also has zero called findings, with module-only advisories disclosed.
- Cache/compiler/env mutation RED/restored GREEN; focused race5.124s and
  Windows test compile PASS. Independent spec/quality review Tasks1/2 PASS.
- Fresh plain40d5d6e6 source, nonrootUID1000 Docker-init, cold compiler cache:
  exact full race/coverage all152 packages PASS, wfctl519.387s within unchanged
  default600s timeout. Root permission bypass and no-init orphan zombies were
  separately reproduced environmental defects, not production/test changes.
- Frozen unchanged policy mutation suite PASS. Actual server returns an
  unpredictable run-specific HTTP identity and shuts down cleanly. Strict live
  Docker builtin/sandbox proof36.661s PASS, digest-pinned image and empty cleanup.
- Fresh plain6051d68d source/native LinuxGo1.26.8: real registry default HTTPS
  proxy fixture forward/no-op/rollback control and all30 denials PASS30.169s;
  listener/process cleanup and clean source read back. Dependency GitHub API is
  fixture-backed, not live immutable-release evidence.

## Real UI Consumer

Direct YAML and actual public editor parseYaml imports: old dependencies RED;
compatible released editor0.2.1 GREEN. Old editor reinstall reproduces the
budget failure. Existing ^0.2.0 range/manifest unchanged. Package registry SRI
and installed gitHead6ed54b08f573cf701d8ba0ee35c16879475e29e4 match the verified
maintenance release, not a local tarball or source replacement.

Node24.14.0/npm11.9.0: clean npmci, focused10, full141 tests, targeted lint,
types/Vite build all PASS. Installed ten-family/duplicate audit PASS. Babel,
Vitest and browser-data changes retain required family alignment. Independent
review found chai/tinyrainbow refreshes unnecessary; an isolated npm resolver
restored base6.2.2/3.1.0, with only those two lock entries changed and no manifest
changes. Fresh clean install/full141 tests/types/build/targeted lint and all ten
installed-family checks PASS after restoration. No ecommerce-lock changes.
Build chunk-size warning
remains, not suppressed or refactored in this security boundary.

## Final Source And Native Scanner

Plain committed550c0f7a binaries all report exact VCS identity, unmodified source
and Go1.26.8; server/wfctl link OTel1.45.0. Shipped Darwin/Linux/Windows import
lists contain no OpenPGP. Actual credential-free server smoke returns the
owned unpredictable identity, exits0 on SIGTERM and leaves no listener.
Fresh plaincf28e5a4 four-artifact metadata also passes with exact revision,
unmodified source/Go1.26.8/OTel1.45.0; owned server smoke returns HTTP200,
loopback-only listener, clean SIGTERM0 and endpoint absence. Go source/modules
remain byte-identical to cold-suite40d5d6e6. Final evidence-doc commits require
only exact-head warm relink/readback, not a repeat cold compiler test.

Original native production scan: OOM137 at7GiB after63.224s, no results or
reporter, not accepted. Same550c0f7a paired resource trial with container-local
GOMEMLIMIT=4GiB completes scanner0/reporter0. Final preserved-resolutioncf28e5a4
full recursive scan completes scanner0/reporter0 in96s, no loading/analysis
errors and no OOM. Image/entrypoint/analysis command unchanged; no exclusions,
ignores, authentication/Go compiler override or repository policy changes.
This local GC budget is not authorized CI configuration; policytool GO* env
restriction remains unchanged.

All14 committed Go modules are discovered. Raw JSON retains root/example
x/crypto0.57.0 GO-2026-5932 with genuine called:false. Other12 modules have no
reported affected groups and therefore no upstream default native-analysis
trigger; do not label them fourteen govulncheck invocations. Exact Go1.26.8
shipped-code and Go1.26.5 policytool proofs independently retain stdlib/module
classification. Current-run source/evidence custody, image/entrypoint, all14
discovery, classification/no-error and reporter assertions pass. A throwaway
collector's entrypoint-shape assertion was corrected from actual Docker/image
readback; raw scanner output was never changed.

Same corrected environment detects the existing real OpenPGP API call as
called:true; scanner1 and unchanged --fail-on-vuln=true reporter1 are expected
rejection, without analysis errors. Original no-import reversal/restored-called
evidence retained. This is a labeled scanner control, not an application demo.

## Remaining Integration Gates
Independent final UI/whole-boundary review SHIP-IT, no Critical/Important
findings; the sole Minor unnecessary-resolution finding is addressed above.
See the committed code-review disposition. Exact-head PR/CI, merge and retro
remain. Protected .github/scripts/Dockerfile/policytool bytes are unchanged.

The separately pinned old CI scanner cannot analyze the root floor; local
source proof does not silently repair public scanner authority. Its failure
must stay visible. No tags/releases or Signal Task35 completion here. Current
editor0.85.4 also requires its own compatible patch outside this plan.
