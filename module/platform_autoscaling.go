package module

import (
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScalingPolicy describes a single autoscaling policy.
type ScalingPolicy struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`           // target_tracking, step, scheduled
	TargetResource  string  `json:"targetResource"` // ECS service, K8s deployment, etc.
	MinCapacity     int     `json:"minCapacity"`
	MaxCapacity     int     `json:"maxCapacity"`
	MetricName      string  `json:"metricName,omitempty"`
	TargetValue     float64 `json:"targetValue,omitempty"`
	Schedule        string  `json:"schedule,omitempty"` // cron expression
	DesiredCapacity int     `json:"desiredCapacity,omitempty"`
}

// ScalingPlan describes the changes needed to reach desired autoscaling state.
type ScalingPlan struct {
	Policies []ScalingPolicy `json:"policies"`
	Changes  []string        `json:"changes"`
}

// ScalingState represents the current state of the autoscaling configuration.
type ScalingState struct {
	ID              string          `json:"id"`
	Policies        []ScalingPolicy `json:"policies"`
	CurrentCapacity int             `json:"currentCapacity"`
	Status          string          `json:"status"` // pending, active, updating, deleted
}

// autoscalingBackend is the internal interface for autoscaling backends.
type autoscalingBackend interface {
	plan(m *PlatformAutoscaling) (*ScalingPlan, error)
	apply(m *PlatformAutoscaling) (*ScalingState, error)
	status(m *PlatformAutoscaling) (*ScalingState, error)
	destroy(m *PlatformAutoscaling) error
}

// PlatformAutoscaling manages autoscaling policies via pluggable backends.
// Config:
//
//	account:  name of a cloud.account module (optional for mock)
//	provider: mock | aws
//	policies: list of scaling policy definitions
type PlatformAutoscaling struct {
	name    string
	config  map[string]any
	account string
	state   *ScalingState
	backend autoscalingBackend
}

// NewPlatformAutoscaling creates a new PlatformAutoscaling module.
func NewPlatformAutoscaling(name string, cfg map[string]any) *PlatformAutoscaling {
	return &PlatformAutoscaling{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformAutoscaling) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformAutoscaling) Init(app modular.Application) error {
	m.account, _ = m.config["account"].(string)
	if m.account != "" {
		if _, ok := app.SvcRegistry()[m.account]; !ok {
			return fmt.Errorf("platform.autoscaling %q: account service %q not found", m.name, m.account)
		}
	}

	provider, _ := m.config["provider"].(string)
	if provider == "" {
		provider = "mock"
	}

	switch provider {
	case "mock":
		m.backend = &mockAutoscalingBackend{}
	case "aws":
		m.backend = &awsAutoscalingBackend{}
	default:
		return fmt.Errorf("platform.autoscaling %q: unsupported provider %q", m.name, provider)
	}

	m.state = &ScalingState{
		ID:              "",
		CurrentCapacity: 0,
		Status:          "pending",
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformAutoscaling) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Autoscaling: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name.
func (m *PlatformAutoscaling) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the proposed autoscaling changes.
func (m *PlatformAutoscaling) Plan() (*ScalingPlan, error) {
	return m.backend.plan(m)
}

// Apply provisions or updates the autoscaling policies.
func (m *PlatformAutoscaling) Apply() (*ScalingState, error) {
	return m.backend.apply(m)
}

// Status returns the current autoscaling state.
func (m *PlatformAutoscaling) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy removes all autoscaling policies.
func (m *PlatformAutoscaling) Destroy() error {
	return m.backend.destroy(m)
}

// policies parses policies from config.
func (m *PlatformAutoscaling) policies() []ScalingPolicy {
	raw, ok := m.config["policies"].([]any)
	if !ok {
		return nil
	}
	var policies []ScalingPolicy
	for _, item := range raw {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		pType, _ := p["type"].(string)
		targetResource, _ := p["target_resource"].(string)
		minCap, _ := intFromAny(p["min_capacity"])
		maxCap, _ := intFromAny(p["max_capacity"])
		metricName, _ := p["metric_name"].(string)
		schedule, _ := p["schedule"].(string)
		desiredCap, _ := intFromAny(p["desired_capacity"])

		var targetValue float64
		switch v := p["target_value"].(type) {
		case float64:
			targetValue = v
		case int:
			targetValue = float64(v)
		}

		policies = append(policies, ScalingPolicy{
			Name:            name,
			Type:            pType,
			TargetResource:  targetResource,
			MinCapacity:     minCap,
			MaxCapacity:     maxCap,
			MetricName:      metricName,
			TargetValue:     targetValue,
			Schedule:        schedule,
			DesiredCapacity: desiredCap,
		})
	}
	return policies
}

// ─── Mock backend ─────────────────────────────────────────────────────────────

// mockAutoscalingBackend implements autoscalingBackend with in-memory state.
type mockAutoscalingBackend struct{}

func (b *mockAutoscalingBackend) plan(m *PlatformAutoscaling) (*ScalingPlan, error) {
	policies := m.policies()
	plan := &ScalingPlan{
		Policies: policies,
	}

	switch m.state.Status {
	case "pending", "deleted":
		plan.Changes = []string{
			fmt.Sprintf("create %d autoscaling policy(s)", len(policies)),
		}
		for _, p := range policies {
			plan.Changes = append(plan.Changes,
				fmt.Sprintf("  add %s policy %q on %q", p.Type, p.Name, p.TargetResource))
		}
	case "active":
		plan.Changes = []string{"autoscaling already active, no changes"}
	default:
		plan.Changes = []string{fmt.Sprintf("autoscaling status=%s, no action", m.state.Status)}
	}

	return plan, nil
}

func (b *mockAutoscalingBackend) apply(m *PlatformAutoscaling) (*ScalingState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}

	policies := m.policies()
	m.state.ID = fmt.Sprintf("mock-scaling-%s", strings.ReplaceAll(m.name, " ", "-"))
	m.state.Policies = policies
	m.state.CurrentCapacity = 1
	if len(policies) > 0 && policies[0].MinCapacity > 0 {
		m.state.CurrentCapacity = policies[0].MinCapacity
	}
	m.state.Status = "active"

	return m.state, nil
}

func (b *mockAutoscalingBackend) status(m *PlatformAutoscaling) (*ScalingState, error) {
	return m.state, nil
}

func (b *mockAutoscalingBackend) destroy(m *PlatformAutoscaling) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.Policies = nil
	m.state.ID = ""
	return nil
}

// ─── AWS stub ─────────────────────────────────────────────────────────────────

// awsAutoscalingBackend is a stub for AWS Auto Scaling.
// Real implementation would use aws-sdk-go-v2/service/autoscaling.
type awsAutoscalingBackend struct{}

func (b *awsAutoscalingBackend) plan(m *PlatformAutoscaling) (*ScalingPlan, error) {
	return &ScalingPlan{
		Policies: m.policies(),
		Changes:  []string{"AWS Auto Scaling (stub — use Terraform or aws-sdk-go-v2/service/autoscaling)"},
	}, nil
}

func (b *awsAutoscalingBackend) apply(m *PlatformAutoscaling) (*ScalingState, error) {
	return nil, fmt.Errorf("aws autoscaling backend: not implemented — use Terraform or aws-sdk-go-v2/service/autoscaling")
}

func (b *awsAutoscalingBackend) status(m *PlatformAutoscaling) (*ScalingState, error) {
	m.state.Status = "unknown"
	return m.state, nil
}

func (b *awsAutoscalingBackend) destroy(m *PlatformAutoscaling) error {
	return fmt.Errorf("aws autoscaling backend: not implemented — use Terraform or aws-sdk-go-v2/service/autoscaling")
}
