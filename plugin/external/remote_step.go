package external

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// RemoteStep implements module.PipelineStep by delegating to a gRPC plugin.
type RemoteStep struct {
	name     string
	handleID string
	config   map[string]any
	client   pb.PluginServiceClient
	tmpl     *module.TemplateEngine
}

// NewRemoteStep creates a remote step proxy.
// config holds the raw (possibly template-containing) step configuration that
// will be resolved against the live pipeline context on each Execute call.
func NewRemoteStep(name, handleID string, client pb.PluginServiceClient, config map[string]any) *RemoteStep {
	return &RemoteStep{
		name:     name,
		handleID: handleID,
		config:   config,
		client:   client,
		tmpl:     module.NewTemplateEngine(),
	}
}

func (s *RemoteStep) Name() string {
	return s.name
}

func (s *RemoteStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
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

	// Convert step outputs to proto map
	stepOutputs := make(map[string]*structpb.Struct)
	for k, v := range pc.StepOutputs {
		stepOutputs[k] = mapToStruct(v)
	}

	resp, err := s.client.ExecuteStep(ctx, &pb.ExecuteStepRequest{
		HandleId:    s.handleID,
		TriggerData: mapToStruct(pc.TriggerData),
		StepOutputs: stepOutputs,
		Current:     mapToStruct(pc.Current),
		Metadata:    mapToStruct(pc.Metadata),
		Config:      mapToStruct(resolvedConfig),
	})
	if err != nil {
		return nil, fmt.Errorf("remote step execute: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("remote step execute: %s", resp.Error)
	}

	output := structToMap(resp.Output)

	// When the plugin signals a pipeline stop with an HTTP response encoded in
	// the output (response_status, response_body, response_headers), write that
	// response to _http_response_writer so the caller sees the correct status
	// code rather than a default 200/202. This mirrors the pattern used by
	// step.auth_validate for 401 responses.
	if resp.StopPipeline {
		if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
			if statusRaw, hasStatus := output["response_status"]; hasStatus {
				var statusCode int
				switch v := statusRaw.(type) {
				case float64:
					statusCode = int(v)
				case int:
					statusCode = v
				}
				if statusCode > 0 {
					if headersRaw, ok := output["response_headers"]; ok {
						if headers, ok := headersRaw.(map[string]any); ok {
							for k, v := range headers {
								if vs, ok := v.(string); ok {
									w.Header().Set(k, vs)
								}
							}
						}
					}
					if w.Header().Get("Content-Type") == "" {
						w.Header().Set("Content-Type", "application/json")
					}
					w.WriteHeader(statusCode)
					if bodyRaw, ok := output["response_body"]; ok {
						if bodyStr, ok := bodyRaw.(string); ok {
							_, _ = io.WriteString(w, bodyStr)
						}
					}
					pc.Metadata["_response_handled"] = true
				}
			}
		}
	}

	return &module.StepResult{
		Output: output,
		Stop:   resp.StopPipeline,
	}, nil
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
