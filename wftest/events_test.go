package wftest_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestHarness_FireEvent_BasicPipeline(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  on-user-created:
    trigger:
      type: eventbus
      config:
        topic: user.created
    steps:
      - name: log_event
        type: step.set
        config:
          values:
            handled: true
            user_id: "{{ .user_id }}"
`))

	result := h.FireEvent("user.created", map[string]any{"user_id": "123"})
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["handled"] != true {
		t.Errorf("expected handled=true, got %v", result.Output["handled"])
	}
	if result.Output["user_id"] != "123" {
		t.Errorf("expected user_id='123', got %v", result.Output["user_id"])
	}
}

func TestHarness_FireEvent_NoSubscriber(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  noop:
    steps:
      - name: s
        type: step.set
        config:
          values:
            x: 1
`))

	// Publishing to a topic with no subscriber should not error.
	result := h.FireEvent("unsubscribed.topic", map[string]any{"key": "val"})
	if result.Error != nil {
		t.Errorf("unexpected error publishing to unsubscribed topic: %v", result.Error)
	}
}

func TestHarness_FireSchedule_DirectPipeline(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  daily-job:
    steps:
      - name: run
        type: step.set
        config:
          values:
            executed: true
`))

	result := h.FireSchedule("daily-job", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["executed"] != true {
		t.Errorf("expected executed=true, got %v", result.Output["executed"])
	}
}

func TestHarness_FireSchedule_WithParams(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  parameterized:
    steps:
      - name: echo
        type: step.set
        config:
          values:
            got: "{{ .input }}"
`))

	result := h.FireSchedule("parameterized", map[string]any{"input": "hello"})
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["got"] != "hello" {
		t.Errorf("expected got='hello', got %v", result.Output["got"])
	}
}

func TestHarness_InjectTrigger_CustomAdapter(t *testing.T) {
	const adapterName = "test-custom-trigger"

	wftest.RegisterTriggerAdapter(&wftest.TriggerAdapterFunc{
		AdapterName: adapterName,
		Fn: func(ctx context.Context, h *wftest.Harness, event string, data map[string]any) (*wftest.Result, error) {
			// Adapter fires the pipeline directly via FireSchedule.
			return h.FireSchedule("custom-pipeline", data), nil
		},
	})
	t.Cleanup(func() { wftest.UnregisterTriggerAdapter(adapterName) })

	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  custom-pipeline:
    steps:
      - name: s
        type: step.set
        config:
          values:
            triggered: true
`))

	result := h.InjectTrigger(adapterName, "fire", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["triggered"] != true {
		t.Errorf("expected triggered=true, got %v", result.Output["triggered"])
	}
}
