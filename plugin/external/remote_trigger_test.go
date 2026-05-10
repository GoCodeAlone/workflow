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

func TestRemoteTriggerInitFailsBeforeConfigure(t *testing.T) {
	trigger := NewRemoteTrigger("trigger.test", "test-trigger", &stubPluginServiceClient{})

	if err := trigger.Init(nil); err == nil {
		t.Fatal("expected Init to fail before Configure")
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
