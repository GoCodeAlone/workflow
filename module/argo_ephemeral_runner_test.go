package module

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
)

// Compile-time assertion: *ArgoEphemeralRunner implements sandbox.SandboxRunner.
var _ sandbox.SandboxRunner = (*ArgoEphemeralRunner)(nil)

// testPollInterval is a tiny poll cadence so status tests don't pay the 2s
// production interval. The ctx is honored mid-call regardless of this value.
const testPollInterval = time.Millisecond

// ─── fake argoSubmitter ───────────────────────────────────────────────────────

// fakeArgoSubmitter records the last submitted spec and simulates configurable
// status progressions and log returns. All methods take a context (matching the
// argoSubmitter interface) so cancellation can be exercised mid-call.
type fakeArgoSubmitter struct {
	// submittedSpec is set when SubmitWorkflow is called.
	submittedSpec *ArgoWorkflowSpec

	// statusSequence is the ordered list of status strings returned on
	// successive WorkflowStatus calls. Once exhausted it always returns
	// the last element (so a terminal status "sticks").
	statusSequence []string
	statusIdx      int

	// logs is the list of log lines returned by WorkflowLogs.
	logs []string

	// submitErr, if non-nil, is returned by SubmitWorkflow.
	submitErr error

	// statusErr, if non-nil, is returned by WorkflowStatus.
	statusErr error

	// logsErr, if non-nil, is returned by WorkflowLogs.
	logsErr error

	// runName is returned by SubmitWorkflow on success.
	runName string

	// blockStatusOnCtx, when true, makes WorkflowStatus block until the passed
	// ctx is cancelled, then returns ctx.Err(). This simulates an in-flight HTTP
	// call that honors ctx cancellation (the real backend's doRequest behavior).
	blockStatusOnCtx bool

	// deleteCalled records whether DeleteWorkflow was invoked (best-effort
	// cleanup on ctx cancellation).
	deleteCalled bool
}

func (f *fakeArgoSubmitter) DeleteWorkflow(_ context.Context, _ string) error {
	f.deleteCalled = true
	return nil
}

func (f *fakeArgoSubmitter) SubmitWorkflow(_ context.Context, spec *ArgoWorkflowSpec) (string, error) {
	f.submittedSpec = spec
	if f.submitErr != nil {
		return "", f.submitErr
	}
	name := f.runName
	if name == "" {
		name = "fake-run"
	}
	return name, nil
}

func (f *fakeArgoSubmitter) WorkflowStatus(ctx context.Context, _ string) (string, error) {
	if f.blockStatusOnCtx {
		// Simulate an in-flight HTTP request that aborts on ctx cancellation.
		<-ctx.Done()
		return "", ctx.Err()
	}
	if f.statusErr != nil {
		return "", f.statusErr
	}
	if len(f.statusSequence) == 0 {
		return "Succeeded", nil
	}
	idx := f.statusIdx
	if idx >= len(f.statusSequence) {
		idx = len(f.statusSequence) - 1
	}
	f.statusIdx++
	return f.statusSequence[idx], nil
}

func (f *fakeArgoSubmitter) WorkflowLogs(_ context.Context, _ string) ([]string, error) {
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return f.logs, nil
}

// ─── helper ──────────────────────────────────────────────────────────────────

// buildTestRunner builds an ArgoEphemeralRunner wired to a fakeArgoSubmitter
// with a 1ms poll interval so status tests run fast.
func buildTestRunner(f *fakeArgoSubmitter, image string, env map[string]string) *ArgoEphemeralRunner {
	cfg := sandbox.SandboxConfig{
		Image: image,
		Env:   env,
	}
	return newArgoEphemeralRunner(f, "test-ns", cfg, testPollInterval)
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestArgoEphemeralRunner_Succeeded verifies that a "Succeeded" Argo status maps
// to ExitCode 0 and that log lines are joined into Stdout.
func TestArgoEphemeralRunner_Succeeded(t *testing.T) {
	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Succeeded"},
		logs:           []string{"hello", "world"},
	}
	runner := buildTestRunner(fake, "alpine:3.19", nil)

	result, err := runner.Exec(context.Background(), []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0 for Succeeded status", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Stdout: expected log content, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "world") {
		t.Errorf("Stdout: expected 'world' in logs, got %q", result.Stdout)
	}
}

// TestArgoEphemeralRunner_Failed verifies that a "Failed" Argo status maps to
// a non-zero ExitCode (1).
func TestArgoEphemeralRunner_Failed(t *testing.T) {
	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Failed"},
		logs:           []string{"step failed"},
	}
	runner := buildTestRunner(fake, "alpine:3.19", nil)

	result, err := runner.Exec(context.Background(), []string{"false"})
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Errorf("ExitCode: got 0 for Failed status, want non-zero")
	}
}

// TestArgoEphemeralRunner_Error verifies that an "Error" Argo status (infrastructure
// failure) also maps to a non-zero ExitCode.
func TestArgoEphemeralRunner_Error(t *testing.T) {
	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Error"},
		logs:           []string{"pod evicted"},
	}
	runner := buildTestRunner(fake, "alpine:3.19", nil)

	result, err := runner.Exec(context.Background(), []string{"true"})
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Errorf("ExitCode: got 0 for Error status, want non-zero")
	}
}

// TestArgoEphemeralRunner_SpecShape verifies that the built ArgoWorkflowSpec
// carries exactly one container template with the correct image, command, and env,
// and that a cleanup TTL is set so completed workflows are garbage-collected.
func TestArgoEphemeralRunner_SpecShape(t *testing.T) {
	env := map[string]string{"FOO": "bar", "GOPATH": "/go"}
	cmd := []string{"/bin/sh", "-c", "echo hello"}

	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Succeeded"},
		logs:           []string{"hello"},
	}
	runner := buildTestRunner(fake, "alpine:3.19", env)

	if _, err := runner.Exec(context.Background(), cmd); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	spec := fake.submittedSpec
	if spec == nil {
		t.Fatal("SubmitWorkflow was not called")
	}

	// Exactly one template.
	if len(spec.Templates) != 1 {
		t.Fatalf("Templates: got %d, want 1", len(spec.Templates))
	}
	tpl := spec.Templates[0]

	if tpl.Kind != "container" {
		t.Errorf("Template.Kind: got %q, want %q", tpl.Kind, "container")
	}
	if tpl.Name != "main" {
		t.Errorf("Template.Name: got %q, want %q", tpl.Name, "main")
	}
	if tpl.Container == nil {
		t.Fatal("Template.Container is nil")
	}
	if tpl.Container.Image != "alpine:3.19" {
		t.Errorf("Container.Image: got %q, want %q", tpl.Container.Image, "alpine:3.19")
	}
	if len(tpl.Container.Command) != len(cmd) {
		t.Errorf("Container.Command length: got %d, want %d", len(tpl.Container.Command), len(cmd))
	}
	for i, c := range cmd {
		if tpl.Container.Command[i] != c {
			t.Errorf("Container.Command[%d]: got %q, want %q", i, tpl.Container.Command[i], c)
		}
	}
	for k, v := range env {
		if tpl.Container.Env[k] != v {
			t.Errorf("Container.Env[%q]: got %q, want %q", k, tpl.Container.Env[k], v)
		}
	}

	// Entrypoint and namespace.
	if spec.Entrypoint != "main" {
		t.Errorf("Entrypoint: got %q, want %q", spec.Entrypoint, "main")
	}
	if spec.Namespace != "test-ns" {
		t.Errorf("Namespace: got %q, want %q", spec.Namespace, "test-ns")
	}
	if spec.Kind != "Workflow" {
		t.Errorf("Kind: got %q, want %q", spec.Kind, "Workflow")
	}

	// Cleanup TTL must be set (prevents namespace accumulation of completed runs).
	if spec.TTLSecondsAfterFinished != argoEphemeralTTLSeconds {
		t.Errorf("TTLSecondsAfterFinished: got %d, want %d", spec.TTLSecondsAfterFinished, argoEphemeralTTLSeconds)
	}
}

// TestArgoEphemeralRunner_TTLRendersInCRD verifies that the TTL set on the spec
// is rendered into the Argo Workflow CRD as spec.ttlStrategy.secondsAfterCompletion
// (the field the Argo controller honors for auto-deletion).
func TestArgoEphemeralRunner_TTLRendersInCRD(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	runner := newArgoEphemeralRunner(&fakeArgoSubmitter{}, "test-ns", cfg, testPollInterval)

	spec := runner.buildSpec("ephemeral-exec-1", []string{"true"})
	crd := argoWorkflowCRD(spec)

	specMap, ok := crd["spec"].(map[string]any)
	if !ok {
		t.Fatalf("crd[spec] not a map: %T", crd["spec"])
	}
	ttlStrategy, ok := specMap["ttlStrategy"].(map[string]any)
	if !ok {
		t.Fatalf("crd spec.ttlStrategy missing or wrong type: %T", specMap["ttlStrategy"])
	}
	if got := ttlStrategy["secondsAfterCompletion"]; got != argoEphemeralTTLSeconds {
		t.Errorf("ttlStrategy.secondsAfterCompletion: got %v, want %d", got, argoEphemeralTTLSeconds)
	}
}

// TestArgoEphemeralRunner_SecretRefPassthrough verifies that a "secret://" value
// in the env map is NOT resolved by the runner — it is forwarded as-is to the
// Argo Workflow spec. Production Kubernetes admission/mutation logic is responsible
// for substituting the real value (ADR 0017).
func TestArgoEphemeralRunner_SecretRefPassthrough(t *testing.T) {
	const secretRef = "secret://vault/my-token"
	env := map[string]string{"TOKEN": secretRef}

	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Succeeded"},
		logs:           nil,
	}
	runner := buildTestRunner(fake, "alpine:3.19", env)

	if _, err := runner.Exec(context.Background(), []string{"true"}); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	spec := fake.submittedSpec
	if spec == nil {
		t.Fatal("SubmitWorkflow was not called")
	}
	if len(spec.Templates) == 0 || spec.Templates[0].Container == nil {
		t.Fatal("container template missing")
	}
	got := spec.Templates[0].Container.Env["TOKEN"]
	if got != secretRef {
		t.Errorf("TOKEN env: got %q, want %q (must NOT be resolved engine-side)", got, secretRef)
	}
}

// TestArgoEphemeralRunner_CtxCancelDuringPoll verifies that ctx cancellation
// between poll ticks causes Exec to return ctx.Err() promptly.
func TestArgoEphemeralRunner_CtxCancelDuringPoll(t *testing.T) {
	// Return a non-terminal status so the poll loop never exits naturally.
	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Running", "Running", "Running"},
	}
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	// Larger interval so the ctx.Done() branch (not a status return) wins.
	runner := newArgoEphemeralRunner(fake, "test-ns", cfg, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := runner.Exec(ctx, []string{"sleep", "9999"})
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got: %v", err)
	}
	// On cancellation the runner best-effort deletes the submitted workflow so it
	// doesn't keep running in the cluster until TTL GC.
	if !fake.deleteCalled {
		t.Error("expected DeleteWorkflow to be called on ctx cancellation")
	}
}

// TestArgoEphemeralRunner_CtxCancelDuringInFlightStatus verifies that ctx
// cancellation while a WorkflowStatus call is in flight (blocked) is honored:
// the runner threads ctx into the submitter call, so cancellation aborts the
// in-flight request rather than waiting out a client timeout. The fake's
// WorkflowStatus blocks on ctx.Done() to model the real backend's HTTP behavior.
func TestArgoEphemeralRunner_CtxCancelDuringInFlightStatus(t *testing.T) {
	fake := &fakeArgoSubmitter{
		blockStatusOnCtx: true,
	}
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	// Long poll interval: the FIRST tick fires quickly, then the status call
	// blocks until ctx is cancelled. This proves ctx reaches the submitter.
	runner := newArgoEphemeralRunner(fake, "test-ns", cfg, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := runner.Exec(ctx, []string{"true"})
		done <- err
	}()

	// Give the runner time to submit, tick once, and enter the blocking status call.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Exec did not return after ctx cancel — in-flight status call ignored ctx")
	}
}

// TestArgoEphemeralRunner_WorkflowStatusError verifies that an error returned by
// WorkflowStatus during polling is propagated as an Exec error.
func TestArgoEphemeralRunner_WorkflowStatusError(t *testing.T) {
	fake := &fakeArgoSubmitter{
		statusErr: errors.New("argo status endpoint 503"),
	}
	runner := buildTestRunner(fake, "alpine:3.19", nil)

	_, err := runner.Exec(context.Background(), []string{"true"})
	if err == nil {
		t.Fatal("expected error from WorkflowStatus, got nil")
	}
	if !strings.Contains(err.Error(), "argo status endpoint 503") {
		t.Errorf("error should contain status error message, got: %v", err)
	}
}

// TestArgoEphemeralRunner_SubmitError verifies that a SubmitWorkflow error is
// propagated as an Exec error.
func TestArgoEphemeralRunner_SubmitError(t *testing.T) {
	fake := &fakeArgoSubmitter{
		submitErr: errors.New("argo server unavailable"),
	}
	runner := buildTestRunner(fake, "alpine:3.19", nil)

	_, err := runner.Exec(context.Background(), []string{"true"})
	if err == nil {
		t.Fatal("expected error from SubmitWorkflow, got nil")
	}
	if !strings.Contains(err.Error(), "argo server unavailable") {
		t.Errorf("error should contain submit error message, got: %v", err)
	}
}

// TestArgoEphemeralRunner_Close_NoOp verifies that Close always returns nil.
func TestArgoEphemeralRunner_Close_NoOp(t *testing.T) {
	runner := buildTestRunner(&fakeArgoSubmitter{}, "alpine:3.19", nil)
	if err := runner.Close(); err != nil {
		t.Errorf("Close: expected nil, got %v", err)
	}
}

// TestArgoEphemeralRunner_DefaultPollInterval verifies the constructor falls back
// to the package default when given a non-positive interval.
func TestArgoEphemeralRunner_DefaultPollInterval(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	runner := newArgoEphemeralRunner(&fakeArgoSubmitter{}, "test-ns", cfg, 0)
	if runner.pollInterval != argoExecPollInterval {
		t.Errorf("pollInterval: got %v, want default %v", runner.pollInterval, argoExecPollInterval)
	}
}

// TestArgoEphemeralRunner_MonotonicNames verifies that successive Exec calls
// produce distinct workflow names (monotonic counter guarantee).
func TestArgoEphemeralRunner_MonotonicNames(t *testing.T) {
	var names []string
	fake := &fakeArgoSubmitter{
		statusSequence: []string{"Succeeded"},
		logs:           nil,
	}
	// Override runName via a small wrapper to capture emitted names.
	capturing := &capturingSubmitter{inner: fake, names: &names}
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}
	runner := newArgoEphemeralRunner(capturing, "test-ns", cfg, testPollInterval)

	for i := 0; i < 3; i++ {
		if _, err := runner.Exec(context.Background(), []string{"true"}); err != nil {
			t.Fatalf("Exec[%d]: %v", i, err)
		}
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 submitted workflow names, got %d", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate workflow name %q in successive Exec calls", n)
		}
		seen[n] = true
	}
}

// capturingSubmitter wraps a fakeArgoSubmitter and records all SubmitWorkflow
// spec names (the Name field in the spec, not the returned runName).
type capturingSubmitter struct {
	inner *fakeArgoSubmitter
	names *[]string
}

func (c *capturingSubmitter) SubmitWorkflow(ctx context.Context, spec *ArgoWorkflowSpec) (string, error) {
	*c.names = append(*c.names, spec.Name)
	return c.inner.SubmitWorkflow(ctx, spec)
}

func (c *capturingSubmitter) WorkflowStatus(ctx context.Context, name string) (string, error) {
	return c.inner.WorkflowStatus(ctx, name)
}

func (c *capturingSubmitter) WorkflowLogs(ctx context.Context, name string) ([]string, error) {
	return c.inner.WorkflowLogs(ctx, name)
}

func (c *capturingSubmitter) DeleteWorkflow(ctx context.Context, name string) error {
	return c.inner.DeleteWorkflow(ctx, name)
}
