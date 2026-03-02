package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/GoCodeAlone/workflow/pkg/k8s/imageloader"
)

// RuntimeInfo holds detected cluster runtime information.
type RuntimeInfo struct {
	Runtime     imageloader.Runtime
	ContextName string
	ClusterName string // extracted from context (e.g. "mycluster" from "kind-mycluster")
}

// DetectRuntime determines the cluster runtime from the kubeconfig context name.
func DetectRuntime(kubeconfigPath, contextOverride string) (*RuntimeInfo, error) {
	path := kubeconfigPath
	if path == "" {
		path = os.Getenv("KUBECONFIG")
	}
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".kube", "config")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
			}
		}
	}
	if path == "" {
		return &RuntimeInfo{Runtime: imageloader.RuntimeRemote, ContextName: "unknown"}, nil
	}

	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	overrides := &clientcmd.ConfigOverrides{}
	if contextOverride != "" {
		overrides.CurrentContext = contextOverride
	}

	rawConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	ctx := rawConfig.CurrentContext
	if contextOverride != "" {
		ctx = contextOverride
	}

	return classifyContext(ctx), nil
}

// classifyContext maps a kubeconfig context name to a RuntimeInfo.
func classifyContext(ctx string) *RuntimeInfo {
	info := &RuntimeInfo{ContextName: ctx}

	switch {
	case ctx == "minikube":
		info.Runtime = imageloader.RuntimeMinikube
		info.ClusterName = "minikube"
	case strings.HasPrefix(ctx, "minikube-"):
		info.Runtime = imageloader.RuntimeMinikube
		info.ClusterName = strings.TrimPrefix(ctx, "minikube-")
	case strings.HasPrefix(ctx, "kind-"):
		info.Runtime = imageloader.RuntimeKind
		info.ClusterName = strings.TrimPrefix(ctx, "kind-")
	case ctx == "docker-desktop":
		info.Runtime = imageloader.RuntimeDockerDesktop
		info.ClusterName = "docker-desktop"
	case strings.HasPrefix(ctx, "k3d-"):
		info.Runtime = imageloader.RuntimeK3d
		info.ClusterName = strings.TrimPrefix(ctx, "k3d-")
	default:
		info.Runtime = imageloader.RuntimeRemote
		info.ClusterName = ctx
	}

	return info
}
