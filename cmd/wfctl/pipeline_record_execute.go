package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
)

type pipelineRecordRequest struct {
	Config      *config.WorkflowConfig `json:"config"`
	Pipeline    string                 `json:"pipeline"`
	ResultStep  string                 `json:"result_step"`
	PluginDir   string                 `json:"plugin_dir"`
	Input       map[string]any         `json:"input"`
	ParentPID   int                    `json:"parent_pid"`
	ParentStart string                 `json:"parent_start"`
}

func executePipelineRecord(ctx context.Context, request pipelineRecordRequest) map[string]any {
	failure := func(code string) map[string]any {
		return map[string]any{"status": "error", "code": code}
	}
	if request.Config == nil {
		return failure("pipeline_failed")
	}
	closure, err := selectPipelineClosure(request.Config, request.Pipeline, true)
	if err != nil || closure.names[request.ResultStep] != 1 {
		return failure("result_invalid")
	}
	installed, err := inspectPipelineInstallations(request.PluginDir)
	if err != nil {
		return failure("pipeline_failed")
	}
	names, err := installed.resolve(closure.types)
	if err != nil {
		return failure("pipeline_failed")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	builtin := pluginpipeline.New()
	engine, err := workflow.NewEngineBuilder().WithLogger(logger).
		WithHandler(handlers.NewPipelineWorkflowHandler()).WithPlugin(builtin).Build()
	if err != nil {
		return failure("pipeline_failed")
	}
	registerPipelineFailureFactories(engine, builtin.StepFactories())
	shutdown, err := loadPipelineExternalPlugins(engine, request.PluginDir, names, logger)
	if err != nil {
		return failure("pipeline_failed")
	}
	defer shutdown()
	if err := engine.BuildFromConfig(closure.config); err != nil {
		return failure("pipeline_failed")
	}
	pipeline, ok := engine.GetPipeline(request.Pipeline)
	if !ok {
		return failure("pipeline_failed")
	}
	pc, err := pipeline.Execute(ctx, request.Input)
	if err != nil || ctx.Err() != nil {
		return failure("pipeline_failed")
	}
	output, exists := pc.StepOutputs[request.ResultStep]
	if !exists {
		return failure("result_missing")
	}
	if _, err := workflowCanonicalJSONV1(output); err != nil {
		return failure("result_invalid")
	}
	return map[string]any{"status": "ok", "output": output}
}

type pipelineFailureStep struct{ inner module.PipelineStep }

func (s pipelineFailureStep) Name() string { return s.inner.Name() }

func (s pipelineFailureStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	result, err := s.inner.Execute(ctx, pc)
	if err == nil {
		err = stoppedPipelineError(result)
	}
	return result, err
}

func registerPipelineFailureFactories(engine *workflow.StdEngine, factories map[string]plugin.StepFactory) {
	for stepType, factory := range factories {
		engine.AddStepType(stepType, func(name string, cfg map[string]any, app modular.Application) (module.PipelineStep, error) {
			raw, err := factory(name, cfg, app)
			if err != nil {
				return nil, err
			}
			step, ok := raw.(module.PipelineStep)
			if !ok || step == nil {
				return nil, fmt.Errorf("record step factory returned an invalid pipeline step")
			}
			return pipelineFailureStep{inner: step}, nil
		})
	}
}
