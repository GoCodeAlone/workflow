package module

import (
	"fmt"
	"os"
)

func init() {
	RegisterCredentialResolver(&gcpStaticResolver{})
	RegisterCredentialResolver(&gcpEnvResolver{})
	RegisterCredentialResolver(&gcpServiceAccountJSONResolver{})
	RegisterCredentialResolver(&gcpServiceAccountKeyResolver{})
	RegisterCredentialResolver(&gcpWorkloadIdentityResolver{})
	RegisterCredentialResolver(&gcpApplicationDefaultResolver{})
}

// gcpStaticResolver resolves GCP credentials from static config fields.
type gcpStaticResolver struct{}

func (r *gcpStaticResolver) Provider() string      { return "gcp" }
func (r *gcpStaticResolver) CredentialType() string { return "static" }

func (r *gcpStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}
	if pid, ok := credsMap["projectId"].(string); ok {
		m.creds.ProjectID = pid
	}
	if saJSON, ok := credsMap["serviceAccountJson"].(string); ok {
		m.creds.ServiceAccountJSON = []byte(saJSON)
	}
	return nil
}

// gcpEnvResolver resolves GCP credentials from environment variables.
type gcpEnvResolver struct{}

func (r *gcpEnvResolver) Provider() string      { return "gcp" }
func (r *gcpEnvResolver) CredentialType() string { return "env" }

func (r *gcpEnvResolver) Resolve(m *CloudAccount) error {
	m.creds.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	if m.creds.ProjectID == "" {
		m.creds.ProjectID = os.Getenv("GCP_PROJECT_ID")
	}
	saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if saPath != "" {
		data, err := os.ReadFile(saPath) //nolint:gosec // G304: path from trusted config data
		if err != nil {
			return fmt.Errorf("reading GOOGLE_APPLICATION_CREDENTIALS: %w", err)
		}
		m.creds.ServiceAccountJSON = data
	}
	return nil
}

// gcpServiceAccountJSONResolver reads a GCP service account JSON key file from the given path.
type gcpServiceAccountJSONResolver struct{}

func (r *gcpServiceAccountJSONResolver) Provider() string      { return "gcp" }
func (r *gcpServiceAccountJSONResolver) CredentialType() string { return "service_account_json" }

func (r *gcpServiceAccountJSONResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return fmt.Errorf("service_account_json credential requires 'path'")
	}
	path, _ := credsMap["path"].(string)
	if path == "" {
		return fmt.Errorf("service_account_json credential requires 'path'")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from trusted config data
	if err != nil {
		return fmt.Errorf("reading service account JSON at %q: %w", path, err)
	}
	m.creds.ServiceAccountJSON = data
	return nil
}

// gcpServiceAccountKeyResolver uses an inline GCP service account JSON key.
type gcpServiceAccountKeyResolver struct{}

func (r *gcpServiceAccountKeyResolver) Provider() string      { return "gcp" }
func (r *gcpServiceAccountKeyResolver) CredentialType() string { return "service_account_key" }

func (r *gcpServiceAccountKeyResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return fmt.Errorf("service_account_key credential requires 'key'")
	}
	key, _ := credsMap["key"].(string)
	if key == "" {
		return fmt.Errorf("service_account_key credential requires 'key'")
	}
	m.creds.ServiceAccountJSON = []byte(key)
	return nil
}

// gcpWorkloadIdentityResolver handles GCP Workload Identity (GKE metadata server).
// Production: use golang.org/x/oauth2/google with google.FindDefaultCredentials.
type gcpWorkloadIdentityResolver struct{}

func (r *gcpWorkloadIdentityResolver) Provider() string      { return "gcp" }
func (r *gcpWorkloadIdentityResolver) CredentialType() string { return "workload_identity" }

func (r *gcpWorkloadIdentityResolver) Resolve(m *CloudAccount) error {
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["credential_source"] = "workload_identity"
	return nil
}

// gcpApplicationDefaultResolver resolves GCP Application Default Credentials.
// Reads GOOGLE_APPLICATION_CREDENTIALS if set; otherwise records the ADC source.
type gcpApplicationDefaultResolver struct{}

func (r *gcpApplicationDefaultResolver) Provider() string      { return "gcp" }
func (r *gcpApplicationDefaultResolver) CredentialType() string { return "application_default" }

func (r *gcpApplicationDefaultResolver) Resolve(m *CloudAccount) error {
	if m.creds.ProjectID == "" {
		m.creds.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if m.creds.ProjectID == "" {
			m.creds.ProjectID = os.Getenv("GCP_PROJECT_ID")
		}
	}
	saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if saPath != "" {
		data, err := os.ReadFile(saPath) //nolint:gosec // G304: path from trusted config data
		if err != nil {
			return fmt.Errorf("reading GOOGLE_APPLICATION_CREDENTIALS: %w", err)
		}
		m.creds.ServiceAccountJSON = data
		return nil
	}
	// No explicit file — production would use the ADC chain (gcloud, metadata server, etc.)
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["credential_source"] = "application_default"
	return nil
}
