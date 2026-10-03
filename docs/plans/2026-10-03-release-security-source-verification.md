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
Vitest and browser-data changes are resolver-required family closure; no
unrelated direct manifest or ecommerce-lock changes. Build chunk-size warning
remains, not suppressed or refactored in this security boundary.

## Remaining Gates

Final four binaries must be rebuilt from the final committed plain checkout
and report exact VCS identity/Go1.26.8, not the enclosing workspace revision.
Native OSV2.6 complete production JSON/analysis is pending. Real native called,
uncalled and restored OpenPGP control already proves reporter rejection; it is
a labeled scanner control, not an application demo. No ignores/exceptions.
Independent final UI/whole-boundary review, exact-head PR/CI, merge and retro
remain. Protected .github/scripts/Dockerfile/policytool bytes are unchanged.

The separately pinned old CI scanner cannot analyze the root floor; local
source proof does not silently repair public scanner authority. Its failure
must stay visible. No tags/releases or Signal Task35 completion here. Current
editor0.85.4 also requires its own compatible patch outside this plan.
