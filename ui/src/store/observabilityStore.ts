import { create } from 'zustand';
import type {
  ActiveView,
  WorkflowExecution,
  ExecutionStep,
  ExecutionLog,
  WorkflowExecution as WorkflowEvent,
  SystemDashboard,
  WorkflowDashboardResponse,
  ExecutionFilter,
  LogFilter,
  IAMProviderConfig,
  IAMRoleMapping,
} from '../types/observability.ts';
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
  apiCreateIAMProvider,
  apiUpdateIAMProvider,
  apiDeleteIAMProvider,
  apiTestIAMProvider,
  apiFetchIAMRoleMappings,
  apiCreateIAMRoleMapping,
  apiDeleteIAMRoleMapping,
  createLogStream,
  createEventStream,
} from '../utils/api.ts';

interface ObservabilityStore {
  // Active view
  activeView: ActiveView;
  selectedWorkflowId: string | null;

  // Dashboard
  systemDashboard: SystemDashboard | null;
  workflowDashboard: WorkflowDashboardResponse | null;
  dashboardLoading: boolean;

  // Executions
  executions: WorkflowExecution[];
  selectedExecution: WorkflowExecution | null;
  executionSteps: ExecutionStep[];
  executionFilter: ExecutionFilter;

  // Logs
  logEntries: ExecutionLog[];
  logFilter: LogFilter;
  logStreaming: boolean;

  // Events
  events: WorkflowEvent[];
  eventStreaming: boolean;

  // IAM
  iamProviders: IAMProviderConfig[];
  iamMappings: Record<string, IAMRoleMapping[]>;

  // Actions
  setActiveView: (view: ActiveView) => void;
  setSelectedWorkflowId: (id: string | null) => void;

  fetchSystemDashboard: () => Promise<void>;
  fetchWorkflowDashboard: (workflowId: string) => Promise<void>;

  fetchExecutions: (workflowId: string, filter?: ExecutionFilter) => Promise<void>;
  fetchExecutionDetail: (executionId: string) => Promise<void>;
  fetchExecutionSteps: (executionId: string) => Promise<void>;
  setExecutionFilter: (filter: ExecutionFilter) => void;
  triggerExecution: (workflowId: string) => Promise<void>;
  cancelExecution: (executionId: string) => Promise<void>;

  fetchLogs: (workflowId: string, filter?: LogFilter) => Promise<void>;
  setLogFilter: (filter: LogFilter) => void;
  startLogStream: (workflowId: string) => void;
  stopLogStream: () => void;

  fetchEvents: (workflowId: string) => Promise<void>;
  startEventStream: (workflowId: string) => void;
  stopEventStream: () => void;

  fetchIAMProviders: (companyId: string) => Promise<void>;
  createIAMProvider: (companyId: string, data: Partial<IAMProviderConfig>) => Promise<void>;
  updateIAMProvider: (providerId: string, data: Partial<IAMProviderConfig>) => Promise<void>;
  deleteIAMProvider: (providerId: string) => Promise<void>;
  testIAMProvider: (providerId: string) => Promise<{ success: boolean; message: string }>;
  fetchIAMRoleMappings: (providerId: string) => Promise<void>;
  createIAMRoleMapping: (providerId: string, data: Partial<IAMRoleMapping>) => Promise<void>;
  deleteIAMRoleMapping: (mappingId: string, providerId: string) => Promise<void>;
}

const MAX_LOG_ENTRIES = 1000;
const MAX_EVENTS = 1000;

let logEventSource: EventSource | null = null;
let eventEventSource: EventSource | null = null;
let logBatchTimer: ReturnType<typeof setTimeout> | null = null;
let eventBatchTimer: ReturnType<typeof setTimeout> | null = null;
let pendingLogs: ExecutionLog[] = [];
let pendingEvents: WorkflowEvent[] = [];

const useObservabilityStore = create<ObservabilityStore>((set, get) => ({
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

  setActiveView: (view) => set({ activeView: view }),
  setSelectedWorkflowId: (id) => set({ selectedWorkflowId: id }),

  fetchSystemDashboard: async () => {
    set({ dashboardLoading: true });
    try {
      const data = await apiFetchDashboard();
      set({ systemDashboard: data, dashboardLoading: false });
    } catch {
      set({ dashboardLoading: false });
    }
  },

  fetchWorkflowDashboard: async (workflowId) => {
    set({ dashboardLoading: true });
    try {
      const data = await apiFetchWorkflowDashboard(workflowId);
      set({ workflowDashboard: data, dashboardLoading: false });
    } catch {
      set({ dashboardLoading: false });
    }
  },

  fetchExecutions: async (workflowId, filter) => {
    try {
      const data = await apiFetchExecutions(workflowId, filter);
      set({ executions: data });
    } catch {
      // ignore
    }
  },

  fetchExecutionDetail: async (executionId) => {
    try {
      const data = await apiFetchExecutionDetail(executionId);
      set({ selectedExecution: data });
    } catch {
      // ignore
    }
  },

  fetchExecutionSteps: async (executionId) => {
    try {
      const data = await apiFetchExecutionSteps(executionId);
      set({ executionSteps: data });
    } catch {
      // ignore
    }
  },

  setExecutionFilter: (filter) => set({ executionFilter: filter }),

  triggerExecution: async (workflowId) => {
    try {
      await apiTriggerExecution(workflowId);
      await get().fetchExecutions(workflowId, get().executionFilter);
    } catch {
      // ignore
    }
  },

  cancelExecution: async (executionId) => {
    try {
      await apiCancelExecution(executionId);
      const wfId = get().selectedWorkflowId;
      if (wfId) {
        await get().fetchExecutions(wfId, get().executionFilter);
      }
    } catch {
      // ignore
    }
  },

  fetchLogs: async (workflowId, filter) => {
    try {
      const data = await apiFetchLogs(workflowId, filter);
      set({ logEntries: data });
    } catch {
      // ignore
    }
  },

  setLogFilter: (filter) => set({ logFilter: filter }),

  startLogStream: (workflowId) => {
    get().stopLogStream();
    const token = localStorage.getItem('auth_token') || '';
    const es = createLogStream(workflowId, token);
    logEventSource = es;
    pendingLogs = [];

    es.onmessage = (event) => {
      try {
        const log: ExecutionLog = JSON.parse(event.data);
        pendingLogs.push(log);
        if (!logBatchTimer) {
          logBatchTimer = setTimeout(() => {
            const { logEntries } = get();
            const merged = [...logEntries, ...pendingLogs].slice(-MAX_LOG_ENTRIES);
            set({ logEntries: merged });
            pendingLogs = [];
            logBatchTimer = null;
          }, 100);
        }
      } catch {
        // ignore parse errors
      }
    };

    es.onerror = () => {
      // reconnect is handled by EventSource
    };

    set({ logStreaming: true });
  },

  stopLogStream: () => {
    if (logEventSource) {
      logEventSource.close();
      logEventSource = null;
    }
    if (logBatchTimer) {
      clearTimeout(logBatchTimer);
      logBatchTimer = null;
    }
    pendingLogs = [];
    set({ logStreaming: false });
  },

  fetchEvents: async (workflowId) => {
    try {
      const data = await apiFetchEvents(workflowId);
      set({ events: data });
    } catch {
      // ignore
    }
  },

  startEventStream: (workflowId) => {
    get().stopEventStream();
    const token = localStorage.getItem('auth_token') || '';
    const es = createEventStream(workflowId, token);
    eventEventSource = es;
    pendingEvents = [];

    es.onmessage = (event) => {
      try {
        const evt: WorkflowEvent = JSON.parse(event.data);
        pendingEvents.push(evt);
        if (!eventBatchTimer) {
          eventBatchTimer = setTimeout(() => {
            const { events } = get();
            const merged = [...events, ...pendingEvents].slice(-MAX_EVENTS);
            set({ events: merged });
            pendingEvents = [];
            eventBatchTimer = null;
          }, 100);
        }
      } catch {
        // ignore parse errors
      }
    };

    set({ eventStreaming: true });
  },

  stopEventStream: () => {
    if (eventEventSource) {
      eventEventSource.close();
      eventEventSource = null;
    }
    if (eventBatchTimer) {
      clearTimeout(eventBatchTimer);
      eventBatchTimer = null;
    }
    pendingEvents = [];
    set({ eventStreaming: false });
  },

  fetchIAMProviders: async (companyId) => {
    try {
      const data = await apiFetchIAMProviders(companyId);
      set({ iamProviders: data });
    } catch {
      // ignore
    }
  },

  createIAMProvider: async (companyId, data) => {
    await apiCreateIAMProvider(companyId, data);
    await get().fetchIAMProviders(companyId);
  },

  updateIAMProvider: async (providerId, data) => {
    await apiUpdateIAMProvider(providerId, data);
  },

  deleteIAMProvider: async (providerId) => {
    await apiDeleteIAMProvider(providerId);
  },

  testIAMProvider: async (providerId) => {
    return apiTestIAMProvider(providerId);
  },

  fetchIAMRoleMappings: async (providerId) => {
    try {
      const data = await apiFetchIAMRoleMappings(providerId);
      set({ iamMappings: { ...get().iamMappings, [providerId]: data } });
    } catch {
      // ignore
    }
  },

  createIAMRoleMapping: async (providerId, data) => {
    await apiCreateIAMRoleMapping(providerId, data);
    await get().fetchIAMRoleMappings(providerId);
  },

  deleteIAMRoleMapping: async (mappingId, providerId) => {
    await apiDeleteIAMRoleMapping(mappingId);
    await get().fetchIAMRoleMappings(providerId);
  },
}));

export default useObservabilityStore;
