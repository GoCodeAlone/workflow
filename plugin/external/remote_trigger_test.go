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
		"pool": "private",
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
	if stub.lastCreateTriggerReq.Type != "trigger.test" || stub.lastCreateTriggerReq.Name != "test-trigger" {
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

func TestRemoteTriggerConfigureRejectsSecondConfigure(t *testing.T) {
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})

	if err := trigger.Configure(nil, map[string]any{"pool": "private"}); err != nil {
		t.Fatalf("first Configure returned error: %v", err)
	}
	if err := trigger.Configure(nil, map[string]any{"pool": "other"}); err == nil {
		t.Fatal("expected second Configure to fail")
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
	if trigger.handleID == "" {
		t.Fatal("expected retry Configure to create trigger handle")
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start after retry Configure returned error: %v", err)
	}
}
