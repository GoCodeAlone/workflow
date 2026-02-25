package module_test

import (
	"context"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// TestCloudAccount_MockProvider verifies that the mock provider always
// returns valid credentials without any configuration.
func TestCloudAccount_MockProvider(t *testing.T) {
	acc := module.NewCloudAccount("test-cloud", map[string]any{
		"provider": "mock",
		"region":   "us-test-1",
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if acc.Provider() != "mock" {
		t.Errorf("want provider mock, got %q", acc.Provider())
	}
	if acc.Region() != "us-test-1" {
		t.Errorf("want region us-test-1, got %q", acc.Region())
	}

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if creds.AccessKey == "" {
		t.Error("expected non-empty AccessKey from mock provider")
	}
	if creds.SecretKey == "" {
		t.Error("expected non-empty SecretKey from mock provider")
	}
}

// TestCloudAccount_MockProvider_WithStaticConfig verifies static keys work on mock.
func TestCloudAccount_MockProvider_WithStaticConfig(t *testing.T) {
	acc := module.NewCloudAccount("test-cloud", map[string]any{
		"provider": "mock",
		"region":   "us-test-1",
		"credentials": map[string]any{
			"accessKey": "test-key",
			"secretKey": "test-secret",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if creds.AccessKey != "test-key" {
		t.Errorf("want AccessKey=test-key, got %q", creds.AccessKey)
	}
	if creds.SecretKey != "test-secret" {
		t.Errorf("want SecretKey=test-secret, got %q", creds.SecretKey)
	}
}

// TestCloudAccount_StaticAWS verifies static credential resolution for AWS.
func TestCloudAccount_StaticAWS(t *testing.T) {
	acc := module.NewCloudAccount("aws-prod", map[string]any{
		"provider": "aws",
		"region":   "us-east-1",
		"credentials": map[string]any{
			"type":      "static",
			"accessKey": "AKIAIOSFODNN7EXAMPLE",
			"secretKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if creds.Provider != "aws" {
		t.Errorf("want provider=aws, got %q", creds.Provider)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("want region=us-east-1, got %q", creds.Region)
	}
	if creds.AccessKey != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("unexpected AccessKey: %q", creds.AccessKey)
	}
	if creds.SecretKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("unexpected SecretKey")
	}
}

// TestCloudAccount_EnvAWS verifies environment variable credential resolution.
func TestCloudAccount_EnvAWS(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "env-session-token")

	acc := module.NewCloudAccount("aws-env", map[string]any{
		"provider": "aws",
		"region":   "eu-west-1",
		"credentials": map[string]any{
			"type": "env",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if creds.AccessKey != "env-access-key" {
		t.Errorf("want env-access-key, got %q", creds.AccessKey)
	}
	if creds.SecretKey != "env-secret-key" {
		t.Errorf("want env-secret-key, got %q", creds.SecretKey)
	}
	if creds.SessionToken != "env-session-token" {
		t.Errorf("want env-session-token, got %q", creds.SessionToken)
	}
}

// TestCloudAccount_KubernetesKubeconfig verifies kubeconfig credential resolution.
func TestCloudAccount_KubernetesKubeconfig(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	fakeKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://minikube:6443
  name: minikube
contexts:
- context:
    cluster: minikube
    user: minikube
  name: minikube
current-context: minikube
users:
- name: minikube
  user:
    token: fake-token
`
	if _, err := tmpFile.WriteString(fakeKubeconfig); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}
	tmpFile.Close()

	acc := module.NewCloudAccount("local-cluster", map[string]any{
		"provider": "kubernetes",
		"credentials": map[string]any{
			"type":    "kubeconfig",
			"path":    tmpFile.Name(),
			"context": "minikube",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	creds, err := acc.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if len(creds.Kubeconfig) == 0 {
		t.Error("expected non-empty Kubeconfig")
	}
	if creds.Context != "minikube" {
		t.Errorf("want context=minikube, got %q", creds.Context)
	}
}

// TestCloudAccount_ServiceRegistryDiscovery verifies that after Init,
// the account is discoverable as a CloudCredentialProvider from the registry.
func TestCloudAccount_ServiceRegistryDiscovery(t *testing.T) {
	acc := module.NewCloudAccount("my-account", map[string]any{
		"provider": "mock",
		"region":   "us-east-1",
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	svc, ok := app.Services["my-account"]
	if !ok {
		t.Fatal("service 'my-account' not found in registry")
	}

	provider, ok := svc.(module.CloudCredentialProvider)
	if !ok {
		t.Fatalf("service does not implement CloudCredentialProvider, got %T", svc)
	}

	if provider.Provider() != "mock" {
		t.Errorf("want provider=mock, got %q", provider.Provider())
	}
}

// TestCloudAccount_ProvidesServices verifies the ProvidesServices declaration.
func TestCloudAccount_ProvidesServices(t *testing.T) {
	acc := module.NewCloudAccount("my-account", map[string]any{"provider": "mock"})
	svcs := acc.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("want 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-account" {
		t.Errorf("want service name=my-account, got %q", svcs[0].Name)
	}
}

// TestCloudValidateStep verifies that step.cloud_validate finds the account
// from the service registry and returns a valid result.
func TestCloudValidateStep(t *testing.T) {
	acc := module.NewCloudAccount("test-account", map[string]any{
		"provider": "mock",
		"region":   "us-test-1",
		"credentials": map[string]any{
			"accessKey": "test-key",
			"secretKey": "test-secret",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init failed: %v", err)
	}

	factory := module.NewCloudValidateStepFactory()
	step, err := factory("validate", map[string]any{"account": "test-account"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{
		Current: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v", result.Output["valid"])
	}
	if result.Output["provider"] != "mock" {
		t.Errorf("expected provider=mock, got %v", result.Output["provider"])
	}
	if result.Output["account"] != "test-account" {
		t.Errorf("expected account=test-account, got %v", result.Output["account"])
	}
}

// TestCloudValidateStep_MissingAccount verifies the factory requires an account name.
func TestCloudValidateStep_MissingAccount(t *testing.T) {
	factory := module.NewCloudValidateStepFactory()
	_, err := factory("validate", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error when account is missing")
	}
}

// TestCloudValidateStep_AccountNotFound verifies the step returns a clear error
// when the account service is not in the registry.
func TestCloudValidateStep_AccountNotFound(t *testing.T) {
	factory := module.NewCloudValidateStepFactory()
	step, err := factory("validate", map[string]any{"account": "missing-account"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error when account not in registry")
	}
}
