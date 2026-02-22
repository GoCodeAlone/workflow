# ADR 003: Yaegi for Dynamic Components

## Status
Accepted

## Context
The engine needs runtime-loadable components for hot-reload and AI-generated code. Options: Yaegi (Go interpreter), HashiCorp go-plugin (gRPC), WebAssembly, or embedded scripting (Lua/JS).

## Decision
We chose Yaegi as the primary dynamic component system. We maintain our own fork at [GoCodeAlone/yaegi](https://github.com/GoCodeAlone/yaegi) (v0.17.0+) which includes community bug fixes, panic recovery in Eval/EvalPath, and stability improvements over the upstream traefik/yaegi. Components are Go source loaded at runtime in a sandboxed interpreter. Stdlib-only imports enforced. Interpreter pool for concurrency. File watcher for hot-reload. Field contracts for input/output validation.

## Consequences
**Positive**: Same language as engine; no compilation for hot-reload; sandbox prevents unsafe ops; AI generates familiar Go; sub-microsecond execution (~1.5us); creation cost (~2.4ms) amortized by pooling.

**Negative**: Stdlib-only restriction limits capabilities; Yaegi has edge cases; interpreter memory overhead. Mitigated by ModuleAdapter bridge, resource limits, contract validation, and go-plugin/Wasm as secondary options.
