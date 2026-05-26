package external

import (
	"context"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestRemoteTriggerConfigureCreatesTriggerWithConfig(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	err := trigger.Configure(nil, map[string]any{
		"workflowType": "pipeline:test-trigger",
		"pool":         "private",
		"limit": map[string]any{
			"concurrency": float64(2),
		},
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if stub.lastCreateTriggerReq == nil {
		t.Fatal("expected CreateTrigger request")
	}
	if stub.lastCreateTriggerReq.Type != "trigger.test" || stub.lastCreateTriggerReq.Name != "pipeline:test-trigger" {
		t.Fatalf("unexpected CreateTrigger request: %#v", stub.lastCreateTriggerReq)
	}
	got := stub.lastCreateTriggerReq.Config.AsMap()
	if got["pool"] != "private" {
		t.Fatalf("expected config pool to be forwarded, got %#v", got)
	}

	if err := trigger.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := trigger.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := trigger.Destroy(); err != nil {
		t.Fatalf("Destroy returned error: %v", err)
	}
	if stub.initModuleCalls != 1 || stub.startModuleCalls != 1 || stub.stopModuleCalls != 1 || stub.destroyModuleCalls != 1 {
		t.Fatalf("unexpected lifecycle calls: init=%d start=%d stop=%d destroy=%d",
			stub.initModuleCalls, stub.startModuleCalls, stub.stopModuleCalls, stub.destroyModuleCalls)
	}
}

func TestRemoteTriggerLifecycleNoopsBeforeConfigure(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Init(nil); err != nil {
		t.Fatalf("Init returned error before Configure: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error before Configure: %v", err)
	}
	if stub.initModuleCalls != 0 || stub.startModuleCalls != 0 {
		t.Fatalf("unexpected remote lifecycle calls before Configure: init=%d start=%d", stub.initModuleCalls, stub.startModuleCalls)
	}
}

func TestRemoteTriggerConfigureRejectsInvalidConfigShape(t *testing.T) {
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})

	if err := trigger.Configure(nil, []string{"not", "a", "map"}); err == nil {
		t.Fatal("expected Configure to reject non-map config")
	}
}

func TestRemoteTriggerConfigureAllowsMultiplePipelines(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:first", "pool": "private"}); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:second", "pool": "other"}); err != nil {
		t.Fatalf("second Configure returned error: %v", err)
	}
	if len(trigger.handleIDs) != 2 {
		t.Fatalf("expected two trigger handles, got %d", len(trigger.handleIDs))
	}
	if stub.lastCreateTriggerReq.Name != "pipeline:second" {
		t.Fatalf("expected second workflow type to be forwarded, got %#v", stub.lastCreateTriggerReq)
	}
	if err := trigger.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := trigger.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := trigger.Destroy(); err != nil {
		t.Fatalf("Destroy returned error: %v", err)
	}
	if stub.initModuleCalls != 2 || stub.startModuleCalls != 2 || stub.stopModuleCalls != 2 || stub.destroyModuleCalls != 2 {
		t.Fatalf("expected lifecycle to fan out to two handles, got init=%d start=%d stop=%d destroy=%d",
			stub.initModuleCalls, stub.startModuleCalls, stub.stopModuleCalls, stub.destroyModuleCalls)
	}
	if len(trigger.handleIDs) != 0 || len(trigger.workflowTypes) != 0 {
		t.Fatalf("expected Destroy to clear local handles, got handles=%v workflowTypes=%v", trigger.handleIDs, trigger.workflowTypes)
	}
	if err := trigger.Destroy(); err != nil {
		t.Fatalf("second Destroy returned error: %v", err)
	}
	if stub.destroyModuleCalls != 2 {
		t.Fatalf("expected second Destroy to no-op, got destroy calls=%d", stub.destroyModuleCalls)
	}
}

func TestRemoteTriggerConfigureDuplicateWorkflowTypeIsIdempotent(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)
	cfg := map[string]any{
		"workflowType": "pipeline:first",
		"limit": map[string]any{
			"concurrency": 2,
		},
	}

	if err := trigger.Configure(nil, cfg); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	cfg["limit"].(map[string]any)["concurrency"] = 99
	if err := trigger.Configure(nil, map[string]any{
		"workflowType": "pipeline:first",
		"limit": map[string]any{
			"concurrency": float64(2),
		},
	}); err != nil {
		t.Fatalf("duplicate Configure returned error: %v", err)
	}
	if stub.createTriggerCalls != 1 || len(trigger.handleIDs) != 1 {
		t.Fatalf("expected duplicate Configure to reuse handle, create calls=%d handles=%v", stub.createTriggerCalls, trigger.handleIDs)
	}
	if err := trigger.Configure(nil, map[string]any{
		"workflowType": "pipeline:first",
		"limit": map[string]any{
			"concurrency": 3,
		},
	}); err == nil {
		t.Fatal("expected conflicting duplicate workflowType to fail")
	}
	if err := trigger.Configure(nil, map[string]any{
		"workflowType": "pipeline:first",
		"limit": map[string]any{
			"concurrency": float64(2),
		},
	}); err != nil {
		t.Fatalf("matching duplicate should clear same-workflow configure error: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start after same-workflow retry returned error: %v", err)
	}
	if stub.createTriggerCalls != 1 {
		t.Fatalf("expected conflicting duplicate to fail before CreateTrigger, calls=%d", stub.createTriggerCalls)
	}
}

func TestRemoteTriggerConfigureRejectsInvalidWorkflowType(t *testing.T) {
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})
	for _, cfg := range []map[string]any{
		{"workflowType": " pipeline:first"},
		{"workflowType": "pipeline:first "},
		{"workflowType": "pipeline:\tfirst"},
		{"workflowType": 123},
	} {
		if err := trigger.Configure(nil, cfg); err == nil {
			t.Fatalf("expected Configure to reject workflowType in %#v", cfg)
		}
	}
}

func TestRemoteTriggerConfigureValidRetryClearsPreWorkflowError(t *testing.T) {
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})

	if err := trigger.Configure(nil, []string{"not", "a", "map"}); err == nil {
		t.Fatal("expected invalid config shape to fail")
	}
	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:first"}); err != nil {
		t.Fatalf("valid Configure after shape error returned error: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start after valid retry returned error: %v", err)
	}

	trigger = NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})
	if err := trigger.Configure(nil, map[string]any{"workflowType": 123}); err == nil {
		t.Fatal("expected invalid workflowType to fail")
	}
	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:first"}); err != nil {
		t.Fatalf("valid Configure after workflowType error returned error: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start after workflowType retry returned error: %v", err)
	}
}

func TestRemoteTriggerPartialRetryDoesNotDuplicateExistingHandle(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)
	firstCfg := map[string]any{"workflowType": "pipeline:first"}
	secondCfg := map[string]any{"workflowType": "pipeline:second"}

	if err := trigger.Configure(nil, firstCfg); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	stub.createTriggerResp = &pb.HandleResponse{Error: "bad trigger config"}
	if err := trigger.Configure(nil, secondCfg); err == nil {
		t.Fatal("expected second Configure to return plugin error")
	}
	if err := trigger.Configure(nil, firstCfg); err != nil {
		t.Fatalf("idempotent retry of first Configure returned error: %v", err)
	}
	stub.createTriggerResp = nil
	if err := trigger.Configure(nil, secondCfg); err != nil {
		t.Fatalf("retry second Configure returned error: %v", err)
	}
	if stub.createTriggerCalls != 3 || len(trigger.handleIDs) != 2 {
		t.Fatalf("expected no duplicate first handle, create calls=%d handles=%v", stub.createTriggerCalls, trigger.handleIDs)
	}
}

func TestRemoteTriggerLifecycleFailsAfterPartialConfigureError(t *testing.T) {
	stub := &stubPluginServiceClient{}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:first"}); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	stub.createTriggerResp = &pb.HandleResponse{Error: "bad trigger config"}
	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:second"}); err == nil {
		t.Fatal("expected second Configure to return plugin error")
	}
	if err := trigger.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail after partial Configure error")
	}
	if stub.startModuleCalls != 0 {
		t.Fatalf("expected Start not to run configured handles after partial Configure error, got %d", stub.startModuleCalls)
	}
}

func TestRemoteTriggerStartRollsBackStartedHandles(t *testing.T) {
	stub := &stubPluginServiceClient{startErrorOnCall: 2}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:first"}); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	if err := trigger.Configure(nil, map[string]any{"workflowType": "pipeline:second"}); err != nil {
		t.Fatalf("second Configure returned error: %v", err)
	}
	if err := trigger.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail")
	}
	if stub.startModuleCalls != 2 {
		t.Fatalf("expected two Start attempts, got %d", stub.startModuleCalls)
	}
	if stub.stopModuleCalls != 1 {
		t.Fatalf("expected rollback Stop for first handle, got %d", stub.stopModuleCalls)
	}
}

func TestRemoteTriggerConfigurePropagatesPluginError(t *testing.T) {
	stub := &stubPluginServiceClient{
		createTriggerResp: &pb.HandleResponse{Error: "bad trigger config"},
	}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err == nil {
		t.Fatal("expected Configure to return plugin error")
	}
}

func TestRemoteTriggerConfigureRejectsNilCreateResponse(t *testing.T) {
	stub := &stubPluginServiceClient{createTriggerNilResp: true}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err == nil {
		t.Fatal("expected Configure to reject nil CreateTrigger response")
	}
	if err := trigger.Init(nil); err == nil {
		t.Fatal("expected Init to fail after nil CreateTrigger response")
	}
	if err := trigger.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail after nil CreateTrigger response")
	}
	if stub.initModuleCalls != 0 || stub.startModuleCalls != 0 {
		t.Fatalf("unexpected remote lifecycle calls after nil CreateTrigger response: init=%d start=%d", stub.initModuleCalls, stub.startModuleCalls)
	}
}

func TestRemoteTriggerConfigureRejectsEmptyHandle(t *testing.T) {
	stub := &stubPluginServiceClient{createTriggerResp: &pb.HandleResponse{}}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err == nil {
		t.Fatal("expected Configure to reject empty CreateTrigger handle")
	}
	if err := trigger.Init(nil); err == nil {
		t.Fatal("expected Init to fail after empty CreateTrigger handle")
	}
	if err := trigger.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail after empty CreateTrigger handle")
	}
	if stub.initModuleCalls != 0 || stub.startModuleCalls != 0 {
		t.Fatalf("unexpected remote lifecycle calls after empty CreateTrigger handle: init=%d start=%d", stub.initModuleCalls, stub.startModuleCalls)
	}
}

func TestRemoteTriggerLifecycleFailsAfterConfigureError(t *testing.T) {
	stub := &stubPluginServiceClient{
		createTriggerResp: &pb.HandleResponse{Error: "bad trigger config"},
	}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err == nil {
		t.Fatal("expected Configure to return plugin error")
	}
	if err := trigger.Init(nil); err == nil {
		t.Fatal("expected Init to fail after Configure error")
	}
	if err := trigger.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail after Configure error")
	}
	if stub.initModuleCalls != 0 || stub.startModuleCalls != 0 {
		t.Fatalf("unexpected remote lifecycle calls after Configure error: init=%d start=%d", stub.initModuleCalls, stub.startModuleCalls)
	}
}

func TestRemoteTriggerConfigureCanRetryAfterError(t *testing.T) {
	stub := &stubPluginServiceClient{
		createTriggerResp: &pb.HandleResponse{Error: "bad trigger config"},
	}
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", stub)

	if err := trigger.Configure(nil, map[string]any{"pool": "bad"}); err == nil {
		t.Fatal("expected first Configure to return plugin error")
	}

	stub.createTriggerResp = nil
	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err != nil {
		t.Fatalf("second Configure returned error: %v", err)
	}
	if len(trigger.handleIDs) != 1 {
		t.Fatal("expected retry Configure to create trigger handle")
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start after retry Configure returned error: %v", err)
	}
}
