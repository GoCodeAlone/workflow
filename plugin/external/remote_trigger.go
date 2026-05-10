package external

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	goproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const globalConfigureErrorKey = "\x00global"

// RemoteTrigger implements module.Trigger by proxying to a plugin trigger type.
// Trigger lifecycle (Start/Stop) is managed through the plugin's module RPCs,
// treating trigger handles as module handles.
type RemoteTrigger struct {
	typeName              string
	name                  string
	handleIDs             []string
	workflowTypes         []string
	client                pb.PluginServiceClient
	handlesByWorkflowType map[string]string
	configsByWorkflowType map[string]string
	configureErrors       map[string]error
	configureErr          error
}

// NewRemoteTrigger creates a remote trigger proxy.
// The plugin handle is allocated after Configure receives YAML trigger config.
func NewRemoteTrigger(typeName, name string, client pb.PluginServiceClient) *RemoteTrigger {
	return &RemoteTrigger{
		typeName:              typeName,
		name:                  name,
		client:                client,
		handlesByWorkflowType: make(map[string]string),
		configsByWorkflowType: make(map[string]string),
		configureErrors:       make(map[string]error),
	}
}

// --- modular.Module ---

func (t *RemoteTrigger) Name() string {
	return t.name
}

func (t *RemoteTrigger) Init(_ modular.Application) error {
	if t.configureErr != nil {
		return fmt.Errorf("remote trigger init: trigger %q configure failed: %w", t.name, t.configureErr)
	}
	if len(t.handleIDs) == 0 {
		return nil
	}
	for _, handleID := range t.handleIDs {
		resp, err := t.client.InitModule(context.Background(), &pb.HandleRequest{
			HandleId: handleID,
		})
		if err != nil {
			return fmt.Errorf("remote trigger init %s: %w", handleID, err)
		}
		if resp == nil {
			return fmt.Errorf("remote trigger init %s: empty response", handleID)
		}
		if resp.Error != "" {
			return fmt.Errorf("remote trigger init %s: %s", handleID, resp.Error)
		}
	}
	return nil
}

// --- modular.Startable ---

func (t *RemoteTrigger) Start(ctx context.Context) error {
	if t.configureErr != nil {
		return fmt.Errorf("remote trigger start: trigger %q configure failed: %w", t.name, t.configureErr)
	}
	if len(t.handleIDs) == 0 {
		return nil
	}
	started := make([]string, 0, len(t.handleIDs))
	for _, handleID := range t.handleIDs {
		resp, err := t.client.StartModule(ctx, &pb.HandleRequest{
			HandleId: handleID,
		})
		if err != nil {
			return errors.Join(fmt.Errorf("remote trigger start %s: %w", handleID, err), t.stopStarted(ctx, started))
		}
		if resp == nil {
			return errors.Join(fmt.Errorf("remote trigger start %s: empty response", handleID), t.stopStarted(ctx, started))
		}
		if resp.Error != "" {
			return errors.Join(fmt.Errorf("remote trigger start %s: %s", handleID, resp.Error), t.stopStarted(ctx, started))
		}
		started = append(started, handleID)
	}
	return nil
}

// --- modular.Stoppable ---

func (t *RemoteTrigger) Stop(ctx context.Context) error {
	if len(t.handleIDs) == 0 {
		return nil
	}
	return t.stopHandles(ctx, t.handleIDs)
}

func (t *RemoteTrigger) stopStarted(ctx context.Context, handleIDs []string) error {
	if len(handleIDs) == 0 {
		return nil
	}
	reversed := make([]string, len(handleIDs))
	for i := range handleIDs {
		reversed[i] = handleIDs[len(handleIDs)-1-i]
	}
	return t.stopHandles(ctx, reversed)
}

func (t *RemoteTrigger) stopHandles(ctx context.Context, handleIDs []string) error {
	var errs []error
	for _, handleID := range handleIDs {
		resp, err := t.client.StopModule(ctx, &pb.HandleRequest{
			HandleId: handleID,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("remote trigger stop %s: %w", handleID, err))
			continue
		}
		if resp == nil {
			errs = append(errs, fmt.Errorf("remote trigger stop %s: empty response", handleID))
			continue
		}
		if resp.Error != "" {
			errs = append(errs, fmt.Errorf("remote trigger stop %s: %s", handleID, resp.Error))
		}
	}
	return errors.Join(errs...)
}

// --- module.Trigger ---

// Configure applies YAML trigger config and creates the remote trigger handle.
func (t *RemoteTrigger) Configure(_ modular.Application, triggerConfig any) error {
	cfg, err := triggerConfigMap(triggerConfig)
	if err != nil {
		t.setConfigureError(globalConfigureErrorKey, err)
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	pbConfig, err := mapToStruct(cfg)
	if err != nil {
		t.setConfigureError(globalConfigureErrorKey, err)
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	workflowType, err := t.workflowType(cfg)
	if err != nil {
		t.setConfigureError(globalConfigureErrorKey, err)
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	configFingerprint, err := triggerConfigFingerprint(pbConfig)
	if err != nil {
		t.setConfigureError(globalConfigureErrorKey, err)
		return fmt.Errorf("remote trigger configure %s: %w", t.name, err)
	}
	if _, ok := t.handlesByWorkflowType[workflowType]; ok {
		if t.configsByWorkflowType[workflowType] != configFingerprint {
			err := fmt.Errorf("already configured with different trigger config")
			t.setConfigureError(workflowType, err)
			return fmt.Errorf("remote trigger configure %s: workflow %q: %w", t.name, workflowType, err)
		}
		t.clearConfigureError(globalConfigureErrorKey)
		t.clearConfigureError(workflowType)
		return nil
	}
	resp, err := t.client.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type:   t.typeName,
		Name:   workflowType,
		Config: pbConfig,
	})
	if err != nil {
		t.setConfigureError(workflowType, err)
		return fmt.Errorf("remote trigger configure %s: workflow %q: %w", t.name, workflowType, err)
	}
	if resp == nil {
		err := errors.New("empty create trigger response")
		t.setConfigureError(workflowType, err)
		return fmt.Errorf("remote trigger configure %s: workflow %q: %w", t.name, workflowType, err)
	}
	if resp.Error != "" {
		err := fmt.Errorf("%s", resp.Error)
		t.setConfigureError(workflowType, err)
		return fmt.Errorf("remote trigger configure %s: workflow %q: %w", t.name, workflowType, err)
	}
	if resp.HandleId == "" {
		err := errors.New("empty create trigger handle")
		t.setConfigureError(workflowType, err)
		return fmt.Errorf("remote trigger configure %s: workflow %q: %w", t.name, workflowType, err)
	}
	t.handleIDs = append(t.handleIDs, resp.HandleId)
	t.workflowTypes = append(t.workflowTypes, workflowType)
	t.handlesByWorkflowType[workflowType] = resp.HandleId
	t.configsByWorkflowType[workflowType] = configFingerprint
	t.clearConfigureError(globalConfigureErrorKey)
	t.clearConfigureError(workflowType)
	return nil
}

// Destroy releases the remote trigger resources in the plugin process.
func (t *RemoteTrigger) Destroy() error {
	if len(t.handleIDs) == 0 {
		return nil
	}
	var errs []error
	var destroyed []string
	for _, workflowType := range t.workflowTypes {
		handleID := t.handlesByWorkflowType[workflowType]
		resp, err := t.client.DestroyModule(context.Background(), &pb.HandleRequest{
			HandleId: handleID,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("remote trigger destroy %s: %w", handleID, err))
			continue
		}
		if resp == nil {
			errs = append(errs, fmt.Errorf("remote trigger destroy %s: empty response", handleID))
			continue
		}
		if resp.Error != "" {
			errs = append(errs, fmt.Errorf("remote trigger destroy %s: %s", handleID, resp.Error))
			continue
		}
		destroyed = append(destroyed, workflowType)
	}
	for _, workflowType := range destroyed {
		t.removeWorkflowType(workflowType)
	}
	if len(t.workflowTypes) == 0 {
		clear(t.configureErrors)
		t.configureErr = nil
	}
	return errors.Join(errs...)
}

func (t *RemoteTrigger) workflowType(cfg map[string]any) (string, error) {
	workflowType := t.name
	if configuredValue, ok := cfg["workflowType"]; ok {
		configuredWorkflowType, ok := configuredValue.(string)
		if !ok {
			return "", fmt.Errorf("workflowType must be string, got %T", configuredValue)
		}
		trimmed := strings.TrimSpace(configuredWorkflowType)
		if configuredWorkflowType != trimmed {
			return "", fmt.Errorf("workflowType %q contains leading or trailing whitespace", configuredWorkflowType)
		}
		workflowType = trimmed
	}
	if workflowType == "" {
		return "", errors.New("workflowType must not be empty")
	}
	if strings.ContainsFunc(workflowType, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) {
		return "", fmt.Errorf("workflowType %q contains whitespace or control characters", workflowType)
	}
	return workflowType, nil
}

func triggerConfigFingerprint(cfg *structpb.Struct) (string, error) {
	if cfg == nil {
		return "", nil
	}
	data, err := (goproto.MarshalOptions{Deterministic: true}).Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal trigger config fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (t *RemoteTrigger) removeWorkflowType(workflowType string) {
	handleID := t.handlesByWorkflowType[workflowType]
	delete(t.handlesByWorkflowType, workflowType)
	delete(t.configsByWorkflowType, workflowType)
	delete(t.configureErrors, workflowType)
	t.workflowTypes = removeString(t.workflowTypes, workflowType)
	t.handleIDs = removeString(t.handleIDs, handleID)
	t.refreshConfigureErr()
}

func (t *RemoteTrigger) setConfigureError(workflowType string, err error) {
	t.configureErrors[workflowType] = err
	t.refreshConfigureErr()
}

func (t *RemoteTrigger) clearConfigureError(workflowType string) {
	delete(t.configureErrors, workflowType)
	t.refreshConfigureErr()
}

func (t *RemoteTrigger) refreshConfigureErr() {
	if len(t.configureErrors) == 0 {
		t.configureErr = nil
		return
	}
	errs := make([]error, 0, len(t.configureErrors))
	for workflowType, err := range t.configureErrors {
		if workflowType == globalConfigureErrorKey {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, fmt.Errorf("workflow %q: %w", workflowType, err))
	}
	t.configureErr = errors.Join(errs...)
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
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
