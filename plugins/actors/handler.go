package actors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
)

// ActorWorkflowHandler handles the "actors" workflow type.
// It parses receive handler configs and wires them to actor pool modules.
type ActorWorkflowHandler struct {
	// poolHandlers maps pool name -> message type -> handler pipeline
	poolHandlers map[string]map[string]*HandlerPipeline
	logger       *slog.Logger
}

// NewActorWorkflowHandler creates a new actor workflow handler.
func NewActorWorkflowHandler() *ActorWorkflowHandler {
	return &ActorWorkflowHandler{
		poolHandlers: make(map[string]map[string]*HandlerPipeline),
	}
}

// CanHandle returns true for "actors" workflow type.
func (h *ActorWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "actors"
}

// ConfigureWorkflow parses the actors workflow config.
func (h *ActorWorkflowHandler) ConfigureWorkflow(_ modular.Application, workflowConfig any) error {
	cfg, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("actor workflow handler: config must be a map")
	}

	poolHandlers, err := parseActorWorkflowConfig(cfg)
	if err != nil {
		return fmt.Errorf("actor workflow handler: %w", err)
	}

	h.poolHandlers = poolHandlers
	return nil
}

// ExecuteWorkflow is not used directly — actors receive messages via step.actor_send/ask.
func (h *ActorWorkflowHandler) ExecuteWorkflow(_ context.Context, _ string, _ string, _ map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("actor workflows are message-driven; use step.actor_send or step.actor_ask to send messages")
}

// PoolHandlers returns the parsed handlers for wiring to actor pools.
func (h *ActorWorkflowHandler) PoolHandlers() map[string]map[string]*HandlerPipeline {
	return h.poolHandlers
}

// SetLogger sets the logger.
func (h *ActorWorkflowHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// parseActorWorkflowConfig parses the workflows.actors config block.
func parseActorWorkflowConfig(cfg map[string]any) (map[string]map[string]*HandlerPipeline, error) {
	poolsCfg, ok := cfg["pools"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("'pools' map is required")
	}

	result := make(map[string]map[string]*HandlerPipeline)

	for poolName, poolRaw := range poolsCfg {
		poolCfg, ok := poolRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pool %q: config must be a map", poolName)
		}

		receiveCfg, ok := poolCfg["receive"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pool %q: 'receive' map is required", poolName)
		}

		handlers := make(map[string]*HandlerPipeline)
		for msgType, handlerRaw := range receiveCfg {
			handlerCfg, ok := handlerRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pool %q handler %q: config must be a map", poolName, msgType)
			}

			stepsRaw, ok := handlerCfg["steps"].([]any)
			if !ok || len(stepsRaw) == 0 {
				return nil, fmt.Errorf("pool %q handler %q: 'steps' list is required and must not be empty", poolName, msgType)
			}

			steps := make([]map[string]any, 0, len(stepsRaw))
			for i, stepRaw := range stepsRaw {
				stepCfg, ok := stepRaw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("pool %q handler %q step %d: must be a map", poolName, msgType, i)
				}
				steps = append(steps, stepCfg)
			}

			description, _ := handlerCfg["description"].(string)
			handlers[msgType] = &HandlerPipeline{
				Description: description,
				Steps:       steps,
			}
		}

		result[poolName] = handlers
	}

	return result, nil
}
