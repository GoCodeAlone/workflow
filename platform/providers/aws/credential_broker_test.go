//go:build aws

package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockSTSClient struct {
	assumeRoleFunc func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

func (m *mockSTSClient) AssumeRole(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if m.assumeRoleFunc != nil {
		return m.assumeRoleFunc(ctx, params, optFns...)
	}
	exp := time.Now().Add(time.Hour)
	return &sts.AssumeRoleOutput{
		Credentials: &ststypes.Credentials{
			AccessKeyId:     aws.String("AKIAIOSFODNN7EXAMPLE"),
			SecretAccessKey: aws.String("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
			SessionToken:    aws.String("FwoGZX..."),
			Expiration:      &exp,
		},
	}, nil
}

func newTestCredBroker(client STSClient) *AWSCredentialBroker {
	return &AWSCredentialBroker{
		stsClient: client,
		roleARN:   "arn:aws:iam::123456789:role/test-role",
	}
}

func TestCredentialBroker_IssueCredential(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{})
	ctx := context.Background()

	pctx := &platform.PlatformContext{
		Org:         "acme",
		Environment: "prod",
		Tier:        platform.TierInfrastructure,
	}
	req := platform.CredentialRequest{
		Name: "db-creds",
		Type: "database",
		TTL:  30 * time.Minute,
	}

	ref, err := broker.IssueCredential(ctx, pctx, req)
	if err != nil {
		t.Fatalf("IssueCredential() error: %v", err)
	}
	if ref == nil {
		t.Fatal("IssueCredential() returned nil")
	}
	if ref.Name != "db-creds" {
		t.Errorf("Name = %q, want db-creds", ref.Name)
	}
	if ref.Provider != "aws" {
		t.Errorf("Provider = %q, want aws", ref.Provider)
	}
	if ref.Tier != platform.TierInfrastructure {
		t.Errorf("Tier = %v, want TierInfrastructure", ref.Tier)
	}
	if ref.ContextPath != "acme/prod" {
		t.Errorf("ContextPath = %q, want acme/prod", ref.ContextPath)
	}
	if ref.ID == "" {
		t.Error("ID is empty")
	}
	if ref.SecretPath == "" {
		t.Error("SecretPath is empty")
	}
}

func TestCredentialBroker_IssueCredentialWithScope(t *testing.T) {
	var receivedPolicy *string
	broker := newTestCredBroker(&mockSTSClient{
		assumeRoleFunc: func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
			receivedPolicy = params.Policy
			exp := time.Now().Add(time.Hour)
			return &sts.AssumeRoleOutput{
				Credentials: &ststypes.Credentials{Expiration: &exp},
			}, nil
		},
	})
	ctx := context.Background()

	pctx := &platform.PlatformContext{Org: "acme", Environment: "prod"}
	req := platform.CredentialRequest{
		Name:  "scoped",
		Scope: []string{"resource-a", "resource-b"},
	}

	_, err := broker.IssueCredential(ctx, pctx, req)
	if err != nil {
		t.Fatalf("IssueCredential() error: %v", err)
	}
	if receivedPolicy == nil {
		t.Error("expected policy to be set when scope is provided")
	}
}

func TestCredentialBroker_IssueCredentialDefaultTTL(t *testing.T) {
	var receivedDuration *int32
	broker := newTestCredBroker(&mockSTSClient{
		assumeRoleFunc: func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
			receivedDuration = params.DurationSeconds
			exp := time.Now().Add(time.Hour)
			return &sts.AssumeRoleOutput{
				Credentials: &ststypes.Credentials{Expiration: &exp},
			}, nil
		},
	})
	ctx := context.Background()

	pctx := &platform.PlatformContext{Org: "acme", Environment: "prod"}
	req := platform.CredentialRequest{Name: "test"}

	_, err := broker.IssueCredential(ctx, pctx, req)
	if err != nil {
		t.Fatalf("IssueCredential() error: %v", err)
	}
	if receivedDuration == nil || *receivedDuration != 3600 {
		t.Errorf("DurationSeconds = %v, want 3600 (default 1 hour)", receivedDuration)
	}
}

func TestCredentialBroker_IssueCredentialError(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{
		assumeRoleFunc: func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	})
	ctx := context.Background()

	pctx := &platform.PlatformContext{Org: "acme", Environment: "prod"}
	req := platform.CredentialRequest{Name: "test"}

	_, err := broker.IssueCredential(ctx, pctx, req)
	if err == nil {
		t.Fatal("expected error from STS failure")
	}
}

func TestCredentialBroker_IssueCredentialLongSessionName(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{
		assumeRoleFunc: func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
			if len(*params.RoleSessionName) > 64 {
				t.Errorf("session name too long: %d chars", len(*params.RoleSessionName))
			}
			exp := time.Now().Add(time.Hour)
			return &sts.AssumeRoleOutput{
				Credentials: &ststypes.Credentials{Expiration: &exp},
			}, nil
		},
	})
	ctx := context.Background()

	pctx := &platform.PlatformContext{
		Org:         "very-long-organization-name-that-exceeds-normal-lengths",
		Environment: "production-environment",
		Application: "my-super-long-application-name",
	}
	req := platform.CredentialRequest{Name: "a-long-credential-name-here"}

	_, err := broker.IssueCredential(ctx, pctx, req)
	if err != nil {
		t.Fatalf("IssueCredential() error: %v", err)
	}
}

func TestCredentialBroker_IssueCredentialNilExpiration(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{
		assumeRoleFunc: func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
			return &sts.AssumeRoleOutput{
				Credentials: nil,
			}, nil
		},
	})
	ctx := context.Background()

	pctx := &platform.PlatformContext{Org: "acme", Environment: "prod"}
	req := platform.CredentialRequest{Name: "test", TTL: time.Hour}

	ref, err := broker.IssueCredential(ctx, pctx, req)
	if err != nil {
		t.Fatalf("IssueCredential() error: %v", err)
	}
	// Should use TTL-based expiration as fallback
	if ref.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
}

func TestCredentialBroker_RevokeCredential(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{})
	ctx := context.Background()

	ref := &platform.CredentialRef{ID: "test-id"}
	err := broker.RevokeCredential(ctx, ref)
	if err != nil {
		t.Fatalf("RevokeCredential() error: %v", err)
	}
}

func TestCredentialBroker_ResolveCredential(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{})
	ctx := context.Background()

	ref := &platform.CredentialRef{ID: "test-id-123"}
	val, err := broker.ResolveCredential(ctx, ref)
	if err != nil {
		t.Fatalf("ResolveCredential() error: %v", err)
	}
	if val != "sts-session:test-id-123" {
		t.Errorf("resolved value = %q, want sts-session:test-id-123", val)
	}
}

func TestCredentialBroker_RotateCredential(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{})
	ctx := context.Background()

	ref := &platform.CredentialRef{
		ID:          "old-id",
		Name:        "db-creds",
		ContextPath: "acme/prod/api",
		Tier:        platform.TierSharedPrimitive,
	}

	newRef, err := broker.RotateCredential(ctx, ref)
	if err != nil {
		t.Fatalf("RotateCredential() error: %v", err)
	}
	if newRef.Name != "db-creds" {
		t.Errorf("Name = %q, want db-creds", newRef.Name)
	}
	if newRef.ID == "old-id" {
		t.Error("expected new ID after rotation")
	}
}

func TestCredentialBroker_ListCredentials(t *testing.T) {
	broker := newTestCredBroker(&mockSTSClient{})
	ctx := context.Background()

	refs, err := broker.ListCredentials(ctx, &platform.PlatformContext{})
	if err != nil {
		t.Fatalf("ListCredentials() error: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil list, got %v", refs)
	}
}

func TestCredentialExpiration(t *testing.T) {
	// nil credentials
	if !credentialExpiration(nil).IsZero() {
		t.Error("expected zero time for nil credentials")
	}

	// nil expiration
	creds := &ststypes.Credentials{}
	if !credentialExpiration(creds).IsZero() {
		t.Error("expected zero time for nil expiration")
	}

	// valid expiration
	exp := time.Now()
	creds.Expiration = &exp
	if credentialExpiration(creds) != exp {
		t.Error("expected matching expiration time")
	}
}
