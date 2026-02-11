import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import SystemDashboard from './SystemDashboard.tsx';
import useObservabilityStore from '../../store/observabilityStore.ts';

// Mock the API module
vi.mock('../../utils/api.ts', () => ({
  apiFetchDashboard: vi.fn().mockResolvedValue({ total_workflows: 0, workflow_summaries: [] }),
  apiFetchWorkflowDashboard: vi.fn(),
  apiFetchExecutions: vi.fn(),
  apiFetchExecutionDetail: vi.fn(),
  apiFetchExecutionSteps: vi.fn(),
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
}));

import { apiFetchDashboard } from '../../utils/api.ts';

function resetStore() {
  useObservabilityStore.setState({
    activeView: 'dashboard',
    selectedWorkflowId: null,
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

describe('SystemDashboard', () => {
  beforeEach(() => {
    resetStore();
    vi.useFakeTimers();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the System Dashboard title', () => {
    render(<SystemDashboard />);
    expect(screen.getByText('System Dashboard')).toBeInTheDocument();
  });

  it('renders loading state when loading and no data', () => {
    useObservabilityStore.setState({ dashboardLoading: true, systemDashboard: null });

    render(<SystemDashboard />);
    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('renders empty state when no workflows', async () => {
    // Mock fetch to return empty data
    vi.mocked(apiFetchDashboard).mockResolvedValue({ total_workflows: 0, workflow_summaries: [] });

    // Use real timers for this test since waitFor needs them
    vi.useRealTimers();

    render(<SystemDashboard />);

    // Wait for the useEffect fetch to complete and render the empty state
    await waitFor(() => {
      expect(screen.getByText('No workflows found.')).toBeInTheDocument();
    });
  });

  it('renders with workflow data', () => {
    useObservabilityStore.setState({
      dashboardLoading: false,
      systemDashboard: {
        total_workflows: 2,
        workflow_summaries: [
          { workflow_id: 'wf-1', workflow_name: 'Order Pipeline', status: 'active', executions: { completed: 10, failed: 2, pending: 0, running: 1, cancelled: 0 }, log_counts: { info: 50, error: 3 } },
          { workflow_id: 'wf-2', workflow_name: 'Email Sender', status: 'draft', executions: { completed: 5, failed: 0, pending: 0, running: 0, cancelled: 0 }, log_counts: { info: 20 } },
        ],
      },
    });

    render(<SystemDashboard />);

    // Use getAllByText since names appear in both the workflow card and the breakdown table
    expect(screen.getAllByText('Order Pipeline').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Email Sender').length).toBeGreaterThanOrEqual(1);
  });

  it('displays metric cards', () => {
    useObservabilityStore.setState({
      dashboardLoading: false,
      systemDashboard: {
        total_workflows: 3,
        workflow_summaries: [
          { workflow_id: 'wf-1', workflow_name: 'WF1', status: 'active', executions: { completed: 8, failed: 2, pending: 0, running: 0, cancelled: 0 }, log_counts: {} },
        ],
      },
    });

    render(<SystemDashboard />);

    expect(screen.getByText('Total Workflows')).toBeInTheDocument();
    expect(screen.getByText('Active Workflows')).toBeInTheDocument();
    expect(screen.getByText('Total Executions')).toBeInTheDocument();
    expect(screen.getByText('Error Rate')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('displays execution status breakdown for workflows', () => {
    useObservabilityStore.setState({
      dashboardLoading: false,
      systemDashboard: {
        total_workflows: 1,
        workflow_summaries: [
          { workflow_id: 'wf-1', workflow_name: 'TestWF', status: 'active', executions: { completed: 5, failed: 1, pending: 2, running: 3, cancelled: 0 }, log_counts: {} },
        ],
      },
    });

    render(<SystemDashboard />);

    expect(screen.getByText('Execution Status Breakdown')).toBeInTheDocument();
    expect(screen.getByText('Pending')).toBeInTheDocument();
    expect(screen.getByText('Running')).toBeInTheDocument();
    expect(screen.getByText('Completed')).toBeInTheDocument();
    expect(screen.getByText('Failed')).toBeInTheDocument();
    expect(screen.getByText('Cancelled')).toBeInTheDocument();
  });
});
