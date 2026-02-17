//go:build aws

package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/GoCodeAlone/workflow/platform"
)

// IAMClient defines the IAM operations used by the driver.
type IAMClient interface {
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	UpdateAssumeRolePolicy(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error)
	DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
	AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	DetachRolePolicy(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	ListAttachedRolePolicies(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
}

// IAMDriver manages IAM roles and policies.
type IAMDriver struct {
	client IAMClient
}

// NewIAMDriver creates a new IAM driver.
func NewIAMDriver(cfg awsv2.Config) *IAMDriver {
	return &IAMDriver{
		client: iam.NewFromConfig(cfg),
	}
}

// NewIAMDriverWithClient creates an IAM driver with a custom client.
func NewIAMDriverWithClient(client IAMClient) *IAMDriver {
	return &IAMDriver{client: client}
}

func (d *IAMDriver) ResourceType() string { return "aws.iam_role" }

func (d *IAMDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	assumeRolePolicy, _ := properties["assume_role_policy"].(string)
	if assumeRolePolicy == "" {
		assumeRolePolicy = defaultAssumeRolePolicy()
	}
	description, _ := properties["description"].(string)
	path, _ := properties["path"].(string)
	if path == "" {
		path = "/"
	}

	out, err := d.client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 awsv2.String(name),
		AssumeRolePolicyDocument: awsv2.String(assumeRolePolicy),
		Description:              awsv2.String(description),
		Path:                     awsv2.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("iam: create role %q: %w", name, err)
	}

	// Attach policies if specified
	policyARNs := stringSliceProp(properties, "policy_arns")
	for _, arn := range policyARNs {
		_, err := d.client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  awsv2.String(name),
			PolicyArn: awsv2.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("iam: attach policy to role %q: %w", name, err)
		}
	}

	props := map[string]any{}
	if out.Role != nil {
		if out.Role.Arn != nil {
			props["arn"] = *out.Role.Arn
		}
		if out.Role.RoleId != nil {
			props["role_id"] = *out.Role.RoleId
		}
	}
	props["policy_arns"] = policyARNs

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "iam_role",
		ProviderType: "aws.iam_role",
		Properties:   props,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

func (d *IAMDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	out, err := d.client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: awsv2.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("iam: get role %q: %w", name, err)
	}

	props := map[string]any{}
	if out.Role != nil {
		if out.Role.Arn != nil {
			props["arn"] = *out.Role.Arn
		}
		if out.Role.RoleId != nil {
			props["role_id"] = *out.Role.RoleId
		}
		if out.Role.Description != nil {
			props["description"] = *out.Role.Description
		}
	}

	// List attached policies
	polOut, err := d.client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: awsv2.String(name),
	})
	if err == nil && polOut != nil {
		var arns []string
		for _, p := range polOut.AttachedPolicies {
			if p.PolicyArn != nil {
				arns = append(arns, *p.PolicyArn)
			}
		}
		props["policy_arns"] = arns
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "iam_role",
		ProviderType: "aws.iam_role",
		Properties:   props,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

func (d *IAMDriver) Update(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error) {
	// Update assume role policy if changed
	if policy, ok := desired["assume_role_policy"].(string); ok && policy != "" {
		_, err := d.client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
			RoleName:       awsv2.String(name),
			PolicyDocument: awsv2.String(policy),
		})
		if err != nil {
			return nil, fmt.Errorf("iam: update assume role policy %q: %w", name, err)
		}
	}

	// Reconcile attached policies
	desiredPolicies := stringSliceProp(desired, "policy_arns")
	currentPolicies := stringSliceProp(current, "policy_arns")

	toAttach := diffSlice(desiredPolicies, currentPolicies)
	toDetach := diffSlice(currentPolicies, desiredPolicies)

	for _, arn := range toAttach {
		_, err := d.client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  awsv2.String(name),
			PolicyArn: awsv2.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("iam: attach policy %q to %q: %w", arn, name, err)
		}
	}
	for _, arn := range toDetach {
		_, err := d.client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  awsv2.String(name),
			PolicyArn: awsv2.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("iam: detach policy %q from %q: %w", arn, name, err)
		}
	}

	return d.Read(ctx, name)
}

func (d *IAMDriver) Delete(ctx context.Context, name string) error {
	// Detach all policies first
	polOut, err := d.client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: awsv2.String(name),
	})
	if err == nil && polOut != nil {
		for _, p := range polOut.AttachedPolicies {
			_, _ = d.client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  awsv2.String(name),
				PolicyArn: p.PolicyArn,
			})
		}
	}

	_, err = d.client.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: awsv2.String(name),
	})
	if err != nil {
		return fmt.Errorf("iam: delete role %q: %w", name, err)
	}
	return nil
}

func (d *IAMDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	_, err := d.Read(ctx, name)
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   "role exists",
		CheckedAt: time.Now(),
	}, nil
}

func (d *IAMDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "aws.iam_role"}
}

func (d *IAMDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func defaultAssumeRolePolicy() string {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":    "Allow",
				"Principal": map[string]string{"Service": "eks.amazonaws.com"},
				"Action":    "sts:AssumeRole",
			},
		},
	}
	data, _ := json.Marshal(policy)
	return string(data)
}

// diffSlice returns elements in a that are not in b.
func diffSlice(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, v := range b {
		bSet[v] = struct{}{}
	}
	var diff []string
	for _, v := range a {
		if _, ok := bSet[v]; !ok {
			diff = append(diff, v)
		}
	}
	return diff
}

var _ platform.ResourceDriver = (*IAMDriver)(nil)
