# ADR 001: YAML-Driven Configuration

## Status
Accepted

## Context
The workflow engine needs a way to define applications. The two main approaches are code-driven (users write Go) or config-driven (declarative files). We need to support non-Go users, enable rapid prototyping, and allow runtime reconfiguration without recompilation.

## Decision
We chose YAML as the primary configuration format. All module composition, workflow definitions, trigger configuration, and runtime behavior are specified in YAML files. One file per application (or per service in distributed mode).

## Consequences
**Positive**: Non-developers can create applications; configs are validatable via JSON Schema; enables visual builder UI with YAML import/export; runtime reconfiguration without recompilation; 27 example configs as documentation; easy to version control and diff.

**Negative**: Complex logic requires dynamic components (Yaegi); YAML syntax errors can be cryptic; some configs are verbose vs code. Mitigated by JSON Schema validation, wfctl validate CLI, example configs, and visual builder UI.
