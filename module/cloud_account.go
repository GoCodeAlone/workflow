package module

import (
	"context"
	"fmt"
	"os"

	"github.com/CrisisTextLine/modular"
)

// CloudCredentialProvider provides cloud credentials to other modules.
type CloudCredentialProvider interface {
	Provider() string
	Region() string
	GetCredentials(ctx context.Context) (*CloudCredentials, error)
}

// CloudCredentials holds resolved credentials for a cloud provider.
type CloudCredentials struct {
	Provider string
	Region   string
	// AWS
	AccessKey    string
	SecretKey    string
	SessionToken string
	RoleARN      string
	// GCP
	ProjectID          string
	ServiceAccountJSON []byte
	// Kubernetes
	Kubeconfig []byte
	Context    string
	// Generic
	Token string
	Extra map[string]string
}

// CloudAccount is a workflow module that stores cloud provider credentials
// and exposes them via the CloudCredentialProvider interface in the service registry.
type CloudAccount struct {
	name     string
	config   map[string]any
	provider string
	region   string
	creds    *CloudCredentials
}

// NewCloudAccount creates a new CloudAccount module.
func NewCloudAccount(name string, cfg map[string]any) *CloudAccount {
	return &CloudAccount{name: name, config: cfg}
}

// Name returns the module name.
func (m *CloudAccount) Name() string { return m.name }

// Init resolves credentials and registers the module as a service.
func (m *CloudAccount) Init(app modular.Application) error {
	m.provider, _ = m.config["provider"].(string)
	if m.provider == "" {
		m.provider = "mock"
	}
	m.region, _ = m.config["region"].(string)

	var err error
	m.creds, err = m.resolveCredentials()
	if err != nil {
		return fmt.Errorf("cloud.account %q: failed to resolve credentials: %w", m.name, err)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *CloudAccount) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Cloud account: " + m.name,
			Instance:    m,
		},
	}
}

// RequiresServices returns nil — cloud.account has no service dependencies.
func (m *CloudAccount) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Provider returns the cloud provider name (e.g. "aws", "gcp", "mock").
func (m *CloudAccount) Provider() string { return m.provider }

// Region returns the primary region.
func (m *CloudAccount) Region() string { return m.region }

// GetCredentials returns the resolved credentials.
func (m *CloudAccount) GetCredentials(_ context.Context) (*CloudCredentials, error) {
	if m.creds == nil {
		return nil, fmt.Errorf("cloud.account %q: credentials not initialized", m.name)
	}
	return m.creds, nil
}

// resolveCredentials resolves credentials based on provider and credential type config.
func (m *CloudAccount) resolveCredentials() (*CloudCredentials, error) {
	creds := &CloudCredentials{
		Provider: m.provider,
		Region:   m.region,
	}

	if m.provider == "mock" {
		return m.resolveMockCredentials(creds)
	}

	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		// No credentials configured — return empty (valid for some providers)
		return creds, nil
	}

	credType, _ := credsMap["type"].(string)
	if credType == "" {
		credType = "static"
	}

	switch credType {
	case "static":
		return m.resolveStaticCredentials(creds, credsMap)
	case "env":
		return m.resolveEnvCredentials(creds)
	case "profile":
		return m.resolveProfileCredentials(creds, credsMap)
	case "role_arn":
		return m.resolveRoleARNCredentials(creds, credsMap)
	case "kubeconfig":
		return m.resolveKubeconfigCredentials(creds, credsMap)
	default:
		return nil, fmt.Errorf("unsupported credential type %q", credType)
	}
}

func (m *CloudAccount) resolveMockCredentials(creds *CloudCredentials) (*CloudCredentials, error) {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		creds.AccessKey, _ = credsMap["accessKey"].(string)
		creds.SecretKey, _ = credsMap["secretKey"].(string)
		creds.Token, _ = credsMap["token"].(string)
	}
	if creds.AccessKey == "" {
		creds.AccessKey = "mock-access-key"
	}
	if creds.SecretKey == "" {
		creds.SecretKey = "mock-secret-key"
	}
	if creds.Region == "" {
		creds.Region = "us-mock-1"
	}
	return creds, nil
}

func (m *CloudAccount) resolveStaticCredentials(creds *CloudCredentials, credsMap map[string]any) (*CloudCredentials, error) {
	switch m.provider {
	case "aws":
		creds.AccessKey, _ = credsMap["accessKey"].(string)
		creds.SecretKey, _ = credsMap["secretKey"].(string)
		creds.SessionToken, _ = credsMap["sessionToken"].(string)
		creds.RoleARN, _ = credsMap["roleArn"].(string)
	case "gcp":
		creds.ProjectID, _ = credsMap["projectId"].(string)
		if saJSON, ok := credsMap["serviceAccountJson"].(string); ok {
			creds.ServiceAccountJSON = []byte(saJSON)
		}
	case "kubernetes":
		if kc, ok := credsMap["kubeconfig"].(string); ok {
			creds.Kubeconfig = []byte(kc)
		}
		creds.Context, _ = credsMap["context"].(string)
	default:
		creds.Token, _ = credsMap["token"].(string)
	}
	return creds, nil
}

func (m *CloudAccount) resolveEnvCredentials(creds *CloudCredentials) (*CloudCredentials, error) {
	switch m.provider {
	case "aws":
		creds.AccessKey = os.Getenv("AWS_ACCESS_KEY_ID")
		if creds.AccessKey == "" {
			creds.AccessKey = os.Getenv("AWS_ACCESS_KEY")
		}
		creds.SecretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
		if creds.SecretKey == "" {
			creds.SecretKey = os.Getenv("AWS_SECRET_KEY")
		}
		creds.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
		creds.RoleARN = os.Getenv("AWS_ROLE_ARN")
	case "gcp":
		creds.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if creds.ProjectID == "" {
			creds.ProjectID = os.Getenv("GCP_PROJECT_ID")
		}
		saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if saPath != "" {
			data, err := os.ReadFile(saPath)
			if err != nil {
				return nil, fmt.Errorf("reading GOOGLE_APPLICATION_CREDENTIALS: %w", err)
			}
			creds.ServiceAccountJSON = data
		}
	case "kubernetes":
		kubeconfigPath := os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			home, _ := os.UserHomeDir()
			kubeconfigPath = home + "/.kube/config"
		}
		data, err := os.ReadFile(kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("reading kubeconfig: %w", err)
		}
		creds.Kubeconfig = data
	default:
		creds.Token = os.Getenv("CLOUD_TOKEN")
	}
	return creds, nil
}

func (m *CloudAccount) resolveProfileCredentials(creds *CloudCredentials, credsMap map[string]any) (*CloudCredentials, error) {
	// AWS named profile from ~/.aws/credentials
	// For now: read AWS_PROFILE or the configured profile name from the shared credentials file.
	profile, _ := credsMap["profile"].(string)
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}
	// Stub: document STS/profile resolution path.
	// Production implementation would use aws-sdk-go-v2/config.LoadDefaultConfig
	// with config.WithSharedConfigProfile(profile).
	creds.Extra = map[string]string{"profile": profile}
	return creds, nil
}

func (m *CloudAccount) resolveRoleARNCredentials(creds *CloudCredentials, credsMap map[string]any) (*CloudCredentials, error) {
	// Stub for STS AssumeRole.
	// Production implementation: use aws-sdk-go-v2/service/sts AssumeRole with
	// the source credentials, then populate AccessKey/SecretKey/SessionToken
	// from the returned Credentials.
	roleARN, _ := credsMap["roleArn"].(string)
	externalID, _ := credsMap["externalId"].(string)
	creds.RoleARN = roleARN
	creds.Extra = map[string]string{"external_id": externalID}
	return creds, nil
}

func (m *CloudAccount) resolveKubeconfigCredentials(creds *CloudCredentials, credsMap map[string]any) (*CloudCredentials, error) {
	path, _ := credsMap["path"].(string)
	if path == "" {
		path = os.Getenv("KUBECONFIG")
	}
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home + "/.kube/config"
	}

	if inline, ok := credsMap["inline"].(string); ok && inline != "" {
		creds.Kubeconfig = []byte(inline)
	} else if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading kubeconfig at %q: %w", path, err)
		}
		creds.Kubeconfig = data
	}

	creds.Context, _ = credsMap["context"].(string)
	return creds, nil
}
