package wftest

import (
	"encoding/json"
	"time"
)

// Result holds the outcome of a pipeline execution or HTTP request.
type Result struct {
	Output      map[string]any            // Final pipeline output
	StepResults map[string]map[string]any // Per-step outputs
	Error       error
	Duration    time.Duration

	// HTTP-specific (populated for HTTP triggers)
	StatusCode int
	Headers    map[string]string
	RawBody    []byte
}

// StepOutput returns the output map for a specific step.
func (r *Result) StepOutput(name string) map[string]any {
	if r.StepResults == nil {
		return nil
	}
	return r.StepResults[name]
}

// StepExecuted returns whether a step was executed.
func (r *Result) StepExecuted(name string) bool {
	_, ok := r.StepResults[name]
	return ok
}

// StepCount returns the number of steps that were executed.
func (r *Result) StepCount() int {
	return len(r.StepResults)
}

// StepOutputs returns all per-step output maps keyed by step name.
func (r *Result) StepOutputs() map[string]map[string]any {
	return r.StepResults
}

// Header returns the value of the named HTTP response header.
func (r *Result) Header(key string) string {
	return r.Headers[key]
}

// JSON parses the HTTP response body as JSON.
func (r *Result) JSON() map[string]any {
	if r.RawBody == nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(r.RawBody, &m)
	return m
}
