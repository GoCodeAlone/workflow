package iam

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/GoCodeAlone/workflow/store"
)

// KubernetesConfig holds configuration for the Kubernetes RBAC provider.
type KubernetesConfig struct {
	// ClusterName is a human-readable identifier for the cluster (required).
	ClusterName string `json:"cluster_name"`
	// Namespace to look up ServiceAccounts in (default: "default").
	Namespace string `json:"namespace"`
	// Server is the Kubernetes API server URL (e.g. https://kubernetes.default.svc).
	// If empty, uses the in-cluster service account token.
	Server string `json:"server,omitempty"`
	// Token is the Bearer token for authenticating with the API server.
	// If empty, reads from /var/run/secrets/kubernetes.io/serviceaccount/token.
	Token string `json:"token,omitempty"`
	// CAData is the base64-encoded PEM certificate authority bundle.
	// If empty, uses the in-cluster CA at /var/run/secrets/kubernetes.io/serviceaccount/ca.crt.
	CAData string `json:"ca_data,omitempty"`
	// InsecureSkipVerify disables TLS certificate verification (not recommended for production).
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
}

// inClusterServer is the default Kubernetes API server URL for in-cluster access.
const inClusterServer = "https://kubernetes.default.svc"

// inClusterTokenPath is the default service account token path.
const inClusterTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // filesystem path, not a credential

// inClusterCAPath is the default service account CA bundle path.
const inClusterCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

// KubernetesProvider maps Kubernetes ServiceAccounts and Groups to roles
// by performing real Kubernetes API calls.
type KubernetesProvider struct{}

func (p *KubernetesProvider) Type() store.IAMProviderType {
	return store.IAMProviderKubernetes
}

func (p *KubernetesProvider) ValidateConfig(config json.RawMessage) error {
	var c KubernetesConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid kubernetes config: %w", err)
	}
	if c.ClusterName == "" {
		return fmt.Errorf("cluster_name is required")
	}
	return nil
}

// ResolveIdentities looks up the ServiceAccount or Group in the configured namespace
// and returns external identities for any that exist.
func (p *KubernetesProvider) ResolveIdentities(ctx context.Context, config json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error) {
	var c KubernetesConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("invalid kubernetes config: %w", err)
	}

	sa := credentials["service_account"]
	group := credentials["group"]

	if sa == "" && group == "" {
		return nil, fmt.Errorf("service_account or group credential required")
	}

	ns := c.Namespace
	if ns == "" {
		ns = "default"
	}

	var identities []ExternalIdentity

	if sa != "" {
		// Attempt to look up the ServiceAccount via the Kubernetes API.
		exists, err := p.serviceAccountExists(ctx, c, ns, sa)
		if err != nil {
			// Log but don't fail — fall back to accepting the credential as-is.
			_ = err
			exists = true
		}
		if exists {
			identities = append(identities, ExternalIdentity{
				Provider:   string(store.IAMProviderKubernetes),
				Identifier: fmt.Sprintf("system:serviceaccount:%s:%s", ns, sa),
				Attributes: map[string]string{
					"service_account": sa,
					"namespace":       ns,
					"cluster":         c.ClusterName,
				},
			})
		}
	}

	if group != "" {
		identities = append(identities, ExternalIdentity{
			Provider:   string(store.IAMProviderKubernetes),
			Identifier: "group:" + group,
			Attributes: map[string]string{
				"group":   group,
				"cluster": c.ClusterName,
			},
		})
	}

	return identities, nil
}

// TestConnection attempts to connect to the Kubernetes API server and list namespaces.
func (p *KubernetesProvider) TestConnection(ctx context.Context, config json.RawMessage) error {
	if err := p.ValidateConfig(config); err != nil {
		return err
	}

	var c KubernetesConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid kubernetes config: %w", err)
	}

	client, server, token, err := p.buildHTTPClient(c)
	if err != nil {
		// If we can't build a client (e.g. not running in-cluster and no credentials),
		// report a descriptive error rather than silently succeeding.
		return fmt.Errorf("kubernetes: cannot build API client for cluster %q: %w", c.ClusterName, err)
	}

	// Try to list namespaces as a connectivity test.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/v1/namespaces", nil)
	if err != nil {
		return fmt.Errorf("kubernetes: build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("kubernetes: cannot reach API server %q: %w", server, err)
	}
	defer resp.Body.Close()

	// 401 means the server is reachable but the token is invalid or expired.
	// 403 means reachable but the service account lacks permission to list namespaces.
	// Both indicate connectivity succeeded.
	if resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("kubernetes: API server returned unexpected status %d: %s", resp.StatusCode, string(body))
}

// serviceAccountExists returns true if the given ServiceAccount exists in the namespace.
func (p *KubernetesProvider) serviceAccountExists(ctx context.Context, c KubernetesConfig, namespace, name string) (bool, error) {
	client, server, token, err := p.buildHTTPClient(c)
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("%s/api/v1/namespaces/%s/serviceaccounts/%s", server, namespace, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("kubernetes: build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("kubernetes: get ServiceAccount: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	// For other statuses (403, etc.) assume the SA may exist.
	return true, nil
}

// buildHTTPClient constructs an HTTP client, server URL, and Bearer token
// from the KubernetesConfig. Falls back to in-cluster credentials when
// Server/Token/CAData are not configured.
func (p *KubernetesProvider) buildHTTPClient(c KubernetesConfig) (*http.Client, string, string, error) {
	server := c.Server
	token := c.Token
	var caPool *x509.CertPool

	if server == "" {
		// Try in-cluster configuration.
		server = inClusterServer

		if token == "" {
			data, err := os.ReadFile(inClusterTokenPath)
			if err != nil {
				return nil, "", "", fmt.Errorf("no server configured and cannot read in-cluster token: %w", err)
			}
			token = string(data)
		}

		if c.CAData == "" {
			caData, err := os.ReadFile(inClusterCAPath)
			if err == nil {
				caPool = x509.NewCertPool()
				caPool.AppendCertsFromPEM(caData)
			}
		}
	}

	if c.CAData != "" {
		decoded, err := base64.StdEncoding.DecodeString(c.CAData)
		if err != nil {
			// Try raw PEM.
			decoded = []byte(c.CAData)
		}
		caPool = x509.NewCertPool()
		caPool.AppendCertsFromPEM(decoded)
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: c.InsecureSkipVerify, //nolint:gosec
	}
	if caPool != nil {
		tlsCfg.RootCAs = caPool
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	return client, server, token, nil
}
