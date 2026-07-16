package module

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testKubernetesBackendName  = "managed-b"
	testKubernetesResourceType = "infra.managed_cluster"
)

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

type fakeCredProvider struct{ creds *CloudCredentials }

func (f *fakeCredProvider) Provider() string { return "gcp" }
func (f *fakeCredProvider) Region() string   { return "us-central1" }
func (f *fakeCredProvider) GetCredentials(context.Context) (*CloudCredentials, error) {
	return f.creds, nil
}

func newManagedTestModule() *PlatformKubernetes {
	return NewPlatformKubernetes("managed-module", map[string]any{
		"type":            testKubernetesBackendName,
		"clusterName":     "cluster-b",
		"version":         "1.31",
		"subscription_id": "provider-owned-value",
	})
}

func newManagedTestBackend(client pb.ResourceDriverClient) *grpcKubernetesBackend {
	return newGRPCKubernetesBackend(testKubernetesBackendName, testKubernetesResourceType, client)
}

func TestGRPCKubernetesBackend_UsesExactProviderBinding(t *testing.T) {
	outputs, err := json.Marshal(map[string]any{
		"status":   "running",
		"endpoint": "https://managed.example.test",
		"version":  "1.31",
	})
	if err != nil {
		t.Fatalf("marshal outputs: %v", err)
	}
	fake := &fakeResourceDriverClient{
		readResp: &pb.ResourceReadResponse{Output: &pb.ResourceOutput{
			Name: "cluster-b", Type: testKubernetesResourceType, Status: "running", OutputsJson: outputs,
		}},
		createResp: &pb.ResourceCreateResponse{Output: &pb.ResourceOutput{
			Name: "cluster-b", Type: testKubernetesResourceType, Status: "creating",
		}},
	}
	backend := newManagedTestBackend(fake)
	cluster := newManagedTestModule()
	cluster.provider = &fakeCredProvider{creds: &CloudCredentials{
		Provider:           "gcp",
		ProjectID:          "must-not-be-injected",
		ServiceAccountJSON: []byte(`{"private_key":"must-not-be-injected"}`),
	}}

	plan, err := backend.plan(cluster)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Provider != testKubernetesBackendName {
		t.Fatalf("plan provider = %q, want exact backend name %q", plan.Provider, testKubernetesBackendName)
	}
	assertKubernetesReadBinding(t, fake.readReq)
	if fake.readReq.GetRef().GetProviderId() != "" {
		t.Fatalf("provider-neutral ResourceRef.ProviderId = %q, want empty", fake.readReq.GetRef().GetProviderId())
	}

	if _, err := backend.apply(cluster); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if fake.createReq.GetResourceType() != testKubernetesResourceType || fake.createReq.GetSpec().GetType() != testKubernetesResourceType {
		t.Fatalf("Create binding = (%q, %q), want exact resource type %q",
			fake.createReq.GetResourceType(), fake.createReq.GetSpec().GetType(), testKubernetesResourceType)
	}
	var config map[string]any
	if err := json.Unmarshal(fake.createReq.GetSpec().GetConfigJson(), &config); err != nil {
		t.Fatalf("decode config_json: %v", err)
	}
	for _, forbidden := range []string{"project_id", "service_account_json"} {
		if _, present := config[forbidden]; present {
			t.Fatalf("provider-neutral config_json injected %s: %v", forbidden, config)
		}
	}
	if config["type"] != testKubernetesBackendName || config["version"] != "1.31" || config["subscription_id"] != "provider-owned-value" {
		t.Fatalf("config_json did not preserve user platform config: %v", config)
	}

	state, err := backend.status(cluster)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Provider != testKubernetesBackendName || state.Name != "managed-module" || state.Status != "running" {
		t.Fatalf("provider state = %+v, want backend=%q module name and running", state, testKubernetesBackendName)
	}
	if err := backend.destroy(cluster); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if fake.deleteReq.GetResourceType() != testKubernetesResourceType || fake.deleteReq.GetRef().GetType() != testKubernetesResourceType {
		t.Fatalf("Delete binding = (%q, %q), want exact resource type %q",
			fake.deleteReq.GetResourceType(), fake.deleteReq.GetRef().GetType(), testKubernetesResourceType)
	}
}

func TestGRPCKubernetesBackend_Plan(t *testing.T) {
	t.Run("not found creates provider-named action", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readErr: status.Error(codes.NotFound, "missing")}
		plan, err := newManagedTestBackend(fake).plan(newManagedTestModule())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Provider != testKubernetesBackendName || len(plan.Actions) != 1 || plan.Actions[0].Type != "create" {
			t.Fatalf("plan = %+v, want provider-named create", plan)
		}
		if !strings.Contains(plan.Actions[0].Detail, testKubernetesBackendName) || strings.Contains(strings.ToLower(plan.Actions[0].Detail), "gke") {
			t.Fatalf("provider-neutral detail = %q", plan.Actions[0].Detail)
		}
		assertKubernetesReadBinding(t, fake.readReq)
	})

	t.Run("existing output creates noop", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readResp: &pb.ResourceReadResponse{Output: &pb.ResourceOutput{Status: "running"}}}
		plan, err := newManagedTestBackend(fake).plan(newManagedTestModule())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(plan.Actions) != 1 || plan.Actions[0].Type != "noop" {
			t.Fatalf("plan actions = %+v, want noop", plan.Actions)
		}
	})

	t.Run("transport error names backend", func(t *testing.T) {
		fake := &fakeResourceDriverClient{readErr: status.Error(codes.Unavailable, "boom")}
		_, err := newManagedTestBackend(fake).plan(newManagedTestModule())
		if err == nil || !strings.Contains(err.Error(), testKubernetesBackendName) || strings.Contains(strings.ToLower(err.Error()), "gke") {
			t.Fatalf("plan error = %v, want provider-neutral backend identity", err)
		}
	})
}

func TestGRPCKubernetesBackend_ApplyErrors(t *testing.T) {
	t.Run("already exists resolves to success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createErr: status.Error(codes.AlreadyExists, "exists")}
		result, err := newManagedTestBackend(fake).apply(newManagedTestModule())
		if err != nil || !result.Success || !strings.Contains(result.Message, testKubernetesBackendName) {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	})

	t.Run("transport error names backend", func(t *testing.T) {
		fake := &fakeResourceDriverClient{createErr: status.Error(codes.Internal, "boom")}
		_, err := newManagedTestBackend(fake).apply(newManagedTestModule())
		if err == nil || !strings.Contains(err.Error(), testKubernetesBackendName) {
			t.Fatalf("apply error = %v, want backend identity", err)
		}
	})
}

func TestGRPCKubernetesBackend_StatusProjection(t *testing.T) {
	outputs, err := json.Marshal(map[string]any{
		"status":   "running",
		"endpoint": "https://managed.example.test",
		"version":  "1.31.1",
		"nodeGroups": []any{map[string]any{
			"name": "pool", "instanceType": "medium", "min": 1, "max": 3, "current": 2,
		}},
	})
	if err != nil {
		t.Fatalf("marshal outputs: %v", err)
	}
	fake := &fakeResourceDriverClient{readResp: &pb.ResourceReadResponse{Output: &pb.ResourceOutput{
		Status: "running", OutputsJson: outputs,
	}}}
	state, err := newManagedTestBackend(fake).status(newManagedTestModule())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Name != "managed-module" || state.Provider != testKubernetesBackendName || state.Status != "running" || state.Endpoint != "https://managed.example.test" || state.Version != "1.31.1" {
		t.Fatalf("state = %+v", state)
	}
	if len(state.NodeGroups) != 1 || state.NodeGroups[0].Name != "pool" || state.NodeGroups[0].Current != 2 {
		t.Fatalf("node groups = %+v", state.NodeGroups)
	}

	notFound := &fakeResourceDriverClient{readErr: status.Error(codes.NotFound, "missing")}
	state, err = newManagedTestBackend(notFound).status(newManagedTestModule())
	if err != nil || state.Status != "not-found" || state.Provider != testKubernetesBackendName {
		t.Fatalf("not-found state = %+v, err = %v", state, err)
	}
}

func TestGRPCKubernetesBackend_DestroyErrors(t *testing.T) {
	t.Run("not found resolves to success", func(t *testing.T) {
		fake := &fakeResourceDriverClient{deleteErr: status.Error(codes.NotFound, "missing")}
		if err := newManagedTestBackend(fake).destroy(newManagedTestModule()); err != nil {
			t.Fatalf("destroy: %v", err)
		}
	})

	t.Run("transport error names backend", func(t *testing.T) {
		fake := &fakeResourceDriverClient{deleteErr: status.Error(codes.Internal, "boom")}
		err := newManagedTestBackend(fake).destroy(newManagedTestModule())
		if err == nil || !strings.Contains(err.Error(), testKubernetesBackendName) {
			t.Fatalf("destroy error = %v, want backend identity", err)
		}
	})
}

func assertKubernetesReadBinding(t *testing.T, request *pb.ResourceReadRequest) {
	t.Helper()
	if request.GetResourceType() != testKubernetesResourceType || request.GetRef().GetType() != testKubernetesResourceType {
		t.Fatalf("Read binding = (%q, %q), want exact resource type %q",
			request.GetResourceType(), request.GetRef().GetType(), testKubernetesResourceType)
	}
}
