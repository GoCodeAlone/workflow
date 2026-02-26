package module

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
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

// ECSBackendFactory creates an ecsBackend for a given provider config.
type ECSBackendFactory func(cfg map[string]any) (ecsBackend, error)

// ecsBackendRegistry maps provider name to its factory.
var ecsBackendRegistry = map[string]ECSBackendFactory{}

// RegisterECSBackend registers an ECSBackendFactory for the given provider name.
func RegisterECSBackend(provider string, factory ECSBackendFactory) {
	ecsBackendRegistry[provider] = factory
}

func init() {
	RegisterECSBackend("mock", func(_ map[string]any) (ecsBackend, error) {
		return &ecsMockBackend{}, nil
	})
	RegisterECSBackend("aws", func(_ map[string]any) (ecsBackend, error) {
		return &awsECSBackend{}, nil
	})
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

	// Determine provider type: use explicit "provider" config field if set,
	// otherwise fall back to the cloud account's provider name (if available).
	providerType, _ := m.config["provider"].(string)
	if providerType == "" && m.provider != nil {
		providerType = m.provider.Provider()
	}
	if providerType == "" {
		providerType = "mock"
	}

	factory, ok := ecsBackendRegistry[providerType]
	if !ok {
		// Fall back to mock for unknown provider types to preserve backward compatibility.
		factory = ecsBackendRegistry["mock"]
	}
	backend, err := factory(m.config)
	if err != nil {
		return fmt.Errorf("platform.ecs %q: creating backend: %w", m.name, err)
	}
	m.backend = backend

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

// ─── AWS ECS backend ──────────────────────────────────────────────────────────

// awsECSBackend manages AWS ECS services using aws-sdk-go-v2/service/ecs.
type awsECSBackend struct{}

func (b *awsECSBackend) plan(e *PlatformECS) (*PlatformPlan, error) {
	awsProv, ok := awsProviderFrom(e.provider)
	if !ok {
		return &PlatformPlan{
			Provider: "ecs",
			Resource: e.serviceName(),
			Actions:  []PlatformAction{{Type: "create", Resource: e.serviceName(), Detail: fmt.Sprintf("create ECS service %q (no AWS config)", e.serviceName())}},
		}, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("ecs plan: AWS config: %w", err)
	}
	client := ecs.NewFromConfig(cfg)

	out, err := client.DescribeServices(context.Background(), &ecs.DescribeServicesInput{
		Cluster:  aws.String(e.state.Cluster),
		Services: []string{e.serviceName()},
	})
	if err != nil {
		return nil, fmt.Errorf("ecs plan: DescribeServices: %w", err)
	}

	for i := range out.Services {
		if out.Services[i].ServiceName != nil && *out.Services[i].ServiceName == e.serviceName() && out.Services[i].Status != nil && *out.Services[i].Status != "INACTIVE" {
			return &PlatformPlan{
				Provider: "ecs",
				Resource: e.serviceName(),
				Actions:  []PlatformAction{{Type: "noop", Resource: e.serviceName(), Detail: fmt.Sprintf("ECS service %q exists (status: %s)", e.serviceName(), *out.Services[i].Status)}},
			}, nil
		}
	}

	return &PlatformPlan{
		Provider: "ecs",
		Resource: e.serviceName(),
		Actions: []PlatformAction{
			{Type: "create", Resource: e.taskFamily(), Detail: fmt.Sprintf("register ECS task definition %q", e.taskFamily())},
			{Type: "create", Resource: e.serviceName(), Detail: fmt.Sprintf("create ECS service %q in cluster %q (%s)", e.serviceName(), e.state.Cluster, e.state.LaunchType)},
		},
	}, nil
}

func (b *awsECSBackend) apply(e *PlatformECS) (*PlatformResult, error) {
	awsProv, ok := awsProviderFrom(e.provider)
	if !ok {
		return nil, fmt.Errorf("ecs apply: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("ecs apply: AWS config: %w", err)
	}
	client := ecs.NewFromConfig(cfg)

	// Build container definitions from config
	containers := parseECSContainers(e.config)
	if len(containers) == 0 {
		containers = []ecstypes.ContainerDefinition{
			{Name: aws.String("app"), Image: aws.String("app:latest"), Essential: aws.Bool(true)},
		}
	}

	cpu, _ := e.config["cpu"].(string)
	memory, _ := e.config["memory"].(string)
	execRoleARN, _ := e.config["execution_role_arn"].(string)

	tdOut, err := client.RegisterTaskDefinition(context.Background(), &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(e.taskFamily()),
		ContainerDefinitions:    containers,
		Cpu:                     optString(cpu),
		Memory:                  optString(memory),
		ExecutionRoleArn:        optString(execRoleARN),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
	})
	if err != nil {
		return nil, fmt.Errorf("ecs apply: RegisterTaskDefinition: %w", err)
	}

	taskDefARN := ""
	revision := 1
	if tdOut.TaskDefinition != nil {
		if tdOut.TaskDefinition.TaskDefinitionArn != nil {
			taskDefARN = *tdOut.TaskDefinition.TaskDefinitionArn
		}
		revision = int(tdOut.TaskDefinition.Revision)
	}

	subnets := parseStringSlice(e.config["vpc_subnets"])
	sgs := parseStringSlice(e.config["security_groups"])
	desiredCount := safeIntToInt32(e.desiredCount())

	_, err = client.CreateService(context.Background(), &ecs.CreateServiceInput{
		ServiceName:    aws.String(e.serviceName()),
		Cluster:        aws.String(e.state.Cluster),
		TaskDefinition: aws.String(taskDefARN),
		DesiredCount:   aws.Int32(desiredCount),
		LaunchType:     ecstypes.LaunchTypeFargate,
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        subnets,
				SecurityGroups: sgs,
			},
		},
	})
	if err != nil {
		// If service already exists, update it instead
		var alreadyExists *ecstypes.InvalidParameterException
		if !errors.As(err, &alreadyExists) {
			_, updateErr := client.UpdateService(context.Background(), &ecs.UpdateServiceInput{
				Service:        aws.String(e.serviceName()),
				Cluster:        aws.String(e.state.Cluster),
				TaskDefinition: aws.String(taskDefARN),
				DesiredCount:   aws.Int32(desiredCount),
			})
			if updateErr != nil {
				return nil, fmt.Errorf("ecs apply: CreateService failed (%v), UpdateService also failed: %w", err, updateErr)
			}
		} else {
			return nil, fmt.Errorf("ecs apply: CreateService: %w", err)
		}
	}

	e.state.Status = "creating"
	e.state.CreatedAt = time.Now()
	e.state.DesiredCount = int(desiredCount)
	e.state.TaskDefinition = ECSTaskDefinition{
		Family:   e.taskFamily(),
		Revision: revision,
		CPU:      cpu,
		Memory:   memory,
	}

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("ECS service %q created in cluster %q", e.serviceName(), e.state.Cluster),
		State:   e.state,
	}, nil
}

func (b *awsECSBackend) status(e *PlatformECS) (*ECSServiceState, error) {
	awsProv, ok := awsProviderFrom(e.provider)
	if !ok {
		return e.state, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return e.state, fmt.Errorf("ecs status: AWS config: %w", err)
	}
	client := ecs.NewFromConfig(cfg)

	out, err := client.DescribeServices(context.Background(), &ecs.DescribeServicesInput{
		Cluster:  aws.String(e.state.Cluster),
		Services: []string{e.serviceName()},
	})
	if err != nil {
		return e.state, fmt.Errorf("ecs status: DescribeServices: %w", err)
	}

	for i := range out.Services {
		if out.Services[i].ServiceName != nil && *out.Services[i].ServiceName == e.serviceName() {
			if out.Services[i].Status != nil {
				e.state.Status = *out.Services[i].Status
			}
			e.state.RunningCount = int(out.Services[i].RunningCount)
			e.state.DesiredCount = int(out.Services[i].DesiredCount)
		}
	}

	return e.state, nil
}

func (b *awsECSBackend) destroy(e *PlatformECS) error {
	awsProv, ok := awsProviderFrom(e.provider)
	if !ok {
		return fmt.Errorf("ecs destroy: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return fmt.Errorf("ecs destroy: AWS config: %w", err)
	}
	client := ecs.NewFromConfig(cfg)

	// Scale down to 0 before deleting
	if _, err := client.UpdateService(context.Background(), &ecs.UpdateServiceInput{
		Service:      aws.String(e.serviceName()),
		Cluster:      aws.String(e.state.Cluster),
		DesiredCount: aws.Int32(0),
	}); err != nil {
		return fmt.Errorf("ecs destroy: UpdateService (scale down): %w", err)
	}

	_, err = client.DeleteService(context.Background(), &ecs.DeleteServiceInput{
		Service: aws.String(e.serviceName()),
		Cluster: aws.String(e.state.Cluster),
	})
	if err != nil {
		return fmt.Errorf("ecs destroy: DeleteService: %w", err)
	}

	e.state.Status = "deleted"
	e.state.RunningCount = 0
	e.state.DesiredCount = 0
	return nil
}

// parseECSContainers parses ECS container definitions from module config.
func parseECSContainers(cfg map[string]any) []ecstypes.ContainerDefinition {
	raw, ok := cfg["containers"].([]any)
	if !ok {
		return nil
	}
	var result []ecstypes.ContainerDefinition
	for _, item := range raw {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := c["name"].(string)
		image, _ := c["image"].(string)
		def := ecstypes.ContainerDefinition{
			Name:      aws.String(name),
			Image:     aws.String(image),
			Essential: aws.Bool(true),
		}
		if port, ok := intFromAny(c["port"]); ok && port > 0 {
			def.PortMappings = []ecstypes.PortMapping{
				{ContainerPort: aws.Int32(safeIntToInt32(port)), Protocol: ecstypes.TransportProtocolTcp},
			}
		}
		result = append(result, def)
	}
	return result
}

// optString returns a *string pointer if the string is non-empty, otherwise nil.
func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
