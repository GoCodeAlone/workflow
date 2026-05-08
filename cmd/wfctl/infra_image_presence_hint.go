package main

import (
	"errors"
	"io"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// imageNotInRegistryHintText is the actionable hint emitted to stderr when
// wfctl detects that a Plan or Apply error is (or wraps, including across a
// gRPC string-flattening boundary) interfaces.ErrImageNotInRegistry. The
// "::error::" prefix is the GitHub Actions error annotation; harmless in
// non-CI environments.
//
// The hint references "the build/deploy workflow" rather than a specific
// wfctl flag, because the recovery path is operator-driven (re-run a
// workflow that re-pushes images) — wfctl itself does not have an
// --image-sha override. Callers that DO have a re-dispatch override
// (e.g. tc2-cutover.yml's workflow_dispatch.inputs.image_sha) can layer
// their own UX on top.
const imageNotInRegistryHintText = "::error::image not found in registry — most likely the SHA was GC'd. Re-run the build/deploy workflow to push fresh images, then re-dispatch this operation.\n"

// emitImageNotInRegistryHint writes an actionable hint to w when err matches
// interfaces.ErrImageNotInRegistry — either via errors.Is (in-process driver
// path) or via verbatim message-string match (gRPC plugin path).
//
// The string-match fallback uses interfaces.ErrImageNotInRegistry.Error()
// directly so the two paths cannot drift. The interfaces package has a unit
// test asserting the message string is stable; that's the guardrail.
//
// Safe to call with err == nil or w == nil (no-op).
func emitImageNotInRegistryHint(w io.Writer, err error) {
	if err == nil || w == nil {
		return
	}
	if errors.Is(err, interfaces.ErrImageNotInRegistry) ||
		strings.Contains(err.Error(), interfaces.ErrImageNotInRegistry.Error()) {
		_, _ = io.WriteString(w, imageNotInRegistryHintText)
	}
}
