package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/secrets"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/structpb"
)

// RemoteStep implements module.PipelineStep by delegating to a gRPC plugin.
type RemoteStep struct {
	name        string
	handleID    string
	config      map[string]any
	client      pb.PluginServiceClient
	contract    *pb.ContractDescriptor
	types       protoregistry.MessageTypeResolver
	tmpl        *module.TemplateEngine
	credentials *interfaces.BoundStepCredentials
}

var errStepCredentialExecution = errors.New("remote step credential execution rejected")

// NewRemoteStep creates a remote step proxy.
// config holds the raw (possibly template-containing) step configuration that
// will be resolved against the live pipeline context on each Execute call.
func NewRemoteStep(name, handleID string, client pb.PluginServiceClient, config map[string]any, contracts ...*pb.ContractDescriptor) *RemoteStep {
	var contract *pb.ContractDescriptor
	if len(contracts) > 0 {
		contract = contracts[0]
	}
	return NewRemoteStepWithContractTypes(name, handleID, client, config, contract, nil)
}

func NewRemoteStepWithContractTypes(name, handleID string, client pb.PluginServiceClient, config map[string]any, contract *pb.ContractDescriptor, types protoregistry.MessageTypeResolver) *RemoteStep {
	return &RemoteStep{
		name:     name,
		handleID: handleID,
		config:   config,
		client:   client,
		contract: contract,
		types:    types,
		tmpl:     module.NewTemplateEngine(),
	}
}

func (s *RemoteStep) Name() string {
	return s.name
}

func (s *RemoteStep) Execute(ctx context.Context, pc *module.PipelineContext) (result *module.StepResult, err error) {
	var detector *secrets.Redactor
	var carrier map[string]any
	if s.credentials != nil {
		if ctx == nil || !stepCredentialContractAllowed(s.contract) {
			return nil, errStepCredentialExecution
		}
		if err := s.credentials.ValidateConfig(s.config); err != nil {
			return nil, errStepCredentialExecution
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, resolveErr := s.credentials.Resolve(ctx)
		if resolveErr != nil || len(resolved) == 0 {
			return nil, errStepCredentialExecution
		}
		detector = secrets.NewRedactor()
		carrier = make(map[string]any, len(resolved))
		for i, credential := range resolved {
			if !strings.HasPrefix(credential.Ref, "config:") || credential.Value == "" {
				return nil, errStepCredentialExecution
			}
			key := strings.TrimPrefix(credential.Ref, "config:")
			if key == "" {
				return nil, errStepCredentialExecution
			}
			if _, duplicate := carrier[key]; duplicate {
				return nil, errStepCredentialExecution
			}
			carrier[key] = credential.Value
			detector.AddValue(fmt.Sprintf("credential%d", i), credential.Value)
		}
		// Arm the guard before evaluating any ordinary templates or encoding data.
		defer func() {
			if err != nil && detector.ContainsValue(err.Error()) {
				result, err = nil, errStepCredentialExecution
			}
		}()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// Resolve template expressions in the step config against the current
	// pipeline context so that dynamic values (e.g. outputs of earlier steps)
	// are available to the plugin. When no config was provided, skip resolution
	// and leave resolvedConfig nil so the Config proto field is omitted.
	var resolvedConfig map[string]any
	if s.config != nil {
		var err error
		resolvedConfig, err = s.tmpl.ResolveMap(s.config, pc)
		if err != nil {
			return nil, fmt.Errorf("remote step %q (handle %s) config resolve: %w", s.name, s.handleID, err)
		}
	}
	if s.credentials != nil {
		if err := s.credentials.ValidateConfig(resolvedConfig); err != nil {
			return nil, errStepCredentialExecution
		}
		if _, collision := resolvedConfig["config"]; collision {
			return nil, errStepCredentialExecution
		}
		// ResolveMap creates a fresh map; the private namespace is never merged
		// with caller data or copied back into the step or pipeline context.
		resolvedConfig["config"] = carrier
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	// Convert step outputs to proto map
	stepOutputs := make(map[string]*structpb.Struct)
	for k, v := range pc.StepOutputs {
		out, err := mapToStruct(v)
		if err != nil {
			return nil, fmt.Errorf("remote step %q (handle %s) encode step output %q as Struct: %w", s.name, s.handleID, k, err)
		}
		stepOutputs[k] = out
	}

	req, err := s.executeRequest(pc, resolvedConfig, stepOutputs)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.ExecuteStep(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote step execute: %w", err)
	}
	if resp == nil {
		return nil, errors.New("remote step execute: empty response")
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote step execute: %s", resp.Error)
	}

	usesTypedOutput := s.contract != nil && s.contract.OutputMessage != "" && contractModeUsesTyped(s.contract.Mode)
	if usesTypedOutput && resp.TypedOutput == nil && s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		return nil, fmt.Errorf("remote step %q STRICT_PROTO output message %q requires typed_output", s.name, s.contract.OutputMessage)
	}

	output := structToMap(resp.Output)
	if usesTypedOutput && resp.TypedOutput != nil {
		output, err = typedAnyToMap(resp.TypedOutput, s.contract.OutputMessage, s.types)
		if err != nil {
			return nil, fmt.Errorf("remote step %q typed output decode: %w", s.name, err)
		}
	}
	if containsStepCredential(output, detector) {
		return nil, errStepCredentialExecution
	}

	return &module.StepResult{
		Output: output,
		Stop:   resp.StopPipeline,
	}, nil
}

func containsStepCredential(value any, detector *secrets.Redactor) bool {
	if detector == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return detector.ContainsValue(v)
	case map[string]any:
		for key, item := range v {
			if detector.ContainsValue(key) || containsStepCredential(item, detector) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsStepCredential(item, detector) {
				return true
			}
		}
	}
	return false
}

func (s *RemoteStep) executeRequest(pc *module.PipelineContext, resolvedConfig map[string]any, stepOutputs map[string]*structpb.Struct) (*pb.ExecuteStepRequest, error) {
	// trigger_data and metadata are always sent as Struct — there's no
	// typed alternative — so encode them up front.
	triggerData, err := mapToStruct(pc.TriggerData)
	if err != nil {
		return nil, fmt.Errorf("remote step %q (handle %s) encode trigger_data as Struct: %w", s.name, s.handleID, err)
	}
	metadata, err := mapToStruct(remotePluginMetadata(pc.Metadata))
	if err != nil {
		return nil, fmt.Errorf("remote step %q (handle %s) encode metadata as Struct: %w", s.name, s.handleID, err)
	}
	req := &pb.ExecuteStepRequest{
		HandleId:    s.handleID,
		TriggerData: triggerData,
		StepOutputs: stepOutputs,
		Metadata:    metadata,
	}
	// Current and Config are sent as Struct only when the contract is
	// UNSPECIFIED, LEGACY_STRUCT, or PROTO_WITH_LEGACY_STRUCT — STRICT_PROTO
	// nils them out and relies on TypedInput/TypedConfig instead. Defer
	// Struct encoding so values that JSON can marshal but Struct cannot
	// (e.g. time.Time fields targeting STRICT_PROTO typed payloads) don't
	// fail the request unnecessarily — Copilot review #555 finding.
	encodeLegacyStruct := s.contract == nil ||
		s.contract.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED ||
		s.contract.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT ||
		s.contract.Mode == pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT
	if encodeLegacyStruct {
		current, err := mapToStruct(pc.Current)
		if err != nil {
			return nil, fmt.Errorf("remote step %q (handle %s) encode current as Struct: %w", s.name, s.handleID, err)
		}
		configStruct, err := mapToStruct(resolvedConfig)
		if err != nil {
			return nil, fmt.Errorf("remote step %q (handle %s) encode config as Struct: %w", s.name, s.handleID, err)
		}
		req.Current = current
		req.Config = configStruct
	}
	if s.contract == nil || s.contract.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED {
		return req, nil
	}
	if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
		return req, nil
	}
	typedConfig, err := mapToTypedAny(s.contract.ConfigMessage, stripInternalKeys(resolvedConfig), s.types)
	if err != nil {
		if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			return nil, fmt.Errorf("remote step %q STRICT_PROTO config message %q cannot use legacy Struct fallback: %w", s.name, s.contract.ConfigMessage, err)
		}
		// PROTO_WITH_LEGACY_STRUCT: typed encoding failed, fall back to
		// the legacy Struct already populated above.
		return req, nil
	}
	typedInput, err := mapToTypedAnyKnownFields(s.contract.InputMessage, pc.Current, s.types)
	if err != nil {
		if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			return nil, fmt.Errorf("remote step %q STRICT_PROTO input message %q cannot use legacy Struct fallback: %w", s.name, s.contract.InputMessage, err)
		}
		return req, nil
	}
	req.TypedConfig = typedConfig
	req.TypedInput = typedInput
	// STRICT_PROTO drops legacy Struct payloads (already not encoded above
	// in the deferred path; this is a no-op safety net for any future
	// branch that ends up here with Current/Config non-nil).
	if s.contract.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		req.Config = nil
		req.Current = nil
	}
	return req, nil
}

func remotePluginMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	// Host request objects, raw bodies and credentials never cross this boundary,
	// even when they happen to be JSON-encodable.
	filtered := make(map[string]any)
	for _, key := range []string{"pipeline", "started_at", "execution_id", "tenant_id", "request_id", "trace_id"} {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		// Marshal only validates JSON safety (including cycles/non-finite
		// numbers). Keep the original value: NewValue rejects invalid UTF8
		// instead of accepting the JSON encoder's replacement characters.
		if _, err := json.Marshal(value); err != nil {
			continue
		}
		if _, err := structpb.NewValue(value); err != nil {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

// Destroy releases the remote step resources.
func (s *RemoteStep) Destroy() error {
	resp, err := s.client.DestroyStep(context.Background(), &pb.HandleRequest{
		HandleId: s.handleID,
	})
	if err != nil {
		return fmt.Errorf("remote step destroy: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("remote step destroy: %s", resp.Error)
	}
	return nil
}

// Ensure RemoteStep satisfies module.PipelineStep at compile time.
var _ module.PipelineStep = (*RemoteStep)(nil)
