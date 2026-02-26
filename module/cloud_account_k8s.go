package module

import (
	"fmt"
	"os"
)

func init() {
	RegisterCredentialResolver(&k8sStaticResolver{})
	RegisterCredentialResolver(&k8sEnvResolver{})
	RegisterCredentialResolver(&k8sKubeconfigResolver{})
}

// k8sStaticResolver resolves Kubernetes credentials from static config fields.
type k8sStaticResolver struct{}

func (r *k8sStaticResolver) Provider() string      { return "kubernetes" }
func (r *k8sStaticResolver) CredentialType() string { return "static" }

func (r *k8sStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}
	if kc, ok := credsMap["kubeconfig"].(string); ok {
		m.creds.Kubeconfig = []byte(kc)
	}
	m.creds.Context, _ = credsMap["context"].(string)
	return nil
}

// k8sEnvResolver resolves Kubernetes credentials from the KUBECONFIG environment variable.
type k8sEnvResolver struct{}

func (r *k8sEnvResolver) Provider() string      { return "kubernetes" }
func (r *k8sEnvResolver) CredentialType() string { return "env" }

func (r *k8sEnvResolver) Resolve(m *CloudAccount) error {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home, _ := os.UserHomeDir()
		kubeconfigPath = home + "/.kube/config"
	}
	data, err := os.ReadFile(kubeconfigPath) //nolint:gosec // G304: path from trusted config data
	if err != nil {
		return fmt.Errorf("reading kubeconfig: %w", err)
	}
	m.creds.Kubeconfig = data
	return nil
}

// k8sKubeconfigResolver resolves Kubernetes credentials from a kubeconfig file or inline content.
type k8sKubeconfigResolver struct{}

func (r *k8sKubeconfigResolver) Provider() string      { return "kubernetes" }
func (r *k8sKubeconfigResolver) CredentialType() string { return "kubeconfig" }

func (r *k8sKubeconfigResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)

	path := ""
	if credsMap != nil {
		path, _ = credsMap["path"].(string)
	}
	if path == "" {
		path = os.Getenv("KUBECONFIG")
	}
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home + "/.kube/config"
	}

	if credsMap != nil {
		if inline, ok := credsMap["inline"].(string); ok && inline != "" {
			m.creds.Kubeconfig = []byte(inline)
			m.creds.Context, _ = credsMap["context"].(string)
			return nil
		}
	}

	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // G304: path from trusted config data
		if err != nil {
			return fmt.Errorf("reading kubeconfig at %q: %w", path, err)
		}
		m.creds.Kubeconfig = data
	}

	if credsMap != nil {
		m.creds.Context, _ = credsMap["context"].(string)
	}
	return nil
}
