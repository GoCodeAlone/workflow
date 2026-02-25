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

// TestCloudAccount_GCP_ServiceAccountJSON verifies reading a GCP service account
// JSON key from a file on disk.
func TestCloudAccount_GCP_ServiceAccountJSON(t *testing.T) {
	fakeJSON := `{"type":"service_account","project_id":"my-proj","private_key":"fake"}`
	tmpFile, err := os.CreateTemp(t.TempDir(), "sa-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := tmpFile.WriteString(fakeJSON); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	tmpFile.Close()

	acc := module.NewCloudAccount("gcp-prod", map[string]any{
		"provider":   "gcp",
		"region":     "us-central1",
		"project_id": "my-proj",
		"credentials": map[string]any{
			"type": "service_account_json",
			"path": tmpFile.Name(),
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
	if creds.Provider != "gcp" {
		t.Errorf("want provider=gcp, got %q", creds.Provider)
	}
	if creds.ProjectID != "my-proj" {
		t.Errorf("want project_id=my-proj, got %q", creds.ProjectID)
	}
	if string(creds.ServiceAccountJSON) != fakeJSON {
		t.Errorf("unexpected ServiceAccountJSON: %q", string(creds.ServiceAccountJSON))
	}
}

// TestCloudAccount_GCP_ServiceAccountKey verifies inline JSON key content.
func TestCloudAccount_GCP_ServiceAccountKey(t *testing.T) {
	inlineKey := `{"type":"service_account","project_id":"inline-proj"}`

	acc := module.NewCloudAccount("gcp-inline", map[string]any{
		"provider": "gcp",
		"region":   "europe-west1",
		"credentials": map[string]any{
			"type": "service_account_key",
			"key":  inlineKey,
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
	if string(creds.ServiceAccountJSON) != inlineKey {
		t.Errorf("unexpected ServiceAccountJSON: %q", string(creds.ServiceAccountJSON))
	}
}

// TestCloudAccount_GCP_WorkloadIdentity verifies workload identity returns a placeholder credential_source.
func TestCloudAccount_GCP_WorkloadIdentity(t *testing.T) {
	acc := module.NewCloudAccount("gcp-gke", map[string]any{
		"provider":   "gcp",
		"region":     "us-central1",
		"project_id": "gke-project",
		"credentials": map[string]any{
			"type": "workload_identity",
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
	if creds.Extra["credential_source"] != "workload_identity" {
		t.Errorf("expected credential_source=workload_identity, got %q", creds.Extra["credential_source"])
	}
	if creds.ProjectID != "gke-project" {
		t.Errorf("want project_id=gke-project, got %q", creds.ProjectID)
	}
}

// TestCloudAccount_GCP_ApplicationDefault verifies application_default reads GOOGLE_APPLICATION_CREDENTIALS.
func TestCloudAccount_GCP_ApplicationDefault(t *testing.T) {
	fakeJSON := `{"type":"service_account","project_id":"adc-proj"}`
	tmpFile, err := os.CreateTemp(t.TempDir(), "adc-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := tmpFile.WriteString(fakeJSON); err != nil {
		t.Fatalf("write: %v", err)
	}
	tmpFile.Close()

	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", tmpFile.Name())
	t.Setenv("GOOGLE_CLOUD_PROJECT", "adc-proj")

	acc := module.NewCloudAccount("gcp-adc", map[string]any{
		"provider": "gcp",
		"region":   "us-east1",
		"credentials": map[string]any{
			"type": "application_default",
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
	if string(creds.ServiceAccountJSON) != fakeJSON {
		t.Errorf("unexpected ServiceAccountJSON: %q", string(creds.ServiceAccountJSON))
	}
	if creds.ProjectID != "adc-proj" {
		t.Errorf("want project_id=adc-proj, got %q", creds.ProjectID)
	}
}

// TestCloudAccount_Azure_ClientCredentials verifies Azure service principal resolution.
func TestCloudAccount_Azure_ClientCredentials(t *testing.T) {
	acc := module.NewCloudAccount("azure-prod", map[string]any{
		"provider":        "azure",
		"region":          "eastus",
		"subscription_id": "sub-123",
		"credentials": map[string]any{
			"type":          "client_credentials",
			"tenant_id":     "tenant-abc",
			"client_id":     "client-def",
			"client_secret": "secret-xyz",
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
	if creds.Provider != "azure" {
		t.Errorf("want provider=azure, got %q", creds.Provider)
	}
	if creds.TenantID != "tenant-abc" {
		t.Errorf("want tenant_id=tenant-abc, got %q", creds.TenantID)
	}
	if creds.ClientID != "client-def" {
		t.Errorf("want client_id=client-def, got %q", creds.ClientID)
	}
	if creds.ClientSecret != "secret-xyz" {
		t.Errorf("want client_secret=secret-xyz, got %q", creds.ClientSecret)
	}
	if creds.SubscriptionID != "sub-123" {
		t.Errorf("want subscription_id=sub-123, got %q", creds.SubscriptionID)
	}
	if creds.Region != "eastus" {
		t.Errorf("want region=eastus, got %q", creds.Region)
	}
}

// TestCloudAccount_Azure_ClientCredentials_MissingFields verifies error on incomplete service principal.
func TestCloudAccount_Azure_ClientCredentials_MissingFields(t *testing.T) {
	acc := module.NewCloudAccount("azure-bad", map[string]any{
		"provider": "azure",
		"credentials": map[string]any{
			"type":      "client_credentials",
			"tenant_id": "tenant-abc",
			// missing client_id and client_secret
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err == nil {
		t.Error("expected error when client_credentials is missing required fields")
	}
}

// TestCloudAccount_Azure_ManagedIdentity verifies managed identity credential_source.
func TestCloudAccount_Azure_ManagedIdentity(t *testing.T) {
	acc := module.NewCloudAccount("azure-mi", map[string]any{
		"provider":        "azure",
		"region":          "westus2",
		"subscription_id": "sub-456",
		"credentials": map[string]any{
			"type":      "managed_identity",
			"client_id": "user-assigned-mi-id",
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
	if creds.ClientID != "user-assigned-mi-id" {
		t.Errorf("want client_id=user-assigned-mi-id, got %q", creds.ClientID)
	}
	if creds.Extra["credential_source"] != "managed_identity" {
		t.Errorf("expected credential_source=managed_identity, got %q", creds.Extra["credential_source"])
	}
	if creds.SubscriptionID != "sub-456" {
		t.Errorf("want subscription_id=sub-456, got %q", creds.SubscriptionID)
	}
}

// TestCloudAccount_Azure_EnvVars verifies Azure env credential type reads AZURE_* variables.
func TestCloudAccount_Azure_EnvVars(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "env-tenant")
	t.Setenv("AZURE_CLIENT_ID", "env-client")
	t.Setenv("AZURE_CLIENT_SECRET", "env-secret")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "env-sub")

	acc := module.NewCloudAccount("azure-env", map[string]any{
		"provider": "azure",
		"region":   "northeurope",
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
	if creds.TenantID != "env-tenant" {
		t.Errorf("want tenant_id=env-tenant, got %q", creds.TenantID)
	}
	if creds.ClientID != "env-client" {
		t.Errorf("want client_id=env-client, got %q", creds.ClientID)
	}
	if creds.ClientSecret != "env-secret" {
		t.Errorf("want client_secret=env-secret, got %q", creds.ClientSecret)
	}
	if creds.SubscriptionID != "env-sub" {
		t.Errorf("want subscription_id=env-sub, got %q", creds.SubscriptionID)
	}
}

// TestCloudAccount_Azure_CLI verifies Azure CLI credential_source.
func TestCloudAccount_Azure_CLI(t *testing.T) {
	acc := module.NewCloudAccount("azure-cli", map[string]any{
		"provider":        "azure",
		"region":          "eastus2",
		"subscription_id": "cli-sub",
		"credentials": map[string]any{
			"type": "cli",
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
	if creds.Extra["credential_source"] != "azure_cli" {
		t.Errorf("expected credential_source=azure_cli, got %q", creds.Extra["credential_source"])
	}
}

// TestCloudValidateStep_GCP verifies step.cloud_validate works for a GCP account.
func TestCloudValidateStep_GCP(t *testing.T) {
	fakeJSON := `{"type":"service_account","project_id":"test-proj"}`

	acc := module.NewCloudAccount("gcp-account", map[string]any{
		"provider":   "gcp",
		"region":     "us-central1",
		"project_id": "test-proj",
		"credentials": map[string]any{
			"type": "service_account_key",
			"key":  fakeJSON,
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	factory := module.NewCloudValidateStepFactory()
	step, err := factory("validate-gcp", map[string]any{"account": "gcp-account"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v", result.Output["valid"])
	}
	if result.Output["provider"] != "gcp" {
		t.Errorf("expected provider=gcp, got %v", result.Output["provider"])
	}
	if result.Output["project_id"] != "test-proj" {
		t.Errorf("expected project_id=test-proj, got %v", result.Output["project_id"])
	}
}

// TestCloudValidateStep_Azure verifies step.cloud_validate works for an Azure account.
func TestCloudValidateStep_Azure(t *testing.T) {
	acc := module.NewCloudAccount("azure-account", map[string]any{
		"provider":        "azure",
		"region":          "eastus",
		"subscription_id": "my-sub",
		"credentials": map[string]any{
			"type":          "client_credentials",
			"tenant_id":     "my-tenant",
			"client_id":     "my-client",
			"client_secret": "my-secret",
		},
	})

	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	factory := module.NewCloudValidateStepFactory()
	step, err := factory("validate-azure", map[string]any{"account": "azure-account"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["valid"] != true {
		t.Errorf("expected valid=true, got %v", result.Output["valid"])
	}
	if result.Output["provider"] != "azure" {
		t.Errorf("expected provider=azure, got %v", result.Output["provider"])
	}
	if result.Output["tenant_id"] != "my-tenant" {
		t.Errorf("expected tenant_id=my-tenant, got %v", result.Output["tenant_id"])
	}
	if result.Output["subscription_id"] != "my-sub" {
		t.Errorf("expected subscription_id=my-sub, got %v", result.Output["subscription_id"])
	}
}

// TestCloudAccount_InvalidCredentialType verifies that an unsupported credential type returns an error.
func TestCloudAccount_InvalidCredentialType(t *testing.T) {
	for _, provider := range []string{"gcp", "azure", "aws"} {
		acc := module.NewCloudAccount("bad-creds", map[string]any{
			"provider": provider,
			"credentials": map[string]any{
				"type": "unsupported_type",
			},
		})
		app := module.NewMockApplication()
		if err := acc.Init(app); err == nil {
			t.Errorf("provider=%s: expected error for unsupported credential type, got nil", provider)
		}
	}
}
