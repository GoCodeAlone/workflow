import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import TraceView from './TraceView.tsx';
import { toTraceStep, toLogEntry } from './traceUtils.ts';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { TraceStep } from '@gocodealone/workflow-ui/trace';

// --- Module mocks ---

vi.mock('@gocodealone/workflow-ui/trace', () => ({
  TraceCanvas: ({
    nodes,
    onStepClick,
  }: {
    nodes: unknown[];
    onStepClick?: (step: TraceStep) => void;
  }) =>
    nodes.length > 0 ? (
      <div data-testid="trace-canvas">
        <button
          data-testid="canvas-step-btn"
          onClick={() =>
            onStepClick?.({
              stepName: 'step1',
              stepType: 'test',
              status: 'completed',
              sequenceNum: 1,
            } as TraceStep)
          }
        >
          click step
        </button>
      </div>
    ) : null,
  ExecutionWaterfall: ({
    onStepClick,
  }: {
    steps: unknown[];
    onStepClick?: (name: string) => void;
  }) => (
    <div data-testid="execution-waterfall">
      <button
        data-testid="waterfall-step-btn"
        onClick={() => onStepClick?.('step1')}
      >
        waterfall step
      </button>
    </div>
  ),
  ExecutionLogViewer: () => <div data-testid="execution-log-viewer" />,
  StepDetailPanel: ({
    step,
    onClose,
  }: {
    step: TraceStep;
    onClose: () => void;
  }) => (
    <div data-testid="step-detail-panel">
      <span data-testid="panel-step-name">{step.stepName}</span>
      <button data-testid="panel-close-btn" onClick={onClose}>
        close
      </button>
    </div>
  ),
}));

vi.mock('@gocodealone/workflow-editor/stores', () => ({
  useWorkflowStore: vi.fn(() => null), // activeWorkflowRecord = null
}));

vi.mock('../../utils/api.ts', () => ({
  apiGetExecutionLogs: vi.fn().mockResolvedValue([]),
  // Return truthy config_yaml so the loadConfig effect can proceed
  apiGetWorkflow: vi.fn().mockResolvedValue({ config_yaml: 'modules: []' }),
  apiFetchDashboard: vi.fn(),
  apiFetchWorkflowDashboard: vi.fn(),
  apiFetchExecutions: vi.fn(),
  apiFetchExecutionDetail: vi.fn().mockResolvedValue(null),
  apiFetchExecutionSteps: vi.fn().mockResolvedValue([]),
  apiFetchLogs: vi.fn(),
  apiFetchEvents: vi.fn(),
  apiTriggerExecution: vi.fn(),
  apiCancelExecution: vi.fn(),
  apiFetchIAMProviders: vi.fn(),
  apiCreateIAMProvider: vi.fn(),
  apiUpdateIAMProvider: vi.fn(),
  apiDeleteIAMProvider: vi.fn(),
  apiTestIAMProvider: vi.fn(),
  apiFetchIAMRoleMappings: vi.fn(),
  apiCreateIAMRoleMapping: vi.fn(),
  apiDeleteIAMRoleMapping: vi.fn(),
  createLogStream: vi.fn(),
  createEventStream: vi.fn(),
  apiFetchRuntimeInstances: vi.fn().mockResolvedValue({ instances: [], total: 0 }),
  apiStopRuntimeInstance: vi.fn(),
}));

vi.mock('@gocodealone/workflow-editor/utils', () => ({
  parseYaml: vi.fn().mockReturnValue({}),
  configToNodes: vi.fn().mockReturnValue({ nodes: [], edges: [] }),
}));

import { configToNodes } from '@gocodealone/workflow-editor/utils';

// --- Store reset ---

function resetStore() {
  useObservabilityStore.setState({
    activeView: 'executions',
    selectedWorkflowId: 'wf-1',
    selectedTraceExecutionId: null,
    systemDashboard: null,
    workflowDashboard: null,
    dashboardLoading: false,
    executions: [],
    selectedExecution: null,
    executionSteps: [],
    executionFilter: {},
    logEntries: [],
    logFilter: {},
    logStreaming: false,
    events: [],
    eventStreaming: false,
    iamProviders: [],
    iamMappings: {},
  });
}

// --- TraceView component tests ---

describe('TraceView', () => {
  beforeEach(() => {
    resetStore();
    vi.clearAllMocks();
    // Restore default empty-nodes mock after each test
    vi.mocked(configToNodes).mockReturnValue({ nodes: [], edges: [] });
  });

  it('renders the back button', () => {
    render(<TraceView executionId="exec-123" />);
    expect(screen.getByText('← Back')).toBeInTheDocument();
  });

  it('calls setSelectedTraceExecutionId(null) when back button is clicked', () => {
    // Pre-set a trace execution ID to verify it gets cleared
    useObservabilityStore.setState({ selectedTraceExecutionId: 'exec-123' });

    render(<TraceView executionId="exec-123" />);
    fireEvent.click(screen.getByText('← Back'));

    expect(useObservabilityStore.getState().selectedTraceExecutionId).toBeNull();
  });

  it('shows "No workflow config loaded" fallback when nodes is empty', () => {
    // configToNodes returns empty nodes by default → fallback renders
    render(<TraceView executionId="exec-123" />);
    expect(screen.getByText('No workflow config loaded')).toBeInTheDocument();
  });

  it('handleStepClick selects a step on first click (via TraceCanvas)', async () => {
    // Make configToNodes return non-empty nodes so TraceCanvas renders
    vi.mocked(configToNodes).mockReturnValue({
      nodes: [{ id: 'n1', type: 'default', position: { x: 0, y: 0 }, data: { moduleType: 'http.server', label: 'n1', config: {} } }],
      edges: [],
    });

    render(<TraceView executionId="exec-123" />);

    // Wait for the async loadConfig effect to set nodes and render TraceCanvas
    await waitFor(() => {
      expect(screen.getByTestId('trace-canvas')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('canvas-step-btn'));
    expect(screen.getByTestId('step-detail-panel')).toBeInTheDocument();
    expect(screen.getByTestId('panel-step-name').textContent).toBe('step1');
  });

  it('handleStepClick toggles: clicking same step twice deselects it (via TraceCanvas)', async () => {
    vi.mocked(configToNodes).mockReturnValue({
      nodes: [{ id: 'n1', type: 'default', position: { x: 0, y: 0 }, data: { moduleType: 'http.server', label: 'n1', config: {} } }],
      edges: [],
    });

    render(<TraceView executionId="exec-123" />);

    await waitFor(() => {
      expect(screen.getByTestId('trace-canvas')).toBeInTheDocument();
    });

    const btn = screen.getByTestId('canvas-step-btn');
    fireEvent.click(btn); // select
    expect(screen.getByTestId('step-detail-panel')).toBeInTheDocument();

    fireEvent.click(btn); // same step → deselect
    expect(screen.queryByTestId('step-detail-panel')).not.toBeInTheDocument();
  });

  it('handleWaterfallStepClick finds and sets step from executionSteps', () => {
    useObservabilityStore.setState({
      executionSteps: [
        {
          id: 's-1',
          execution_id: 'exec-123',
          step_name: 'step1',
          step_type: 'http.call',
          status: 'completed',
          sequence_num: 1,
          duration_ms: 50,
          started_at: '2026-01-01T00:00:00Z',
        },
      ],
    });

    render(<TraceView executionId="exec-123" />);
    fireEvent.click(screen.getByTestId('waterfall-step-btn'));

    expect(screen.getByTestId('step-detail-panel')).toBeInTheDocument();
    expect(screen.getByTestId('panel-step-name').textContent).toBe('step1');
  });

  it('handleWaterfallStepClick toggles: clicking same step name twice deselects', () => {
    useObservabilityStore.setState({
      executionSteps: [
        {
          id: 's-1',
          execution_id: 'exec-123',
          step_name: 'step1',
          step_type: 'http.call',
          status: 'completed',
          sequence_num: 1,
        },
      ],
    });

    render(<TraceView executionId="exec-123" />);
    const btn = screen.getByTestId('waterfall-step-btn');

    fireEvent.click(btn); // select
    expect(screen.getByTestId('step-detail-panel')).toBeInTheDocument();

    fireEvent.click(btn); // same step → deselect
    expect(screen.queryByTestId('step-detail-panel')).not.toBeInTheDocument();
  });

  it('StepDetailPanel onClose clears selected step', () => {
    useObservabilityStore.setState({
      executionSteps: [
        {
          id: 's-1',
          execution_id: 'exec-123',
          step_name: 'step1',
          step_type: 'http.call',
          status: 'completed',
          sequence_num: 1,
        },
      ],
    });

    render(<TraceView executionId="exec-123" />);
    fireEvent.click(screen.getByTestId('waterfall-step-btn')); // select
    fireEvent.click(screen.getByTestId('panel-close-btn')); // close
    expect(screen.queryByTestId('step-detail-panel')).not.toBeInTheDocument();
  });
});

// --- toTraceStep unit tests ---

describe('toTraceStep', () => {
  it('maps snake_case fields to camelCase', () => {
    const step = {
      id: 's-1',
      execution_id: 'exec-1',
      step_name: 'my-step',
      step_type: 'http.call',
      status: 'completed' as const,
      sequence_num: 3,
      duration_ms: 120,
      started_at: '2026-01-01T00:00:00Z',
      input_data: { key: 'value' },
      output_data: { result: 42 },
      error_message: undefined,
    };

    const result = toTraceStep(step);

    expect(result.stepName).toBe('my-step');
    expect(result.stepType).toBe('http.call');
    expect(result.status).toBe('completed');
    expect(result.sequenceNum).toBe(3);
    expect(result.durationMs).toBe(120);
    expect(result.inputData).toEqual({ key: 'value' });
    expect(result.outputData).toEqual({ result: 42 });
  });

  it('handles undefined optional fields', () => {
    const step = {
      id: 's-2',
      execution_id: 'exec-1',
      step_name: 'step2',
      step_type: 'set',
      status: 'failed' as const,
      sequence_num: 1,
    };

    const result = toTraceStep(step);

    expect(result.durationMs).toBeUndefined();
    expect(result.inputData).toBeUndefined();
    expect(result.outputData).toBeUndefined();
    expect(result.errorMessage).toBeUndefined();
  });
});

// --- toLogEntry unit tests ---

describe('toLogEntry', () => {
  it('maps snake_case fields to camelCase', () => {
    const log = {
      id: 7,
      workflow_id: 'wf-1',
      execution_id: 'exec-1',
      level: 'info' as const,
      message: 'Step started',
      module_name: 'step1',
      fields: { elapsed: '5ms' },
      created_at: '2026-01-01T00:00:01Z',
    };

    const result = toLogEntry(log);

    expect(result.id).toBe('7');
    expect(result.level).toBe('info');
    expect(result.message).toBe('Step started');
    expect(result.moduleName).toBe('step1');
    expect(result.fields).toEqual({ elapsed: '5ms' });
    expect(result.createdAt).toBe('2026-01-01T00:00:01Z');
  });

  it('maps "fatal" level to "error"', () => {
    const log = {
      id: 1,
      workflow_id: 'wf-1',
      level: 'fatal' as const,
      message: 'Panic',
      created_at: '2026-01-01T00:00:00Z',
    };

    expect(toLogEntry(log).level).toBe('error');
  });

  it('preserves non-fatal levels unchanged', () => {
    for (const level of ['info', 'error', 'warn', 'debug'] as const) {
      const log = {
        id: 1,
        workflow_id: 'wf-1',
        level,
        message: 'test',
        created_at: '',
      };
      expect(toLogEntry(log).level).toBe(level);
    }
  });

  it('passes "event" level through unchanged (caller should filter before mapping)', () => {
    const log = {
      id: 1,
      workflow_id: 'wf-1',
      level: 'event' as const,
      message: 'step.started',
      created_at: '',
    };
    // event level is not remapped; callers must filter these rows out before display
    expect(toLogEntry(log).level).toBe('event');
  });
});
