package module

import (
	"fmt"
	"log"
	"os"
)

func init() {
	RegisterCredentialResolver(&awsStaticResolver{})
	RegisterCredentialResolver(&awsEnvResolver{})
	RegisterCredentialResolver(&awsProfileResolver{})
	RegisterCredentialResolver(&awsRoleARNResolver{})
}

// awsStaticResolver resolves AWS credentials from static config fields.
type awsStaticResolver struct{}

func (r *awsStaticResolver) Provider() string       { return "aws" }
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

func (r *awsEnvResolver) Provider() string       { return "aws" }
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

// awsProfileResolver records a profile credential_source marker; SDK-bearing
// resolution happens in the aws plugin (decisions/0036 + 0038). Core no longer
// imports aws-sdk-go-v2/config — keeping the workflow binary SDK-free.
type awsProfileResolver struct{}

func (r *awsProfileResolver) Provider() string       { return "aws" }
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

	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["profile"] = profile

	m.creds.Extra["credential_source"] = "profile"
	logCredentialSourceMarker("aws", "profile")
	return nil
}

// awsRoleARNResolver records a role_arn credential_source marker; the actual
// sts:AssumeRole call is performed by the aws plugin (decisions/0036 + 0038).
type awsRoleARNResolver struct{}

func (r *awsRoleARNResolver) Provider() string       { return "aws" }
func (r *awsRoleARNResolver) CredentialType() string { return "role_arn" }

func (r *awsRoleARNResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}

	roleARN, _ := credsMap["roleArn"].(string)
	externalID, _ := credsMap["externalId"].(string)

	// Always record the role ARN so the plugin can use stscreds.AssumeRoleProvider.
	m.creds.RoleARN = roleARN
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["external_id"] = externalID

	if roleARN == "" {
		return fmt.Errorf("awsRoleARNResolver: roleArn is required")
	}

	m.creds.Extra["credential_source"] = "role_arn"
	logCredentialSourceMarker("aws", "role_arn")
	return nil
}

// logCredentialSourceMarker emits via the stdlib `log` package (not the app
// logger). The resolver path runs before module Init / app-logger plumbing,
// so it has no handle on `app.Logger()`. Future migration to the structured
// logger would require storing the logger on `CloudAccount` at construction
// time; that's out of scope for the credential_source marker rollout.
//
// The warning matters during the gap window where an old plugin version may
// see a marker it doesn't yet understand — the message tells operators where
// the resolution moved.
func logCredentialSourceMarker(provider, source string) {
	log.Printf("workflow: %s credential_source=%q recorded; resolution deferred to plugin (decisions/0036+0038)", provider, source)
}
