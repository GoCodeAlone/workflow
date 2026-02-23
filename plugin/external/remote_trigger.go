package external

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// RemoteTrigger implements module.Trigger by proxying to a plugin trigger type.
// Trigger lifecycle (Start/Stop) is managed through the plugin's module RPCs,
// treating trigger handles as module handles.
type RemoteTrigger struct {
	typeName string
	name     string
	handleID string
	client   pb.PluginServiceClient
	config   map[string]any
}

// NewRemoteTrigger creates a remote trigger proxy.
// The handleID is allocated by the plugin when the trigger module is created.
func NewRemoteTrigger(typeName, name, handleID string, client pb.PluginServiceClient, config map[string]any) *RemoteTrigger {
	return &RemoteTrigger{
		typeName: typeName,
		name:     name,
		handleID: handleID,
		client:   client,
		config:   config,
	}
}

// --- modular.Module ---

func (t *RemoteTrigger) Name() string {
	return t.name
}

func (t *RemoteTrigger) Init(_ modular.Application) error {
	resp, err := t.client.InitModule(context.Background(), &pb.HandleRequest{
		HandleId: t.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote trigger init: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote trigger init: %s", resp.Error)
	}
	return nil
}

// --- modular.Startable ---

func (t *RemoteTrigger) Start(ctx context.Context) error {
	resp, err := t.client.StartModule(ctx, &pb.HandleRequest{
		HandleId: t.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote trigger start: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote trigger start: %s", resp.Error)
	}
	return nil
}

// --- modular.Stoppable ---

func (t *RemoteTrigger) Stop(ctx context.Context) error {
	resp, err := t.client.StopModule(ctx, &pb.HandleRequest{
		HandleId: t.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote trigger stop: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote trigger stop: %s", resp.Error)
	}
	return nil
}

// --- module.Trigger ---

// Configure applies the trigger configuration. The config was already applied at
// creation time (CreateModule); this is a no-op for remote triggers unless the
// plugin exposes a dedicated configure step.
func (t *RemoteTrigger) Configure(_ modular.Application, _ any) error {
	return nil
}

// Destroy releases the remote trigger resources in the plugin process.
func (t *RemoteTrigger) Destroy() error {
	resp, err := t.client.DestroyModule(context.Background(), &pb.HandleRequest{
		HandleId: t.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote trigger destroy: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote trigger destroy: %s", resp.Error)
	}
	return nil
}
