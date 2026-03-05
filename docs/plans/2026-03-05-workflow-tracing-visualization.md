# Workflow Tracing & Visualization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add tiered tracing — explicit test-request tracing in the engine/admin UI (Tier 1), and full historical tracing with sampling, retention, and PII redaction in workflow-cloud (Tier 2) — with a read-only ReactFlow canvas for trace visualization.

**Architecture:** Engine gets X-Workflow-Trace header detection and step I/O capture. Shared UI components (ReactFlow canvas, observability views) are extracted from workflow/ui into @gocodealone/workflow-ui. Admin UI gets a trace canvas. Cloud gets historical tracing, sampling, retention, config versioning. Cloud-UI incorporates shared components for admin-equivalent capabilities.

**Tech Stack:** Go 1.26, workflow engine, SQLite/PostgreSQL, React 19, @xyflow/react 12.10, Zustand 5, Vite 7, Playwright, OTEL SDK

**Repos:**
- `workflow` — `/Users/jon/workspace/workflow/`
- `workflow-ui` (shared lib) — `/Users/jon/workspace/workflow-ui/`
- `workflow-plugin-admin` (uses workflow/ui) — `/Users/jon/workspace/workflow-plugin-admin/`
- `workflow-cloud` — `/Users/jon/workspace/workflow-cloud/`
- `workflow-cloud-ui` — `/Users/jon/workspace/workflow-cloud-ui/`

**Design doc:** `docs/plans/2026-03-05-workflow-tracing-visualization-design.md`

---

## Phase 1: Engine Enhancements (workflow repo)

### Task 1: Add X-Workflow-Trace Header Detection

**Files:**
- Modify: `/Users/jon/workspace/workflow/module/execution_tracker.go`
- Modify: `/Users/jon/workspace/workflow/module/pipeline_executor.go`
- Test: `/Users/jon/workspace/workflow/module/execution_tracker_test.go`

**Context:** The `ExecutionTracker.TrackPipelineExecution()` method (execution_tracker.go:265-366) already wraps pipeline execution with DB recording + OTEL spans. We need to detect `X-Workflow-Trace: true` header on the HTTP request and, when present, enable step I/O capture for that specific execution.

**Step 1: Write failing test**

```go
// execution_tracker_test.go
func TestTrackPipelineExecution_ExplicitTraceHeader(t *testing.T) {
	store := setupTestV1Store(t)
	defer store.Close()

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	// Create a request with X-Workflow-Trace header
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	// Create a simple pipeline with one step
	step := &mockStep{name: "step1", output: map[string]any{"result": "ok"}}
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
}
```

**Step 2:** Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestTrackPipelineExecution_ExplicitTraceHeader -v`
Expected: FAIL (test helpers and explicit_trace logic don't exist yet)

**Step 3: Implement**

In `execution_tracker.go`, modify `TrackPipelineExecution()`:

```go
// After line 275 (execID := uuid.New().String()):
// Check for explicit trace header
explicitTrace := false
if r != nil && r.Header.Get("X-Workflow-Trace") == "true" {
    explicitTrace = true
}

// After line 315 (InsertExecution call):
// Store explicit trace flag in metadata
if explicitTrace {
    metaJSON, _ := json.Marshal(map[string]any{"explicit_trace": true, "capture_io": true})
    _ = store.db.Exec("UPDATE workflow_executions SET metadata = ? WHERE id = ?", string(metaJSON), execID)
}
```

Also add a field to `ExecutionTracker`:
```go
// Add after line 47 (execSpan field):
explicitTrace bool // whether this execution was explicitly requested to be traced
```

Set it in TrackPipelineExecution and expose it for step I/O capture (Task 2).

**Step 4:** Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestTrackPipelineExecution_ExplicitTraceHeader -v`
Expected: PASS

**Step 5: Commit**
```bash
cd /Users/jon/workspace/workflow
git add module/execution_tracker.go module/execution_tracker_test.go
git commit -m "feat: detect X-Workflow-Trace header for explicit trace requests"
```

---

### Task 2: Capture Step I/O in execution_steps

**Files:**
- Modify: `/Users/jon/workspace/workflow/module/execution_tracker.go`
- Modify: `/Users/jon/workspace/workflow/module/api_v1_store.go`
- Test: `/Users/jon/workspace/workflow/module/execution_tracker_test.go`

**Context:** The `execution_steps` table already has `input_data` and `output_data` TEXT columns (api_v1_store.go:132-147), but `InsertExecutionStep()` (line 901) never sets them, and `CompleteExecutionStep()` (line 910) doesn't update them. The pipeline executor emits `step.input_recorded` and `step.output_recorded` events (pipeline_executor.go) but the `ExecutionTracker.RecordEvent()` only handles `step.started`, `step.completed`, `step.failed`.

**Step 1: Write failing test**

```go
func TestExecutionTracker_CapturesStepIO_WhenExplicitTrace(t *testing.T) {
	store := setupTestV1Store(t)
	defer store.Close()

	tracker := &ExecutionTracker{Store: store, WorkflowID: "test-wf"}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Workflow-Trace", "true")

	step := &mockStep{
		name:   "step1",
		output: map[string]any{"result": "hello", "count": 42},
	}
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
```

**Step 2:** Run test, expect FAIL.

**Step 3: Implement**

Add `UpdateStepIO` method to V1Store (api_v1_store.go):

```go
// UpdateStepIO updates the input and output data for an execution step.
func (s *V1Store) UpdateStepIO(stepID, inputData, outputData string) error {
	_, err := s.db.Exec(
		"UPDATE execution_steps SET input_data = ?, output_data = ? WHERE id = ?",
		inputData, outputData, stepID,
	)
	return err
}
```

In `execution_tracker.go`, add handlers for I/O events:

```go
// In RecordEvent switch (after step.failed case):
case "step.input_recorded":
	if t.explicitTrace {
		t.handleStepInputRecorded(data)
	}
case "step.output_recorded":
	if t.explicitTrace {
		t.handleStepOutputRecorded(data)
	}
```

```go
func (t *ExecutionTracker) handleStepInputRecorded(data map[string]any) {
	stepName, _ := data["step_name"].(string)
	if stepName == "" {
		return
	}
	t.mu.Lock()
	stepID := t.stepIDs[stepName]
	t.mu.Unlock()
	if stepID == "" {
		return
	}
	inputJSON := "{}"
	if input, ok := data["input"]; ok {
		if b, err := json.Marshal(input); err == nil {
			// Truncate to 10KB max
			if len(b) > 10240 {
				b = append(b[:10237], []byte("...")...)
			}
			inputJSON = string(b)
		}
	}
	_ = t.Store.UpdateStepIO(stepID, inputJSON, "")
}

func (t *ExecutionTracker) handleStepOutputRecorded(data map[string]any) {
	stepName, _ := data["step_name"].(string)
	if stepName == "" {
		return
	}
	t.mu.Lock()
	stepID := t.stepIDs[stepName]
	t.mu.Unlock()
	if stepID == "" {
		return
	}
	outputJSON := "{}"
	if output, ok := data["output"]; ok {
		if b, err := json.Marshal(output); err == nil {
			if len(b) > 10240 {
				b = append(b[:10237], []byte("...")...)
			}
			outputJSON = string(b)
		}
	}
	// Read existing input_data so we don't overwrite it
	_ = t.Store.UpdateStepOutput(stepID, outputJSON)
}
```

Add `UpdateStepOutput` to V1Store:
```go
func (s *V1Store) UpdateStepOutput(stepID, outputData string) error {
	_, err := s.db.Exec("UPDATE execution_steps SET output_data = ? WHERE id = ?", outputData, stepID)
	return err
}
```

**Step 4:** Run tests, expect PASS.

**Step 5: Verify pipeline emits I/O events**

Check `pipeline_executor.go` — the engine already emits `step.input_recorded` and `step.output_recorded` events during execution. If not, we need to add them. Search for these event types in the executor.

If the events aren't emitted, add them in `Pipeline.Execute()`:
```go
// Before step execution:
p.recordEvent(ctx, "step.input_recorded", map[string]any{
    "step_name": step.Name(),
    "input":     pc.Current,
})

// After step execution succeeds:
p.recordEvent(ctx, "step.output_recorded", map[string]any{
    "step_name": step.Name(),
    "output":    result.Output,
})
```

**Step 6: Commit**
```bash
cd /Users/jon/workspace/workflow
git add module/execution_tracker.go module/api_v1_store.go module/execution_tracker_test.go module/pipeline_executor.go
git commit -m "feat: capture step I/O in execution_steps for explicit trace requests"
```

---

### Task 3: Add Execution Logs Query Endpoint

**Files:**
- Modify: `/Users/jon/workspace/workflow/store/timeline_handler.go`
- Modify: `/Users/jon/workspace/workflow/module/api_v1_store.go`
- Test: `/Users/jon/workspace/workflow/store/timeline_handler_test.go`

**Context:** The timeline handler (store/timeline_handler.go) exposes execution listing and timeline APIs. We need an endpoint to fetch logs for a specific execution: `GET /api/v1/admin/executions/{id}/logs`.

**Step 1: Write test**

```go
func TestTimelineHandler_GetExecutionLogs(t *testing.T) {
	// Setup store, insert execution, insert logs
	// GET /api/v1/admin/executions/{id}/logs
	// Verify response contains log entries with level filtering
}
```

**Step 2: Implement**

Add to `TimelineHandler.RegisterRoutes()`:
```go
mux.HandleFunc("GET /api/v1/admin/executions/{id}/logs", h.getExecutionLogs)
```

Add `ListExecutionLogs` to V1Store:
```go
func (s *V1Store) ListExecutionLogs(executionID string, level string, limit int) ([]map[string]any, error) {
	query := "SELECT id, workflow_id, execution_id, level, message, module_name, fields, created_at FROM execution_logs WHERE execution_id = ?"
	args := []any{executionID}
	if level != "" {
		query += " AND level = ?"
		args = append(args, level)
	}
	query += " ORDER BY created_at ASC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	// ... scan into []map[string]any and return
}
```

**Step 3: Commit**
```bash
git commit -m "feat: add GET /api/v1/admin/executions/{id}/logs endpoint"
```

---

### Task 4: Add Config Hash to Execution Metadata

**Files:**
- Modify: `/Users/jon/workspace/workflow/module/execution_tracker.go`
- Modify: `/Users/jon/workspace/workflow/engine.go`
- Test: `/Users/jon/workspace/workflow/module/execution_tracker_test.go`

**Context:** The engine's `BuildFromConfig()` in engine.go processes the YAML config. We need to compute SHA-256 of the config content, store it, and inject it into every execution's metadata for config-to-trace linking.

**Step 1: Implement config hash computation**

In `engine.go`, after loading config:
```go
import "crypto/sha256"

// Compute config hash
configBytes, _ := yaml.Marshal(cfg)
hash := sha256.Sum256(configBytes)
configHash := fmt.Sprintf("sha256:%x", hash)
```

Store `configHash` on the engine or pass to `ExecutionTracker`.

**Step 2: Inject into execution metadata**

In `ExecutionTracker`, add `ConfigHash string` field. In `TrackPipelineExecution()`, include it in metadata:
```go
meta := map[string]any{"config_hash": t.ConfigHash}
if explicitTrace {
    meta["explicit_trace"] = true
    meta["capture_io"] = true
}
metaJSON, _ := json.Marshal(meta)
```

**Step 3: Test and commit**
```bash
git commit -m "feat: include config hash in execution metadata for trace-to-config linking"
```

---

## Phase 2: Shared Component Extraction (@gocodealone/workflow-ui)

### Task 5: Extract Read-Only TraceCanvas Component

**Files:**
- Create: `/Users/jon/workspace/workflow-ui/src/components/TraceCanvas.tsx`
- Create: `/Users/jon/workspace/workflow-ui/src/components/TraceNodeOverlay.tsx`
- Modify: `/Users/jon/workspace/workflow-ui/src/index.ts` (add exports)
- Modify: `/Users/jon/workspace/workflow-ui/package.json` (add @xyflow/react peer dep)

**Context:** The full admin UI canvas is at `/Users/jon/workspace/workflow/ui/src/components/canvas/WorkflowCanvas.tsx` (574 lines). We need a simplified read-only version that takes nodes/edges + execution data and renders a non-interactive trace view.

**Step 1: Create TraceCanvas component**

```tsx
// TraceCanvas.tsx
import { ReactFlow, Background, Controls, MiniMap, BackgroundVariant } from '@xyflow/react';
import '@xyflow/react/dist/style.css';

export interface TraceStep {
  stepName: string;
  stepType: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
  durationMs?: number;
  inputData?: Record<string, unknown>;
  outputData?: Record<string, unknown>;
  errorMessage?: string;
  sequenceNum: number;
}

export interface TraceData {
  executionId: string;
  pipeline: string;
  status: string;
  steps: TraceStep[];
  configHash?: string;
  startedAt?: string;
  completedAt?: string;
}

interface TraceCanvasProps {
  nodes: Node[];
  edges: Edge[];
  traceData: TraceData;
  onStepClick?: (stepName: string) => void;
  nodeTypes?: Record<string, React.ComponentType>;
}

export function TraceCanvas({ nodes, edges, traceData, onStepClick, nodeTypes }: TraceCanvasProps) {
  // Apply execution overlay to nodes (status colors, duration badges)
  const traceNodes = applyTraceOverlay(nodes, traceData);
  // Highlight taken vs untaken edges
  const traceEdges = applyEdgeHighlighting(edges, traceData);

  return (
    <ReactFlow
      nodes={traceNodes}
      edges={traceEdges}
      nodeTypes={nodeTypes}
      onNodeClick={(_, node) => onStepClick?.(node.data?.stepName || node.id)}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable={true}
      fitView
    >
      <Background variant={BackgroundVariant.Dots} gap={20} color="#313244" />
      <Controls showInteractive={false} />
      <MiniMap pannable zoomable />
    </ReactFlow>
  );
}
```

**Step 2: Create TraceNodeOverlay**

```tsx
// TraceNodeOverlay.tsx — renders status badge + duration on each node
export function TraceNodeOverlay({ status, durationMs }: { status: string; durationMs?: number }) {
  const STATUS_COLORS = {
    completed: '#a6e3a1', failed: '#f38ba8', running: '#89b4fa',
    skipped: '#6c7086', pending: '#6c7086',
  };
  // Render colored dot + formatted duration
}
```

**Step 3: Update package.json**

Add `@xyflow/react` as optional peer dependency. Add `"./trace"` export map entry.

**Step 4: Build and verify**
```bash
cd /Users/jon/workspace/workflow-ui && npm run build
```

**Step 5: Commit**
```bash
git commit -m "feat: add TraceCanvas and TraceNodeOverlay shared components"
```

---

### Task 6: Extract StepDetailPanel Component

**Files:**
- Create: `/Users/jon/workspace/workflow-ui/src/components/StepDetailPanel.tsx`
- Create: `/Users/jon/workspace/workflow-ui/src/components/JsonTreeViewer.tsx`
- Modify: `/Users/jon/workspace/workflow-ui/src/index.ts`

**Context:** The admin UI's `ExecutionDetail.tsx` has a `JsonViewer` component. We extract and enhance it as a shared component.

**Step 1: Create JsonTreeViewer**

Expandable JSON tree with syntax highlighting, supporting collapsed/expanded nodes, copy-to-clipboard, and search. Based on the existing `JsonViewer` pattern from `ExecutionDetail.tsx` (line 40-86).

**Step 2: Create StepDetailPanel**

```tsx
export interface StepDetailPanelProps {
  step: TraceStep | null;
  onClose: () => void;
}

export function StepDetailPanel({ step, onClose }: StepDetailPanelProps) {
  if (!step) return null;
  return (
    <div style={{ /* right sidebar panel styles */ }}>
      <header>{step.stepName} <StatusBadge status={step.status} /></header>
      <section>
        <label>Type</label><span>{step.stepType}</span>
        <label>Duration</label><span>{formatDuration(step.durationMs)}</span>
        <label>Sequence</label><span>#{step.sequenceNum}</span>
      </section>
      {step.inputData && <JsonTreeViewer data={step.inputData} label="Input" />}
      {step.outputData && <JsonTreeViewer data={step.outputData} label="Output" />}
      {step.errorMessage && <div className="error">{step.errorMessage}</div>}
    </div>
  );
}
```

**Step 3: Commit**
```bash
git commit -m "feat: add StepDetailPanel and JsonTreeViewer shared components"
```

---

### Task 7: Extract ExecutionTimeline (Waterfall) Component

**Files:**
- Create: `/Users/jon/workspace/workflow-ui/src/components/ExecutionWaterfall.tsx`
- Modify: `/Users/jon/workspace/workflow-ui/src/index.ts`

**Context:** The admin UI has `ExecutionTimeline.tsx`. We create a reusable waterfall component.

**Step 1: Create ExecutionWaterfall**

Horizontal bar chart showing step timing. Each bar positioned by start offset, width = duration. Color-coded by status. Click selects step.

**Step 2: Commit**
```bash
git commit -m "feat: add ExecutionWaterfall shared component"
```

---

### Task 8: Extract ExecutionLogViewer Component

**Files:**
- Create: `/Users/jon/workspace/workflow-ui/src/components/ExecutionLogViewer.tsx`
- Modify: `/Users/jon/workspace/workflow-ui/src/index.ts`

**Context:** Chronological log entries with level coloring, filtering, and step linking.

**Step 1: Create ExecutionLogViewer**

```tsx
export interface LogEntry {
  id: number;
  level: string;
  message: string;
  moduleName: string;
  fields: Record<string, unknown>;
  createdAt: string;
}

interface ExecutionLogViewerProps {
  logs: LogEntry[];
  onStepClick?: (stepName: string) => void;
  filter?: { level?: string; search?: string };
}
```

**Step 2: Commit**
```bash
git commit -m "feat: add ExecutionLogViewer shared component"
```

---

### Task 9: Publish @gocodealone/workflow-ui Update

**Files:**
- Modify: `/Users/jon/workspace/workflow-ui/package.json` (bump version)
- Run: npm publish

**Step 1: Update version to 0.2.0**

**Step 2: Update export map**

Add new export paths:
```json
{
  "./trace": "./src/components/TraceCanvas.tsx",
  "./components/StepDetailPanel": "./src/components/StepDetailPanel.tsx",
  "./components/ExecutionWaterfall": "./src/components/ExecutionWaterfall.tsx",
  "./components/ExecutionLogViewer": "./src/components/ExecutionLogViewer.tsx",
  "./components/JsonTreeViewer": "./src/components/JsonTreeViewer.tsx"
}
```

**Step 3: Build and publish**
```bash
cd /Users/jon/workspace/workflow-ui
npm run build
npm publish
```

**Step 4: Commit and tag**
```bash
git commit -m "feat: v0.2.0 — add trace canvas and observability shared components"
git tag v0.2.0
git push origin main --tags
```

---

## Phase 3: Admin UI Trace View (workflow/ui)

### Task 10: Add Trace View Page to Admin UI

**Files:**
- Create: `/Users/jon/workspace/workflow/ui/src/components/trace/TraceView.tsx`
- Modify: `/Users/jon/workspace/workflow/ui/src/App.tsx` (add to VIEW_REGISTRY)
- Modify: `/Users/jon/workspace/workflow/ui/package.json` (update @gocodealone/workflow-ui to ^0.2.0)

**Context:** The admin UI's `App.tsx` has a `VIEW_REGISTRY` (line ~30) that maps view IDs to React components. The sidebar navigation is plugin-driven from `pluginStore.ts`.

**Step 1: Create TraceView page**

Wraps the shared `TraceCanvas` with admin-specific data fetching:

```tsx
import { TraceCanvas, StepDetailPanel, ExecutionWaterfall, ExecutionLogViewer } from '@gocodealone/workflow-ui';
import useObservabilityStore from '../../store/observabilityStore';
import { configToNodes } from '../../utils/serialization';

export default function TraceView() {
  const { selectedExecution, executionSteps } = useObservabilityStore();
  const [selectedStep, setSelectedStep] = useState<string | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);

  // Fetch execution detail + steps + config
  // Parse config YAML → ReactFlow nodes/edges via configToNodes()
  // Map execution steps to TraceData
  // Render: TraceCanvas (top), ExecutionWaterfall (middle), LogViewer (bottom)
  // StepDetailPanel as right sidebar on step click
}
```

**Step 2: Register in VIEW_REGISTRY**

In `App.tsx`:
```tsx
import TraceView from './components/trace/TraceView';

const VIEW_REGISTRY = {
  // ... existing entries ...
  trace: TraceView,
};
```

**Step 3: Commit**
```bash
cd /Users/jon/workspace/workflow/ui
git commit -m "feat: add trace visualization view to admin UI"
```

---

### Task 11: Add "Trace Request" Button to Execution Dashboard

**Files:**
- Modify: `/Users/jon/workspace/workflow/ui/src/components/dashboard/WorkflowDashboard.tsx`
- Modify: `/Users/jon/workspace/workflow/ui/src/utils/api.ts`

**Context:** `WorkflowDashboard.tsx` already has a "Trigger Execution" button. We add a "Trace Request" button that sends the request with `X-Workflow-Trace: true` header and navigates to the trace view on completion.

**Step 1: Add API function**

In `api.ts`:
```typescript
export async function apiTriggerTracedExecution(workflowId: string): Promise<WorkflowExecution> {
  return apiFetch(`/api/v1/admin/workflows/${workflowId}/execute`, {
    method: 'POST',
    headers: { 'X-Workflow-Trace': 'true' },
  });
}
```

**Step 2: Add button to WorkflowDashboard**

Next to "Trigger Execution", add "Trace Request" button. On click: call `apiTriggerTracedExecution`, then navigate to `trace` view with the execution ID.

**Step 3: Commit**
```bash
git commit -m "feat: add Trace Request button to execution dashboard"
```

---

### Task 12: Wire Trace Navigation from Execution List

**Files:**
- Modify: `/Users/jon/workspace/workflow/ui/src/components/executions/ExecutionDetail.tsx`

**Context:** `ExecutionDetail.tsx` shows step details for an execution. Add a "View Trace" link that opens the TraceView for explicitly-traced executions (those with `metadata.explicit_trace = true`).

**Step 1: Add "View Trace" button** when execution metadata contains `explicit_trace: true`.

**Step 2: Commit**
```bash
git commit -m "feat: add View Trace link on explicitly-traced executions"
```

---

## Phase 4: Cloud Backend (workflow-cloud)

### Task 13: Add Execution Tracking Routes to Cloud

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_execution_list.go`
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_execution_detail.go`
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_execution_logs.go`
- Modify: `/Users/jon/workspace/workflow-cloud/cloud.yaml` (add routes)
- Test: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_execution_list_test.go`

**Context:** Cloud needs admin-equivalent execution tracking APIs, scoped to tenants. These are new pipeline steps (not shared with admin, because cloud uses PostgreSQL and has tenant isolation).

**Step 1: Create step_execution_list.go**

Pipeline step `step.tenant_executions` that queries `workflow_executions` table for a tenant's deployed engine:

```go
type TenantExecutionListStep struct{}

func (s *TenantExecutionListStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
    tenantID := pc.Current["auth_tenant_id"].(string)
    // Query executions from tenant's engine store
    // Support filters: status, pipeline, since, until, limit, offset
    // Return {executions: [...], count: N}
}
```

**Step 2: Add cloud.yaml routes**

```yaml
- method: GET
  path: "/api/v1/me/executions"
  handler: cloud-commands
  middlewares: *auth_mw
  pipeline:
    steps:
      - name: list-executions
        type: step.tenant_executions
```

Similarly for detail, timeline, and logs endpoints.

**Step 3: Test and commit**
```bash
cd /Users/jon/workspace/workflow-cloud
go test ./cloudplugin/ -run TestTenantExecutionList -v
git commit -m "feat: add tenant-scoped execution tracking routes"
```

---

### Task 14: Add Sampling Configuration to Cloud

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_tracing_config.go`
- Modify: `/Users/jon/workspace/workflow-cloud/cloud.yaml`
- Create: `/Users/jon/workspace/workflow-cloud/migrations/013_tracing_config.sql`
- Test: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_tracing_config_test.go`

**Context:** Cloud tenants configure sampling rate (0.01–1.0) for their deployed engines. This is stored in a new `tracing_config` table.

**Step 1: Create migration**

```sql
-- 013_tracing_config.sql
CREATE TABLE IF NOT EXISTS tracing_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES cloud_tenants(id) ON DELETE CASCADE,
    sampling_rate NUMERIC(5,4) NOT NULL DEFAULT 1.0,
    capture_step_io BOOLEAN NOT NULL DEFAULT false,
    max_io_size INTEGER NOT NULL DEFAULT 10240,
    always_sample_errors BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id)
);
```

**Step 2: Create step for CRUD**

`step.tracing_config_get` and `step.tracing_config_update` pipeline steps.

**Step 3: Add routes**

```yaml
- method: GET
  path: "/api/v1/me/tracing/config"
  # ...
- method: PUT
  path: "/api/v1/me/tracing/config"
  # ...
```

**Step 4: Test and commit**
```bash
git commit -m "feat: add sampling configuration for cloud tenants"
```

---

### Task 15: Add Config Version Store to Cloud

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_config_versions.go`
- Create: `/Users/jon/workspace/workflow-cloud/migrations/014_config_versions.sql`
- Modify: `/Users/jon/workspace/workflow-cloud/cloud.yaml`
- Test: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_config_versions_test.go`

**Context:** Content-addressed config version store, linked to tenant executions.

**Step 1: Create migration**

```sql
-- 014_config_versions.sql
CREATE TABLE IF NOT EXISTS config_versions (
    hash TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES cloud_tenants(id),
    config_yaml TEXT NOT NULL,
    source_files JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB DEFAULT '{}'
);
CREATE INDEX idx_config_versions_tenant ON config_versions(tenant_id, created_at DESC);
```

**Step 2: Create steps** for listing versions, getting by hash, diffing two versions.

**Step 3: Add routes and test**

**Step 4: Commit**
```bash
git commit -m "feat: add config version store for tenant config tracking"
```

---

### Task 16: Add Data Retention Policies to Cloud

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_retention.go`
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_retention_purge.go`
- Create: `/Users/jon/workspace/workflow-cloud/migrations/015_retention_policies.sql`
- Modify: `/Users/jon/workspace/workflow-cloud/cloud.yaml` (add routes + scheduled trigger)
- Test: `/Users/jon/workspace/workflow-cloud/cloudplugin/step_retention_test.go`

**Context:** Tenants configure retention period (days). A scheduled trigger runs daily to purge expired trace data.

**Step 1: Create migration**

```sql
-- 015_retention_policies.sql
CREATE TABLE IF NOT EXISTS trace_retention_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES cloud_tenants(id) ON DELETE CASCADE,
    retention_days INTEGER NOT NULL DEFAULT 30,
    max_traces INTEGER,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_purge_at TIMESTAMPTZ,
    purged_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id)
);
```

**Step 2: Create CRUD step** (`step.retention_config`)

**Step 3: Create purge step** (`step.retention_purge`)

Deletes `workflow_executions` (cascading to steps + logs) older than `retention_days`. Updates `last_purge_at` and `purged_count`.

```go
func (s *RetentionPurgeStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
    // Query all enabled retention policies
    // For each: DELETE FROM workflow_executions WHERE tenant_id = ? AND started_at < NOW() - retention_days
    // Also enforce max_traces cap if set
    // Return {purged_tenants: N, purged_executions: N}
}
```

**Step 4: Add scheduled trigger**

In `cloud.yaml`:
```yaml
triggers:
  - name: retention-purge
    type: schedule
    config:
      cronExpression: "0 3 * * *"  # daily at 3am
    pipeline:
      steps:
        - name: purge
          type: step.retention_purge
```

**Step 5: Add API routes for CRUD**

```yaml
- method: GET
  path: "/api/v1/me/tracing/retention"
- method: PUT
  path: "/api/v1/me/tracing/retention"
```

**Step 6: Test and commit**
```bash
git commit -m "feat: add data retention policies with scheduled purge"
```

---

### Task 17: Add PII Redaction Hook to Cloud

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/trace_redactor.go`
- Test: `/Users/jon/workspace/workflow-cloud/cloudplugin/trace_redactor_test.go`

**Context:** Optional PII/PHI redaction before storing step I/O. If data-protection plugin is loaded, use its local PII detector. Otherwise, apply basic regex patterns.

**Step 1: Create TraceRedactor**

```go
type TraceRedactor struct {
    patterns []PIIPattern // email, SSN, credit card, phone
}

func (r *TraceRedactor) Redact(data map[string]any) map[string]any {
    // Walk all string values recursively
    // Apply regex patterns, replace matches with [REDACTED]
    // Return sanitized copy (don't mutate original)
}
```

Built-in patterns: email, SSN, credit card (Luhn), US phone.

**Step 2: Integrate** into execution step I/O capture.

**Step 3: Test and commit**
```bash
git commit -m "feat: add PII redaction for trace step I/O data"
```

---

## Phase 5: Cloud UI Enhancement (workflow-cloud-ui)

### Task 18: Add @gocodealone/workflow-ui Dependency

**Files:**
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/package.json`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/vite.config.ts` (if needed for optimizeDeps)

**Step 1:** Add dependency:
```bash
cd /Users/jon/workspace/workflow-cloud-ui/ui
npm install @gocodealone/workflow-ui@^0.2.0 @xyflow/react@^12.10.0 dagre@^0.8.5
```

**Step 2:** Verify build passes.

**Step 3: Commit**
```bash
git commit -m "feat: add workflow-ui and ReactFlow dependencies to cloud-ui"
```

---

### Task 19: Add Executions Page to Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/src/pages/ExecutionsPage.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/App.tsx` (add route)
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/components/Sidebar.tsx` (add nav item)
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/api.ts` (add fetch functions)

**Context:** Cloud UI needs a page to browse historical executions with filtering.

**Step 1: Add API functions**

```typescript
export async function getMyExecutions(filter?: ExecutionFilter) {
  const params = new URLSearchParams();
  if (filter?.status) params.set('status', filter.status);
  if (filter?.pipeline) params.set('pipeline', filter.pipeline);
  if (filter?.limit) params.set('limit', String(filter.limit));
  return apiFetch(`/api/v1/me/executions?${params}`);
}

export async function getExecutionDetail(executionId: string) {
  return apiFetch(`/api/v1/me/executions/${executionId}/timeline`);
}

export async function getExecutionLogs(executionId: string, level?: string) {
  const params = level ? `?level=${level}` : '';
  return apiFetch(`/api/v1/me/executions/${executionId}/logs${params}`);
}
```

**Step 2: Create ExecutionsPage**

Table with: Execution ID (truncated), Pipeline, Status (badge), Duration, Started At.
Filters: status dropdown, pipeline text search, date range.
Click row → navigate to trace detail.

**Step 3: Add to routing and sidebar**

**Step 4: Commit**
```bash
git commit -m "feat: add executions page to cloud UI"
```

---

### Task 20: Add Trace Detail Page to Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/src/pages/TraceDetailPage.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/App.tsx` (add route)

**Context:** Uses shared `TraceCanvas`, `StepDetailPanel`, `ExecutionWaterfall`, `ExecutionLogViewer` from @gocodealone/workflow-ui.

**Step 1: Create TraceDetailPage**

```tsx
import { TraceCanvas, StepDetailPanel, ExecutionWaterfall, ExecutionLogViewer } from '@gocodealone/workflow-ui/trace';

export default function TraceDetailPage() {
  const { executionId } = useParams();
  // Fetch execution timeline, config, logs
  // Parse config → nodes/edges (simplified: just render steps as sequential nodes)
  // Render TraceCanvas + StepDetailPanel + ExecutionWaterfall + ExecutionLogViewer
}
```

**Step 2: Add route**: `/executions/:executionId`

**Step 3: Commit**
```bash
git commit -m "feat: add trace detail page with ReactFlow canvas to cloud UI"
```

---

### Task 21: Add Config Viewer Page to Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/src/pages/ConfigViewerPage.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/App.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/components/Sidebar.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/api.ts`

**Context:** Read-only view of the deployed workflow config as a ReactFlow graph. Lists config versions with diff capability.

**Step 1: Add API functions**

```typescript
export async function getConfigVersions() {
  return apiFetch('/api/v1/me/config-versions');
}
export async function getConfigVersion(hash: string) {
  return apiFetch(`/api/v1/me/config-versions/${hash}`);
}
```

**Step 2: Create ConfigViewerPage**

- Version list (hash, created_at)
- Click version → read-only ReactFlow canvas of that config
- Diff button between two selected versions

**Step 3: Commit**
```bash
git commit -m "feat: add config viewer page with version history to cloud UI"
```

---

### Task 22: Add Tracing Settings Page to Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/src/pages/TracingSettingsPage.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/App.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/components/Sidebar.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/api.ts`

**Context:** Settings page for sampling rate, step I/O capture toggle, and data retention policy.

**Step 1: Create TracingSettingsPage**

- Sampling rate slider (0% → 100%)
- Toggle: "Capture step inputs/outputs"
- Max I/O size input
- Toggle: "Always sample errors"
- Retention days input
- Max traces input
- Enable/disable retention toggle
- Last purge info display

**Step 2: Add API functions for tracing config + retention CRUD**

**Step 3: Commit**
```bash
git commit -m "feat: add tracing settings page to cloud UI"
```

---

### Task 23: Add Admin-Equivalent Log Viewer to Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/src/pages/LogsPage.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/App.tsx`
- Modify: `/Users/jon/workspace/workflow-cloud-ui/ui/src/components/Sidebar.tsx`

**Context:** Uses shared `ExecutionLogViewer`. Adds cloud-specific log streaming via SSE.

**Step 1: Create LogsPage** using shared component.

**Step 2: Commit**
```bash
git commit -m "feat: add log viewer page to cloud UI"
```

---

## Phase 6: Testing (Critical — Stringent QA)

### Task 24: Go Unit Tests for Engine Tracing

**Files:**
- Create: `/Users/jon/workspace/workflow/module/trace_capture_test.go`

**Tests:**
- `TestExplicitTraceHeader_Detected` — header present → flag set
- `TestExplicitTraceHeader_Missing` — no header → flag not set
- `TestStepIO_CapturedWhenExplicit` — I/O stored in execution_steps
- `TestStepIO_NotCapturedWhenNotExplicit` — I/O not stored normally
- `TestStepIO_TruncatedAt10KB` — large outputs truncated
- `TestConfigHash_Deterministic` — same config → same hash
- `TestConfigHash_InExecutionMetadata` — hash appears in metadata

```bash
cd /Users/jon/workspace/workflow && go test ./module/ -run TestExplicitTrace -v -race
cd /Users/jon/workspace/workflow && go test ./module/ -run TestStepIO -v -race
cd /Users/jon/workspace/workflow && go test ./module/ -run TestConfigHash -v -race
```

**Commit:**
```bash
git commit -m "test: comprehensive unit tests for trace capture"
```

---

### Task 25: Go Unit Tests for Cloud Tracing

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/tracing_test.go`

**Tests:**
- `TestTenantExecutionList_FiltersByStatus`
- `TestTenantExecutionList_FiltersByDateRange`
- `TestTenantExecutionList_Pagination`
- `TestTracingConfig_CRUD`
- `TestSamplingRate_ValidRange` — 0.0-1.0
- `TestConfigVersionStore_Dedup` — same content → same hash
- `TestConfigVersionStore_Diff`
- `TestRetentionPurge_DeletesExpired`
- `TestRetentionPurge_RespectsMaxTraces`
- `TestRetentionPurge_SkipsDisabled`
- `TestTraceRedactor_MasksEmail`
- `TestTraceRedactor_MasksSSN`
- `TestTraceRedactor_MasksCreditCard`
- `TestTraceRedactor_PreservesNonSensitive`

```bash
cd /Users/jon/workspace/workflow-cloud && go test ./cloudplugin/ -run TestTenant -v -race
cd /Users/jon/workspace/workflow-cloud && go test ./cloudplugin/ -run TestTracing -v -race
cd /Users/jon/workspace/workflow-cloud && go test ./cloudplugin/ -run TestRetention -v -race
cd /Users/jon/workspace/workflow-cloud && go test ./cloudplugin/ -run TestTraceRedactor -v -race
```

**Commit:**
```bash
git commit -m "test: comprehensive unit tests for cloud tracing features"
```

---

### Task 26: API Integration Tests

**Files:**
- Create: `/Users/jon/workspace/workflow/store/timeline_handler_test.go` (expand)
- Create: `/Users/jon/workspace/workflow-cloud/cloudplugin/api_tracing_test.go`

**Tests (engine):**
- `TestAPI_ExecutionLogs_LevelFilter` — GET /api/v1/admin/executions/{id}/logs?level=error
- `TestAPI_ExplicitTrace_EndToEnd` — POST with X-Workflow-Trace → GET timeline → verify I/O

**Tests (cloud):**
- `TestAPI_TenantExecutions_Auth` — unauthenticated → 401
- `TestAPI_TenantExecutions_TenantIsolation` — tenant A can't see tenant B
- `TestAPI_TracingConfig_Update` — PUT and verify
- `TestAPI_RetentionConfig_CRUD` — full lifecycle
- `TestAPI_ConfigVersions_ListAndGet` — version listing and hash lookup

```bash
cd /Users/jon/workspace/workflow && go test ./store/ -run TestAPI -v
cd /Users/jon/workspace/workflow-cloud && go test ./cloudplugin/ -run TestAPI -v
```

**Commit:**
```bash
git commit -m "test: API integration tests for tracing endpoints"
```

---

### Task 27: Playwright Tests for Admin UI Trace

**Files:**
- Create: `/Users/jon/workspace/workflow/ui/tests/trace.spec.ts`

**Setup:** Need a running workflow server with admin plugin. Use `playwright-cli` in headless mode.

**Tests:**
```typescript
test.describe('Admin Trace View', () => {
  test('trace request button triggers traced execution', async ({ page }) => {
    // Navigate to workflow dashboard
    // Click "Trace Request" button
    // Wait for execution to complete
    // Verify navigation to trace view
  });

  test('trace canvas renders nodes from config', async ({ page }) => {
    // Navigate to trace view for a traced execution
    // Verify ReactFlow canvas is visible
    // Verify nodes exist matching pipeline steps
  });

  test('clicking step opens detail panel', async ({ page }) => {
    // Click on a step node in the canvas
    // Verify detail panel opens
    // Verify input/output data is displayed
  });

  test('execution path is highlighted', async ({ page }) => {
    // Verify taken path edges are bold/colored
    // Verify untaken edges are dimmed
  });

  test('waterfall shows step timing', async ({ page }) => {
    // Verify waterfall bars are rendered
    // Verify bar widths correlate to duration
  });

  test('log viewer shows execution logs', async ({ page }) => {
    // Verify log entries are displayed
    // Test level filter (error only)
    // Click log entry → step highlighted in canvas
  });

  test('read-only mode prevents editing', async ({ page }) => {
    // Verify nodes cannot be dragged
    // Verify no connect handles appear
    // Verify no context menus for editing
  });
});
```

Run: `cd /Users/jon/workspace/workflow/ui && npx playwright test tests/trace.spec.ts --headed`

**Commit:**
```bash
git commit -m "test: Playwright tests for admin UI trace visualization"
```

---

### Task 28: Playwright Tests for Cloud UI

**Files:**
- Create: `/Users/jon/workspace/workflow-cloud-ui/ui/tests/tracing.spec.ts`

**Tests:**
```typescript
test.describe('Cloud Tracing', () => {
  test('executions page shows historical list', async ({ page }) => {
    // Login → navigate to executions
    // Verify table renders with columns
    // Test status filter
    // Test pagination
  });

  test('trace detail loads canvas', async ({ page }) => {
    // Click execution row → trace detail page
    // Verify ReactFlow canvas renders
    // Verify steps are displayed
  });

  test('step detail panel shows I/O data', async ({ page }) => {
    // Click step → verify panel
    // Verify JSON viewer for input/output
    // Verify error display for failed steps
  });

  test('config viewer shows versions', async ({ page }) => {
    // Navigate to config viewer
    // Verify version list renders
    // Click version → canvas shows config
  });

  test('tracing settings page works', async ({ page }) => {
    // Navigate to tracing settings
    // Change sampling rate → save → verify
    // Toggle step I/O capture → save → verify
    // Set retention days → save → verify
  });

  test('log viewer shows logs with filtering', async ({ page }) => {
    // Navigate to logs page
    // Verify entries render
    // Filter by error level → only errors shown
  });

  test('retention purge info displays', async ({ page }) => {
    // Check tracing settings page
    // Verify last purge timestamp shown
    // Verify purged count shown
  });
});
```

Run: `cd /Users/jon/workspace/workflow-cloud-ui/ui && npx playwright test tests/tracing.spec.ts --headed`

**Commit:**
```bash
git commit -m "test: Playwright tests for cloud UI tracing features"
```

---

## Phase 7: Integration & Deployment

### Task 29: Update Ratchet Config with Tracing

**Files:**
- Modify: `/Users/jon/workspace/ratchet/config/modules.yaml` (update otel config)

**Context:** Ratchet already has `observability.otel` module. Just verify that explicit tracing works with X-Workflow-Trace header.

**Step 1: Test**
```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Workflow-Trace: true" http://localhost:9090/api/tasks | jq
# Then verify execution was traced via admin API
```

**Step 2: Commit if config changes needed**

---

### Task 30: Build and Deploy Cloud v28

**Steps:**
1. Build workflow-cloud with new cloudplugin changes
2. Build workflow-cloud-ui with new pages
3. Deploy to minikube

```bash
cd /Users/jon/workspace/workflow-cloud
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/workflow-cloud-linux ./cmd/server/
# Build cloud-ui
cd /Users/jon/workspace/workflow-cloud-ui
make build
# Docker build + deploy
```

Use `wfctl` where applicable for deployment validation.

**Step 3: Verify endpoints**
```bash
curl -sf http://localhost:18081/api/v1/me/executions
curl -sf http://localhost:18081/api/v1/me/tracing/config
curl -sf http://localhost:18081/api/v1/me/tracing/retention
curl -sf http://localhost:18081/api/v1/me/config-versions
```

---

## Parallelism Map

- **Phase 1 (Tasks 1-4)**: Sequential within phase (each builds on prior)
- **Phase 2 (Tasks 5-9)**: Tasks 5-8 parallel (independent components), Task 9 after all
- **Phase 3 (Tasks 10-12)**: Sequential (trace view → trace button → navigation wiring)
- **Phase 4 (Tasks 13-17)**: Tasks 13-16 partially parallel (different DB migrations, different steps), Task 17 independent
- **Phase 5 (Tasks 18-23)**: Task 18 first (add deps), then Tasks 19-23 partially parallel (independent pages)
- **Phase 6 (Tasks 24-28)**: Tasks 24-25 parallel (different repos), Tasks 27-28 parallel (different UIs)
- **Phase 7 (Tasks 29-30)**: Sequential

**Agent team sizing:** 2 implementers (one for engine+admin, one for cloud+cloud-ui) + shared component extraction can be done by either

---

## Key Reference Files

| File | Repo | Purpose |
|------|------|---------|
| `module/execution_tracker.go` | workflow | Main instrumentation point |
| `module/pipeline_executor.go` | workflow | Pipeline execution + event recording |
| `module/api_v1_store.go` | workflow | V1Store schema + execution tracking methods |
| `store/event_store.go` | workflow | Event sourcing store |
| `store/timeline_handler.go` | workflow | Timeline API endpoints |
| `observability/tracing/provider.go` | workflow | OTEL provider setup |
| `ui/src/components/canvas/WorkflowCanvas.tsx` | workflow | ReactFlow canvas (reference for TraceCanvas) |
| `ui/src/store/observabilityStore.ts` | workflow | Execution/log state management |
| `ui/src/utils/serialization.ts` | workflow | Config YAML → ReactFlow nodes/edges |
| `ui/src/components/executions/ExecutionDetail.tsx` | workflow | Existing execution detail (reference) |
| `cloudplugin/step_tenant_deploy.go` | workflow-cloud | Tenant deployment pattern |
| `cloudplugin/step_audit_log.go` | workflow-cloud | Audit log pattern (reference) |
| `ui/src/pages/DashboardPage.tsx` | workflow-cloud-ui | Cloud UI page pattern |
| `ui/src/api.ts` | workflow-cloud-ui | Cloud API client pattern |
