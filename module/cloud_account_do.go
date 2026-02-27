package module

import (
	"context"
	"fmt"
	"os"

	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

func init() {
	RegisterCredentialResolver(&doStaticResolver{})
	RegisterCredentialResolver(&doEnvResolver{})
	RegisterCredentialResolver(&doAPITokenResolver{})
}

// doStaticResolver resolves DigitalOcean credentials from static config fields.
type doStaticResolver struct{}

func (r *doStaticResolver) Provider() string       { return "digitalocean" }
func (r *doStaticResolver) CredentialType() string { return "static" }

func (r *doStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		m.creds.Token, _ = credsMap["token"].(string)
	}
	return nil
}

// doEnvResolver resolves DigitalOcean credentials from environment variables.
type doEnvResolver struct{}

func (r *doEnvResolver) Provider() string       { return "digitalocean" }
func (r *doEnvResolver) CredentialType() string { return "env" }

func (r *doEnvResolver) Resolve(m *CloudAccount) error {
	m.creds.Token = os.Getenv("DIGITALOCEAN_TOKEN")
	if m.creds.Token == "" {
		m.creds.Token = os.Getenv("DO_TOKEN")
	}
	return nil
}

// doAPITokenResolver resolves a DigitalOcean API token from explicit config.
type doAPITokenResolver struct{}

func (r *doAPITokenResolver) Provider() string       { return "digitalocean" }
func (r *doAPITokenResolver) CredentialType() string { return "api_token" }

func (r *doAPITokenResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return fmt.Errorf("api_token credential requires 'token'")
	}
	token, _ := credsMap["token"].(string)
	if token == "" {
		return fmt.Errorf("api_token credential requires 'token'")
	}
	m.creds.Token = token
	return nil
}

// doClient returns a configured *godo.Client using the Token credential.
// The caller must have resolved credentials with provider=digitalocean before calling this.
func (m *CloudAccount) doClient() (*godo.Client, error) {
	if m.creds == nil || m.creds.Token == "" {
		return nil, fmt.Errorf("cloud.account %q: DigitalOcean token not set", m.name)
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: m.creds.Token})
	httpClient := oauth2.NewClient(context.Background(), ts)
	return godo.NewClient(httpClient), nil
}
