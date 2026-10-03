package interfaces

import "context"

// ResourceSensitiveInputDeclarer is an optional driver capability declaring
// sensitive Config leaves as canonical RFC 6901 pointers. A whole "*" segment
// matches one map key or list index. Declarations contain no secret values.
// The engine validates present leaves as references before runtime resolution.
// Remote drivers return ErrProviderMethodUnimplemented when the capability is
// absent; other errors must stop dispatch rather than omit input validation.
type ResourceSensitiveInputDeclarer interface {
	SensitiveInputPaths(ctx context.Context) ([]string, error)
}
