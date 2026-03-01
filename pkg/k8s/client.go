package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientConfig holds options for creating a Kubernetes client.
type ClientConfig struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// Client wraps Kubernetes clientsets for typed and dynamic operations.
type Client struct {
	Typed     kubernetes.Interface
	Dynamic   dynamic.Interface
	Config    *rest.Config
	Namespace string
}

// NewClient creates a Kubernetes client using the following resolution order:
// 1. Explicit kubeconfig path
// 2. KUBECONFIG environment variable
// 3. ~/.kube/config
// 4. In-cluster config
func NewClient(cfg ClientConfig) (*Client, error) {
	restConfig, err := resolveConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig: %w", err)
	}

	typed, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}

	return &Client{
		Typed:     typed,
		Dynamic:   dyn,
		Config:    restConfig,
		Namespace: ns,
	}, nil
}

func resolveConfig(cfg ClientConfig) (*rest.Config, error) {
	if cfg.Kubeconfig != "" {
		return buildConfigFromPath(cfg.Kubeconfig, cfg.Context)
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return buildConfigFromPath(env, cfg.Context)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		defaultPath := filepath.Join(home, ".kube", "config")
		if _, statErr := os.Stat(defaultPath); statErr == nil {
			return buildConfigFromPath(defaultPath, cfg.Context)
		}
	}
	return rest.InClusterConfig()
}

func buildConfigFromPath(path, context string) (*rest.Config, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}
