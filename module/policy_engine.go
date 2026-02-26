package module

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// PolicyEngine is the interface implemented by all policy backends.
type PolicyEngine interface {
	Evaluate(ctx context.Context, input map[string]any) (*PolicyDecision, error)
	LoadPolicy(name, content string) error
	ListPolicies() []PolicyInfo
}

// PolicyDecision is the result of a policy evaluation.
type PolicyDecision struct {
	Allowed  bool           `json:"allowed"`
	Reasons  []string       `json:"reasons,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PolicyInfo describes a loaded policy.
type PolicyInfo struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	Content string `json:"content"`
}

// PolicyEngineModule is a workflow module wrapping a pluggable PolicyEngine backend.
// Supported backends: "mock", "opa", "cedar".
type PolicyEngineModule struct {
	name    string
	config  map[string]any
	backend string
	engine  PolicyEngine
}

// NewPolicyEngineModule creates a new PolicyEngineModule.
func NewPolicyEngineModule(name string, cfg map[string]any) *PolicyEngineModule {
	return &PolicyEngineModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *PolicyEngineModule) Name() string { return m.name }

// Init initialises the backend and registers the module as a service.
func (m *PolicyEngineModule) Init(app modular.Application) error {
	m.backend, _ = m.config["backend"].(string)
	if m.backend == "" {
		m.backend = "mock"
	}

	allowStub := isTruthy(m.config["allow_stub_backends"])

	switch m.backend {
	case "mock":
		m.engine = newMockPolicyEngine()
	case "opa":
		endpoint, _ := m.config["endpoint"].(string)
		m.engine = newOPAPolicyEngine(endpoint, allowStub)
		slog.Warn("WARNING: using stub policy engine — all requests will be DENIED. Set allow_stub_backends: true in config to use stub backends for testing.",
			"module", m.name, "backend", "opa", "allow_stub_backends", allowStub)
	case "cedar":
		m.engine = newCedarPolicyEngine(allowStub)
		slog.Warn("WARNING: using stub policy engine — all requests will be DENIED. Set allow_stub_backends: true in config to use stub backends for testing.",
			"module", m.name, "backend", "cedar", "allow_stub_backends", allowStub)
	default:
		return fmt.Errorf("policy.engine %q: unsupported backend %q", m.name, m.backend)
	}

	// Pre-load policies from config.
	if policies, ok := m.config["policies"].([]any); ok {
		for i, p := range policies {
			pm, ok := p.(map[string]any)
			if !ok {
				return fmt.Errorf("policy.engine %q: policies[%d] must be a map", m.name, i)
			}
			pname, _ := pm["name"].(string)
			content, _ := pm["content"].(string)
			if pname == "" {
				pname = fmt.Sprintf("policy-%d", i)
			}
			if err := m.engine.LoadPolicy(pname, content); err != nil {
				return fmt.Errorf("policy.engine %q: load policy %q: %w", m.name, pname, err)
			}
		}
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PolicyEngineModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Policy engine: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — policy.engine has no required service dependencies.
func (m *PolicyEngineModule) RequiresServices() []modular.ServiceDependency { return nil }

// Engine returns the underlying PolicyEngine for direct use by pipeline steps.
func (m *PolicyEngineModule) Engine() PolicyEngine { return m.engine }

// Backend returns the configured backend name.
func (m *PolicyEngineModule) Backend() string { return m.backend }

// ─── Mock backend ────────────────────────────────────────────────────────────

// mockPolicyEngine is an in-memory policy engine for testing.
// Each policy is stored as a string; evaluation checks for a "deny" keyword.
type mockPolicyEngine struct {
	mu       sync.RWMutex
	policies map[string]string
}

func newMockPolicyEngine() *mockPolicyEngine {
	return &mockPolicyEngine{policies: make(map[string]string)}
}

func (e *mockPolicyEngine) LoadPolicy(name, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies[name] = content
	return nil
}

func (e *mockPolicyEngine) ListPolicies() []PolicyInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PolicyInfo, 0, len(e.policies))
	for n, c := range e.policies {
		out = append(out, PolicyInfo{Name: n, Backend: "mock", Content: c})
	}
	return out
}

// Evaluate allows the request unless any loaded policy content contains "deny"
// or the input map contains key "action" with value "deny".
func (e *mockPolicyEngine) Evaluate(_ context.Context, input map[string]any) (*PolicyDecision, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check input for explicit deny action.
	if action, _ := input["action"].(string); action == "deny" {
		return &PolicyDecision{
			Allowed: false,
			Reasons: []string{"input action is deny"},
			Metadata: map[string]any{"backend": "mock", "input": input},
		}, nil
	}

	// Check each loaded policy for a "deny" keyword.
	for name, content := range e.policies {
		if containsString(content, "deny") {
			return &PolicyDecision{
				Allowed: false,
				Reasons: []string{fmt.Sprintf("policy %q contains deny rule", name)},
				Metadata: map[string]any{"backend": "mock", "matched_policy": name},
			}, nil
		}
	}

	return &PolicyDecision{
		Allowed:  true,
		Reasons:  []string{"default allow"},
		Metadata: map[string]any{"backend": "mock"},
	}, nil
}

// isTruthy returns true if v is a bool true, or a string "true"/"1"/"yes".
func isTruthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1" || val == "yes"
	}
	return false
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// ─── OPA backend stub ────────────────────────────────────────────────────────

// opaPolicyEngine is a stub for OPA (Open Policy Agent) integration.
// Production: POST to the OPA REST API at <endpoint>/v1/data/<policy-path>.
type opaPolicyEngine struct {
	endpoint   string
	allowStub  bool
	mu         sync.RWMutex
	policies   map[string]string
}

func newOPAPolicyEngine(endpoint string, allowStub bool) *opaPolicyEngine {
	if endpoint == "" {
		endpoint = "http://localhost:8181"
	}
	return &opaPolicyEngine{endpoint: endpoint, allowStub: allowStub, policies: make(map[string]string)}
}

func (e *opaPolicyEngine) LoadPolicy(name, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Production: PUT to <endpoint>/v1/policies/<name> with content as Rego source.
	e.policies[name] = content
	return nil
}

func (e *opaPolicyEngine) ListPolicies() []PolicyInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PolicyInfo, 0, len(e.policies))
	for n, c := range e.policies {
		out = append(out, PolicyInfo{Name: n, Backend: "opa", Content: c})
	}
	return out
}

func (e *opaPolicyEngine) Evaluate(_ context.Context, input map[string]any) (*PolicyDecision, error) {
	// Production: POST {"input": input} to <endpoint>/v1/data/<default-policy>
	// and parse the result body for {"result": {"allow": true}}.
	if e.allowStub {
		return &PolicyDecision{
			Allowed:  true,
			Reasons:  []string{"opa stub: allow_stub_backends enabled"},
			Metadata: map[string]any{"backend": "opa", "endpoint": e.endpoint, "input": input},
		}, nil
	}
	return &PolicyDecision{
		Allowed:  false,
		Reasons:  []string{"STUB IMPLEMENTATION - not connected to real backend - denied for safety"},
		Metadata: map[string]any{"backend": "opa", "endpoint": e.endpoint, "input": input},
	}, nil
}

// ─── Cedar backend stub ──────────────────────────────────────────────────────

// cedarPolicyEngine is a stub for Cedar policy language integration.
// Production: use the cedar-go library (github.com/cedar-policy/cedar-go).
type cedarPolicyEngine struct {
	allowStub bool
	mu        sync.RWMutex
	policies  map[string]string
}

func newCedarPolicyEngine(allowStub bool) *cedarPolicyEngine {
	return &cedarPolicyEngine{allowStub: allowStub, policies: make(map[string]string)}
}

func (e *cedarPolicyEngine) LoadPolicy(name, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Production: parse and compile Cedar policy set via cedar-go.
	e.policies[name] = content
	return nil
}

func (e *cedarPolicyEngine) ListPolicies() []PolicyInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PolicyInfo, 0, len(e.policies))
	for n, c := range e.policies {
		out = append(out, PolicyInfo{Name: n, Backend: "cedar", Content: c})
	}
	return out
}

func (e *cedarPolicyEngine) Evaluate(_ context.Context, input map[string]any) (*PolicyDecision, error) {
	// Production: build a cedar.Request from input (principal, action, resource, context)
	// and call policySet.IsAuthorized(request).
	if e.allowStub {
		return &PolicyDecision{
			Allowed:  true,
			Reasons:  []string{"cedar stub: allow_stub_backends enabled"},
			Metadata: map[string]any{"backend": "cedar", "input": input},
		}, nil
	}
	return &PolicyDecision{
		Allowed:  false,
		Reasons:  []string{"STUB IMPLEMENTATION - not connected to real backend - denied for safety"},
		Metadata: map[string]any{"backend": "cedar", "input": input},
	}, nil
}
