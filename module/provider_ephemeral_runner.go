package module

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/sandbox"
)

const providerEphemeralPollInterval = 2 * time.Second

var providerEphemeralCounter atomic.Uint64

// ProviderEphemeralRunner implements sandbox.SandboxRunner by delegating
// one-off job execution to an IaCProviderRunner optional provider capability.
type ProviderEphemeralRunner struct {
	runner       interfaces.IaCProviderRunner
	providerName string
	cfg          sandbox.SandboxConfig
	pollInterval time.Duration
}

var _ sandbox.SandboxRunner = (*ProviderEphemeralRunner)(nil)

func newProviderEphemeralRunner(runner interfaces.IaCProviderRunner, providerName string, cfg sandbox.SandboxConfig, pollInterval time.Duration) *ProviderEphemeralRunner {
	if pollInterval <= 0 {
		pollInterval = providerEphemeralPollInterval
	}
	return &ProviderEphemeralRunner{
		runner:       runner,
		providerName: providerName,
		cfg:          cfg,
		pollInterval: pollInterval,
	}
}

func (r *ProviderEphemeralRunner) Exec(ctx context.Context, cmd []string) (*sandbox.ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("provider ephemeral runner: command is required")
	}
	seq := providerEphemeralCounter.Add(1)
	spec := interfaces.JobSpec{
		Name:       fmt.Sprintf("provider-ephemeral-exec-%d", seq),
		Kind:       "EPHEMERAL",
		Image:      r.cfg.Image,
		RunCommand: strings.Join(cmd, " "),
		EnvVars:    copyStringMapModule(r.cfg.Env),
	}

	handle, err := r.runner.RunJob(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("provider ephemeral runner: run job: %w", err)
	}
	if handle == nil || strings.TrimSpace(handle.ID) == "" {
		return nil, fmt.Errorf("provider ephemeral runner: provider %q returned empty job handle", r.providerName)
	}

	status, err := r.waitForTerminalStatus(ctx, *handle)
	if err != nil {
		return nil, err
	}

	sink := &providerLogSink{}
	if err := r.runner.JobLogs(ctx, *handle, sink); err != nil {
		_, _ = fmt.Fprintf(&sink.stdout, "[provider ephemeral runner] warning: could not retrieve logs: %v", err)
	}

	exitCode := status.ExitCode
	if status.State != interfaces.JobStateSucceeded && exitCode == 0 {
		exitCode = 1
	}
	return &sandbox.ExecResult{
		ExitCode: exitCode,
		Stdout:   sink.stdout.String(),
		Stderr:   sink.stderr.String(),
	}, nil
}

func (r *ProviderEphemeralRunner) waitForTerminalStatus(ctx context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	for {
		status, err := r.runner.JobStatus(ctx, handle)
		if err != nil {
			return nil, fmt.Errorf("provider ephemeral runner: poll job status: %w", err)
		}
		if status != nil && providerJobStateTerminal(status.State) {
			return status, nil
		}
		timer := time.NewTimer(r.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func providerJobStateTerminal(state interfaces.JobState) bool {
	switch state {
	case interfaces.JobStateSucceeded, interfaces.JobStateFailed, interfaces.JobStateCancelled:
		return true
	default:
		return false
	}
}

func (r *ProviderEphemeralRunner) Close() error { return nil }

type providerLogSink struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func (s *providerLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	if chunk.EOF {
		return nil
	}
	if strings.Contains(strings.ToLower(chunk.Source), "stderr") {
		_, err := s.stderr.Write(chunk.Data)
		return err
	}
	_, err := s.stdout.Write(chunk.Data)
	return err
}

func copyStringMapModule(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
