// platform_kubernetes_grpc.go — the host-side kubernetesBackend that dispatches
// the `gke` cluster type to a plugin-served ResourceDriver gRPC client.
//
// Per ADR 0037 (decisions/0037-gke-cross-process-contract.md), `gke` folds into
// the existing ResourceDriver contract — ZERO new proto surface. A GKE cluster
// is a managed resource served by workflow-plugin-gcp under the resource type
// `infra.k8s_cluster`:
//
//	kubernetesBackend  →  ResourceDriver RPC
//	plan               →  Read   (probe existence, synthesize create|noop plan)
//	apply              →  Create (AlreadyExists resolves to success)
//	status             →  Read   (project outputs_json onto KubernetesClusterState)
//	destroy            →  Delete (NotFound resolves to success)
//
// Precedent: the Phase A grpcIaCStateStore adapter (module/iac_state_grpc_client.go).
package module

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// gkeResourceType is the ResourceDriver resource type workflow-plugin-gcp serves
// GKE clusters under (provider/provider.go registers GKEDriver here). ADR 0037
// Option 1 dispatches the in-core `gke` kubernetesBackend to this driver.
const gkeResourceType = "infra.k8s_cluster"

// KubernetesClusterState outputs_json key contract — the READ side. ADR 0037
// makes the host adapter the owner of these key names; workflow-plugin-gcp's
// GKEDriver.Read (Task 22) conforms its output to them. Keys mirror the
// KubernetesClusterState JSON field tags so the projection is a direct
// re-marshal.
const (
	k8sOutputKeyStatus     = "status"
	k8sOutputKeyEndpoint   = "endpoint"
	k8sOutputKeyVersion    = "version"
	k8sOutputKeyNodeGroups = "nodeGroups"
)

// ResourceSpec.config_json key contract — the WRITE side. These are the keys
// buildResourceSpec injects into the spec config_json that workflow-plugin-gcp's
// GKEDriver (Task 22) reads to resolve project + credentials. snake_case to
// match the deleted in-core gkeBackend's config keys (it read
// k.config["project_id"]) — see ADR 0037: "the host adapter define the key
// contract and Task 22 conform." The user-supplied platform.kubernetes config
// (e.g. `version`, `zone`, `nodeGroups`) is copied through verbatim; only these
// resolved-credential keys are host-adapter-owned.
const (
	k8sConfigKeyProjectID          = "project_id"
	k8sConfigKeyServiceAccountJSON = "service_account_json" //nolint:gosec // G101: config map key name, not a credential
)

// grpcKubernetesBackend adapts a pb.ResourceDriverClient (resource type
// infra.k8s_cluster) to the in-core kubernetesBackend interface, so a
// plugin-served `gke` backend is dispatched exactly like the deleted in-core
// gkeBackend was.
type grpcKubernetesBackend struct {
	client pb.ResourceDriverClient
}

// newGRPCKubernetesBackend wraps a ResourceDriverClient as a kubernetesBackend.
func newGRPCKubernetesBackend(c pb.ResourceDriverClient) *grpcKubernetesBackend {
	return &grpcKubernetesBackend{client: c}
}

// Compile-time guard: the gRPC adapter MUST satisfy the in-core contract so the
// engine seam (Task 26) can register it like any other kubernetesBackend.
var _ kubernetesBackend = (*grpcKubernetesBackend)(nil)

// plan probes the cluster's existence via ResourceDriver.Read and synthesizes a
// PlatformPlan — a single `create` action when the cluster is absent
// (codes.NotFound), a `noop` action when it already exists. This mirrors the
// deleted in-core gkeBackend.plan, whose own logic was a Get-or-create check.
func (b *grpcKubernetesBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	plan := &PlatformPlan{Provider: "gke", Resource: k.clusterName()}
	resp, err := b.client.Read(context.Background(), &pb.ResourceReadRequest{
		ResourceType: gkeResourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			plan.Actions = []PlatformAction{{
				Type:     "create",
				Resource: k.clusterName(),
				Detail:   fmt.Sprintf("create GKE cluster %q", k.clusterName()),
			}}
			return plan, nil
		}
		return nil, fmt.Errorf("gke plan: Read %q: %w", k.clusterName(), err)
	}
	if resp.GetOutput() != nil {
		plan.Actions = []PlatformAction{{
			Type:     "noop",
			Resource: k.clusterName(),
			Detail:   fmt.Sprintf("GKE cluster %q exists (status: %s)", k.clusterName(), resp.GetOutput().GetStatus()),
		}}
		return plan, nil
	}
	plan.Actions = []PlatformAction{{
		Type:     "create",
		Resource: k.clusterName(),
		Detail:   fmt.Sprintf("create GKE cluster %q", k.clusterName()),
	}}
	return plan, nil
}

// apply creates the cluster via ResourceDriver.Create. Per ADR 0037 a
// codes.AlreadyExists response resolves to success — preserving the in-core
// gkeBackend.apply behavior that swallowed ALREADY_EXISTS.
func (b *grpcKubernetesBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	spec, err := b.buildResourceSpec(k)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.Create(context.Background(), &pb.ResourceCreateRequest{
		ResourceType: gkeResourceType,
		Spec:         spec,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return &PlatformResult{
				Success: true,
				Message: fmt.Sprintf("GKE cluster %q already exists", k.clusterName()),
				State:   k.state,
			}, nil
		}
		return nil, fmt.Errorf("gke apply: Create %q: %w", k.clusterName(), err)
	}
	clusterState, err := kubernetesClusterStateFromOutput(k.clusterName(), resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("gke apply: %w", err)
	}
	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("GKE cluster %q creation initiated", k.clusterName()),
		State:   clusterState,
	}, nil
}

// status reads the cluster via ResourceDriver.Read and projects the
// outputs_json map onto the typed KubernetesClusterState. A codes.NotFound
// response yields a clean not-found state rather than an error — matching the
// in-core gkeBackend.status, which set Status="not-found" on a failed Get.
func (b *grpcKubernetesBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	resp, err := b.client.Read(context.Background(), &pb.ResourceReadRequest{
		ResourceType: gkeResourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &KubernetesClusterState{Name: k.clusterName(), Provider: "gke", Status: "not-found"}, nil
		}
		return nil, fmt.Errorf("gke status: Read %q: %w", k.clusterName(), err)
	}
	st, err := kubernetesClusterStateFromOutput(k.clusterName(), resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("gke status: %w", err)
	}
	return st, nil
}

// destroy deletes the cluster via ResourceDriver.Delete. Per ADR 0037 a
// codes.NotFound response resolves to success — preserving the in-core
// gkeBackend.destroy behavior that swallowed NOT_FOUND.
func (b *grpcKubernetesBackend) destroy(k *PlatformKubernetes) error {
	_, err := b.client.Delete(context.Background(), &pb.ResourceDeleteRequest{
		ResourceType: gkeResourceType,
		Ref:          b.buildResourceRef(k),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("gke destroy: Delete %q: %w", k.clusterName(), err)
	}
	return nil
}

// buildResourceRef builds the ResourceRef the ResourceDriver RPCs address the
// cluster by — keyed on the cluster name and the infra.k8s_cluster type.
func (b *grpcKubernetesBackend) buildResourceRef(k *PlatformKubernetes) *pb.ResourceRef {
	return &pb.ResourceRef{
		Name: k.clusterName(),
		Type: gkeResourceType,
	}
}

// buildResourceSpec builds the ResourceSpec for a Create RPC. The user-supplied
// platform.kubernetes module config is carried through as config_json verbatim
// (the plugin GKEDriver reads location/zone/version/nodeGroups from it — those
// keys stay exactly as the user authored them); buildResourceSpec then folds in
// the host-adapter-owned resolved-credential keys (k8sConfigKeyProjectID /
// k8sConfigKeyServiceAccountJSON) when a cloud account is wired. No GKE-version
// default is injected here — version defaulting is GKE-domain knowledge that
// belongs in the plugin's GKEDriver, not this generic host adapter.
func (b *grpcKubernetesBackend) buildResourceSpec(k *PlatformKubernetes) (*pb.ResourceSpec, error) {
	cfg := make(map[string]any, len(k.config)+2)
	for key, val := range k.config {
		cfg[key] = val
	}
	if err := b.injectCredentials(k, cfg); err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("gke: encode resource spec config: %w", err)
	}
	return &pb.ResourceSpec{
		Name:       k.clusterName(),
		Type:       gkeResourceType,
		ConfigJson: configJSON,
	}, nil
}

// injectCredentials resolves the cloud account (when one is wired) and folds the
// GCP project ID + service-account JSON into the spec config under the pinned
// k8sConfigKey* names — the cross-process equivalent of the in-core
// containerService building the SDK client from CloudCredentials. The key names
// (snake_case) match the deleted in-core gkeBackend's config keys and are the
// contract workflow-plugin-gcp's GKEDriver (Task 22) reads.
func (b *grpcKubernetesBackend) injectCredentials(k *PlatformKubernetes, cfg map[string]any) error {
	if k.provider == nil {
		return nil
	}
	creds, err := k.provider.GetCredentials(context.Background())
	if err != nil {
		return fmt.Errorf("gke: resolve cloud credentials: %w", err)
	}
	if creds == nil {
		return nil
	}
	if creds.ProjectID != "" {
		cfg[k8sConfigKeyProjectID] = creds.ProjectID
	}
	if len(creds.ServiceAccountJSON) > 0 {
		cfg[k8sConfigKeyServiceAccountJSON] = string(creds.ServiceAccountJSON)
	}
	return nil
}

// kubernetesClusterStateFromOutput projects a ResourceDriver ResourceOutput onto
// the typed KubernetesClusterState. The free-form outputs_json map crosses the
// wire as JSON bytes (the iac.proto invariant); this is the host-owned
// map→struct projection ADR 0037 assigns to Tasks 25/26. The adapter sets
// Provider="gke" itself and tolerates a missing/empty outputs_json.
func kubernetesClusterStateFromOutput(name string, out *pb.ResourceOutput) (*KubernetesClusterState, error) {
	st := &KubernetesClusterState{Name: name, Provider: "gke", Status: "not-found"}
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
	if e, ok := outputs[k8sOutputKeyEndpoint].(string); ok {
		st.Endpoint = e
	}
	if v, ok := outputs[k8sOutputKeyVersion].(string); ok {
		st.Version = v
	}
	if ngRaw, ok := outputs[k8sOutputKeyNodeGroups]; ok && ngRaw != nil {
		groups, err := nodeGroupsFromAny(ngRaw)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", k8sOutputKeyNodeGroups, err)
		}
		st.NodeGroups = groups
	}
	return st, nil
}

// nodeGroupsFromAny re-marshals the free-form nodeGroups value (an []any of
// map[string]any decoded from JSON) into typed NodeGroupState slices.
func nodeGroupsFromAny(v any) ([]NodeGroupState, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var groups []NodeGroupState
	if err := json.Unmarshal(b, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}
