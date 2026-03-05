package module

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigHash_InExecutionMetadata(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{
		Store:      store,
		WorkflowID: "test-wf",
		ConfigHash: "sha256:abc123",
	}

	req := httptest.NewRequest("GET", "/test", nil)
	step := newMockStep("step1", map[string]any{"result": "ok"})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.Contains(t, metadata, `"config_hash":"sha256:abc123"`)
}

func TestConfigHash_IncludedAlongsideExplicitTrace(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{
		Store:      store,
		WorkflowID: "test-wf",
		ConfigHash: "sha256:deadbeef",
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")
	step := newMockStep("step1", map[string]any{"result": "ok"})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.Contains(t, metadata, `"config_hash":"sha256:deadbeef"`)
	require.Contains(t, metadata, `"explicit_trace":true`)
	require.Contains(t, metadata, `"capture_io":true`)
}

// setupTestStoreWithWorkflow creates a test V1Store and inserts a minimal
// workflow record (bypassing FK chain) so execution tracking can store records.
func setupTestStoreWithWorkflow(t *testing.T, workflowID string) *V1Store {
	t.Helper()
	store := setupTestStore(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// Disable FKs temporarily to insert minimal test records
	_, err := store.DB().Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)

	_, err = store.DB().Exec(
		`INSERT INTO workflows (id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at)
		 VALUES (?, 'test-project', ?, ?, '', '', 1, 'active', 0, '', '', ?, ?)`,
		workflowID, workflowID, workflowID, now, now,
	)
	require.NoError(t, err)

	_, err = store.DB().Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	return store
}

func TestTrackPipelineExecution_ExplicitTraceHeader(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	step := newMockStep("step1", map[string]any{"result": "ok"})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	pc, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)
	require.NotNil(t, pc)

	// Verify the execution was marked as explicitly traced
	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	require.Contains(t, metadata, `"explicit_trace":true`)
	require.Contains(t, metadata, `"capture_io":true`)
}

func TestTrackPipelineExecution_NoTraceHeader(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	// No X-Workflow-Trace header

	step := newMockStep("step1", map[string]any{"result": "ok"})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var metadata string
	err = store.DB().QueryRow(
		"SELECT metadata FROM workflow_executions WHERE workflow_id = 'test-wf'",
	).Scan(&metadata)
	require.NoError(t, err)
	// Default metadata should NOT contain explicit_trace
	require.NotContains(t, metadata, `"explicit_trace":true`)
}

func TestExecutionTracker_CapturesStepIO_WhenExplicitTrace(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	step := newMockStep("step1", map[string]any{"result": "hello", "count": 42})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, map[string]any{"input_key": "input_value"}, req)
	require.NoError(t, err)

	// Query the step record to check I/O was captured
	var inputData, outputData string
	err = store.DB().QueryRow(
		"SELECT input_data, output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&inputData, &outputData)
	require.NoError(t, err)
	require.Contains(t, outputData, "hello")
}

func TestExecutionTracker_NoIO_WhenNoTraceHeader(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	// No X-Workflow-Trace header

	step := newMockStep("step1", map[string]any{"result": "hello"})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var inputData, outputData string
	err = store.DB().QueryRow(
		"SELECT input_data, output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&inputData, &outputData)
	require.NoError(t, err)
	// Without explicit trace, I/O should not be populated (remain as default '{}')
	require.Equal(t, "{}", outputData)
}

func TestExecutionTracker_TruncatesLargeIO(t *testing.T) {
	store := setupTestStoreWithWorkflow(t, "test-wf")

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	// Create a large output value that exceeds 10KB
	largeValue := make([]byte, 20000)
	for i := range largeValue {
		largeValue[i] = 'x'
	}
	step := newMockStep("step1", map[string]any{"big": string(largeValue)})
	pipeline := &Pipeline{
		Name:  "test-pipeline",
		Steps: []PipelineStep{step},
	}

	_, err := tracker.TrackPipelineExecution(context.Background(), pipeline, nil, req)
	require.NoError(t, err)

	var outputData string
	err = store.DB().QueryRow(
		"SELECT output_data FROM execution_steps WHERE step_name = 'step1'",
	).Scan(&outputData)
	require.NoError(t, err)
	require.LessOrEqual(t, len(outputData), 10240)
	require.Contains(t, outputData, "[truncated]")
}
