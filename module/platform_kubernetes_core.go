// platform_kubernetes_core.go holds the SDK-free Kubernetes backends
// (kind/k3s/eks/aks) and registers them. The lone SDK-bearing backend, gke,
// lives in platform_kubernetes_gke.go — see the cloud-SDK-extraction design.
package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/internal/legacyaws"
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

// eksErrorBackend replaces the former eksBackend. The real EKS backend has been
// removed from workflow core in issue #653; install workflow-plugin-aws for EKS
// cluster management.
type eksErrorBackend struct{}

func (b *eksErrorBackend) err(k *PlatformKubernetes) error {
	return fmt.Errorf(
		"platform.kubernetes %q: EKS cluster backend removed from workflow core in %s (issue #653).\n"+
			"Use cluster_type: kind for local development.\n"+
			"Install workflow-plugin-aws to manage EKS clusters: https://github.com/GoCodeAlone/workflow-plugin-aws",
		k.name, legacyaws.RemovedInVersion,
	)
}

func (b *eksErrorBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	return nil, b.err(k)
}

func (b *eksErrorBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	return nil, b.err(k)
}

func (b *eksErrorBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	return nil, b.err(k)
}

func (b *eksErrorBackend) destroy(k *PlatformKubernetes) error {
	return b.err(k)
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
			"clientId": creds.ClientID,
			"secret":   creds.ClientSecret,
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
			"name":   "default",
			"count":  1,
			"vmSize": "Standard_DS2_v2",
			"mode":   "System",
			"osType": "Linux",
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
			"name":              ng.Name,
			"count":             ng.Min,
			"minCount":          ng.Min,
			"maxCount":          ng.Max,
			"vmSize":            vmSize,
			"mode":              mode,
			"osType":            "Linux",
			"enableAutoScaling": true,
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
		return &eksErrorBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
