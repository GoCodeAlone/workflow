// Package secrets provides secret storage backends (Provider), secret://
// reference resolution (Resolver), and two complementary redaction utilities.
//
// # Two redaction categories — pick the right one
//
// The package offers two DISTINCT ways to scrub secret material, addressing two
// different threat shapes. They are NOT interchangeable:
//
//   - KEY-name masking — MaskSensitiveOutputs (masking.go). Given a structured
//     outputs map (e.g. a step's key/value results) and a set of sensitive KEY
//     names ("password", "token", "connection_string", ...), it replaces the
//     VALUES of those keys with "(sensitive)". It never scans free text and
//     never needs to know the secret's value — only its key. Use it for
//     structured maps whose key set is known and trusted.
//
//   - VALUE scanning — Redactor (redactor.go). Given an arbitrary blob of free
//     text (a log line, a chat message, an LLM prompt body) and a set of
//     previously-seen secret VALUES, it replaces every substring equal to a
//     known value with "[REDACTED:label]". It does not care about KEY names in
//     the text; it matches purely on value. Use it when the secret may appear
//     anywhere in free text and only its value is known.
//
// A typical engine integration arms a Redactor from a Provider (List+Get) at
// startup and on hot-swap, then runs Redact over every message/transcript body
// before it leaves the process. Structured step outputs flow through
// MaskSensitiveOutputs instead.
//
// # Redactor graceful-degrade contract
//
// Redactor.LoadFromProvider is designed to be wired BEFORE its backing provider
// is reachable. If the provider's List call fails (read-only-without-list,
// not-yet-started, temporarily unavailable) LoadFromProvider returns nil and
// leaves the existing known-value set UNTOUCHED — it never panics and never
// empties the redactor on a List error. This lets a caller register a Redactor
// at construction time and re-arm it once the provider comes online (lazy
// resolve + hot-swap) without risking a window where the redactor is empty.
//
// AddValue is the additive path: it merges a single value into the set without
// disturbing the rest. LoadFromProvider is the bulk/full-replace path: it
// swaps the entire set from the provider's current state. The two compose —
// callers may seed a few values via AddValue and then refresh from a provider.
package secrets
