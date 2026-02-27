package module

import (
	"fmt"
	"os"
)

func init() {
	RegisterCredentialResolver(&azureStaticResolver{})
	RegisterCredentialResolver(&azureEnvResolver{})
	RegisterCredentialResolver(&azureClientCredentialsResolver{})
	RegisterCredentialResolver(&azureManagedIdentityResolver{})
	RegisterCredentialResolver(&azureCLIResolver{})
}

// azureStaticResolver resolves Azure credentials from static config fields.
type azureStaticResolver struct{}

func (r *azureStaticResolver) Provider() string       { return "azure" }
func (r *azureStaticResolver) CredentialType() string { return "static" }

func (r *azureStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}
	m.creds.TenantID, _ = credsMap["tenant_id"].(string)
	m.creds.ClientID, _ = credsMap["client_id"].(string)
	m.creds.ClientSecret, _ = credsMap["client_secret"].(string)
	if sub, ok := credsMap["subscription_id"].(string); ok {
		m.creds.SubscriptionID = sub
	}
	return nil
}

// azureEnvResolver resolves Azure credentials from environment variables.
type azureEnvResolver struct{}

func (r *azureEnvResolver) Provider() string       { return "azure" }
func (r *azureEnvResolver) CredentialType() string { return "env" }

func (r *azureEnvResolver) Resolve(m *CloudAccount) error {
	m.creds.TenantID = os.Getenv("AZURE_TENANT_ID")
	m.creds.ClientID = os.Getenv("AZURE_CLIENT_ID")
	m.creds.ClientSecret = os.Getenv("AZURE_CLIENT_SECRET")
	if sub := os.Getenv("AZURE_SUBSCRIPTION_ID"); sub != "" {
		m.creds.SubscriptionID = sub
	}
	return nil
}

// azureClientCredentialsResolver resolves Azure service principal client credentials.
type azureClientCredentialsResolver struct{}

func (r *azureClientCredentialsResolver) Provider() string       { return "azure" }
func (r *azureClientCredentialsResolver) CredentialType() string { return "client_credentials" }

func (r *azureClientCredentialsResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return fmt.Errorf("client_credentials requires tenant_id, client_id, and client_secret")
	}
	m.creds.TenantID, _ = credsMap["tenant_id"].(string)
	m.creds.ClientID, _ = credsMap["client_id"].(string)
	m.creds.ClientSecret, _ = credsMap["client_secret"].(string)
	if m.creds.TenantID == "" || m.creds.ClientID == "" || m.creds.ClientSecret == "" {
		return fmt.Errorf("client_credentials requires tenant_id, client_id, and client_secret")
	}
	return nil
}

// azureManagedIdentityResolver handles Azure Managed Identity (VMs, AKS, etc.).
// Optional client_id selects a user-assigned managed identity.
// Production: use github.com/Azure/azure-sdk-for-go/sdk/azidentity ManagedIdentityCredential.
type azureManagedIdentityResolver struct{}

func (r *azureManagedIdentityResolver) Provider() string       { return "azure" }
func (r *azureManagedIdentityResolver) CredentialType() string { return "managed_identity" }

func (r *azureManagedIdentityResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		if clientID, ok := credsMap["client_id"].(string); ok {
			m.creds.ClientID = clientID
		}
	}
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["credential_source"] = "managed_identity"
	return nil
}

// azureCLIResolver handles Azure CLI credentials (az login).
// Production: use github.com/Azure/azure-sdk-for-go/sdk/azidentity AzureCLICredential.
type azureCLIResolver struct{}

func (r *azureCLIResolver) Provider() string       { return "azure" }
func (r *azureCLIResolver) CredentialType() string { return "cli" }

func (r *azureCLIResolver) Resolve(m *CloudAccount) error {
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["credential_source"] = "azure_cli"
	return nil
}
