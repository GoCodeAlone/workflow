# ADR 004: Hybrid AI Approach

## Status
Accepted

## Context
The engine integrates AI for component generation, response suggestions, classification, and sentiment analysis. Options: Anthropic Claude only, GitHub Copilot SDK only, hybrid, or self-hosted models.

## Decision
Hybrid approach: Anthropic Claude (direct API with tool use) for server-side operations and GitHub Copilot SDK for developer tooling. All AI features have rule-based/template fallbacks when AI is unavailable.

## Consequences
**Positive**: No single-provider dependency; template fallback ensures features work offline; direct API gives full prompt control; each provider used for its strengths.

**Negative**: Two integrations to maintain; Copilot SDK requires CLI binary; API costs; template fallbacks less capable. Mitigated by configurable provider selection, suggestion caching, and graceful degradation.
