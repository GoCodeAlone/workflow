package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
)

// gkeBackend manages Google Kubernetes Engine clusters via the GCP Container API.
type gkeBackend struct{}

func (b *gkeBackend) gkeLocation(k *PlatformKubernetes) string {
	if z, ok := k.config["zone"].(string); ok && z != "" {
		return z
	}
	if r, ok := k.config["location"].(string); ok && r != "" {
		return r
	}
	if k.provider != nil {
		return k.provider.Region()
	}
	return "us-central1"
}

func (b *gkeBackend) gkeProject(k *PlatformKubernetes) string {
	if p, ok := k.config["project_id"].(string); ok && p != "" {
		return p
	}
	if k.provider != nil {
		if creds, err := k.provider.GetCredentials(context.Background()); err == nil && creds.ProjectID != "" {
			return creds.ProjectID
		}
	}
	return ""
}

func (b *gkeBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	project := b.gkeProject(k)
	if project == "" {
		return nil, fmt.Errorf("gke plan: 'project_id' is required in module config or cloud account")
	}
	location := b.gkeLocation(k)

	plan := &PlatformPlan{
		Provider: "gke",
		Resource: k.clusterName(),
	}

	action := PlatformAction{Type: "create", Resource: k.clusterName(), Detail: fmt.Sprintf("create GKE cluster %q in %s", k.clusterName(), location)}

	if svc, svcErr := b.containerService(k); svcErr == nil {
		name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, k.clusterName())
		if cluster, getErr := svc.Projects.Locations.Clusters.Get(name).Context(context.Background()).Do(); getErr == nil {
			action = PlatformAction{Type: "noop", Resource: k.clusterName(), Detail: fmt.Sprintf("GKE cluster %q exists (status: %s)", k.clusterName(), cluster.Status)}
		}
	}

	plan.Actions = []PlatformAction{action}
	return plan, nil
}

func (b *gkeBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	project := b.gkeProject(k)
	if project == "" {
		return nil, fmt.Errorf("gke apply: 'project_id' is required in module config or cloud account")
	}
	location := b.gkeLocation(k)

	svc, err := b.containerService(k)
	if err != nil {
		return nil, fmt.Errorf("gke apply: GCP credentials: %w", err)
	}

	version := k.state.Version
	if version == "" {
		version = "1.29"
	}

	// Build node pools from nodeGroups config
	var nodePools []*container.NodePool
	for _, ng := range k.nodeGroups() {
		machineType := ng.InstanceType
		if machineType == "" {
			machineType = "e2-medium"
		}
		nodePools = append(nodePools, &container.NodePool{
			Name:             ng.Name,
			InitialNodeCount: int64(ng.Min),
			Config: &container.NodeConfig{
				MachineType: machineType,
			},
			Autoscaling: &container.NodePoolAutoscaling{
				Enabled:      true,
				MinNodeCount: int64(ng.Min),
				MaxNodeCount: int64(ng.Max),
			},
		})
	}
	if len(nodePools) == 0 {
		nodePools = []*container.NodePool{{
			Name:             "default-pool",
			InitialNodeCount: 1,
			Config:           &container.NodeConfig{MachineType: "e2-medium"},
		}}
	}

	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	req := &container.CreateClusterRequest{
		Cluster: &container.Cluster{
			Name:                  k.clusterName(),
			InitialClusterVersion: version,
			NodePools:             nodePools,
		},
	}

	_, err = svc.Projects.Locations.Clusters.Create(parent, req).Context(context.Background()).Do()
	if err != nil {
		if strings.Contains(err.Error(), "Already Exists") || strings.Contains(err.Error(), "ALREADY_EXISTS") {
			return &PlatformResult{
				Success: true,
				Message: fmt.Sprintf("GKE cluster %q already exists", k.clusterName()),
				State:   k.state,
			}, nil
		}
		return nil, fmt.Errorf("gke apply: CreateCluster: %w", err)
	}

	k.state.Status = "creating"
	k.state.NodeGroups = k.nodeGroups()
	k.state.CreatedAt = time.Now()

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("GKE cluster %q creation initiated in %s", k.clusterName(), location),
		State:   k.state,
	}, nil
}

func (b *gkeBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	project := b.gkeProject(k)
	if project == "" {
		k.state.Status = "unknown"
		return k.state, nil
	}
	location := b.gkeLocation(k)

	if svc, svcErr := b.containerService(k); svcErr == nil {
		name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, k.clusterName())
		if cluster, getErr := svc.Projects.Locations.Clusters.Get(name).Context(context.Background()).Do(); getErr == nil {
			k.state.Status = strings.ToLower(cluster.Status)
			k.state.Endpoint = cluster.Endpoint
			if cluster.CurrentMasterVersion != "" {
				k.state.Version = cluster.CurrentMasterVersion
			}
			var groups []NodeGroupState
			for _, np := range cluster.NodePools {
				groups = append(groups, NodeGroupState{
					Name:    np.Name,
					Current: int(np.InitialNodeCount),
				})
			}
			k.state.NodeGroups = groups
		} else {
			k.state.Status = "not-found"
		}
	}

	return k.state, nil
}

func (b *gkeBackend) destroy(k *PlatformKubernetes) error {
	project := b.gkeProject(k)
	if project == "" {
		return fmt.Errorf("gke destroy: 'project_id' is required")
	}
	location := b.gkeLocation(k)

	svc, err := b.containerService(k)
	if err != nil {
		return fmt.Errorf("gke destroy: GCP credentials: %w", err)
	}

	name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, k.clusterName())
	_, err = svc.Projects.Locations.Clusters.Delete(name).Context(context.Background()).Do()
	if err != nil {
		if strings.Contains(err.Error(), "NOT_FOUND") || strings.Contains(err.Error(), "notFound") {
			k.state.Status = "deleted"
			return nil
		}
		return fmt.Errorf("gke destroy: DeleteCluster: %w", err)
	}

	k.state.Status = "deleting"
	return nil
}

func (b *gkeBackend) containerService(k *PlatformKubernetes) (*container.Service, error) {
	if k.provider == nil {
		return nil, fmt.Errorf("no GCP cloud account configured")
	}

	creds, err := k.provider.GetCredentials(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get GCP credentials: %w", err)
	}

	var opts []option.ClientOption
	if len(creds.ServiceAccountJSON) > 0 {
		opts = append(opts, option.WithCredentialsJSON(creds.ServiceAccountJSON)) //nolint:staticcheck // SA1019: no alternative available without security advisory scope
	}

	svc, err := container.NewService(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("create container service: %w", err)
	}
	return svc, nil
}

func init() {
	RegisterKubernetesBackend("gke", func(_ map[string]any) (kubernetesBackend, error) {
		return &gkeBackend{}, nil
	})
}
