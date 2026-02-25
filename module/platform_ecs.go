package module

import (
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// ECSServiceState holds the current state of a managed ECS service.
type ECSServiceState struct {
	Name           string             `json:"name"`
	Cluster        string             `json:"cluster"`
	Region         string             `json:"region"`
	LaunchType     string             `json:"launchType"`
	Status         string             `json:"status"` // pending, creating, running, deleting, deleted
	DesiredCount   int                `json:"desiredCount"`
	RunningCount   int                `json:"runningCount"`
	TaskDefinition ECSTaskDefinition  `json:"taskDefinition"`
	LoadBalancer   *ECSLoadBalancer   `json:"loadBalancer,omitempty"`
	CreatedAt      time.Time          `json:"createdAt"`
}

// ECSTaskDefinition describes an ECS task definition.
type ECSTaskDefinition struct {
	Family   string         `json:"family"`
	Revision int            `json:"revision"`
	CPU      string         `json:"cpu"`
	Memory   string         `json:"memory"`
	Containers []ECSContainer `json:"containers"`
}

// ECSContainer describes a container within a task definition.
type ECSContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Port  int    `json:"port,omitempty"`
}

// ECSLoadBalancer describes the ALB/NLB configuration for an ECS service.
type ECSLoadBalancer struct {
	TargetGroupARN string `json:"targetGroupArn"`
	ContainerName  string `json:"containerName"`
	ContainerPort  int    `json:"containerPort"`
}

// PlatformECS manages AWS ECS/Fargate services via pluggable backends.
// Config:
//
//	account:        name of a cloud.account module (resolved from service registry)
//	cluster:        ECS cluster name
//	region:         AWS region (e.g. us-east-1)
//	launch_type:    FARGATE or EC2 (default: FARGATE)
//	vpc_subnets:    list of subnet IDs
//	security_groups: list of security group IDs
type PlatformECS struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider // resolved from service registry
	state    *ECSServiceState
	backend  ecsBackend
}

// ecsBackend is the internal interface that ECS backends implement.
type ecsBackend interface {
	plan(e *PlatformECS) (*PlatformPlan, error)
	apply(e *PlatformECS) (*PlatformResult, error)
	status(e *PlatformECS) (*ECSServiceState, error)
	destroy(e *PlatformECS) error
}

// NewPlatformECS creates a new PlatformECS module.
func NewPlatformECS(name string, cfg map[string]any) *PlatformECS {
	return &PlatformECS{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformECS) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformECS) Init(app modular.Application) error {
	cluster, _ := m.config["cluster"].(string)
	if cluster == "" {
		return fmt.Errorf("platform.ecs %q: 'cluster' is required", m.name)
	}

	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.ecs %q: account service %q not found", m.name, accountName)
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.ecs %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = provider
	}

	launchType, _ := m.config["launch_type"].(string)
	if launchType == "" {
		launchType = "FARGATE"
	}

	region, _ := m.config["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	m.state = &ECSServiceState{
		Name:       m.name,
		Cluster:    cluster,
		Region:     region,
		LaunchType: launchType,
		Status:     "pending",
	}

	// Default backend is mock (in-memory); real FARGATE would call AWS SDK.
	m.backend = &ecsMockBackend{}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformECS) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "ECS service: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name, not declared.
func (m *PlatformECS) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the changes that would be made to bring the ECS service to desired state.
func (m *PlatformECS) Plan() (*PlatformPlan, error) {
	return m.backend.plan(m)
}

// Apply creates or updates the ECS task definition and service.
func (m *PlatformECS) Apply() (*PlatformResult, error) {
	return m.backend.apply(m)
}

// Status returns the current ECS service state.
func (m *PlatformECS) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy deletes the ECS service and task definition.
func (m *PlatformECS) Destroy() error {
	return m.backend.destroy(m)
}

// serviceName returns the ECS service name (defaults to module name).
func (m *PlatformECS) serviceName() string {
	if n, ok := m.config["service_name"].(string); ok && n != "" {
		return n
	}
	return m.name
}

// taskFamily returns the ECS task definition family name.
func (m *PlatformECS) taskFamily() string {
	if f, ok := m.config["task_family"].(string); ok && f != "" {
		return f
	}
	return m.name + "-task"
}

// desiredCount returns the desired task count.
func (m *PlatformECS) desiredCount() int {
	if n, ok := m.config["desired_count"]; ok {
		if count, ok := intFromAny(n); ok && count > 0 {
			return count
		}
	}
	return 1
}

// ─── mock backend ─────────────────────────────────────────────────────────────

// ecsMockBackend implements ecsBackend using in-memory state for local testing.
// Real implementation would use aws-sdk-go-v2/service/ecs to manage services.
type ecsMockBackend struct{}

func (b *ecsMockBackend) plan(e *PlatformECS) (*PlatformPlan, error) {
	plan := &PlatformPlan{
		Provider: "ecs",
		Resource: e.serviceName(),
	}

	switch e.state.Status {
	case "pending", "deleted":
		plan.Actions = []PlatformAction{
			{
				Type:     "create",
				Resource: e.serviceName(),
				Detail:   fmt.Sprintf("create ECS service %q in cluster %q (%s)", e.serviceName(), e.state.Cluster, e.state.LaunchType),
			},
			{
				Type:     "create",
				Resource: e.taskFamily(),
				Detail:   fmt.Sprintf("register ECS task definition %q", e.taskFamily()),
			},
		}
	case "running":
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: e.serviceName(), Detail: "ECS service already running"},
		}
	default:
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: e.serviceName(), Detail: fmt.Sprintf("ECS service status=%s, no action", e.state.Status)},
		}
	}

	return plan, nil
}

func (b *ecsMockBackend) apply(e *PlatformECS) (*PlatformResult, error) {
	if e.state.Status == "running" {
		return &PlatformResult{Success: true, Message: "ECS service already running", State: e.state}, nil
	}

	e.state.Status = "creating"
	e.state.CreatedAt = time.Now()
	e.state.DesiredCount = e.desiredCount()

	// Simulate task definition registration.
	e.state.TaskDefinition = ECSTaskDefinition{
		Family:   e.taskFamily(),
		Revision: 1,
		CPU:      "256",
		Memory:   "512",
		Containers: []ECSContainer{
			{Name: "app", Image: "app:latest", Port: 8080},
		},
	}

	// Simulate ALB target group assignment.
	e.state.LoadBalancer = &ECSLoadBalancer{
		TargetGroupARN: fmt.Sprintf("arn:aws:elasticloadbalancing:%s:123456789012:targetgroup/%s/mock", e.state.Region, e.serviceName()),
		ContainerName:  "app",
		ContainerPort:  8080,
	}

	// In-memory: immediately transition to running.
	// Real implementation: call ecs.RegisterTaskDefinition + ecs.CreateService / ecs.UpdateService.
	e.state.Status = "running"
	e.state.RunningCount = e.state.DesiredCount

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("ECS service %q created in cluster %q (in-memory mock)", e.serviceName(), e.state.Cluster),
		State:   e.state,
	}, nil
}

func (b *ecsMockBackend) status(e *PlatformECS) (*ECSServiceState, error) {
	return e.state, nil
}

func (b *ecsMockBackend) destroy(e *PlatformECS) error {
	if e.state.Status == "deleted" {
		return nil
	}
	e.state.Status = "deleting"
	// In-memory: immediately mark deleted.
	// Real implementation: call ecs.DeleteService + ecs.DeregisterTaskDefinition.
	e.state.Status = "deleted"
	e.state.RunningCount = 0
	e.state.DesiredCount = 0
	e.state.LoadBalancer = nil
	return nil
}

// ─── fargate stub ─────────────────────────────────────────────────────────────

// ecsFargateBackend is a stub for real AWS ECS Fargate.
// Real implementation would use aws-sdk-go-v2/service/ecs.
type ecsFargateBackend struct{}

func (b *ecsFargateBackend) plan(e *PlatformECS) (*PlatformPlan, error) {
	return &PlatformPlan{
		Provider: "ecs-fargate",
		Resource: e.serviceName(),
		Actions:  []PlatformAction{{Type: "create", Resource: e.serviceName(), Detail: "ECS Fargate service (stub — use aws-sdk-go-v2/service/ecs)"}},
	}, nil
}

func (b *ecsFargateBackend) apply(e *PlatformECS) (*PlatformResult, error) {
	return nil, fmt.Errorf("ecs fargate backend: not implemented — use aws-sdk-go-v2/service/ecs")
}

func (b *ecsFargateBackend) status(e *PlatformECS) (*ECSServiceState, error) {
	e.state.Status = "unknown"
	return e.state, nil
}

func (b *ecsFargateBackend) destroy(e *PlatformECS) error {
	return fmt.Errorf("ecs fargate backend: not implemented — use aws-sdk-go-v2/service/ecs")
}
