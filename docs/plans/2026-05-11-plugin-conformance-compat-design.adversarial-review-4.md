### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-05-11-plugin-conformance-compat-design.md`
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [missing failure modes] Conformance evidence was bound primarily to a staged binary hash, while users install release archives. A maintainer could test one binary and publish a different archive with the same plugin version. Recommendation: make release archive SHA-256 the resolver's authoritative digest and keep binary SHA-256 as diagnostic metadata.
- [unstated assumptions] The design treated local source-tree conformance as equivalent to release artifact conformance. That collapses if packaging scripts omit files, mutate manifests, or build different platform binaries. Recommendation: add `wfctl plugin conformance --artifact <tar.gz>` and mark local-dir runs advisory unless later bound to archive checksums.
- [missing failure modes] The engine compatibility matrix used exact evidence plus min/latest checks but did not define how intermediate Workflow releases become trusted. Recommendation: store exact evidence by default; derive ranges only when the updater can enumerate and prove no failed engine exists inside the range.
- [repo-precedent conflicts] Configuration ownership for warn/enforce/ignore was undefined and risked scattering policy across registry metadata, environment variables, and installer flags. Recommendation: define CLI/env/project/global precedence and keep registry metadata limited to evidence trust.

**Findings (Minor):**
- [YAGNI] A `signature` field appeared in first-scope evidence without key, envelope, or canonical-byte semantics. Recommendation: reject signed trust mode until a separate signature ADR exists.
- [repo-precedent conflicts] The design referenced production `ExternalPluginManager` while also requiring conformance-specific timeout behavior. Recommendation: use a dedicated conformance launcher first, then share internals after semantics prove out.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Local source-tree tests were assumed to prove packaged release artifacts. |
| Repo-precedent conflicts | Finding | Policy ownership was not aligned with existing wfctl project/global config patterns. |
| YAGNI violations | Finding | Signature support appeared before trust envelope requirements existed. |
| Missing failure modes | Finding | Release archive substitution and intermediate engine gaps were not covered. |
| Security / privacy at architecture level | Clean | No new secret flow or network-exposed service in this design. |
| Rollback story | Clean | Resolver modes permit warning-only rollout and emergency ignore. |
| Simpler alternative not considered | Clean | Manifest-only minEngine was already compared and rejected because it cannot catch strict protocol drift. |
| User-intent drift | Clean | The design stays focused on scalable plugin/engine compatibility evidence for wfctl and plugins. |

**Options the author may not have considered:**
1. Archive-only evidence, no binary hash: simpler resolver state, but weaker diagnostics when a packaged archive contains multiple binaries or the staged binary differs by platform.
2. Per-release GitHub artifact attestations first: stronger provenance, but pushes this work into a signature/attestation design before the immediate compatibility workflow can ship.

**Verdict reasoning:** FAIL because install decisions must bind to the artifact users actually download, and because trusted range semantics and policy ownership must be explicit before implementation.
