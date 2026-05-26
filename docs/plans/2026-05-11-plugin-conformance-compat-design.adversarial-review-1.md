### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-05-11-plugin-conformance-compat-design.md
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [user-intent drift / repo-precedent conflict] The design promised install-time compatible-version sorting while deferring the version index required to do it. Current `RegistrySource.FetchManifest`, `MultiRegistry.FetchManifest`, and `runPluginInstall` resolve one manifest and rewrite URLs for requested versions.
- [security / missing failure modes] Compatibility evidence would affect install decisions but had no artifact digest binding, provenance, signature, or workflow identity.
- [repo-precedent conflict / user-intent drift] The design introduced `legacy-module` and `--strict=false` transition behavior without reconciling recent strict IaC hard-cutover precedent.
- [missing failure modes] Evidence matching did not define precedence across engine version, `wfctl` version, mode, platform, artifact digest, and failure status.
- [rollback gap / repo-precedent conflict] Forced incompatible installs require lockfile schema support, and the rollback feature flag was unnamed.

**Findings (Minor):**
- [YAGNI / API shape] Positional `<plugin-dir>` plus `--plugin-dir` was redundant.
- [missing failure modes] “Low-risk methods” for typed IaC conformance was undefined.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | CI-generated evidence trust and registry version history were assumed. |
| Repo-precedent conflicts | Finding | Current registry API is single-manifest; strict IaC precedent was not explicit enough. |
| YAGNI violations | Finding | Legacy modes and strict-disable knob were premature. |
| Missing failure modes | Finding | Evidence matching and non-mutating IaC boundaries were underspecified. |
| Security / privacy | Finding | Evidence lacked provenance and digest binding. |
| Rollback story | Finding | Lockfile and feature-flag rollback were not concrete. |
| Simpler alternative not considered | Finding | Registry-native compatibility matrix should be a first primitive. |
| User-intent drift | Finding | Install sorting was not actually delivered by the first design slice. |

**Verdict reasoning:** FAIL. The revised design must make version indexes part of the first scope, make unsigned/unbound evidence advisory only, remove legacy IaC compatibility ambiguity, define evidence precedence, and specify lockfile/rollback behavior.
