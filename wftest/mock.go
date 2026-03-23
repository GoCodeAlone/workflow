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

// Call records one invocation of a mock step.
type Call struct {
	// Config is the step's YAML config from BuildFromConfig.
	Config map[string]any
	// Input is the merged pipeline state (pc.Current) at execution time.
	Input map[string]any
}

// Recorder is a StepHandler that records every call made to a mock step.
// Use NewRecorder to create one, then pass it to MockStep.
// Recorder also implements StepHandler so it can return fixed output.
type Recorder struct {
	mu     sync.Mutex
	calls  []Call
	output map[string]any
}

// NewRecorder creates a Recorder that returns an empty output map by default.
func NewRecorder() *Recorder { return &Recorder{} }

// WithOutput sets fixed output that the recorder returns on every call.
func (r *Recorder) WithOutput(output map[string]any) *Recorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = output
	return r
}

// Handle records the call and returns configured output (or empty map).
func (r *Recorder) Handle(_ context.Context, config, input map[string]any) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{Config: config, Input: input})
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
