package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/GoCodeAlone/workflow/auth"
)

// IAMClient defines the AWS IAM operations used by AWSIAMProvider.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
	ListAttachedRolePolicies(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
	ListAttachedUserPolicies(ctx context.Context, params *iam.ListAttachedUserPoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedUserPoliciesOutput, error)
	GetPolicy(ctx context.Context, params *iam.GetPolicyInput, optFns ...func(*iam.Options)) (*iam.GetPolicyOutput, error)
	GetPolicyVersion(ctx context.Context, params *iam.GetPolicyVersionInput, optFns ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error)
	CreatePolicy(ctx context.Context, params *iam.CreatePolicyInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyOutput, error)
	CreatePolicyVersion(ctx context.Context, params *iam.CreatePolicyVersionInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error)
	AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
}

// AWSIAMProvider implements PermissionProvider via AWS IAM policy simulation.
type AWSIAMProvider struct {
	region  string
	roleARN string
	client  IAMClient
	initErr error
}

// NewAWSIAMProvider creates an AWSIAMProvider for the given region and role ARN.
// It loads the default AWS configuration for the region. Use
// NewAWSIAMProviderWithClient to inject a custom IAM client (e.g. in tests).
func NewAWSIAMProvider(region, roleARN string) *AWSIAMProvider {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		return &AWSIAMProvider{region: region, roleARN: roleARN, initErr: err}
	}
	return &AWSIAMProvider{
		region:  region,
		roleARN: roleARN,
		client:  iam.NewFromConfig(cfg),
	}
}

// NewAWSIAMProviderWithClient creates an AWSIAMProvider with an injectable IAM
// client, useful for testing.
func NewAWSIAMProviderWithClient(region, roleARN string, client IAMClient) *AWSIAMProvider {
	return &AWSIAMProvider{region: region, roleARN: roleARN, client: client}
}

// Name returns the provider identifier.
func (a *AWSIAMProvider) Name() string { return "aws-iam" }

// CheckPermission evaluates whether the subject (IAM principal ARN) is allowed
// to perform action on resource by calling SimulatePrincipalPolicy.
func (a *AWSIAMProvider) CheckPermission(ctx context.Context, subject, resource, action string) (bool, error) {
	if a.initErr != nil {
		return false, a.initErr
	}
	// Map workflow resource:action to IAM action format.
	iamAction := fmt.Sprintf("%s:%s", resource, action)
	out, err := a.client.SimulatePrincipalPolicy(ctx, &iam.SimulatePrincipalPolicyInput{
		PolicySourceArn: awsv2.String(subject),
		ActionNames:     []string{iamAction},
		ResourceArns:    []string{"*"},
	})
	if err != nil {
		return false, fmt.Errorf("iam simulate principal policy: %w", err)
	}
	for i := range out.EvaluationResults {
		if out.EvaluationResults[i].EvalDecision == iamtypes.PolicyEvaluationDecisionTypeAllowed {
			return true, nil
		}
	}
	return false, nil
}

// iamPolicyStatement represents a single IAM policy statement.
type iamPolicyStatement struct {
	Effect   string          `json:"Effect"`
	Action   json.RawMessage `json:"Action"`
	Resource json.RawMessage `json:"Resource"`
}

// iamPolicyDocument represents an IAM policy document.
type iamPolicyDocument struct {
	Version   string               `json:"Version"`
	Statement []iamPolicyStatement `json:"Statement"`
}

// ListPermissions lists IAM permissions for the subject by inspecting attached
// policies. The subject must be a user ARN (containing ":user/") or a role ARN.
func (a *AWSIAMProvider) ListPermissions(ctx context.Context, subject string) ([]auth.Permission, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	var attached []iamtypes.AttachedPolicy
	if strings.Contains(subject, ":user/") {
		userName := subject[strings.LastIndex(subject, ":user/")+len(":user/"):]
		out, err := a.client.ListAttachedUserPolicies(ctx, &iam.ListAttachedUserPoliciesInput{
			UserName: awsv2.String(userName),
		})
		if err != nil {
			return nil, fmt.Errorf("list attached user policies: %w", err)
		}
		attached = out.AttachedPolicies
	} else {
		roleName := roleNameFromARN(subject)
		out, err := a.client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
			RoleName: awsv2.String(roleName),
		})
		if err != nil {
			return nil, fmt.Errorf("list attached role policies: %w", err)
		}
		attached = out.AttachedPolicies
	}

	var perms []auth.Permission
	for _, p := range attached {
		if p.PolicyArn == nil {
			continue
		}
		policyOut, err := a.client.GetPolicy(ctx, &iam.GetPolicyInput{
			PolicyArn: p.PolicyArn,
		})
		if err != nil || policyOut.Policy == nil || policyOut.Policy.DefaultVersionId == nil {
			continue
		}
		versionOut, err := a.client.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
			PolicyArn: p.PolicyArn,
			VersionId: policyOut.Policy.DefaultVersionId,
		})
		if err != nil || versionOut.PolicyVersion == nil || versionOut.PolicyVersion.Document == nil {
			continue
		}
		// Policy documents are URL-encoded per RFC 3986.
		decoded, err := url.QueryUnescape(*versionOut.PolicyVersion.Document)
		if err != nil {
			continue
		}
		parsed, err := parseIAMPolicyDocument(decoded)
		if err != nil {
			continue
		}
		perms = append(perms, parsed...)
	}
	return perms, nil
}

// SyncRoles creates or updates IAM managed policies for each RoleDefinition and
// attaches them to the configured IAM role.
func (a *AWSIAMProvider) SyncRoles(ctx context.Context, roles []auth.RoleDefinition) error {
	if a.initErr != nil {
		return a.initErr
	}
	accountID := accountIDFromARN(a.roleARN)
	roleName := roleNameFromARN(a.roleARN)

	for _, rd := range roles {
		doc, err := buildPolicyDocument(rd)
		if err != nil {
			return fmt.Errorf("build policy document for %q: %w", rd.Name, err)
		}
		policyName := "workflow-" + rd.Name

		var policyARN string
		createOut, err := a.client.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyName:     awsv2.String(policyName),
			PolicyDocument: awsv2.String(doc),
			Description:    awsv2.String(rd.Description),
		})
		if err != nil {
			var entityExists *iamtypes.EntityAlreadyExistsException
			if !errors.As(err, &entityExists) {
				return fmt.Errorf("create policy %q: %w", policyName, err)
			}
			// Policy already exists: create a new default version.
			if accountID != "" {
				policyARN = fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountID, policyName)
				if _, err := a.client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
					PolicyArn:      awsv2.String(policyARN),
					PolicyDocument: awsv2.String(doc),
					SetAsDefault:   true,
				}); err != nil {
					return fmt.Errorf("create policy version for %q: %w", policyName, err)
				}
			}
		} else if createOut.Policy != nil && createOut.Policy.Arn != nil {
			policyARN = *createOut.Policy.Arn
		}

		if policyARN == "" {
			continue
		}
		if _, err := a.client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  awsv2.String(roleName),
			PolicyArn: awsv2.String(policyARN),
		}); err != nil {
			return fmt.Errorf("attach policy %q to role %q: %w", policyARN, roleName, err)
		}
	}
	return nil
}

// parseIAMPolicyDocument converts an IAM policy document JSON string into
// a slice of auth.Permission values.
func parseIAMPolicyDocument(doc string) ([]auth.Permission, error) {
	var pd iamPolicyDocument
	if err := json.Unmarshal([]byte(doc), &pd); err != nil {
		return nil, err
	}
	var perms []auth.Permission
	for _, stmt := range pd.Statement {
		effect := strings.ToLower(stmt.Effect)
		actions := parseStringOrSlice(stmt.Action)
		for _, act := range actions {
			resource, action := splitIAMAction(act)
			perms = append(perms, auth.Permission{
				Resource: resource,
				Action:   action,
				Effect:   effect,
			})
		}
	}
	return perms, nil
}

// parseStringOrSlice unmarshals a JSON field that may be a string or []string.
func parseStringOrSlice(raw json.RawMessage) []string {
	if raw == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var ss []string
	_ = json.Unmarshal(raw, &ss)
	return ss
}

// splitIAMAction splits an IAM action like "s3:GetObject" into ("s3", "GetObject").
func splitIAMAction(action string) (resource, act string) {
	if i := strings.IndexByte(action, ':'); i >= 0 {
		return action[:i], action[i+1:]
	}
	return action, ""
}

// roleNameFromARN extracts the role name from an ARN like
// "arn:aws:iam::123:role/my-role" → "my-role", or returns the input unchanged.
func roleNameFromARN(arn string) string {
	if i := strings.Index(arn, ":role/"); i >= 0 {
		return arn[i+len(":role/"):]
	}
	return arn
}

// accountIDFromARN extracts the account ID from an ARN like
// "arn:aws:iam::123456789012:role/my-role" → "123456789012".
func accountIDFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// buildPolicyDocument creates an IAM policy document JSON granting all
// permissions in the RoleDefinition.
func buildPolicyDocument(rd auth.RoleDefinition) (string, error) {
	type statement struct {
		Effect   string   `json:"Effect"`
		Action   []string `json:"Action"`
		Resource string   `json:"Resource"`
	}
	actions := make([]string, 0, len(rd.Permissions))
	for _, p := range rd.Permissions {
		actions = append(actions, fmt.Sprintf("%s:%s", p.Resource, p.Action))
	}
	doc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []statement{
			{
				Effect:   "Allow",
				Action:   actions,
				Resource: "*",
			},
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
