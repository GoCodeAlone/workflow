# ADR 002: CrisisTextLine/modular Framework

## Status
Accepted

## Context
The workflow engine needs a module lifecycle framework providing dependency injection, configuration management, and service registry. Options: GoCodeAlone/modular (v1.4.3), CrisisTextLine/modular (v1.11.11), custom framework, or Wire/Dig.

## Decision
We chose CrisisTextLine/modular (v1.11.11). It provides module lifecycle (RegisterConfig, Init, Start, Stop), service registry with name and interface matching, config feeders (YAML/JSON/TOML/env), multi-tenancy with context-based tenant propagation, and 14 pre-built modules.

## Consequences
**Positive**: Rich module ecosystem reduces boilerplate; multi-tenancy built-in; config feeders support multiple sources; interface-based service matching enables loose coupling.

**Negative**: Dependency on external fork; learning curve for lifecycle conventions; global state in ConfigFeeders requires careful test isolation. Mitigated by migration guides, using app.SetConfigFeeders() in tests, and pinning to v1.11.11.
