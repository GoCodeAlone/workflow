package wftest

import (
	"context"
	"maps"
	"sync"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// StepHandler is invoked by a mock step during pipeline execution.
// config is the step's YAML config; input is the merged pipeline state (pc.Current).
type StepHandler interface {
	Handle(ctx context.Context, config, input map[string]any) (map[string]any, error)
}

// StepHandlerFunc adapts a plain function to StepHandler.
type StepHandlerFunc func(ctx context.Context, config, input map[string]any) (map[string]any, error)

func (f StepHandlerFunc) Handle(ctx context.Context, config, input map[string]any) (map[string]any, error) {
	return f(ctx, config, input)
}

// Returns creates a StepHandler that always returns the given output map.
func Returns(output map[string]any) StepHandler {
	return StepHandlerFunc(func(_ context.Context, _, _ map[string]any) (map[string]any, error) {
		return output, nil
	})
}

// StepContext holds the full execution context passed to a mock step handler.
type StepContext struct {
	// Ctx is the context from the pipeline execution.
	Ctx context.Context
	// Config is the step's YAML config from BuildFromConfig.
	Config map[string]any
	// Input is the merged pipeline state (pc.Current) at execution time.
	Input map[string]any
}

// Call records one invocation of a mock step.
type Call struct {
	StepContext
}

// Recorder is a StepHandler that records every call made to a mock step.
// Use NewRecorder to create one, then pass it to MockStep.
// Recorder also implements StepHandler and Option so a Recorder returned by
// RecordStep can be passed directly to New without a separate MockStep call.
type Recorder struct {
	mu       sync.Mutex
	stepType string // set by RecordStep; enables Recorder to implement Option
	calls    []Call
	output   map[string]any
}

// NewRecorder creates a Recorder that returns an empty output map by default.
func NewRecorder() *Recorder { return &Recorder{} }

// RecordStep creates a Recorder bound to stepType and returns it. The returned
// *Recorder implements Option, so it can be passed directly to New:
//
//	rec := wftest.RecordStep("step.db_query")
//	h   := wftest.New(t, wftest.WithYAML(`...`), rec)
//	h.ExecutePipeline("fetch", nil)
//	t.Logf("called %d times", rec.CallCount())
func RecordStep(stepType string) *Recorder {
	return &Recorder{stepType: stepType}
}

// applyTo implements Option. It registers the Recorder as a mock for its
// bound step type so *Recorder returned by RecordStep can be passed to New.
func (r *Recorder) applyTo(h *Harness) {
	if h.mockSteps == nil {
		h.mockSteps = make(map[string]StepHandler)
	}
	h.mockSteps[r.stepType] = r
}

// WithOutput sets fixed output that the recorder returns on every call.
func (r *Recorder) WithOutput(output map[string]any) *Recorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = output
	return r
}

// Handle records the call and returns configured output (or empty map).
func (r *Recorder) Handle(ctx context.Context, config, input map[string]any) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{StepContext: StepContext{Ctx: ctx, Config: config, Input: input}})
	if r.output != nil {
		return r.output, nil
	}
	return map[string]any{}, nil
}

// Calls returns a snapshot of all recorded invocations.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Call, len(r.calls))
	copy(out, r.calls)
	return out
}

// CallCount returns the number of times the step was called.
func (r *Recorder) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// Reset clears all recorded calls.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = r.calls[:0]
}

// mockStep is a PipelineStep that delegates to a StepHandler.
type mockStep struct {
	name    string
	config  map[string]any
	handler StepHandler
}

func (s *mockStep) Name() string { return s.name }

func (s *mockStep) Execute(ctx context.Context, pc *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	input := make(map[string]any, len(pc.Current))
	maps.Copy(input, pc.Current)

	output, err := s.handler.Handle(ctx, s.config, input)
	if err != nil {
		return nil, err
	}
	return &interfaces.StepResult{Output: output}, nil
}

// newMockStepFactory returns a module.StepFactory that always calls handler.
func newMockStepFactory(handler StepHandler) module.StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (module.PipelineStep, error) {
		return &mockStep{name: name, config: config, handler: handler}, nil
	}
}

// MockModule is a minimal modular.Module that exposes a service in the
// app's service registry under a given name. Use it so steps that look up
// a named dependency (e.g. a database connection) find this mock instead of
// a real module.
type MockModule struct {
	name    string
	service any
}

// NewMockModule creates a MockModule that registers service under name.
func NewMockModule(name string, service any) *MockModule {
	return &MockModule{name: name, service: service}
}

// Name satisfies modular.Module.
func (m *MockModule) Name() string { return m.name }

// Init registers the service in the application's service registry.
func (m *MockModule) Init(app modular.Application) error {
	return app.RegisterService(m.name, m.service)
}

// ProvidesServices advertises the service for dependency wiring.
func (m *MockModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "mock", Instance: m.service},
	}
}

// RequiresServices returns nil — mock modules have no dependencies.
func (m *MockModule) RequiresServices() []modular.ServiceDependency { return nil }
