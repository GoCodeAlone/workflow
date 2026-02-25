package module

import (
	"fmt"
	"time"
)

// kindBackend implements kubernetesBackend using in-memory state.
// In a production deployment this would shell out to `kind create/delete cluster`,
// but for testing we track state in memory so no tooling is required.
type kindBackend struct{}

func (b *kindBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	plan := &PlatformPlan{
		Provider: k.state.Provider,
		Resource: k.clusterName(),
	}

	switch k.state.Status {
	case "pending", "deleted":
		plan.Actions = []PlatformAction{
			{Type: "create", Resource: k.clusterName(), Detail: fmt.Sprintf("create kind cluster %q (version %s)", k.clusterName(), k.state.Version)},
		}
	case "running":
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: k.clusterName(), Detail: "cluster already running"},
		}
	default:
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: k.clusterName(), Detail: fmt.Sprintf("cluster status=%s, no action", k.state.Status)},
		}
	}

	return plan, nil
}

func (b *kindBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	if k.state.Status == "running" {
		return &PlatformResult{Success: true, Message: "cluster already running", State: k.state}, nil
	}

	k.state.Status = "creating"
	k.state.NodeGroups = k.nodeGroups()
	k.state.CreatedAt = time.Now()

	// In-memory: immediately transition to running.
	// Real implementation: exec `kind create cluster --name <name> --image kindest/node:v<version>`
	k.state.Status = "running"
	k.state.Endpoint = fmt.Sprintf("https://127.0.0.1:6443")

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("kind cluster %q created (in-memory)", k.clusterName()),
		State:   k.state,
	}, nil
}

func (b *kindBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	return k.state, nil
}

func (b *kindBackend) destroy(k *PlatformKubernetes) error {
	if k.state.Status == "deleted" {
		return nil
	}
	k.state.Status = "deleting"
	// In-memory: immediately mark deleted.
	// Real implementation: exec `kind delete cluster --name <name>`
	k.state.Status = "deleted"
	k.state.Endpoint = ""
	return nil
}

// ─── EKS stub ────────────────────────────────────────────────────────────────

// eksBackend is a stub for Amazon EKS.
// Real implementation would use aws-sdk-go-v2/service/eks to:
//   - CreateCluster / DescribeCluster / DeleteCluster
//   - CreateNodegroup / DescribeNodegroup / DeleteNodegroup
type eksBackend struct{}

func (b *eksBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	return &PlatformPlan{
		Provider: "eks",
		Resource: k.clusterName(),
		Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: "EKS cluster (stub — use Terraform or AWS CLI)"}},
	}, nil
}

func (b *eksBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	return nil, fmt.Errorf("eks backend: not implemented — use Terraform or aws-sdk-go-v2/service/eks")
}

func (b *eksBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	k.state.Status = "unknown"
	return k.state, nil
}

func (b *eksBackend) destroy(k *PlatformKubernetes) error {
	return fmt.Errorf("eks backend: not implemented — use Terraform or aws-sdk-go-v2/service/eks")
}

// ─── GKE stub ────────────────────────────────────────────────────────────────

// gkeBackend is a stub for Google Kubernetes Engine.
// Real implementation would use google.golang.org/api/container/v1 to:
//   - Projects.Zones.Clusters.Create / Get / Delete
type gkeBackend struct{}

func (b *gkeBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	return &PlatformPlan{
		Provider: "gke",
		Resource: k.clusterName(),
		Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: "GKE cluster (stub — use Terraform or gcloud CLI)"}},
	}, nil
}

func (b *gkeBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	return nil, fmt.Errorf("gke backend: not implemented — use Terraform or google.golang.org/api/container/v1")
}

func (b *gkeBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	k.state.Status = "unknown"
	return k.state, nil
}

func (b *gkeBackend) destroy(k *PlatformKubernetes) error {
	return fmt.Errorf("gke backend: not implemented — use Terraform or google.golang.org/api/container/v1")
}

// ─── AKS stub ────────────────────────────────────────────────────────────────

// aksBackend is a stub for Azure Kubernetes Service.
// Real implementation would use github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice
// to create/update/delete ManagedCluster resources.
type aksBackend struct{}

func (b *aksBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	return &PlatformPlan{
		Provider: "aks",
		Resource: k.clusterName(),
		Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: "AKS cluster (stub — use Terraform or az CLI)"}},
	}, nil
}

func (b *aksBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	return nil, fmt.Errorf("aks backend: not implemented — use Terraform or Azure SDK armcontainerservice")
}

func (b *aksBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	k.state.Status = "unknown"
	return k.state, nil
}

func (b *aksBackend) destroy(k *PlatformKubernetes) error {
	return fmt.Errorf("aks backend: not implemented — use Terraform or Azure SDK armcontainerservice")
}
