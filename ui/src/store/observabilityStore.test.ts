import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { act } from '@testing-library/react';
import useObservabilityStore from './observabilityStore.ts';

// Mock the API module
vi.mock('../utils/api.ts', () => ({
  apiFetchDashboard: vi.fn(),
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

import {
  apiFetchDashboard,
  apiFetchWorkflowDashboard,
  apiFetchExecutions,
  apiFetchExecutionDetail,
  apiFetchExecutionSteps,
  apiFetchLogs,
  apiFetchEvents,
  apiTriggerExecution,
  apiCancelExecution,
  apiFetchIAMProviders,
  apiTestIAMProvider,
} from '../utils/api.ts';

function resetStore() {
  useObservabilityStore.setState({
    activeView: 'editor',
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

describe('observabilityStore', () => {
  beforeEach(() => {
    resetStore();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('initial state', () => {
    it('has editor as default active view', () => {
      expect(useObservabilityStore.getState().activeView).toBe('editor');
    });

    it('has null selectedWorkflowId', () => {
      expect(useObservabilityStore.getState().selectedWorkflowId).toBeNull();
    });

    it('has null systemDashboard', () => {
      expect(useObservabilityStore.getState().systemDashboard).toBeNull();
    });

    it('has empty executions', () => {
      expect(useObservabilityStore.getState().executions).toEqual([]);
    });

    it('is not loading', () => {
      expect(useObservabilityStore.getState().dashboardLoading).toBe(false);
    });
  });

  describe('setActiveView', () => {
    it('changes the active view', () => {
      act(() => {
        useObservabilityStore.getState().setActiveView('dashboard');
      });
      expect(useObservabilityStore.getState().activeView).toBe('dashboard');
    });

    it('can switch between views', () => {
      act(() => {
        useObservabilityStore.getState().setActiveView('logs');
      });
      expect(useObservabilityStore.getState().activeView).toBe('logs');

      act(() => {
        useObservabilityStore.getState().setActiveView('events');
      });
      expect(useObservabilityStore.getState().activeView).toBe('events');
    });
  });

  describe('setSelectedWorkflowId', () => {
    it('sets the selected workflow id', () => {
      act(() => {
        useObservabilityStore.getState().setSelectedWorkflowId('wf-123');
      });
      expect(useObservabilityStore.getState().selectedWorkflowId).toBe('wf-123');
    });

    it('can clear the selection', () => {
      act(() => {
        useObservabilityStore.getState().setSelectedWorkflowId('wf-123');
        useObservabilityStore.getState().setSelectedWorkflowId(null);
      });
      expect(useObservabilityStore.getState().selectedWorkflowId).toBeNull();
    });
  });

  describe('fetchSystemDashboard', () => {
    it('populates systemDashboard on success', async () => {
      const dashboardData = {
        total_workflows: 5,
        workflow_summaries: [
          { workflow_id: 'wf-1', workflow_name: 'Test WF', status: 'active', executions: { completed: 10 }, log_counts: {} },
        ],
      };
      vi.mocked(apiFetchDashboard).mockResolvedValue(dashboardData);

      await act(async () => {
        await useObservabilityStore.getState().fetchSystemDashboard();
      });

      expect(useObservabilityStore.getState().systemDashboard).toEqual(dashboardData);
      expect(useObservabilityStore.getState().dashboardLoading).toBe(false);
    });

    it('sets dashboardLoading during fetch', async () => {
      let resolve: (v: unknown) => void;
      vi.mocked(apiFetchDashboard).mockReturnValue(new Promise((r) => { resolve = r as (v: unknown) => void; }));

      const promise = act(async () => {
        return useObservabilityStore.getState().fetchSystemDashboard();
      });

      // Resolve eventually
      resolve!({ total_workflows: 0, workflow_summaries: [] });
      await promise;

      expect(useObservabilityStore.getState().dashboardLoading).toBe(false);
    });

    it('handles fetch failure gracefully', async () => {
      vi.mocked(apiFetchDashboard).mockRejectedValue(new Error('Network error'));

      await act(async () => {
        await useObservabilityStore.getState().fetchSystemDashboard();
      });

      expect(useObservabilityStore.getState().systemDashboard).toBeNull();
      expect(useObservabilityStore.getState().dashboardLoading).toBe(false);
    });
  });

  describe('fetchWorkflowDashboard', () => {
    it('populates workflowDashboard on success', async () => {
      const wfDashboard = {
        workflow: { id: 'wf-1', name: 'Test', status: 'active' },
        execution_counts: { completed: 5 },
        log_counts: { info: 100 },
        recent_executions: [],
      };
      vi.mocked(apiFetchWorkflowDashboard).mockResolvedValue(wfDashboard);

      await act(async () => {
        await useObservabilityStore.getState().fetchWorkflowDashboard('wf-1');
      });

      expect(useObservabilityStore.getState().workflowDashboard).toEqual(wfDashboard);
    });
  });

  describe('fetchExecutions', () => {
    it('populates executions on success', async () => {
      const executions = [
        { id: 'ex-1', workflow_id: 'wf-1', trigger_type: 'http', status: 'completed' as const, started_at: '2025-01-01' },
      ];
      vi.mocked(apiFetchExecutions).mockResolvedValue(executions);

      await act(async () => {
        await useObservabilityStore.getState().fetchExecutions('wf-1');
      });

      expect(useObservabilityStore.getState().executions).toEqual(executions);
    });

    it('passes filter to API', async () => {
      vi.mocked(apiFetchExecutions).mockResolvedValue([]);
      const filter = { status: 'failed' };

      await act(async () => {
        await useObservabilityStore.getState().fetchExecutions('wf-1', filter);
      });

      expect(apiFetchExecutions).toHaveBeenCalledWith('wf-1', filter);
    });

    it('handles error silently', async () => {
      vi.mocked(apiFetchExecutions).mockRejectedValue(new Error('fail'));

      await act(async () => {
        await useObservabilityStore.getState().fetchExecutions('wf-1');
      });

      expect(useObservabilityStore.getState().executions).toEqual([]);
    });
  });

  describe('fetchExecutionDetail', () => {
    it('populates selectedExecution', async () => {
      const execution = { id: 'ex-1', workflow_id: 'wf-1', trigger_type: 'http', status: 'completed' as const, started_at: '2025-01-01' };
      vi.mocked(apiFetchExecutionDetail).mockResolvedValue(execution);

      await act(async () => {
        await useObservabilityStore.getState().fetchExecutionDetail('ex-1');
      });

      expect(useObservabilityStore.getState().selectedExecution).toEqual(execution);
    });
  });

  describe('fetchExecutionSteps', () => {
    it('populates executionSteps', async () => {
      const steps = [
        { id: 's-1', execution_id: 'ex-1', step_name: 'step1', step_type: 'http', status: 'completed' as const, sequence_num: 1 },
      ];
      vi.mocked(apiFetchExecutionSteps).mockResolvedValue(steps);

      await act(async () => {
        await useObservabilityStore.getState().fetchExecutionSteps('ex-1');
      });

      expect(useObservabilityStore.getState().executionSteps).toEqual(steps);
    });
  });

  describe('setExecutionFilter', () => {
    it('sets the execution filter', () => {
      act(() => {
        useObservabilityStore.getState().setExecutionFilter({ status: 'failed' });
      });
      expect(useObservabilityStore.getState().executionFilter).toEqual({ status: 'failed' });
    });
  });

  describe('triggerExecution', () => {
    it('triggers and refetches executions', async () => {
      vi.mocked(apiTriggerExecution).mockResolvedValue(undefined as never);
      vi.mocked(apiFetchExecutions).mockResolvedValue([]);

      act(() => {
        useObservabilityStore.getState().setSelectedWorkflowId('wf-1');
      });

      await act(async () => {
        await useObservabilityStore.getState().triggerExecution('wf-1');
      });

      expect(apiTriggerExecution).toHaveBeenCalledWith('wf-1');
      expect(apiFetchExecutions).toHaveBeenCalled();
    });
  });

  describe('cancelExecution', () => {
    it('cancels and refetches executions when workflow is selected', async () => {
      vi.mocked(apiCancelExecution).mockResolvedValue(undefined as never);
      vi.mocked(apiFetchExecutions).mockResolvedValue([]);

      act(() => {
        useObservabilityStore.getState().setSelectedWorkflowId('wf-1');
      });

      await act(async () => {
        await useObservabilityStore.getState().cancelExecution('ex-1');
      });

      expect(apiCancelExecution).toHaveBeenCalledWith('ex-1');
      expect(apiFetchExecutions).toHaveBeenCalled();
    });
  });

  describe('fetchLogs', () => {
    it('populates logEntries', async () => {
      const logs = [
        { id: 1, workflow_id: 'wf-1', level: 'info' as const, message: 'test log', created_at: '2025-01-01' },
      ];
      vi.mocked(apiFetchLogs).mockResolvedValue(logs);

      await act(async () => {
        await useObservabilityStore.getState().fetchLogs('wf-1');
      });

      expect(useObservabilityStore.getState().logEntries).toEqual(logs);
    });
  });

  describe('fetchEvents', () => {
    it('populates events', async () => {
      const events = [
        { id: 'ev-1', workflow_id: 'wf-1', trigger_type: 'event', status: 'completed' as const, started_at: '2025-01-01' },
      ];
      vi.mocked(apiFetchEvents).mockResolvedValue(events);

      await act(async () => {
        await useObservabilityStore.getState().fetchEvents('wf-1');
      });

      expect(useObservabilityStore.getState().events).toEqual(events);
    });
  });

  describe('setLogFilter', () => {
    it('sets the log filter', () => {
      act(() => {
        useObservabilityStore.getState().setLogFilter({ level: 'error' });
      });
      expect(useObservabilityStore.getState().logFilter).toEqual({ level: 'error' });
    });
  });

  describe('fetchIAMProviders', () => {
    it('populates iamProviders', async () => {
      const providers = [
        { id: 'p-1', company_id: 'c-1', provider_type: 'oidc' as const, name: 'Test OIDC', config: {}, enabled: true, created_at: '', updated_at: '' },
      ];
      vi.mocked(apiFetchIAMProviders).mockResolvedValue(providers);

      await act(async () => {
        await useObservabilityStore.getState().fetchIAMProviders('c-1');
      });

      expect(useObservabilityStore.getState().iamProviders).toEqual(providers);
    });
  });

  describe('testIAMProvider', () => {
    it('returns test result', async () => {
      vi.mocked(apiTestIAMProvider).mockResolvedValue({ success: true, message: 'OK' });

      let result: { success: boolean; message: string } | undefined;
      await act(async () => {
        result = await useObservabilityStore.getState().testIAMProvider('p-1');
      });

      expect(result).toEqual({ success: true, message: 'OK' });
    });
  });
});
