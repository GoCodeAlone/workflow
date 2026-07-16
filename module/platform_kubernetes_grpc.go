// platform_kubernetes_grpc.go adapts an exact plugin-declared Kubernetes
// backend binding to the host's provider-neutral kubernetesBackend lifecycle.
package module

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	k8sOutputKeyStatus     = "status"
	k8sOutputKeyEndpoint   = "endpoint"
	k8sOutputKeyVersion    = "version"
	k8sOutputKeyNodeGroups = "nodeGroups"
)

// grpcKubernetesBackend carries the complete manifest/runtime binding. The
// host owns generic lifecycle translation only; provider-specific addressing,
// credentials, defaults, and validation remain in the selected plugin.
type grpcKubernetesBackend struct {
	backendName  string
	resourceType string
	client       pb.ResourceDriverClient
}

func newGRPCKubernetesBackend(backendName, resourceType string, client pb.ResourceDriverClient) *grpcKubernetesBackend {
	return &grpcKubernetesBackend{
		backendName:  backendName,
		resourceType: resourceType,
		client:       client,
	}
}

var _ kubernetesBackend = (*grpcKubernetesBackend)(nil)

func (b *grpcKubernetesBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	plan := &PlatformPlan{Provider: b.backendName, Resource: k.clusterName()}
	resp, err := b.client.Read(context.Background(), &pb.ResourceReadRequest{
		ResourceType: b.resourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			plan.Actions = []PlatformAction{{
				Type:     "create",
				Resource: k.clusterName(),
				Detail:   fmt.Sprintf("create %s cluster %q", b.backendName, k.clusterName()),
			}}
			return plan, nil
		}
		return nil, fmt.Errorf("%s plan: Read %q: %w", b.backendName, k.clusterName(), err)
	}
	if resp.GetOutput() != nil {
		plan.Actions = []PlatformAction{{
			Type:     "noop",
			Resource: k.clusterName(),
			Detail:   fmt.Sprintf("%s cluster %q exists (status: %s)", b.backendName, k.clusterName(), resp.GetOutput().GetStatus()),
		}}
		return plan, nil
	}
	plan.Actions = []PlatformAction{{
		Type:     "create",
		Resource: k.clusterName(),
		Detail:   fmt.Sprintf("create %s cluster %q", b.backendName, k.clusterName()),
	}}
	return plan, nil
}

func (b *grpcKubernetesBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	spec, err := b.buildResourceSpec(k)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.Create(context.Background(), &pb.ResourceCreateRequest{
		ResourceType: b.resourceType,
		Spec:         spec,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return &PlatformResult{
				Success: true,
				Message: fmt.Sprintf("%s cluster %q already exists", b.backendName, k.clusterName()),
				State:   k.state,
			}, nil
		}
		return nil, fmt.Errorf("%s apply: Create %q: %w", b.backendName, k.clusterName(), err)
	}
	clusterState, err := kubernetesClusterStateFromOutput(k.name, b.backendName, resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("%s apply: %w", b.backendName, err)
	}
	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("%s cluster %q creation initiated", b.backendName, k.clusterName()),
		State:   clusterState,
	}, nil
}

func (b *grpcKubernetesBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	resp, err := b.client.Read(context.Background(), &pb.ResourceReadRequest{
		ResourceType: b.resourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &KubernetesClusterState{Name: k.name, Provider: b.backendName, Status: "not-found"}, nil
		}
		return nil, fmt.Errorf("%s status: Read %q: %w", b.backendName, k.clusterName(), err)
	}
	st, err := kubernetesClusterStateFromOutput(k.name, b.backendName, resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("%s status: %w", b.backendName, err)
	}
	return st, nil
}

func (b *grpcKubernetesBackend) destroy(k *PlatformKubernetes) error {
	_, err := b.client.Delete(context.Background(), &pb.ResourceDeleteRequest{
		ResourceType: b.resourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("%s destroy: Delete %q: %w", b.backendName, k.clusterName(), err)
	}
	return nil
}

func (b *grpcKubernetesBackend) buildResourceRef(k *PlatformKubernetes) *pb.ResourceRef {
	return &pb.ResourceRef{
		Name: k.clusterName(),
		Type: b.resourceType,
	}
}

func (b *grpcKubernetesBackend) buildResourceSpec(k *PlatformKubernetes) (*pb.ResourceSpec, error) {
	configJSON, err := json.Marshal(k.config)
	if err != nil {
		return nil, fmt.Errorf("%s: encode resource spec config: %w", b.backendName, err)
	}
	return &pb.ResourceSpec{
		Name:       k.clusterName(),
		Type:       b.resourceType,
		ConfigJson: configJSON,
	}, nil
}

// kubernetesClusterStateFromOutput projects the provider-neutral Kubernetes
// output contract onto the host state while preserving the selected backend's
// exact public name.
func kubernetesClusterStateFromOutput(moduleName, backendName string, out *pb.ResourceOutput) (*KubernetesClusterState, error) {
	st := &KubernetesClusterState{Name: moduleName, Provider: backendName, Status: "not-found"}
	if out == nil {
		return st, nil
	}
	if out.GetStatus() != "" {
		st.Status = out.GetStatus()
	}
	outputs, err := jsonBytesToMap(out.GetOutputsJson())
	if err != nil {
		return nil, fmt.Errorf("decode outputs_json: %w", err)
	}
	if outputs == nil {
		return st, nil
	}
	if s, ok := outputs[k8sOutputKeyStatus].(string); ok && s != "" {
		st.Status = s
	}
	if endpoint, ok := outputs[k8sOutputKeyEndpoint].(string); ok {
		st.Endpoint = endpoint
	}
	if version, ok := outputs[k8sOutputKeyVersion].(string); ok {
		st.Version = version
	}
	if rawGroups, ok := outputs[k8sOutputKeyNodeGroups]; ok && rawGroups != nil {
		groups, err := nodeGroupsFromAny(rawGroups)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", k8sOutputKeyNodeGroups, err)
		}
		st.NodeGroups = groups
	}
	return st, nil
}

func nodeGroupsFromAny(value any) ([]NodeGroupState, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var groups []NodeGroupState
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}
