package actors

import (
	"testing"
)

func TestParseActorWorkflowConfig(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"order-processors": map[string]any{
				"receive": map[string]any{
					"OrderPlaced": map[string]any{
						"description": "Process a new order",
						"steps": []any{
							map[string]any{
								"name": "set-status",
								"type": "step.set",
								"config": map[string]any{
									"values": map[string]any{
										"status": "processing",
									},
								},
							},
						},
					},
					"GetStatus": map[string]any{
						"steps": []any{
							map[string]any{
								"name": "respond",
								"type": "step.set",
								"config": map[string]any{
									"values": map[string]any{
										"status": "{{ .state.status }}",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	poolHandlers, err := parseActorWorkflowConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handlers, ok := poolHandlers["order-processors"]
	if !ok {
		t.Fatal("expected handlers for 'order-processors'")
	}

	if len(handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(handlers))
	}

	orderHandler, ok := handlers["OrderPlaced"]
	if !ok {
		t.Fatal("expected OrderPlaced handler")
	}
	if orderHandler.Description != "Process a new order" {
		t.Errorf("expected description 'Process a new order', got %q", orderHandler.Description)
	}
	if len(orderHandler.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(orderHandler.Steps))
	}
}

func TestParseActorWorkflowConfig_MissingPools(t *testing.T) {
	_, err := parseActorWorkflowConfig(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pools")
	}
}

func TestParseActorWorkflowConfig_MissingReceive(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"my-pool": map[string]any{},
		},
	}
	_, err := parseActorWorkflowConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing receive")
	}
}

func TestParseActorWorkflowConfig_EmptySteps(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"my-pool": map[string]any{
				"receive": map[string]any{
					"MyMessage": map[string]any{
						"steps": []any{},
					},
				},
			},
		},
	}
	_, err := parseActorWorkflowConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}
