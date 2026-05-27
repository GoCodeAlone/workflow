package wfctlhelpers

// Internal (white-box) test for the spaces/s3/gcs plugin-served code path.
// Lives in `package wfctlhelpers` (not wfctlhelpers_test) so it can swap
// the unexported `loadPluginStateBackendClients` seam variable without
// touching production binaries.
//
// Per code-reviewer I-2.3 on commit 7a064b824: the seam exists
// specifically for tests to bypass real plugin binary loading, but no
// test exercised it — leaving the spaces/s3/gcs branch entirely
// uncovered by unit tests.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// fakeIaCStateBackendClient is a minimal pb.IaCStateBackendClient that
// records Configure inputs + serves a fixed ListStates response. Other
// methods return zero-valued OK responses; tests that need richer
// behavior can extend per scenario.
type fakeIaCStateBackendClient struct {
	configureBackend string
	configureJSON    []byte
	states           []*pb.IaCState
}

func (c *fakeIaCStateBackendClient) Configure(_ context.Context, req *pb.ConfigureRequest, _ ...grpc.CallOption) (*pb.ConfigureResponse, error) {
	c.configureBackend = req.BackendName
	c.configureJSON = req.ConfigJson
	return &pb.ConfigureResponse{}, nil
}
func (c *fakeIaCStateBackendClient) GetState(_ context.Context, _ *pb.GetStateRequest, _ ...grpc.CallOption) (*pb.GetStateResponse, error) {
	return &pb.GetStateResponse{}, nil
}
func (c *fakeIaCStateBackendClient) SaveState(_ context.Context, _ *pb.SaveStateRequest, _ ...grpc.CallOption) (*pb.SaveStateResponse, error) {
	return &pb.SaveStateResponse{}, nil
}
func (c *fakeIaCStateBackendClient) ListStates(_ context.Context, _ *pb.ListStatesRequest, _ ...grpc.CallOption) (*pb.ListStatesResponse, error) {
	return &pb.ListStatesResponse{States: c.states}, nil
}
func (c *fakeIaCStateBackendClient) DeleteState(_ context.Context, _ *pb.DeleteStateRequest, _ ...grpc.CallOption) (*pb.DeleteStateResponse, error) {
	return &pb.DeleteStateResponse{}, nil
}
func (c *fakeIaCStateBackendClient) Lock(_ context.Context, _ *pb.LockRequest, _ ...grpc.CallOption) (*pb.LockResponse, error) {
	return &pb.LockResponse{}, nil
}
func (c *fakeIaCStateBackendClient) Unlock(_ context.Context, _ *pb.UnlockRequest, _ ...grpc.CallOption) (*pb.UnlockResponse, error) {
	return &pb.UnlockResponse{}, nil
}
func (c *fakeIaCStateBackendClient) ListBackendNames(_ context.Context, _ *pb.ListBackendNamesRequest, _ ...grpc.CallOption) (*pb.ListBackendNamesResponse, error) {
	return &pb.ListBackendNamesResponse{BackendNames: []string{"spaces"}}, nil
}

// TestResolvePluginStore_ConfiguresAdvertisedBackend exercises the
// spaces/s3/gcs branch end-to-end with a fake plugin loader. The seam
// swap proves:
//  1. The candidate ordering puts digitalocean first for `spaces`.
//  2. Configure is invoked with the requested backend name + JSON cfg.
//  3. ListResources round-trips a state record returned by the fake.
//
// Without this test the spaces/s3/gcs path would ship untested.
func TestResolvePluginStore_ConfiguresAdvertisedBackend(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	for _, name := range []string{"auth", "digitalocean"} {
		if err := os.MkdirAll(filepath.Join(pluginDir, name), 0o750); err != nil {
			t.Fatalf("mkdir plugin %s: %v", name, err)
		}
	}

	client := &fakeIaCStateBackendClient{
		states: []*pb.IaCState{{
			ResourceId:   "site-vpc",
			ResourceType: "infra.vpc",
			Provider:     "digitalocean",
			ProviderId:   "vpc-123",
			ConfigJson:   []byte(`{"region":"nyc3"}`),
			OutputsJson:  []byte(`{"id":"vpc-123"}`),
		}},
	}
	var loaded []string
	orig := loadPluginStateBackendClients
	loadPluginStateBackendClients = func(_ *external.ExternalPluginManager, pluginName, backend string) (map[string]pb.IaCStateBackendClient, error) {
		loaded = append(loaded, pluginName)
		if pluginName != "digitalocean" {
			return map[string]pb.IaCStateBackendClient{}, nil
		}
		return map[string]pb.IaCStateBackendClient{backend: client}, nil
	}
	t.Cleanup(func() { loadPluginStateBackendClients = orig })

	store, err := resolvePluginStore(context.Background(), "spaces", map[string]any{
		"backend": "spaces",
		"bucket":  "bmw-iac-state",
	}, pluginDir)
	if err != nil {
		t.Fatalf("resolvePluginStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if len(loaded) == 0 || loaded[0] != "digitalocean" {
		t.Fatalf("loaded plugins = %#v, want digitalocean first (priority list)", loaded)
	}
	if client.configureBackend != "spaces" {
		t.Errorf("Configure backend = %q, want spaces", client.configureBackend)
	}
	if !containsSubstring(string(client.configureJSON), "bmw-iac-state") {
		t.Errorf("Configure JSON %q missing bucket name", string(client.configureJSON))
	}

	states, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(states) != 1 || states[0].ProviderID != "vpc-123" {
		t.Fatalf("states = %+v, want vpc-123 record returned by fake plugin", states)
	}
}

// TestResolvePluginStore_NoAdvertisingPlugin returns a clear error
// naming the plugin directory so operators know where to drop the
// plugin binary.
func TestResolvePluginStore_NoAdvertisingPlugin(t *testing.T) {
	pluginDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(pluginDir, "irrelevant"), 0o750); err != nil {
		t.Fatal(err)
	}
	orig := loadPluginStateBackendClients
	loadPluginStateBackendClients = func(_ *external.ExternalPluginManager, _, _ string) (map[string]pb.IaCStateBackendClient, error) {
		// Every candidate returns an empty map → "no plugin advertises this backend".
		return map[string]pb.IaCStateBackendClient{}, nil
	}
	t.Cleanup(func() { loadPluginStateBackendClients = orig })

	_, err := resolvePluginStore(context.Background(), "spaces", map[string]any{}, pluginDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsSubstring(err.Error(), pluginDir) {
		t.Errorf("error %q does not name pluginDir %q", err.Error(), pluginDir)
	}
	if !containsSubstring(err.Error(), "spaces") {
		t.Errorf("error %q does not name backend 'spaces'", err.Error())
	}
}

// containsSubstring is a tiny helper used by the plugin tests so we
// don't pull in the strings package alongside the tests' minimal
// import set.
func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
