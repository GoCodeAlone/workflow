package module

import "os"

func init() {
	RegisterCredentialResolver(&awsStaticResolver{})
	RegisterCredentialResolver(&awsEnvResolver{})
	RegisterCredentialResolver(&awsProfileResolver{})
	RegisterCredentialResolver(&awsRoleARNResolver{})
}

// awsStaticResolver resolves AWS credentials from static config fields.
type awsStaticResolver struct{}

func (r *awsStaticResolver) Provider() string      { return "aws" }
func (r *awsStaticResolver) CredentialType() string { return "static" }

func (r *awsStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		m.creds.AccessKey, _ = credsMap["accessKey"].(string)
		m.creds.SecretKey, _ = credsMap["secretKey"].(string)
		m.creds.SessionToken, _ = credsMap["sessionToken"].(string)
		m.creds.RoleARN, _ = credsMap["roleArn"].(string)
	}
	return nil
}

// awsEnvResolver resolves AWS credentials from environment variables.
type awsEnvResolver struct{}

func (r *awsEnvResolver) Provider() string      { return "aws" }
func (r *awsEnvResolver) CredentialType() string { return "env" }

func (r *awsEnvResolver) Resolve(m *CloudAccount) error {
	m.creds.AccessKey = os.Getenv("AWS_ACCESS_KEY_ID")
	if m.creds.AccessKey == "" {
		m.creds.AccessKey = os.Getenv("AWS_ACCESS_KEY")
	}
	m.creds.SecretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	if m.creds.SecretKey == "" {
		m.creds.SecretKey = os.Getenv("AWS_SECRET_KEY")
	}
	m.creds.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
	m.creds.RoleARN = os.Getenv("AWS_ROLE_ARN")
	return nil
}

// awsProfileResolver resolves AWS credentials from a named profile.
type awsProfileResolver struct{}

func (r *awsProfileResolver) Provider() string      { return "aws" }
func (r *awsProfileResolver) CredentialType() string { return "profile" }

func (r *awsProfileResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	profile := ""
	if credsMap != nil {
		profile, _ = credsMap["profile"].(string)
	}
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}
	// Stub: production implementation would use aws-sdk-go-v2/config.LoadDefaultConfig
	// with config.WithSharedConfigProfile(profile).
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["profile"] = profile
	return nil
}

// awsRoleARNResolver resolves AWS credentials via STS AssumeRole.
type awsRoleARNResolver struct{}

func (r *awsRoleARNResolver) Provider() string      { return "aws" }
func (r *awsRoleARNResolver) CredentialType() string { return "role_arn" }

func (r *awsRoleARNResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}
	// Stub for STS AssumeRole.
	// Production implementation: use aws-sdk-go-v2/service/sts AssumeRole with
	// the source credentials, then populate AccessKey/SecretKey/SessionToken
	// from the returned Credentials.
	roleARN, _ := credsMap["roleArn"].(string)
	externalID, _ := credsMap["externalId"].(string)
	m.creds.RoleARN = roleARN
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["external_id"] = externalID
	return nil
}
