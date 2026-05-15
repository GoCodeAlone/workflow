package module_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// captureLog redirects log output to a buffer for the duration of fn and
// returns the captured bytes. The default log destination is restored when fn
// returns.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(orig)
		log.SetFlags(flags)
	}()
	fn()
	return buf.String()
}

// TestCloudAccount_AWS_ProfileResolver_Marker verifies the awsProfileResolver
// declares Extra["credential_source"]="profile" + Extra["profile"]=<name> and
// does NOT touch the AWS SDK. The actual profile resolution is deferred to the
// aws plugin (decisions/0036 + 0038).
func TestCloudAccount_AWS_ProfileResolver_Marker(t *testing.T) {
	acc := module.NewCloudAccount("aws-profile", map[string]any{
		"provider": "aws",
		"region":   "us-west-2",
		"credentials": map[string]any{
			"type":    "profile",
			"profile": "my-team",
		},
	})

	app := module.NewMockApplication()
	logged := captureLog(t, func() {
		if err := acc.Init(app); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
	})

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got := creds.Extra["credential_source"]; got != "profile" {
		t.Errorf("Extra[credential_source] = %q, want %q", got, "profile")
	}
	if got := creds.Extra["profile"]; got != "my-team" {
		t.Errorf("Extra[profile] = %q, want %q", got, "my-team")
	}
	// Resolution is deferred — the core resolver must not populate access/secret keys.
	if creds.AccessKey != "" || creds.SecretKey != "" {
		t.Errorf("expected empty AccessKey/SecretKey (resolution deferred), got %q/%q", creds.AccessKey, creds.SecretKey)
	}
	if !strings.Contains(logged, `credential_source="profile"`) {
		t.Errorf("expected warning log mentioning credential_source=\"profile\", got: %q", logged)
	}
}

// TestCloudAccount_AWS_ProfileResolver_DefaultProfile verifies the resolver
// falls back to "default" when neither the config nor AWS_PROFILE is set.
func TestCloudAccount_AWS_ProfileResolver_DefaultProfile(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")

	acc := module.NewCloudAccount("aws-default-profile", map[string]any{
		"provider": "aws",
		"region":   "us-west-2",
		"credentials": map[string]any{
			"type": "profile",
		},
	})

	app := module.NewMockApplication()
	captureLog(t, func() {
		if err := acc.Init(app); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
	})

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got := creds.Extra["profile"]; got != "default" {
		t.Errorf("Extra[profile] = %q, want %q", got, "default")
	}
	if got := creds.Extra["credential_source"]; got != "profile" {
		t.Errorf("Extra[credential_source] = %q, want %q", got, "profile")
	}
}

// TestCloudAccount_AWS_RoleARNResolver_Marker verifies awsRoleARNResolver
// declares Extra["credential_source"]="role_arn", records the role ARN +
// external_id, and does NOT call STS. Resolution is deferred to the aws plugin.
func TestCloudAccount_AWS_RoleARNResolver_Marker(t *testing.T) {
	acc := module.NewCloudAccount("aws-role", map[string]any{
		"provider": "aws",
		"region":   "us-east-1",
		"credentials": map[string]any{
			"type":       "role_arn",
			"roleArn":    "arn:aws:iam::123456789012:role/workflow",
			"externalId": "ext-token-7",
		},
	})

	app := module.NewMockApplication()
	logged := captureLog(t, func() {
		if err := acc.Init(app); err != nil {
			t.Fatalf("Init failed: %v", err)
		}
	})

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got := creds.Extra["credential_source"]; got != "role_arn" {
		t.Errorf("Extra[credential_source] = %q, want %q", got, "role_arn")
	}
	if creds.RoleARN != "arn:aws:iam::123456789012:role/workflow" {
		t.Errorf("RoleARN = %q, want %q", creds.RoleARN, "arn:aws:iam::123456789012:role/workflow")
	}
	if got := creds.Extra["external_id"]; got != "ext-token-7" {
		t.Errorf("Extra[external_id] = %q, want %q", got, "ext-token-7")
	}
	// Resolution deferred — no STS call, so no temporary keys.
	if creds.AccessKey != "" || creds.SecretKey != "" || creds.SessionToken != "" {
		t.Errorf("expected empty access/secret/session (resolution deferred), got %q/%q/%q",
			creds.AccessKey, creds.SecretKey, creds.SessionToken)
	}
	if !strings.Contains(logged, `credential_source="role_arn"`) {
		t.Errorf("expected warning log mentioning credential_source=\"role_arn\", got: %q", logged)
	}
}

// TestCloudAccount_AWS_RoleARNResolver_MissingRoleArn verifies the required-check:
// an empty roleArn must surface as an error from Init (propagated by resolveCredentials).
func TestCloudAccount_AWS_RoleARNResolver_MissingRoleArn(t *testing.T) {
	acc := module.NewCloudAccount("aws-role-missing", map[string]any{
		"provider": "aws",
		"region":   "us-east-1",
		"credentials": map[string]any{
			"type":    "role_arn",
			"roleArn": "",
		},
	})

	app := module.NewMockApplication()
	err := acc.Init(app)
	if err == nil {
		t.Fatal("expected Init to fail for empty roleArn, got nil")
	}
	if !strings.Contains(err.Error(), "roleArn is required") {
		t.Errorf("error = %q, want substring %q", err.Error(), "roleArn is required")
	}
}
