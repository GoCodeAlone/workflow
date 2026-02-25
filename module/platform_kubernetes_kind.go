package module

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
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

// ─── EKS backend ─────────────────────────────────────────────────────────────

// eksBackend manages Amazon EKS clusters using aws-sdk-go-v2/service/eks.
// When no AWSConfigProvider is available (e.g., tests without a cloud account),
// plan() returns a synthetic create action and apply()/destroy() return errors.
type eksBackend struct{}

func (b *eksBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	awsProv, ok := awsProviderFrom(k.provider)
	if !ok {
		return &PlatformPlan{
			Provider: "eks",
			Resource: k.clusterName(),
			Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: fmt.Sprintf("create EKS cluster %q", k.clusterName())}},
		}, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("eks plan: AWS config: %w", err)
	}
	client := eks.NewFromConfig(cfg)

	out, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{
		Name: aws.String(k.clusterName()),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return &PlatformPlan{
				Provider: "eks",
				Resource: k.clusterName(),
				Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: fmt.Sprintf("create EKS cluster %q (version %s)", k.clusterName(), k.state.Version)}},
			}, nil
		}
		return nil, fmt.Errorf("eks plan: DescribeCluster: %w", err)
	}

	clusterStatus := "unknown"
	if out.Cluster != nil {
		clusterStatus = string(out.Cluster.Status)
	}
	return &PlatformPlan{
		Provider: "eks",
		Resource: k.clusterName(),
		Actions:  []PlatformAction{{Type: "noop", Resource: k.clusterName(), Detail: fmt.Sprintf("EKS cluster %q exists (status: %s)", k.clusterName(), clusterStatus)}},
	}, nil
}

func (b *eksBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	awsProv, ok := awsProviderFrom(k.provider)
	if !ok {
		return nil, fmt.Errorf("eks apply: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("eks apply: AWS config: %w", err)
	}

	roleARN, _ := k.config["role_arn"].(string)
	if roleARN == "" {
		return nil, fmt.Errorf("eks apply: 'role_arn' is required in module config")
	}

	subnetIDs := parseStringSlice(k.config["subnet_ids"])
	version := k.state.Version
	if version == "" {
		version = "1.29"
	}

	client := eks.NewFromConfig(cfg)
	createOut, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String(k.clusterName()),
		Version: aws.String(version),
		RoleArn: aws.String(roleARN),
		ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
			SubnetIds: subnetIDs,
		},
	})
	if err != nil {
		var alreadyExists *ekstypes.ResourceInUseException
		if errors.As(err, &alreadyExists) {
			return &PlatformResult{
				Success: true,
				Message: fmt.Sprintf("EKS cluster %q already exists", k.clusterName()),
				State:   k.state,
			}, nil
		}
		return nil, fmt.Errorf("eks apply: CreateCluster: %w", err)
	}

	k.state.Status = "creating"
	k.state.NodeGroups = k.nodeGroups()
	k.state.CreatedAt = time.Now()
	if createOut.Cluster != nil && createOut.Cluster.Endpoint != nil {
		k.state.Endpoint = *createOut.Cluster.Endpoint
	}

	// Create node groups
	for _, ng := range k.nodeGroups() {
		ngRoleARN, _ := k.config["node_role_arn"].(string)
		if ngRoleARN == "" {
			ngRoleARN = roleARN
		}
		_, ngErr := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
			ClusterName:   aws.String(k.clusterName()),
			NodegroupName: aws.String(ng.Name),
			NodeRole:      aws.String(ngRoleARN),
			InstanceTypes: []string{ng.InstanceType},
			Subnets:       subnetIDs,
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				MinSize:     aws.Int32(int32(ng.Min)),
				MaxSize:     aws.Int32(int32(ng.Max)),
				DesiredSize: aws.Int32(int32(ng.Current)),
			},
		})
		if ngErr != nil {
			return nil, fmt.Errorf("eks apply: CreateNodegroup %q: %w", ng.Name, ngErr)
		}
	}

	k.state.Status = "creating" // EKS cluster takes time to become ACTIVE
	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("EKS cluster %q creation initiated", k.clusterName()),
		State:   k.state,
	}, nil
}

func (b *eksBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	awsProv, ok := awsProviderFrom(k.provider)
	if !ok {
		return k.state, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return k.state, fmt.Errorf("eks status: AWS config: %w", err)
	}
	client := eks.NewFromConfig(cfg)

	out, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{
		Name: aws.String(k.clusterName()),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			k.state.Status = "deleted"
			return k.state, nil
		}
		return k.state, fmt.Errorf("eks status: DescribeCluster: %w", err)
	}

	if out.Cluster != nil {
		k.state.Status = strings.ToLower(string(out.Cluster.Status))
		if out.Cluster.Endpoint != nil {
			k.state.Endpoint = *out.Cluster.Endpoint
		}
		if out.Cluster.Version != nil {
			k.state.Version = *out.Cluster.Version
		}
	}

	// Fetch node groups
	ngOut, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{
		ClusterName: aws.String(k.clusterName()),
	})
	if err == nil && ngOut != nil {
		var groups []NodeGroupState
		for _, ngName := range ngOut.Nodegroups {
			groups = append(groups, NodeGroupState{Name: ngName})
		}
		k.state.NodeGroups = groups
	}

	return k.state, nil
}

func (b *eksBackend) destroy(k *PlatformKubernetes) error {
	awsProv, ok := awsProviderFrom(k.provider)
	if !ok {
		return fmt.Errorf("eks destroy: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return fmt.Errorf("eks destroy: AWS config: %w", err)
	}
	client := eks.NewFromConfig(cfg)

	// Delete node groups first
	ngOut, _ := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{
		ClusterName: aws.String(k.clusterName()),
	})
	if ngOut != nil {
		for _, ngName := range ngOut.Nodegroups {
			if _, err := client.DeleteNodegroup(context.Background(), &eks.DeleteNodegroupInput{
				ClusterName:   aws.String(k.clusterName()),
				NodegroupName: aws.String(ngName),
			}); err != nil {
				return fmt.Errorf("eks destroy: DeleteNodegroup %q: %w", ngName, err)
			}
		}
	}

	_, err = client.DeleteCluster(context.Background(), &eks.DeleteClusterInput{
		Name: aws.String(k.clusterName()),
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			k.state.Status = "deleted"
			return nil
		}
		return fmt.Errorf("eks destroy: DeleteCluster: %w", err)
	}

	k.state.Status = "deleting"
	return nil
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
