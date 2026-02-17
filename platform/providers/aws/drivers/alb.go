//go:build aws

package drivers

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/GoCodeAlone/workflow/platform"
)

// ELBv2Client defines the ELBv2 operations used by the ALB driver.
type ELBv2Client interface {
	CreateLoadBalancer(ctx context.Context, params *elbv2.CreateLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.CreateLoadBalancerOutput, error)
	DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error)
	ModifyLoadBalancerAttributes(ctx context.Context, params *elbv2.ModifyLoadBalancerAttributesInput, optFns ...func(*elbv2.Options)) (*elbv2.ModifyLoadBalancerAttributesOutput, error)
}

// ALBDriver manages Application Load Balancer resources.
type ALBDriver struct {
	client ELBv2Client
}

// NewALBDriver creates a new ALB driver.
func NewALBDriver(cfg awsv2.Config) *ALBDriver {
	return &ALBDriver{
		client: elbv2.NewFromConfig(cfg),
	}
}

// NewALBDriverWithClient creates an ALB driver with a custom client.
func NewALBDriverWithClient(client ELBv2Client) *ALBDriver {
	return &ALBDriver{client: client}
}

func (d *ALBDriver) ResourceType() string { return "aws.alb" }

func (d *ALBDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	scheme, _ := properties["scheme"].(string)
	if scheme == "" {
		scheme = "internet-facing"
	}
	subnetIDs := stringSliceProp(properties, "subnet_ids")
	securityGroups := stringSliceProp(properties, "security_group_ids")

	lbScheme := elbtypes.LoadBalancerSchemeEnumInternetFacing
	if scheme == "internal" {
		lbScheme = elbtypes.LoadBalancerSchemeEnumInternal
	}

	input := &elbv2.CreateLoadBalancerInput{
		Name:           awsv2.String(name),
		Type:           elbtypes.LoadBalancerTypeEnumApplication,
		Scheme:         lbScheme,
		Subnets:        subnetIDs,
		SecurityGroups: securityGroups,
	}

	out, err := d.client.CreateLoadBalancer(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("alb: create %q: %w", name, err)
	}

	if len(out.LoadBalancers) == 0 {
		return nil, fmt.Errorf("alb: create %q returned no load balancers", name)
	}

	return albToOutput(&out.LoadBalancers[0]), nil
}

func (d *ALBDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	out, err := d.client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{
		Names: []string{name},
	})
	if err != nil {
		return nil, fmt.Errorf("alb: describe %q: %w", name, err)
	}
	if len(out.LoadBalancers) == 0 {
		return nil, &platform.ResourceNotFoundError{Name: name, Provider: "aws"}
	}
	return albToOutput(&out.LoadBalancers[0]), nil
}

func (d *ALBDriver) Update(ctx context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	// Read current to get the ARN
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	arn, _ := current.Properties["arn"].(string)
	if arn == "" {
		return nil, fmt.Errorf("alb: update %q: missing ARN", name)
	}

	// Modify attributes if provided
	var attrs []elbtypes.LoadBalancerAttribute
	if idleTimeout, ok := desired["idle_timeout"].(string); ok {
		attrs = append(attrs, elbtypes.LoadBalancerAttribute{
			Key:   awsv2.String("idle_timeout.timeout_seconds"),
			Value: awsv2.String(idleTimeout),
		})
	}

	if len(attrs) > 0 {
		_, err := d.client.ModifyLoadBalancerAttributes(ctx, &elbv2.ModifyLoadBalancerAttributesInput{
			LoadBalancerArn: awsv2.String(arn),
			Attributes:      attrs,
		})
		if err != nil {
			return nil, fmt.Errorf("alb: modify %q: %w", name, err)
		}
	}

	return d.Read(ctx, name)
}

func (d *ALBDriver) Delete(ctx context.Context, name string) error {
	current, err := d.Read(ctx, name)
	if err != nil {
		return err
	}
	arn, _ := current.Properties["arn"].(string)
	if arn == "" {
		return fmt.Errorf("alb: delete %q: missing ARN", name)
	}

	_, err = d.client.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{
		LoadBalancerArn: awsv2.String(arn),
	})
	if err != nil {
		return fmt.Errorf("alb: delete %q: %w", name, err)
	}
	return nil
}

func (d *ALBDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	out, err := d.Read(ctx, name)
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}
	status := "healthy"
	if out.Status != platform.ResourceStatusActive {
		status = "degraded"
	}
	return &platform.HealthStatus{
		Status:    status,
		Message:   string(out.Status),
		CheckedAt: time.Now(),
	}, nil
}

func (d *ALBDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "aws.alb"}
}

func (d *ALBDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func albToOutput(lb *elbtypes.LoadBalancer) *platform.ResourceOutput {
	if lb == nil {
		return nil
	}

	status := platform.ResourceStatusActive
	if lb.State != nil {
		switch lb.State.Code {
		case elbtypes.LoadBalancerStateEnumProvisioning:
			status = platform.ResourceStatusCreating
		case elbtypes.LoadBalancerStateEnumFailed:
			status = platform.ResourceStatusFailed
		}
	}

	props := map[string]any{
		"scheme": string(lb.Scheme),
		"type":   string(lb.Type),
	}
	if lb.LoadBalancerArn != nil {
		props["arn"] = *lb.LoadBalancerArn
	}
	if lb.DNSName != nil {
		props["dns_name"] = *lb.DNSName
	}
	if lb.VpcId != nil {
		props["vpc_id"] = *lb.VpcId
	}

	endpoint := ""
	if lb.DNSName != nil {
		endpoint = *lb.DNSName
	}

	name := ""
	if lb.LoadBalancerName != nil {
		name = *lb.LoadBalancerName
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "load_balancer",
		ProviderType: "aws.alb",
		Endpoint:     endpoint,
		Properties:   props,
		Status:       status,
		LastSynced:   time.Now(),
	}
}

var _ platform.ResourceDriver = (*ALBDriver)(nil)
