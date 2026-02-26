package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
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
	k.state.Endpoint = "https://127.0.0.1:6443"

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
		ngMin := safeIntToInt32(ng.Min)
		ngMax := safeIntToInt32(ng.Max)
		ngCurrent := safeIntToInt32(ng.Current)
		_, ngErr := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
			ClusterName:   aws.String(k.clusterName()),
			NodegroupName: aws.String(ng.Name),
			NodeRole:      aws.String(ngRoleARN),
			InstanceTypes: []string{ng.InstanceType},
			Subnets:       subnetIDs,
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				MinSize:     aws.Int32(ngMin),
				MaxSize:     aws.Int32(ngMax),
				DesiredSize: aws.Int32(ngCurrent),
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

// ─── GKE backend ──────────────────────────────────────────────────────────────

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
			Name:             k.clusterName(),
			InitialClusterVersion: version,
			NodePools:        nodePools,
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

// ─── AKS backend ──────────────────────────────────────────────────────────────

// aksBackend manages Azure Kubernetes Service clusters.
// Requires the Azure SDK (github.com/Azure/azure-sdk-for-go) to be available.
// When Azure credentials are not configured, returns clear errors.
type aksBackend struct{}

func (b *aksBackend) aksResourceGroup(k *PlatformKubernetes) string {
	if rg, ok := k.config["resource_group"].(string); ok && rg != "" {
		return rg
	}
	return ""
}

func (b *aksBackend) aksLocation(k *PlatformKubernetes) string {
	if l, ok := k.config["location"].(string); ok && l != "" {
		return l
	}
	if k.provider != nil {
		return k.provider.Region()
	}
	return "eastus"
}

func (b *aksBackend) aksSubscriptionID(k *PlatformKubernetes) string {
	if s, ok := k.config["subscription_id"].(string); ok && s != "" {
		return s
	}
	if k.provider != nil {
		if creds, err := k.provider.GetCredentials(context.Background()); err == nil && creds.SubscriptionID != "" {
			return creds.SubscriptionID
		}
	}
	return ""
}

func (b *aksBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	rg := b.aksResourceGroup(k)
	if rg == "" {
		return nil, fmt.Errorf("aks plan: 'resource_group' is required in module config")
	}
	subID := b.aksSubscriptionID(k)
	if subID == "" {
		return nil, fmt.Errorf("aks plan: 'subscription_id' is required in module config or cloud account")
	}

	return &PlatformPlan{
		Provider: "aks",
		Resource: k.clusterName(),
		Actions:  []PlatformAction{{Type: "create", Resource: k.clusterName(), Detail: fmt.Sprintf("create AKS cluster %q in %s/%s", k.clusterName(), rg, b.aksLocation(k))}},
	}, nil
}

func (b *aksBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	rg := b.aksResourceGroup(k)
	if rg == "" {
		return nil, fmt.Errorf("aks apply: 'resource_group' is required in module config")
	}
	subID := b.aksSubscriptionID(k)
	if subID == "" {
		return nil, fmt.Errorf("aks apply: 'subscription_id' is required in module config or cloud account")
	}

	if k.provider == nil {
		return nil, fmt.Errorf("aks apply: no Azure cloud account configured")
	}

	creds, err := k.provider.GetCredentials(context.Background())
	if err != nil {
		return nil, fmt.Errorf("aks apply: Azure credentials: %w", err)
	}

	// AKS cluster creation via Azure REST API
	version := k.state.Version
	if version == "" {
		version = "1.29"
	}

	location := b.aksLocation(k)
	clusterBody := map[string]any{
		"location": location,
		"properties": map[string]any{
			"kubernetesVersion": version,
			"dnsPrefix":         k.clusterName(),
			"agentPoolProfiles": b.buildAgentPools(k),
		},
	}
	if creds.ClientID != "" {
		clusterBody["properties"].(map[string]any)["servicePrincipalProfile"] = map[string]any{
			"clientId":     creds.ClientID,
			"secret":       creds.ClientSecret,
		}
	}

	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s?api-version=2024-01-01",
		subID, rg, k.clusterName())

	body, marshalErr := json.Marshal(clusterBody)
	if marshalErr != nil {
		return nil, fmt.Errorf("aks apply: marshal request: %w", marshalErr)
	}

	token, tokenErr := b.azureToken(creds)
	if tokenErr != nil {
		return nil, fmt.Errorf("aks apply: get token: %w", tokenErr)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, httpErr := http.DefaultClient.Do(req)
	if httpErr != nil {
		return nil, fmt.Errorf("aks apply: HTTP request: %w", httpErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aks apply: Azure API returned %d: %s", resp.StatusCode, string(respBody))
	}

	k.state.Status = "creating"
	k.state.NodeGroups = k.nodeGroups()
	k.state.CreatedAt = time.Now()

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("AKS cluster %q creation initiated in %s/%s", k.clusterName(), rg, location),
		State:   k.state,
	}, nil
}

func (b *aksBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	rg := b.aksResourceGroup(k)
	subID := b.aksSubscriptionID(k)
	if rg == "" || subID == "" {
		k.state.Status = "unknown"
		return k.state, nil
	}

	if k.provider == nil {
		return k.state, nil
	}

	creds, err := k.provider.GetCredentials(context.Background())
	if err != nil {
		return k.state, nil
	}

	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s?api-version=2024-01-01",
		subID, rg, k.clusterName())

	token, tokenErr := b.azureToken(creds)
	if tokenErr != nil {
		return k.state, nil
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, httpErr := http.DefaultClient.Do(req)
	if httpErr != nil {
		return k.state, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		k.state.Status = "not-found"
		return k.state, nil
	}

	var result map[string]any
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr == nil {
		if props, ok := result["properties"].(map[string]any); ok {
			if state, ok := props["provisioningState"].(string); ok {
				k.state.Status = strings.ToLower(state)
			}
			if fqdn, ok := props["fqdn"].(string); ok {
				k.state.Endpoint = fqdn
			}
		}
	}

	return k.state, nil
}

func (b *aksBackend) destroy(k *PlatformKubernetes) error {
	rg := b.aksResourceGroup(k)
	if rg == "" {
		return fmt.Errorf("aks destroy: 'resource_group' is required")
	}
	subID := b.aksSubscriptionID(k)
	if subID == "" {
		return fmt.Errorf("aks destroy: 'subscription_id' is required")
	}

	if k.provider == nil {
		return fmt.Errorf("aks destroy: no Azure cloud account configured")
	}

	creds, err := k.provider.GetCredentials(context.Background())
	if err != nil {
		return fmt.Errorf("aks destroy: Azure credentials: %w", err)
	}

	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s?api-version=2024-01-01",
		subID, rg, k.clusterName())

	token, tokenErr := b.azureToken(creds)
	if tokenErr != nil {
		return fmt.Errorf("aks destroy: get token: %w", tokenErr)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, httpErr := http.DefaultClient.Do(req)
	if httpErr != nil {
		return fmt.Errorf("aks destroy: HTTP request: %w", httpErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		k.state.Status = "deleted"
		return nil
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aks destroy: Azure API returned %d: %s", resp.StatusCode, string(respBody))
	}

	k.state.Status = "deleting"
	return nil
}

func (b *aksBackend) buildAgentPools(k *PlatformKubernetes) []map[string]any {
	groups := k.nodeGroups()
	if len(groups) == 0 {
		return []map[string]any{{
			"name":         "default",
			"count":        1,
			"vmSize":       "Standard_DS2_v2",
			"mode":         "System",
			"osType":       "Linux",
		}}
	}
	var pools []map[string]any
	for i, ng := range groups {
		vmSize := ng.InstanceType
		if vmSize == "" {
			vmSize = "Standard_DS2_v2"
		}
		mode := "User"
		if i == 0 {
			mode = "System"
		}
		pools = append(pools, map[string]any{
			"name":                ng.Name,
			"count":               ng.Min,
			"minCount":            ng.Min,
			"maxCount":            ng.Max,
			"vmSize":              vmSize,
			"mode":                mode,
			"osType":              "Linux",
			"enableAutoScaling":   true,
		})
	}
	return pools
}

func (b *aksBackend) azureToken(creds *CloudCredentials) (string, error) {
	if creds.Token != "" {
		return creds.Token, nil
	}
	if creds.TenantID == "" || creds.ClientID == "" || creds.ClientSecret == "" {
		return "", fmt.Errorf("azure client_credentials require tenant_id, client_id, and client_secret")
	}

	// OAuth2 client_credentials flow for Azure management API
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", creds.TenantID)
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
		"scope":         {"https://management.azure.com/.default"},
	}
	resp, err := http.PostForm(tokenURL, form) //nolint:gosec // G107: Azure OAuth2 token endpoint requires dynamic tenant URL
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp map[string]any
	if decErr := json.NewDecoder(resp.Body).Decode(&tokenResp); decErr != nil {
		return "", fmt.Errorf("parse token response: %w", decErr)
	}
	token, ok := tokenResp["access_token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return token, nil
}

func init() {
	RegisterKubernetesBackend("kind", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("k3s", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
		return &eksBackend{}, nil
	})
	RegisterKubernetesBackend("gke", func(_ map[string]any) (kubernetesBackend, error) {
		return &gkeBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
