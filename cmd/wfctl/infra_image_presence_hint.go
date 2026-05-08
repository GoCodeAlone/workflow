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
const imageNotInRegistryHintText = "::error::image not found in registry — most likely the SHA was GC'd. Re-run the build/deploy workflow to push fresh images, or pass --image-sha override.\n"

// errImageNotInRegistryMessage is the exact string in interfaces.ErrImageNotInRegistry.
// We match it verbatim as a fallback for plugin (gRPC) drivers where structpb
// does not preserve sentinel identity. The interfaces package has a unit test
// asserting this string is stable.
const errImageNotInRegistryMessage = "iac: image tag or digest not found in registry"

// emitImageNotInRegistryHint writes an actionable hint to w when err matches
// interfaces.ErrImageNotInRegistry — either via errors.Is (in-process driver
// path) or via verbatim message-string match (gRPC plugin path).
//
// Safe to call with err == nil (no-op).
func emitImageNotInRegistryHint(w io.Writer, err error) {
	if err == nil || w == nil {
		return
	}
	if errors.Is(err, interfaces.ErrImageNotInRegistry) ||
		strings.Contains(err.Error(), errImageNotInRegistryMessage) {
		_, _ = io.WriteString(w, imageNotInRegistryHintText)
	}
}
