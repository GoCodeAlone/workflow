package iam

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/store"
)

// KubernetesConfig holds configuration for the Kubernetes RBAC provider.
type KubernetesConfig struct {
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace"`
}

// KubernetesProvider maps Kubernetes ServiceAccounts and Groups to roles.
// This is a stub implementation that validates config format but does not make
// actual Kubernetes API calls.
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

func (p *KubernetesProvider) ResolveIdentities(_ context.Context, _ json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error) {
	sa := credentials["service_account"]
	group := credentials["group"]

	if sa == "" && group == "" {
		return nil, fmt.Errorf("service_account or group credential required")
	}

	var identities []ExternalIdentity
	if sa != "" {
		identities = append(identities, ExternalIdentity{
			Provider:   string(store.IAMProviderKubernetes),
			Identifier: "sa:" + sa,
			Attributes: map[string]string{"service_account": sa},
		})
	}
	if group != "" {
		identities = append(identities, ExternalIdentity{
			Provider:   string(store.IAMProviderKubernetes),
			Identifier: "group:" + group,
			Attributes: map[string]string{"group": group},
		})
	}
	return identities, nil
}

func (p *KubernetesProvider) TestConnection(_ context.Context, config json.RawMessage) error {
	return p.ValidateConfig(config)
}
