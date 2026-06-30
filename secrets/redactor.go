package secrets

import (
	"context"
	"strings"
	"sync"
)

// Redactor is a concurrency-safe, free-text VALUE scanner that replaces any
// occurrence of a known secret value inside an arbitrary string with the token
// "[REDACTED:label]". It is the engine's shared utility for scrubbing secret
// material out of human-facing text (logs, transcripts, agent messages, LLM
// prompts) where the secret's KEY is not known ahead of time — only its VALUE.
//
// # VALUE-scan (Redactor) vs KEY-mask (MaskSensitiveOutputs)
//
// The secrets package provides two DISTINCT redaction categories; do not
// conflate them:
//
//   - MaskSensitiveOutputs (masking.go) is a KEY-name mask: given a structured
//     outputs map, it zeroes the VALUES of a fixed set of sensitive KEYS
//     ("password", "token", ...). It does not scan free text and does not need
//     to know the secret value.
//
//   - Redactor (this type) is a VALUE scan: given an arbitrary blob of text, it
//     finds substrings equal to a previously-seen secret VALUE and replaces
//     them. It does not care about KEY names in the text; it matches on value.
//
// Use MaskSensitiveOutputs for structured key/value maps whose key set is
// known. Use Redactor when the secret may appear anywhere in free text (chat
// messages, log lines, prompt bodies) and only its value is known.
//
// # Graceful-degrade contract
//
// LoadFromProvider degrades gracefully: if the provider's List call fails (for
// example because the provider is read-only-without-list, unavailable, or not
// yet started) LoadFromProvider returns nil and leaves the existing known-value
// set untouched rather than panicking or emptying the redactor. This lets a
// caller wire a Redactor before its backing provider is reachable and re-arm
// it once the provider comes online. A nil Redactor is also safe to call:
// NewRedactor is not required for zero-value usage, but the helper makes the
// intent explicit and pre-allocates the map.
type Redactor struct {
	mu    sync.RWMutex
	known map[string]string // label -> secret value
}

// NewRedactor returns a ready-to-use Redactor with an empty known-value set.
// The returned value is safe for concurrent use immediately.
func NewRedactor() *Redactor {
	return &Redactor{known: make(map[string]string)}
}

// AddValue registers a single secret value under the given label and merges it
// additively into the existing known-value set. Subsequent calls to Redact will
// replace every occurrence of value with "[REDACTED:label]".
//
// If value is the empty string the call is a no-op: empty values cannot be
// meaningfully scanned for as substrings and would over-match.
//
// AddValue is concurrency-safe and may be called concurrently with Redact and
// LoadFromProvider.
func (r *Redactor) AddValue(label, value string) {
	if value == "" {
		return
	}
	r.mu.Lock()
	if r.known == nil {
		r.known = make(map[string]string)
	}
	r.known[label] = value
	r.mu.Unlock()
}

// LoadFromProvider performs a FULL-REPLACE of the known-value set from a
// secrets.Provider: it enumerates the provider's keys via List, resolves each
// via Get, builds a fresh map off-lock, then swaps it in under a single write
// lock. Any value previously added via AddValue that is not present in the
// provider is dropped.
//
// LoadFromProvider degrades gracefully (see the graceful-degrade contract on
// Redactor): if List returns an error the call returns nil and leaves the
// existing known-value set untouched — it does NOT panic and does NOT empty the
// redactor. Individual Get errors for a key skip that key but do not fail the
// whole load. Empty-string values from the provider are skipped.
//
// A nil Redactor is tolerated: LoadFromProvider is a no-op.
func (r *Redactor) LoadFromProvider(ctx context.Context, p Provider) error {
	if r == nil || p == nil {
		return nil
	}
	keys, err := p.List(ctx)
	if err != nil {
		// Graceful-degrade: leave the existing set untouched.
		return nil
	}
	fresh := make(map[string]string, len(keys))
	for _, k := range keys {
		v, gerr := p.Get(ctx, k)
		if gerr != nil {
			continue
		}
		if v == "" {
			continue
		}
		fresh[k] = v
	}
	r.mu.Lock()
	r.known = fresh
	r.mu.Unlock()
	return nil
}

// Redact returns text with every occurrence of each known secret value replaced
// by "[REDACTED:label]". The scan is a plain substring match (not a regexp):
// it is safe to use on untrusted input but callers should not rely on it for
// canonicalization — only for scrubbing.
//
// Redact is concurrency-safe and may be called concurrently with AddValue and
// LoadFromProvider. A nil Redactor returns the input text unchanged.
func (r *Redactor) Redact(text string) string {
	if r == nil {
		return text
	}
	r.mu.RLock()
	if len(r.known) == 0 {
		r.mu.RUnlock()
		return text
	}
	out := text
	for label, value := range r.known {
		if value == "" || !strings.Contains(out, value) {
			continue
		}
		out = strings.ReplaceAll(out, value, "[REDACTED:"+label+"]")
	}
	r.mu.RUnlock()
	return out
}

// RedactMessage redacts content and reports whether any substitution occurred.
// It is the convenience wrapper for callers (e.g. message pipelines) that want
// to know whether a message was altered without diffing the strings. A nil
// Redactor returns (content, false).
func (r *Redactor) RedactMessage(content string) (string, bool) {
	redacted := r.Redact(content)
	return redacted, redacted != content
}
