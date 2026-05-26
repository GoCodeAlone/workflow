package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// runAWSValidate is a helper that initialises a `cloud.account` (provider=aws)
// with the given credentials config, runs `step.cloud_validate`, and returns
// the `valid` output. Each sub-test of TestCloudValidateStep_AWS_Markers uses
// a fresh MockApplication to keep the service-registry entries isolated.
func runAWSValidate(t *testing.T, name string, credentials map[string]any) bool {
	t.Helper()
	cfg := map[string]any{
		"provider": "aws",
		"region":   "us-east-1",
	}
	if credentials != nil {
		cfg["credentials"] = credentials
	}
	acc := module.NewCloudAccount(name, cfg)

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account %q Init failed: %v", name, err)
	}

	factory := module.NewCloudValidateStepFactory()
	step, err := factory("validate-"+name, map[string]any{"account": name}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{
		Current: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	valid, _ := result.Output["valid"].(bool)
	return valid
}

// TestCloudValidateStep_AWS_StaticKeys verifies the classic static-key path
// still validates after the Task 13 resolver rewrite.
func TestCloudValidateStep_AWS_StaticKeys(t *testing.T) {
	if !runAWSValidate(t, "aws-static", map[string]any{
		"type":      "static",
		"accessKey": "AKIA-test",
		"secretKey": "secret-test",
	}) {
		t.Error("expected valid=true for static AccessKey+SecretKey, got false")
	}
}

// TestCloudValidateStep_AWS_ProfileMarker verifies that a profile-resolved
// account (no access/secret keys, only Extra["credential_source"]) validates.
func TestCloudValidateStep_AWS_ProfileMarker(t *testing.T) {
	if !runAWSValidate(t, "aws-profile", map[string]any{
		"type":    "profile",
		"profile": "team-prod",
	}) {
		t.Error("expected valid=true for profile credential_source marker, got false")
	}
}

// TestCloudValidateStep_AWS_RoleARNMarker verifies the role_arn marker path:
// the resolver populates RoleARN + Extra["credential_source"]="role_arn", no
// AccessKey/SecretKey, and validate must accept it.
func TestCloudValidateStep_AWS_RoleARNMarker(t *testing.T) {
	if !runAWSValidate(t, "aws-role", map[string]any{
		"type":    "role_arn",
		"roleArn": "arn:aws:iam::123456789012:role/workflow",
	}) {
		t.Error("expected valid=true for role_arn marker, got false")
	}
}

// TestCloudValidateStep_AWS_EnvMarker verifies the env resolver path: when
// AWS_* env vars are unset the resolver leaves AccessKey/SecretKey empty and
// validate must report invalid (no marker is emitted for the env type).
func TestCloudValidateStep_AWS_EnvUnset(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_ACCESS_KEY", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SECRET_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_ROLE_ARN", "")

	if runAWSValidate(t, "aws-env-unset", map[string]any{"type": "env"}) {
		t.Error("expected valid=false when AWS_* env vars are all unset, got true")
	}
}

// TestCloudValidateStep_AWS_EnvSet verifies the env resolver path: when
// AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY are set the resolver populates
// the credential fields and validate reports valid.
func TestCloudValidateStep_AWS_EnvSet(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA-env")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-env")

	if !runAWSValidate(t, "aws-env-set", map[string]any{"type": "env"}) {
		t.Error("expected valid=true for env credentials, got false")
	}
}
