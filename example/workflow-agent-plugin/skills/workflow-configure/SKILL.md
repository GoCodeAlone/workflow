---
name: workflow-configure
description: Use when a user wants to create, configure or validate a GoCodeAlone Workflow app using public wfctl capabilities.
---

# Configure a Workflow app

First read and follow the [shared contract](../../references/workflow-contract.md).
Use this for Workflow YAML configuration, not generic feature development.

1. Establish the app's intended behavior, target engine version and minimal
   sanitized input. Reuse supplied context; ask only for missing requirements.
   Check the single-document/import/prerequisite boundary before interpreting
   validation. Start from public built-in types when no external plugin is needed.
2. Discover the connected server/version and live tool schemas. List the relevant
   module/step/trigger/workflow inventories, then fetch schemas for the chosen
   types. Never copy argument names from an unverified reference example.
3. Produce the smallest editable YAML satisfying the request. A skeleton is a
   starting point and may contain empty required fields. Populate from observed
   schemas; keep credentials as references and avoid private domains/accounts.
   The bundled `fixtures/minimal.yaml.txt` is a secret-free server config, not a
   functioning application endpoint or deployment.
4. Run basic and deep checks, compare relevant type inventories and declared
   prerequisites, then inspect the same sanitized snapshot. Run current-registry
   compatibility if available; unsupported target-version claims stay unknown.
5. Return the proposed config and compact evidence table from the shared
   contract. State gaps and the smallest next proof. If a tool/version/executable
   is missing, give the useful partial proposal and mark dependent checks
   unavailable. Do not claim runtime acceptance, install anything, start the app,
   generate deployment assets or alter active host configuration.
