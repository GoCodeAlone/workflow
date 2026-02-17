package rbac

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/auth"
)

// AWSIAMProvider is a stub for AWS IAM policy evaluation.
// It defines the interface shape; the full AWS SDK integration is left for
// when the AWS dependency is added.
type AWSIAMProvider struct {
	region  string
	roleARN string
}

// NewAWSIAMProvider creates an AWSIAMProvider for the given region and role ARN.
func NewAWSIAMProvider(region, roleARN string) *AWSIAMProvider {
	return &AWSIAMProvider{region: region, roleARN: roleARN}
}

// Name returns the provider identifier.
func (a *AWSIAMProvider) Name() string { return "aws-iam" }

// CheckPermission evaluates an IAM policy for the given subject/resource/action.
func (a *AWSIAMProvider) CheckPermission(_ context.Context, _, _, _ string) (bool, error) {
	return false, fmt.Errorf("AWS IAM provider not implemented: region=%s, role=%s", a.region, a.roleARN)
}

// ListPermissions lists IAM permissions for the subject.
func (a *AWSIAMProvider) ListPermissions(_ context.Context, _ string) ([]auth.Permission, error) {
	return nil, fmt.Errorf("AWS IAM provider not implemented")
}

// SyncRoles pushes role definitions to AWS IAM.
func (a *AWSIAMProvider) SyncRoles(_ context.Context, _ []auth.RoleDefinition) error {
	return fmt.Errorf("AWS IAM provider not implemented")
}
