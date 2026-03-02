package k8s

import (
	"testing"

	"github.com/GoCodeAlone/workflow/pkg/k8s/imageloader"
)

func TestClassifyContext(t *testing.T) {
	tests := []struct {
		ctx         string
		wantRuntime imageloader.Runtime
		wantCluster string
	}{
		{"minikube", imageloader.RuntimeMinikube, "minikube"},
		{"minikube-dev", imageloader.RuntimeMinikube, "dev"},
		{"minikube-staging", imageloader.RuntimeMinikube, "staging"},
		{"kind-kind", imageloader.RuntimeKind, "kind"},
		{"kind-mycluster", imageloader.RuntimeKind, "mycluster"},
		{"kind-test-cluster", imageloader.RuntimeKind, "test-cluster"},
		{"docker-desktop", imageloader.RuntimeDockerDesktop, "docker-desktop"},
		{"k3d-mycluster", imageloader.RuntimeK3d, "mycluster"},
		{"k3d-dev", imageloader.RuntimeK3d, "dev"},
		// Remote clusters
		{"arn:aws:eks:us-east-1:123456:cluster/prod", imageloader.RuntimeRemote, "arn:aws:eks:us-east-1:123456:cluster/prod"},
		{"gke_my-project_us-central1-a_my-cluster", imageloader.RuntimeRemote, "gke_my-project_us-central1-a_my-cluster"},
		{"my-aks-cluster", imageloader.RuntimeRemote, "my-aks-cluster"},
		{"do-nyc1-k8s-cluster", imageloader.RuntimeRemote, "do-nyc1-k8s-cluster"},
		{"some-random-context", imageloader.RuntimeRemote, "some-random-context"},
	}

	for _, tt := range tests {
		t.Run(tt.ctx, func(t *testing.T) {
			info := classifyContext(tt.ctx)
			if info.Runtime != tt.wantRuntime {
				t.Errorf("runtime: got %q, want %q", info.Runtime, tt.wantRuntime)
			}
			if info.ClusterName != tt.wantCluster {
				t.Errorf("cluster: got %q, want %q", info.ClusterName, tt.wantCluster)
			}
			if info.ContextName != tt.ctx {
				t.Errorf("context: got %q, want %q", info.ContextName, tt.ctx)
			}
		})
	}
}
