package external

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
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
// The plugin handle is allocated after Configure receives YAML trigger config.
func NewRemoteTrigger(typeName, name string, client pb.PluginServiceClient) *RemoteTrigger {
	return &RemoteTrigger{
		typeName: typeName,
		name:     name,
		client:   client,
	}
}

// --- modular.Module ---

func (t *RemoteTrigger) Name() string {
	return t.name
}

func (t *RemoteTrigger) Init(_ modular.Application) error {
	if t.handleID == "" {
		return fmt.Errorf("remote trigger init: trigger %q is not configured", t.name)
	}
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
	if t.handleID == "" {
		return fmt.Errorf("remote trigger start: trigger %q is not configured", t.name)
	}
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
	if t.handleID == "" {
		return nil
	}
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

// Configure applies YAML trigger config and creates the remote trigger handle.
func (t *RemoteTrigger) Configure(_ modular.Application, triggerConfig any) error {
	if t.handleID != "" {
		return fmt.Errorf("remote trigger configure %s: already configured", t.name)
	}
	cfg, err := triggerConfigMap(triggerConfig)
	if err != nil {
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	pbConfig, err := mapToStruct(cfg)
	if err != nil {
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	resp, err := t.client.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type:   t.typeName,
		Name:   t.name,
		Config: pbConfig,
	})
	if err != nil {
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote trigger configure %s: %s", t.name, resp.Error)
	}
	t.handleID = resp.HandleId
	t.config = cfg
	return nil
}

// Destroy releases the remote trigger resources in the plugin process.
func (t *RemoteTrigger) Destroy() error {
	if t.handleID == "" {
		return nil
	}
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

func triggerConfigMap(config any) (map[string]any, error) {
	if config == nil {
		return nil, nil
	}
	cfg, ok := config.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config must be map[string]any, got %T", config)
	}
	return cfg, nil
}
