package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/GoCodeAlone/workflow/auth"
)

// mockIAMClient is a test double for IAMClient.
type mockIAMClient struct {
	simulateFunc           func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
	listAttachedRolesFunc  func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
	listAttachedUsersFunc  func(ctx context.Context, params *iam.ListAttachedUserPoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedUserPoliciesOutput, error)
	getPolicyFunc          func(ctx context.Context, params *iam.GetPolicyInput, optFns ...func(*iam.Options)) (*iam.GetPolicyOutput, error)
	getPolicyVersionFunc   func(ctx context.Context, params *iam.GetPolicyVersionInput, optFns ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error)
	createPolicyFunc       func(ctx context.Context, params *iam.CreatePolicyInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyOutput, error)
	createPolicyVerFunc    func(ctx context.Context, params *iam.CreatePolicyVersionInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error)
	attachRolePolicyFunc   func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
}

func (m *mockIAMClient) SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
	if m.simulateFunc != nil {
		return m.simulateFunc(ctx, params, optFns...)
	}
	return &iam.SimulatePrincipalPolicyOutput{}, nil
}

func (m *mockIAMClient) ListAttachedRolePolicies(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
	if m.listAttachedRolesFunc != nil {
		return m.listAttachedRolesFunc(ctx, params, optFns...)
	}
	return &iam.ListAttachedRolePoliciesOutput{}, nil
}

func (m *mockIAMClient) ListAttachedUserPolicies(ctx context.Context, params *iam.ListAttachedUserPoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedUserPoliciesOutput, error) {
	if m.listAttachedUsersFunc != nil {
		return m.listAttachedUsersFunc(ctx, params, optFns...)
	}
	return &iam.ListAttachedUserPoliciesOutput{}, nil
}

func (m *mockIAMClient) GetPolicy(ctx context.Context, params *iam.GetPolicyInput, optFns ...func(*iam.Options)) (*iam.GetPolicyOutput, error) {
	if m.getPolicyFunc != nil {
		return m.getPolicyFunc(ctx, params, optFns...)
	}
	return &iam.GetPolicyOutput{}, nil
}

func (m *mockIAMClient) GetPolicyVersion(ctx context.Context, params *iam.GetPolicyVersionInput, optFns ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error) {
	if m.getPolicyVersionFunc != nil {
		return m.getPolicyVersionFunc(ctx, params, optFns...)
	}
	return &iam.GetPolicyVersionOutput{}, nil
}

func (m *mockIAMClient) CreatePolicy(ctx context.Context, params *iam.CreatePolicyInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
	if m.createPolicyFunc != nil {
		return m.createPolicyFunc(ctx, params, optFns...)
	}
	return &iam.CreatePolicyOutput{
		Policy: &iamtypes.Policy{
			Arn:        awsv2.String("arn:aws:iam::123456789012:policy/" + *params.PolicyName),
			PolicyName: params.PolicyName,
		},
	}, nil
}

func (m *mockIAMClient) CreatePolicyVersion(ctx context.Context, params *iam.CreatePolicyVersionInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error) {
	if m.createPolicyVerFunc != nil {
		return m.createPolicyVerFunc(ctx, params, optFns...)
	}
	return &iam.CreatePolicyVersionOutput{}, nil
}

func (m *mockIAMClient) AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	if m.attachRolePolicyFunc != nil {
		return m.attachRolePolicyFunc(ctx, params, optFns...)
	}
	return &iam.AttachRolePolicyOutput{}, nil
}

// --- AWSIAMProvider.Name ---

func TestAWSIAMProvider_Name(t *testing.T) {
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/test", &mockIAMClient{})
	if p.Name() != "aws-iam" {
		t.Errorf("Name() = %q, want aws-iam", p.Name())
	}
}

// --- AWSIAMProvider.CheckPermission ---

func TestAWSIAMProvider_CheckPermission_Allowed(t *testing.T) {
	mock := &mockIAMClient{
		simulateFunc: func(_ context.Context, params *iam.SimulatePrincipalPolicyInput, _ ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			return &iam.SimulatePrincipalPolicyOutput{
				EvaluationResults: []iamtypes.EvaluationResult{
					{
						EvalActionName: awsv2.String("workflows:execute"),
						EvalDecision:   iamtypes.PolicyEvaluationDecisionTypeAllowed,
					},
				},
			}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/test", mock)
	allowed, err := p.CheckPermission(context.Background(), "arn:aws:iam::123:user/alice", "workflows", "execute")
	if err != nil {
		t.Fatalf("CheckPermission unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed=true when decision is 'allowed'")
	}
}

func TestAWSIAMProvider_CheckPermission_ImplicitDeny(t *testing.T) {
	mock := &mockIAMClient{
		simulateFunc: func(_ context.Context, _ *iam.SimulatePrincipalPolicyInput, _ ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			return &iam.SimulatePrincipalPolicyOutput{
				EvaluationResults: []iamtypes.EvaluationResult{
					{
						EvalActionName: awsv2.String("workflows:execute"),
						EvalDecision:   iamtypes.PolicyEvaluationDecisionType("implicitDeny"),
					},
				},
			}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/test", mock)
	allowed, err := p.CheckPermission(context.Background(), "arn:aws:iam::123:user/alice", "workflows", "execute")
	if err != nil {
		t.Fatalf("CheckPermission unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected allowed=false when decision is 'implicitDeny'")
	}
}

func TestAWSIAMProvider_CheckPermission_APIError(t *testing.T) {
	mock := &mockIAMClient{
		simulateFunc: func(_ context.Context, _ *iam.SimulatePrincipalPolicyInput, _ ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			return nil, fmt.Errorf("no credentials")
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/test", mock)
	allowed, err := p.CheckPermission(context.Background(), "arn:aws:iam::123:user/alice", "workflows", "execute")
	if err == nil {
		t.Fatal("expected error from API failure")
	}
	if allowed {
		t.Error("expected allowed=false on API error")
	}
}

func TestAWSIAMProvider_CheckPermission_ActionFormat(t *testing.T) {
	var capturedAction string
	mock := &mockIAMClient{
		simulateFunc: func(_ context.Context, params *iam.SimulatePrincipalPolicyInput, _ ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			if len(params.ActionNames) > 0 {
				capturedAction = params.ActionNames[0]
			}
			return &iam.SimulatePrincipalPolicyOutput{}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/test", mock)
	_, _ = p.CheckPermission(context.Background(), "arn:aws:iam::123:user/alice", "workflows", "execute")
	if capturedAction != "workflows:execute" {
		t.Errorf("expected IAM action 'workflows:execute', got %q", capturedAction)
	}
}

// --- AWSIAMProvider.ListPermissions ---

func urlEncodePolicy(doc interface{}) string {
	data, _ := json.Marshal(doc)
	return url.QueryEscape(string(data))
}

func TestAWSIAMProvider_ListPermissions_RoleARN(t *testing.T) {
	policyDoc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{"Effect": "Allow", "Action": []string{"workflows:read", "workflows:write"}, "Resource": "*"},
		},
	}
	encoded := urlEncodePolicy(policyDoc)

	mock := &mockIAMClient{
		listAttachedRolesFunc: func(_ context.Context, params *iam.ListAttachedRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
			return &iam.ListAttachedRolePoliciesOutput{
				AttachedPolicies: []iamtypes.AttachedPolicy{
					{PolicyArn: awsv2.String("arn:aws:iam::123:policy/workflow-editor"), PolicyName: awsv2.String("workflow-editor")},
				},
			}, nil
		},
		getPolicyFunc: func(_ context.Context, _ *iam.GetPolicyInput, _ ...func(*iam.Options)) (*iam.GetPolicyOutput, error) {
			return &iam.GetPolicyOutput{
				Policy: &iamtypes.Policy{
					Arn:              awsv2.String("arn:aws:iam::123:policy/workflow-editor"),
					DefaultVersionId: awsv2.String("v1"),
				},
			}, nil
		},
		getPolicyVersionFunc: func(_ context.Context, _ *iam.GetPolicyVersionInput, _ ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error) {
			return &iam.GetPolicyVersionOutput{
				PolicyVersion: &iamtypes.PolicyVersion{
					Document:  awsv2.String(encoded),
					VersionId: awsv2.String("v1"),
				},
			}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	perms, err := p.ListPermissions(context.Background(), "arn:aws:iam::123:role/my-role")
	if err != nil {
		t.Fatalf("ListPermissions unexpected error: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d: %v", len(perms), perms)
	}
	for _, perm := range perms {
		if perm.Resource != "workflows" {
			t.Errorf("expected resource 'workflows', got %q", perm.Resource)
		}
		if perm.Effect != "allow" {
			t.Errorf("expected effect 'allow', got %q", perm.Effect)
		}
	}
}

func TestAWSIAMProvider_ListPermissions_UserARN(t *testing.T) {
	policyDoc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"},
		},
	}
	encoded := urlEncodePolicy(policyDoc)

	var capturedUser string
	mock := &mockIAMClient{
		listAttachedUsersFunc: func(_ context.Context, params *iam.ListAttachedUserPoliciesInput, _ ...func(*iam.Options)) (*iam.ListAttachedUserPoliciesOutput, error) {
			capturedUser = *params.UserName
			return &iam.ListAttachedUserPoliciesOutput{
				AttachedPolicies: []iamtypes.AttachedPolicy{
					{PolicyArn: awsv2.String("arn:aws:iam::123:policy/s3-read"), PolicyName: awsv2.String("s3-read")},
				},
			}, nil
		},
		getPolicyFunc: func(_ context.Context, _ *iam.GetPolicyInput, _ ...func(*iam.Options)) (*iam.GetPolicyOutput, error) {
			return &iam.GetPolicyOutput{
				Policy: &iamtypes.Policy{DefaultVersionId: awsv2.String("v1")},
			}, nil
		},
		getPolicyVersionFunc: func(_ context.Context, _ *iam.GetPolicyVersionInput, _ ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error) {
			return &iam.GetPolicyVersionOutput{
				PolicyVersion: &iamtypes.PolicyVersion{
					Document: awsv2.String(encoded),
				},
			}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	perms, err := p.ListPermissions(context.Background(), "arn:aws:iam::123:user/alice")
	if err != nil {
		t.Fatalf("ListPermissions unexpected error: %v", err)
	}
	if capturedUser != "alice" {
		t.Errorf("expected username 'alice', got %q", capturedUser)
	}
	if len(perms) != 1 || perms[0].Action != "GetObject" || perms[0].Resource != "s3" {
		t.Errorf("unexpected permissions: %v", perms)
	}
}

func TestAWSIAMProvider_ListPermissions_APIError(t *testing.T) {
	mock := &mockIAMClient{
		listAttachedRolesFunc: func(_ context.Context, _ *iam.ListAttachedRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	_, err := p.ListPermissions(context.Background(), "arn:aws:iam::123:role/my-role")
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

func TestAWSIAMProvider_ListPermissions_ParseSingleActionString(t *testing.T) {
	policyDoc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{"Effect": "Deny", "Action": "iam:*", "Resource": "*"},
		},
	}
	encoded := urlEncodePolicy(policyDoc)
	mock := &mockIAMClient{
		listAttachedRolesFunc: func(_ context.Context, _ *iam.ListAttachedRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
			return &iam.ListAttachedRolePoliciesOutput{
				AttachedPolicies: []iamtypes.AttachedPolicy{
					{PolicyArn: awsv2.String("arn:aws:iam::123:policy/deny-iam")},
				},
			}, nil
		},
		getPolicyFunc: func(_ context.Context, _ *iam.GetPolicyInput, _ ...func(*iam.Options)) (*iam.GetPolicyOutput, error) {
			return &iam.GetPolicyOutput{Policy: &iamtypes.Policy{DefaultVersionId: awsv2.String("v1")}}, nil
		},
		getPolicyVersionFunc: func(_ context.Context, _ *iam.GetPolicyVersionInput, _ ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error) {
			return &iam.GetPolicyVersionOutput{PolicyVersion: &iamtypes.PolicyVersion{Document: awsv2.String(encoded)}}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	perms, err := p.ListPermissions(context.Background(), "arn:aws:iam::123:role/my-role")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(perms))
	}
	if perms[0].Effect != "deny" || perms[0].Resource != "iam" || perms[0].Action != "*" {
		t.Errorf("unexpected permission: %+v", perms[0])
	}
}

// --- AWSIAMProvider.SyncRoles ---

func TestAWSIAMProvider_SyncRoles_CreatesPoliciesAndAttaches(t *testing.T) {
	var createdPolicy, attachedPolicy, attachedRole string
	mock := &mockIAMClient{
		createPolicyFunc: func(_ context.Context, params *iam.CreatePolicyInput, _ ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
			createdPolicy = *params.PolicyName
			return &iam.CreatePolicyOutput{
				Policy: &iamtypes.Policy{
					Arn:        awsv2.String("arn:aws:iam::123456789012:policy/" + *params.PolicyName),
					PolicyName: params.PolicyName,
				},
			}, nil
		},
		attachRolePolicyFunc: func(_ context.Context, params *iam.AttachRolePolicyInput, _ ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
			attachedPolicy = *params.PolicyArn
			attachedRole = *params.RoleName
			return &iam.AttachRolePolicyOutput{}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123456789012:role/my-role", mock)

	roles := []auth.RoleDefinition{
		{
			Name:        "editor",
			Description: "Can edit workflows",
			Permissions: []auth.Permission{
				{Resource: "workflows", Action: "read", Effect: "allow"},
				{Resource: "workflows", Action: "write", Effect: "allow"},
			},
		},
	}
	if err := p.SyncRoles(context.Background(), roles); err != nil {
		t.Fatalf("SyncRoles error: %v", err)
	}
	if createdPolicy != "workflow-editor" {
		t.Errorf("expected policy name 'workflow-editor', got %q", createdPolicy)
	}
	if attachedRole != "my-role" {
		t.Errorf("expected role 'my-role', got %q", attachedRole)
	}
	if attachedPolicy != "arn:aws:iam::123456789012:policy/workflow-editor" {
		t.Errorf("unexpected attached policy ARN: %q", attachedPolicy)
	}
}

func TestAWSIAMProvider_SyncRoles_UpdatesExistingPolicy(t *testing.T) {
	var updatedVersionARN string
	var versionSetAsDefault bool
	mock := &mockIAMClient{
		createPolicyFunc: func(_ context.Context, params *iam.CreatePolicyInput, _ ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
			return nil, &iamtypes.EntityAlreadyExistsException{
				Message: awsv2.String("policy already exists"),
			}
		},
		createPolicyVerFunc: func(_ context.Context, params *iam.CreatePolicyVersionInput, _ ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error) {
			updatedVersionARN = *params.PolicyArn
			versionSetAsDefault = params.SetAsDefault
			return &iam.CreatePolicyVersionOutput{}, nil
		},
		attachRolePolicyFunc: func(_ context.Context, _ *iam.AttachRolePolicyInput, _ ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
			return &iam.AttachRolePolicyOutput{}, nil
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123456789012:role/my-role", mock)

	roles := []auth.RoleDefinition{
		{
			Name:        "editor",
			Description: "Updated editor",
			Permissions: []auth.Permission{
				{Resource: "workflows", Action: "read", Effect: "allow"},
			},
		},
	}
	if err := p.SyncRoles(context.Background(), roles); err != nil {
		t.Fatalf("SyncRoles error: %v", err)
	}
	if updatedVersionARN != "arn:aws:iam::123456789012:policy/workflow-editor" {
		t.Errorf("unexpected policy ARN for version update: %q", updatedVersionARN)
	}
	if !versionSetAsDefault {
		t.Error("expected SetAsDefault=true when updating policy version")
	}
}

func TestAWSIAMProvider_SyncRoles_EmptyRoles(t *testing.T) {
	mock := &mockIAMClient{}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	if err := p.SyncRoles(context.Background(), nil); err != nil {
		t.Errorf("SyncRoles(nil) unexpected error: %v", err)
	}
	if err := p.SyncRoles(context.Background(), []auth.RoleDefinition{}); err != nil {
		t.Errorf("SyncRoles([]) unexpected error: %v", err)
	}
}

func TestAWSIAMProvider_SyncRoles_CreatePolicyError(t *testing.T) {
	mock := &mockIAMClient{
		createPolicyFunc: func(_ context.Context, _ *iam.CreatePolicyInput, _ ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}
	p := NewAWSIAMProviderWithClient("us-east-1", "arn:aws:iam::123:role/my-role", mock)
	roles := []auth.RoleDefinition{{Name: "reader", Permissions: []auth.Permission{{Resource: "r", Action: "a"}}}}
	if err := p.SyncRoles(context.Background(), roles); err == nil {
		t.Fatal("expected error when CreatePolicy fails with non-exists error")
	}
}

// --- helper unit tests ---

func TestParseIAMPolicyDocument(t *testing.T) {
	doc := `{
		"Version": "2012-10-17",
		"Statement": [
			{"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": "*"},
			{"Effect": "Deny", "Action": "iam:*", "Resource": "*"}
		]
	}`
	perms, err := parseIAMPolicyDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 3 {
		t.Fatalf("expected 3 permissions, got %d: %v", len(perms), perms)
	}
}

func TestBuildPolicyDocument(t *testing.T) {
	rd := auth.RoleDefinition{
		Name: "test-role",
		Permissions: []auth.Permission{
			{Resource: "workflows", Action: "read"},
			{Resource: "workflows", Action: "write"},
		},
	}
	doc, err := buildPolicyDocument(rd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var pd iamPolicyDocument
	if err := json.Unmarshal([]byte(doc), &pd); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if pd.Version != "2012-10-17" {
		t.Errorf("expected version '2012-10-17', got %q", pd.Version)
	}
	if len(pd.Statement) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(pd.Statement))
	}
	actions := parseStringOrSlice(pd.Statement[0].Action)
	if len(actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(actions))
	}
}

func TestRoleNameFromARN(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"arn:aws:iam::123:role/my-role", "my-role"},
		{"my-role", "my-role"},
		{"arn:aws:iam::123:user/alice", "arn:aws:iam::123:user/alice"},
	}
	for _, tt := range tests {
		got := roleNameFromARN(tt.input)
		if got != tt.want {
			t.Errorf("roleNameFromARN(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAccountIDFromARN(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"arn:aws:iam::123456789012:role/my-role", "123456789012"},
		{"arn:aws:iam::000000000000:user/alice", "000000000000"},
		{"not-an-arn", ""},
	}
	for _, tt := range tests {
		got := accountIDFromARN(tt.input)
		if got != tt.want {
			t.Errorf("accountIDFromARN(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
