//go:build integration

package module_test

import (
	"context"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// TestCloudAccount_AWS_ValidateCredentials verifies that a cloud.account with
// real AWS credentials can call sts:GetCallerIdentity.
// Requires: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION env vars.
func TestCloudAccount_AWS_ValidateCredentials(t *testing.T) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	acc := module.NewCloudAccount("aws-integration", map[string]any{
		"provider": "aws",
		"region":   region,
		"credentials": map[string]any{
			"type": "env",
		},
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := acc.ValidateCredentials(context.Background()); err != nil {
		t.Fatalf("ValidateCredentials: %v", err)
	}
}

// TestCloudAccount_AWS_AWSConfig_Static verifies that a static credential
// cloud.account produces a valid aws.Config.
func TestCloudAccount_AWS_AWSConfig_Static(t *testing.T) {
	acc := module.NewCloudAccount("aws-static", map[string]any{
		"provider": "aws",
		"region":   "us-east-1",
		"credentials": map[string]any{
			"type":      "static",
			"accessKey": os.Getenv("AWS_ACCESS_KEY_ID"),
			"secretKey": os.Getenv("AWS_SECRET_ACCESS_KEY"),
		},
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	awsProv, ok := any(acc).(module.AWSConfigProvider)
	if !ok {
		t.Fatal("CloudAccount does not implement AWSConfigProvider")
	}
	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		t.Fatalf("AWSConfig: %v", err)
	}
	if cfg.Region == "" {
		t.Error("expected non-empty region in aws.Config")
	}
}
