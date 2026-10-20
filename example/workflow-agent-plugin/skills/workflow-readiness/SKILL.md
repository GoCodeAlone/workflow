---
name: workflow-readiness
description: Use when a user requests source-linked and version-linked validation evidence, compatibility assessment or readiness review for a GoCodeAlone Workflow configuration.
---

# Produce Workflow readiness evidence

First read and follow the [shared contract](../../references/workflow-contract.md).
This is an evidence review, not permission to deploy or a certification.

1. Identify the public source repository, exact immutable commit/path, intended
   target version and exact authorized sanitized single-document snapshot.
   Distinguish user reports from observed data. Missing source identity remains
   unknown; never substitute a mutable branch name for a commit.
2. Record snapshot SHA256 only if safely computed from the exact tested UTF-8
   bytes; otherwise mark unavailable. State redacted/partial status and keep
   equivalence to the original unredacted app unknown unless separately proved.
   Imported, external-file, multi-document and uncertain inputs have incomplete
   coverage. Do not silently flatten or retrieve sensitive inputs.
3. Discover server name/version, live tools and relevant schemas/inventories.
   Check basic validation, deep validation, referenced types, declared plugin
   prerequisites, inspection and current-registry compatibility separately when
   available. Do not accept a supplied `valid:true` as proof of skipped checks.
   Requested release compatibility remains unknown when unsupported by the tool.
4. Use the shared contract's per-check table. Link the public Workflow source at
   the observed release and the supplied config source at its immutable commit.
   Label every row observed, user-reported, inferred, unavailable or not checked.
   Summarize warnings safely; do not publish raw configs/errors or secret values.
5. End with missing proof and the smallest next check, without a global app PASS
   or green deployment badge. Runtime acceptance, resolved inputs/dependencies,
   plugin binary/version health and clean desktop activation require independent
   evidence. Do not start apps, install/upgrade tools, deploy, publish a report,
   or modify host settings as part of this skill.
