package debug

// BreakpointInterceptor is the interface that Pipeline executors use to
// check for breakpoints before each step. When a breakpoint fires,
// WaitForResume blocks the execution goroutine until an external actor
// (typically the HTTP debug API) sends a ResumeAction.
//
// The Pipeline executor can optionally hold a reference to this interface.
// If nil, no breakpoint checking occurs and execution proceeds normally.
type BreakpointInterceptor interface {
	// ShouldPause checks whether execution should pause before the given
	// pipeline step. The context map contains the current pipeline state.
	ShouldPause(pipeline, step string, context map[string]any) bool

	// WaitForResume blocks until a ResumeAction is received for the given
	// execution. It returns the action to take (continue, skip, abort, step_over)
	// along with any modified context data.
	WaitForResume(executionID, pipeline, step string, stepIndex int, context map[string]any) (ResumeAction, error)
}
