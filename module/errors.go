package module

import "errors"

// ErrNotImplemented is returned by pipeline steps that are defined but not yet
// backed by a real execution engine (e.g. steps that require sandbox.DockerSandbox).
// Callers should treat this as a hard failure; relying on a stub step would give
// false confidence in CI/CD pipelines.
var ErrNotImplemented = errors.New("step not implemented: requires sandbox.DockerSandbox (not yet available)")
