package module

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
)

// argoSubmitter is the minimal interface satisfied by *ArgoWorkflowsModule that
// the ephemeral runner depends on. Keeping it narrow allows tests to inject a
// fake without pulling in the full module struct.
//
// Every method takes a context so the underlying HTTP call (in the real backend)
// honors cancellation/deadline mid-flight — not just between the runner's poll
// ticks.
type argoSubmitter interface {
	SubmitWorkflow(ctx context.Context, spec *ArgoWorkflowSpec) (string, error)
	WorkflowStatus(ctx context.Context, workflowName string) (string, error)
	WorkflowLogs(ctx context.Context, workflowName string) ([]string, error)
	DeleteWorkflow(ctx context.Context, workflowName string) error
}

// Compile-time assertion: *ArgoWorkflowsModule satisfies argoSubmitter.
var _ argoSubmitter = (*ArgoWorkflowsModule)(nil)

// argoEphemeralCounter is a module-level monotonic counter used to generate
// deterministic, collision-resistant workflow names without time or random
// sources. It is safe for concurrent use.
var argoEphemeralCounter atomic.Uint64

// argoTerminalStatuses contains the Argo Workflows status phase strings that
// indicate a workflow has reached a terminal state.
//
// Source: Argo Workflows status.phase values from the Argo Workflows API
// (argoproj.io/v1alpha1 Workflow .status.phase):
//   - "Succeeded" — all steps completed successfully.
//   - "Failed"    — one or more steps failed (exit code non-zero or assertion error).
//   - "Error"     — the workflow encountered an infrastructure / controller error
//     (e.g. pod eviction, OOM, missing image). Distinct from "Failed".
var argoTerminalStatuses = map[string]bool{
	"Succeeded": true,
	"Failed":    true,
	"Error":     true,
}

// argoExecPollInterval is the default poll interval for WorkflowStatus checks
// during Exec. It uses a constant duration to avoid time.Now / random sources.
// In production the Argo controller typically updates status within seconds.
// Tests inject a much shorter interval via newArgoEphemeralRunner.
const argoExecPollInterval = 2 * time.Second

// argoEphemeralTTLSeconds is the default TTL applied to ephemeral workflows so
// the Argo controller garbage-collects completed runs (prevents namespace
// accumulation). Maps to spec.ttlStrategy.secondsAfterCompletion (300s = 5m).
const argoEphemeralTTLSeconds = 300

// ArgoEphemeralRunner implements sandbox.SandboxRunner by submitting a
// one-off Argo Workflow on Kubernetes and polling until it reaches a terminal
// status. It is wired as exec_env: ephemeral in step.sandbox_exec.
//
// Exit-code limitation: Argo exposes a workflow-level status phase, not the
// individual container exit code. ArgoEphemeralRunner maps:
//   - "Succeeded" → ExitCode 0
//   - "Failed" / "Error" → ExitCode 1
//
// Fine-grained exit codes (e.g. 2, 127) are not available from the Argo status
// API and cannot be recovered without instrumenting the workflow template to
// capture them. This is documented as a known limitation (ADR 0020).
//
// Secret refs: env values may carry "secret://" references. ArgoEphemeralRunner
// does NOT resolve them engine-side; it passes them through as-is into the Argo
// Workflow spec. Production deployments are expected to resolve secret refs via
// Kubernetes secretKeyRef / projected volumes at pod-launch time (ADR 0017).
// The engine-side secret:// string is intentionally preserved so the k8s
// admission/mutation webhook or a sidecar can substitute the real value.
type ArgoEphemeralRunner struct {
	submitter    argoSubmitter
	namespace    string
	cfg          sandbox.SandboxConfig
	pollInterval time.Duration
}

// Compile-time assertion: *ArgoEphemeralRunner implements sandbox.SandboxRunner.
var _ sandbox.SandboxRunner = (*ArgoEphemeralRunner)(nil)

// newArgoEphemeralRunner constructs an ArgoEphemeralRunner.
// namespace is the Kubernetes namespace where Argo Workflows are submitted.
// cfg carries the image, env, and profile for this execution.
// pollInterval sets the WorkflowStatus poll cadence; a non-positive value
// falls back to the package default (argoExecPollInterval). Tests pass a small
// interval (e.g. 1ms) so status polling does not dominate test runtime.
func newArgoEphemeralRunner(submitter argoSubmitter, namespace string, cfg sandbox.SandboxConfig, pollInterval time.Duration) *ArgoEphemeralRunner {
	if pollInterval <= 0 {
		pollInterval = argoExecPollInterval
	}
	return &ArgoEphemeralRunner{
		submitter:    submitter,
		namespace:    namespace,
		cfg:          cfg,
		pollInterval: pollInterval,
	}
}

// Exec submits a single-container Argo Workflow for cmd, polls until terminal,
// then returns the combined log output as Stdout and maps Argo status to ExitCode.
//
// Workflow name: derived as "ephemeral-exec-<monotonic-counter>" so that names
// are unique per process lifetime without relying on time or random sources.
// The counter is module-global and thread-safe (atomic.Uint64).
//
// ctx cancellation: ctx is threaded into every SubmitWorkflow/WorkflowStatus/
// WorkflowLogs call so an in-flight HTTP request aborts on cancellation, and a
// select on ctx.Done() is also checked between poll ticks. On cancellation
// ctx.Err() is returned promptly.
func (r *ArgoEphemeralRunner) Exec(ctx context.Context, cmd []string) (*sandbox.ExecResult, error) {
	seq := argoEphemeralCounter.Add(1)
	wfName := fmt.Sprintf("ephemeral-exec-%d", seq)

	spec := r.buildSpec(wfName, cmd)

	runName, err := r.submitter.SubmitWorkflow(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("argo ephemeral runner: submit workflow: %w", err)
	}

	// Poll until the workflow reaches a terminal status, respecting ctx cancellation.
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	var finalStatus string
	for {
		select {
		case <-ctx.Done():
			// The caller cancelled/timed out. Best-effort terminate the submitted
			// workflow so it doesn't keep running (and billing) in the cluster
			// until TTL GC — analogous to the local runner stopping its container.
			// Use a fresh short-lived ctx since the caller's is already done.
			delCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if delErr := r.submitter.DeleteWorkflow(delCtx, runName); delErr != nil {
				slog.Warn("argo ephemeral runner: failed to delete workflow after ctx cancellation",
					"workflow", runName, "err", delErr)
			}
			cancel()
			return nil, ctx.Err()
		case <-ticker.C:
			status, err := r.submitter.WorkflowStatus(ctx, runName)
			if err != nil {
				return nil, fmt.Errorf("argo ephemeral runner: poll workflow status: %w", err)
			}
			if argoTerminalStatuses[status] {
				finalStatus = status
				goto done
			}
		}
	}

done:
	// Fetch logs and join into a single Stdout string.
	lines, err := r.submitter.WorkflowLogs(ctx, runName)
	if err != nil {
		// Non-fatal: surface the failure as a warning line in stdout (so the
		// caller still gets the exit-code verdict) rather than aborting.
		lines = []string{fmt.Sprintf("[argo ephemeral runner] warning: could not retrieve logs: %v", err)}
	}
	stdout := strings.Join(lines, "\n")

	// Map Argo status phase to a process-style exit code.
	// NOTE: Argo does not expose individual container exit codes via the status
	// phase API. "Succeeded" maps to 0; any terminal failure maps to 1.
	// Callers requiring fine-grained exit codes must instrument the workflow
	// template to capture and surface them (e.g. via output parameters).
	// See ArgoEphemeralRunner godoc and ADR 0020 for full discussion.
	exitCode := 0
	if finalStatus != "Succeeded" {
		exitCode = 1
	}

	return &sandbox.ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   "", // Argo log endpoint does not distinguish stdout/stderr streams.
	}, nil
}

// Close is a no-op: ArgoEphemeralRunner holds no persistent connections.
func (r *ArgoEphemeralRunner) Close() error { return nil }

// buildSpec constructs the ArgoWorkflowSpec for a one-shot command execution.
// The workflow has a single container template named "main" and entrypoint "main".
//
// TTL: a TTLSecondsAfterFinished is set so the Argo controller auto-deletes the
// completed Workflow object (ttlStrategy.secondsAfterCompletion), preventing
// ephemeral-exec-N workflows from accumulating in the namespace. No extra API
// call from the engine is needed.
//
// Env passthrough: env values that carry "secret://" refs are forwarded as-is
// into the Argo Workflow spec. The engine does NOT resolve them engine-side;
// production Kubernetes admission / mutation logic is responsible for substituting
// real values (ADR 0017). See ArgoEphemeralRunner type-level godoc for details.
func (r *ArgoEphemeralRunner) buildSpec(name string, cmd []string) *ArgoWorkflowSpec {
	return &ArgoWorkflowSpec{
		APIVersion:              "argoproj.io/v1alpha1",
		Kind:                    "Workflow",
		Name:                    name,
		Namespace:               r.namespace,
		Entrypoint:              "main",
		TTLSecondsAfterFinished: argoEphemeralTTLSeconds,
		Templates: []ArgoTemplate{
			{
				Name: "main",
				Kind: "container",
				Container: &ArgoContainer{
					Image:   r.cfg.Image,
					Command: cmd,
					Env:     r.cfg.Env,
				},
			},
		},
	}
}
