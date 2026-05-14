package module

import (
	"context"
	"encoding/json"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeResourceDriverClient is a pb.ResourceDriverClient stub for the
// grpcKubernetesBackend adapter tests. It records the requests it received and
// returns canned responses/errors per RPC. The 6 RPCs the adapter never calls
// (Update/Diff/Scale/HealthCheck/SensitiveKeys/Troubleshoot) are no-ops.
type fakeResourceDriverClient struct {
	createReq  *pb.ResourceCreateRequest
	createResp *pb.ResourceCreateResponse
	createErr  error

	readReq  *pb.ResourceReadRequest
	readResp *pb.ResourceReadResponse
	readErr  error

	deleteReq *pb.ResourceDeleteRequest
	deleteErr error
}

func (f *fakeResourceDriverClient) Create(_ context.Context, in *pb.ResourceCreateRequest, _ ...grpc.CallOption) (*pb.ResourceCreateResponse, error) {
	f.createReq = in
	return f.createResp, f.createErr
}
func (f *fakeResourceDriverClient) Read(_ context.Context, in *pb.ResourceReadRequest, _ ...grpc.CallOption) (*pb.ResourceReadResponse, error) {
	f.readReq = in
	return f.readResp, f.readErr
}
func (f *fakeResourceDriverClient) Delete(_ context.Context, in *pb.ResourceDeleteRequest, _ ...grpc.CallOption) (*pb.ResourceDeleteResponse, error) {
	f.deleteReq = in
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &pb.ResourceDeleteResponse{}, nil
}
func (*fakeResourceDriverClient) Update(context.Context, *pb.ResourceUpdateRequest, ...grpc.CallOption) (*pb.ResourceUpdateResponse, error) {
	return nil, nil
}
func (*fakeResourceDriverClient) Diff(context.Context, *pb.ResourceDiffRequest, ...grpc.CallOption) (*pb.ResourceDiffResponse, error) {
	return nil, nil
}
func (*fakeResourceDriverClient) Scale(context.Context, *pb.ResourceScaleRequest, ...grpc.CallOption) (*pb.ResourceScaleResponse, error) {
	return nil, nil
}
func (*fakeResourceDriverClient) HealthCheck(context.Context, *pb.ResourceHealthCheckRequest, ...grpc.CallOption) (*pb.ResourceHealthCheckResponse, error) {
	return nil, nil
}
func (*fakeResourceDriverClient) SensitiveKeys(context.Context, *pb.SensitiveKeysRequest, ...grpc.CallOption) (*pb.SensitiveKeysResponse, error) {
	return nil, nil
}
func (*fakeResourceDriverClient) Troubleshoot(context.Context, *pb.TroubleshootRequest, ...grpc.CallOption) (*pb.TroubleshootResponse, error) {
	return nil, nil
}

func newGKETestModule() *PlatformKubernetes {
	return NewPlatformKubernetes("my-cluster", map[string]any{
		"type":        "gke",
		"clusterName": "my-cluster",
		"version":     "1.29",
	})
}

func TestGRPCKubernetesBackend_Plan(t *testing.T) {
	t.Run("not found → create action", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readErr: status.Error(codes.NotFound, "no such cluster")}
		b := newGRPCKubernetesBackend(fake)
		plan, err := b.plan(newGKETestModule())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Provider != "gke" || plan.Resource != "my-cluster" {
			t.Fatalf("plan header mismatch: %+v", plan)
		}
		if len(plan.Actions) != 1 || plan.Actions[0].Type != "create" {
			t.Fatalf("expected one create action, got %+v", plan.Actions)
		}
		if fake.readReq.GetResourceType() != gkeResourceType {
			t.Fatalf("Read resource_type = %q, want %q", fake.readReq.GetResourceType(), gkeResourceType)
		}
	})

	t.Run("exists → noop action", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readResp: &pb.ResourceReadResponse{
			Output: &pb.ResourceOutput{Name: "my-cluster", Type: gkeResourceType, Status: "running"},
		}}
		b := newGRPCKubernetesBackend(fake)
		plan, err := b.plan(newGKETestModule())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(plan.Actions) != 1 || plan.Actions[0].Type != "noop" {
			t.Fatalf("expected one noop action, got %+v", plan.Actions)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readErr: status.Error(codes.Unavailable, "boom")}
		b := newGRPCKubernetesBackend(fake)
		if _, err := b.plan(newGKETestModule()); err == nil {
			t.Fatal("plan must propagate a non-NotFound transport error")
		}
	})
}

// fakeCredProvider is a minimal CloudCredentialProvider for exercising the
// credential-injection path of buildResourceSpec.
type fakeCredProvider struct{ creds *CloudCredentials }

func (f *fakeCredProvider) Provider() string { return "gcp" }
func (f *fakeCredProvider) Region() string   { return "us-central1" }
func (f *fakeCredProvider) GetCredentials(context.Context) (*CloudCredentials, error) {
	return f.creds, nil
}

func TestGRPCKubernetesBackend_Apply(t *testing.T) {
	t.Run("create success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createResp: &pb.ResourceCreateResponse{
			Output: &pb.ResourceOutput{Name: "my-cluster", Type: gkeResourceType, Status: "creating"},
		}}
		b := newGRPCKubernetesBackend(fake)
		res, err := b.apply(newGKETestModule())
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if !res.Success {
			t.Fatalf("apply Success = false: %+v", res)
		}
		if fake.createReq.GetResourceType() != gkeResourceType {
			t.Fatalf("Create resource_type = %q, want %q", fake.createReq.GetResourceType(), gkeResourceType)
		}
		spec := fake.createReq.GetSpec()
		if spec.GetName() != "my-cluster" || spec.GetType() != gkeResourceType {
			t.Fatalf("Create spec mismatch: name=%q type=%q", spec.GetName(), spec.GetType())
		}
		if len(spec.GetConfigJson()) == 0 {
			t.Fatal("Create spec config_json must carry the platform.kubernetes config")
		}
	})

	t.Run("resolved credentials use the pinned snake_case config keys", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createResp: &pb.ResourceCreateResponse{
			Output: &pb.ResourceOutput{Name: "my-cluster", Type: gkeResourceType, Status: "creating"},
		}}
		b := newGRPCKubernetesBackend(fake)
		m := newGKETestModule()
		m.provider = &fakeCredProvider{creds: &CloudCredentials{
			Provider:           "gcp",
			ProjectID:          "my-gcp-project",
			ServiceAccountJSON: []byte(`{"type":"service_account"}`),
		}}
		if _, err := b.apply(m); err != nil {
			t.Fatalf("apply: %v", err)
		}
		cfg, err := jsonBytesToMap(fake.createReq.GetSpec().GetConfigJson())
		if err != nil {
			t.Fatalf("config_json decode: %v", err)
		}
		// The host-adapter-owned credential keys are snake_case — the contract
		// workflow-plugin-gcp's GKEDriver (Task 22) reads.
		if cfg[k8sConfigKeyProjectID] != "my-gcp-project" {
			t.Errorf("config_json[%q] = %v, want my-gcp-project", k8sConfigKeyProjectID, cfg[k8sConfigKeyProjectID])
		}
		if cfg[k8sConfigKeyServiceAccountJSON] != `{"type":"service_account"}` {
			t.Errorf("config_json[%q] = %v, want the service-account JSON", k8sConfigKeyServiceAccountJSON, cfg[k8sConfigKeyServiceAccountJSON])
		}
		// Guard against a camelCase regression — the GKEDriver reads snake_case.
		if _, bad := cfg["projectId"]; bad {
			t.Error("config_json must not use camelCase 'projectId' — the GKEDriver reads snake_case 'project_id'")
		}
		if _, bad := cfg["serviceAccountJSON"]; bad {
			t.Error("config_json must not use camelCase 'serviceAccountJSON' — the GKEDriver reads snake_case 'service_account_json'")
		}
	})

	t.Run("already exists resolves to success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createErr: status.Error(codes.AlreadyExists, "exists")}
		b := newGRPCKubernetesBackend(fake)
		res, err := b.apply(newGKETestModule())
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if !res.Success {
			t.Fatalf("apply on AlreadyExists must be Success=true, got %+v", res)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createErr: status.Error(codes.Internal, "boom")}
		b := newGRPCKubernetesBackend(fake)
		if _, err := b.apply(newGKETestModule()); err == nil {
			t.Fatal("apply must propagate a non-AlreadyExists error")
		}
	})
}

func TestGRPCKubernetesBackend_Status(t *testing.T) {
	t.Run("outputs_json projects onto KubernetesClusterState", func(t *testing.T) {
		outputs := map[string]any{
			"status":   "running",
			"endpoint": "https://1.2.3.4",
			"version":  "1.29.1",
			"nodeGroups": []any{
				map[string]any{
					"name":         "default-pool",
					"instanceType": "e2-medium",
					"min":          1,
					"max":          3,
					"current":      2,
				},
			},
		}
		outJSON, err := json.Marshal(outputs)
		if err != nil {
			t.Fatalf("marshal outputs: %v", err)
		}
		fake := &fakeResourceDriverClient{readResp: &pb.ResourceReadResponse{
			Output: &pb.ResourceOutput{
				Name:        "my-cluster",
				Type:        gkeResourceType,
				ProviderId:  "projects/p/locations/l/clusters/my-cluster",
				OutputsJson: outJSON,
				Status:      "running",
			},
		}}
		b := newGRPCKubernetesBackend(fake)
		st, err := b.status(newGKETestModule())
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Name != "my-cluster" || st.Provider != "gke" {
			t.Fatalf("status identity mismatch: %+v", st)
		}
		if st.Status != "running" || st.Endpoint != "https://1.2.3.4" || st.Version != "1.29.1" {
			t.Fatalf("status fields mismatch: %+v", st)
		}
		if len(st.NodeGroups) != 1 {
			t.Fatalf("expected 1 node group, got %d", len(st.NodeGroups))
		}
		ng := st.NodeGroups[0]
		if ng.Name != "default-pool" || ng.InstanceType != "e2-medium" || ng.Min != 1 || ng.Max != 3 || ng.Current != 2 {
			t.Fatalf("node group did not survive the JSON-bytes round-trip: %+v", ng)
		}
	})

	t.Run("not found → not-found state", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readErr: status.Error(codes.NotFound, "no such cluster")}
		b := newGRPCKubernetesBackend(fake)
		st, err := b.status(newGKETestModule())
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Status != "not-found" || st.Provider != "gke" {
			t.Fatalf("expected not-found gke state, got %+v", st)
		}
	})
}

func TestGRPCKubernetesBackend_Destroy(t *testing.T) {
	t.Run("delete success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{}
		b := newGRPCKubernetesBackend(fake)
		if err := b.destroy(newGKETestModule()); err != nil {
			t.Fatalf("destroy: %v", err)
		}
		if fake.deleteReq.GetResourceType() != gkeResourceType {
			t.Fatalf("Delete resource_type = %q, want %q", fake.deleteReq.GetResourceType(), gkeResourceType)
		}
		if fake.deleteReq.GetRef().GetName() != "my-cluster" {
			t.Fatalf("Delete ref name = %q, want my-cluster", fake.deleteReq.GetRef().GetName())
		}
	})

	t.Run("not found resolves to success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{deleteErr: status.Error(codes.NotFound, "gone")}
		b := newGRPCKubernetesBackend(fake)
		if err := b.destroy(newGKETestModule()); err != nil {
			t.Fatalf("destroy on NotFound must succeed, got %v", err)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		fake := &fakeResourceDriverClient{deleteErr: status.Error(codes.Internal, "boom")}
		b := newGRPCKubernetesBackend(fake)
		if err := b.destroy(newGKETestModule()); err == nil {
			t.Fatal("destroy must propagate a non-NotFound error")
		}
	})
}
