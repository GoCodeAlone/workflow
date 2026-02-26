package module

import (
	"context"
	"fmt"
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
// Supported backends: "mock". For OPA or Cedar, use external plugins.
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

	switch m.backend {
	case "mock":
		m.engine = newMockPolicyEngine()
	case "opa":
		return fmt.Errorf("opa backend not built-in; use the workflow-plugin-policy-opa external plugin")
	case "cedar":
		return fmt.Errorf("cedar backend not built-in; use the workflow-plugin-policy-cedar external plugin")
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

