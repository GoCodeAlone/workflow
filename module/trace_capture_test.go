package module

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExplicitTraceHeader_Detected verifies that X-Workflow-Trace: true activates
// step I/O capture and marks the execution metadata with explicit_trace and capture_io.
func TestExplicitTraceHeader_Detected(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")
	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"ok": true})},
	}

	pc, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)
	require.NotNil(t, pc)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.Contains(t, metadata, `"explicit_trace":true`)
	require.Contains(t, metadata, `"capture_io":true`)
}

// TestExplicitTraceHeader_Missing verifies that when no X-Workflow-Trace header
// is present, execution metadata does NOT contain the explicit_trace flag.
func TestExplicitTraceHeader_Missing(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")
	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)

	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"ok": true})},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.NotContains(t, metadata, `"explicit_trace":true`)
}

// TestStepIO_CapturedWhenExplicit verifies that step input and output data are
// stored in execution_steps when X-Workflow-Trace: true is set on the request.
func TestStepIO_CapturedWhenExplicit(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")
	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"result": "captured", "count": 99})},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, map[string]any{"input_key": "val"}, req)
	require.NoError(t, err)

	var inputData, outputData string
	err = store.DB().QueryRow(
		"SELECT input_data, output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&inputData, &outputData)
	require.NoError(t, err)
	require.Contains(t, inputData, "input_key")
	require.Contains(t, outputData, "captured")
}

// TestStepIO_NotCapturedWhenNotExplicit verifies that step I/O remains at the
// default empty value when no X-Workflow-Trace header is present.
func TestStepIO_NotCapturedWhenNotExplicit(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")
	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)

	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"result": "secret"})},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var outputData string
	err = store.DB().QueryRow(
		"SELECT output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&outputData)
	require.NoError(t, err)
	require.Equal(t, "{}", outputData)
}

// TestStepIO_TruncatedAt10KB verifies that step output data larger than 10 KB
// is truncated to at most maxIOBytes and appended with a [truncated] marker.
func TestStepIO_TruncatedAt10KB(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")
	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	// Build a step output that JSON-serialises to more than 10 KB.
	largeValue := strings.Repeat("x", 20000)
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"big": largeValue})},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var outputData string
	err = store.DB().QueryRow(
		"SELECT output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&outputData)
	require.NoError(t, err)
	require.LessOrEqual(t, len(outputData), maxIOBytes)
	require.Contains(t, outputData, "[truncated]")
}

// TestConfigHash_Deterministic verifies that SHA-256 produces a stable, deterministic
// hash for the same input bytes, using the "sha256:<hex>" format expected by the engine.
func TestConfigHash_Deterministic(t *testing.T) {
	input := []byte("modules:\n  - name: api\n    type: http.server\n")

	h1 := sha256.Sum256(input)
	h2 := sha256.Sum256(input)

	hash1 := fmt.Sprintf("sha256:%x", h1)
	hash2 := fmt.Sprintf("sha256:%x", h2)

	require.Equal(t, hash1, hash2, "same config bytes must produce the same hash")
	require.True(t, strings.HasPrefix(hash1, "sha256:"), "hash must start with 'sha256:'")
	// sha256: (7 chars) + 64 hex chars from 32 bytes
	require.Equal(t, 7+64, len(hash1), "hash must be 71 characters long")

	// Different input must produce a different hash.
	other := sha256.Sum256([]byte("modules:\n  - name: other\n    type: http.server\n"))
	require.NotEqual(t, fmt.Sprintf("sha256:%x", h1), fmt.Sprintf("sha256:%x", other))
}

// TestConfigHash_InExecutionMetadata verifies that when ExecutionTracker.ConfigHash
// is set, the value is persisted in the execution's metadata as "config_version".
func TestConfigHash_InExecutionMetadata(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{
		Store:      store,
		WorkflowID: "test-wf",
		ConfigHash: "sha256:abc123",
	}

	req := httptest.NewRequest("GET", "/test", nil)
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{newMockStep("step1", map[string]any{"result": "ok"})},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.Contains(t, metadata, `"config_version":"sha256:abc123"`)
}
