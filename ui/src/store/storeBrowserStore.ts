import { create } from 'zustand';

const V1_BASE = '/api/v1';

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('auth_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

async function sbFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${V1_BASE}/admin/plugins/store-browser${path}`, {
    ...options,
    headers: { ...getAuthHeaders(), ...(options?.headers as Record<string, string>) },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`API ${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export interface TableColumn {
  name: string;
  type: string;
  notnull: boolean;
  pk: boolean;
}

export interface StoreEvent {
  id: string;
  execution_id: string;
  event_type: string;
  created_at: string;
  event_data: unknown;
}

export interface DLQEntry {
  id: string;
  pipeline_name: string;
  step_name: string;
  error_message: string;
  status: string;
  retry_count: number;
  created_at: string;
}

export interface SQLResult {
  columns: string[];
  rows: Array<Record<string, unknown>>;
}

type TabKey = 'tables' | 'events' | 'dlq' | 'sql';

interface StoreBrowserStore {
  // Tab state
  activeTab: TabKey;
  setActiveTab: (tab: TabKey) => void;

  // Tables
  tables: string[];
  selectedTable: string | null;
  tableSchema: TableColumn[];
  tableRows: Array<Record<string, unknown>>;
  tablePage: number;
  tableTotal: number;
  tableLoading: boolean;
  fetchTables: () => Promise<void>;
  selectTable: (name: string) => Promise<void>;
  fetchTableRows: (page?: number) => Promise<void>;

  // Events
  events: StoreEvent[];
  eventFilters: { execution_id?: string; event_type?: string };
  eventsLoading: boolean;
  fetchEvents: () => Promise<void>;
  setEventFilter: (key: string, value: string) => void;

  // DLQ
  dlqEntries: DLQEntry[];
  dlqFilters: { status?: string; pipeline?: string };
  dlqLoading: boolean;
  fetchDLQ: () => Promise<void>;
  setDLQFilter: (key: string, value: string) => void;
  retryDLQ: (id: string) => Promise<void>;
  discardDLQ: (id: string) => Promise<void>;

  // SQL Console
  sqlQuery: string;
  sqlResults: SQLResult | null;
  sqlError: string | null;
  sqlHistory: string[];
  sqlLoading: boolean;
  setSqlQuery: (q: string) => void;
  executeSql: () => Promise<void>;
}

const PAGE_SIZE = 50;

function loadSqlHistory(): string[] {
  try {
    const raw = localStorage.getItem('store_browser_sql_history');
    if (raw) return JSON.parse(raw);
  } catch {
    // ignore
  }
  return [];
}

function saveSqlHistory(history: string[]) {
  try {
    localStorage.setItem('store_browser_sql_history', JSON.stringify(history.slice(-50)));
  } catch {
    // ignore
  }
}

const useStoreBrowserStore = create<StoreBrowserStore>((set, get) => ({
  activeTab: 'tables',
  setActiveTab: (tab) => set({ activeTab: tab }),

  // Tables
  tables: [],
  selectedTable: null,
  tableSchema: [],
  tableRows: [],
  tablePage: 1,
  tableTotal: 0,
  tableLoading: false,

  fetchTables: async () => {
    set({ tableLoading: true });
    try {
      const data = await sbFetch<{ tables: string[] }>('/tables');
      set({ tables: data.tables || [], tableLoading: false });
    } catch {
      set({ tables: [], tableLoading: false });
    }
  },

  selectTable: async (name) => {
    set({ selectedTable: name, tableSchema: [], tableRows: [], tablePage: 1, tableTotal: 0, tableLoading: true });
    try {
      const schema = await sbFetch<{ columns: TableColumn[] }>(`/tables/${encodeURIComponent(name)}/schema`);
      set({ tableSchema: schema.columns || [] });
    } catch {
      set({ tableSchema: [] });
    }
    await get().fetchTableRows(1);
  },

  fetchTableRows: async (page) => {
    const table = get().selectedTable;
    if (!table) return;
    const p = page ?? get().tablePage;
    set({ tableLoading: true, tablePage: p });
    try {
      const data = await sbFetch<{ rows: Array<Record<string, unknown>>; total: number }>(
        `/tables/${encodeURIComponent(table)}/rows?page=${p}&page_size=${PAGE_SIZE}`,
      );
      set({ tableRows: data.rows || [], tableTotal: data.total || 0, tableLoading: false });
    } catch {
      set({ tableRows: [], tableLoading: false });
    }
  },

  // Events
  events: [],
  eventFilters: {},
  eventsLoading: false,

  fetchEvents: async () => {
    set({ eventsLoading: true });
    try {
      const params = new URLSearchParams();
      const { eventFilters } = get();
      if (eventFilters.execution_id) params.set('execution_id', eventFilters.execution_id);
      if (eventFilters.event_type) params.set('event_type', eventFilters.event_type);
      const qs = params.toString();
      const data = await sbFetch<{ events: StoreEvent[] }>(`/events${qs ? '?' + qs : ''}`);
      set({ events: data.events || [], eventsLoading: false });
    } catch {
      set({ events: [], eventsLoading: false });
    }
  },

  setEventFilter: (key, value) => {
    set({ eventFilters: { ...get().eventFilters, [key]: value || undefined } });
  },

  // DLQ
  dlqEntries: [],
  dlqFilters: {},
  dlqLoading: false,

  fetchDLQ: async () => {
    set({ dlqLoading: true });
    try {
      const params = new URLSearchParams();
      const { dlqFilters } = get();
      if (dlqFilters.status) params.set('status', dlqFilters.status);
      if (dlqFilters.pipeline) params.set('pipeline', dlqFilters.pipeline);
      const qs = params.toString();
      const data = await sbFetch<{ entries: DLQEntry[] }>(`/dlq${qs ? '?' + qs : ''}`);
      set({ dlqEntries: data.entries || [], dlqLoading: false });
    } catch {
      set({ dlqEntries: [], dlqLoading: false });
    }
  },

  setDLQFilter: (key, value) => {
    set({ dlqFilters: { ...get().dlqFilters, [key]: value || undefined } });
  },

  retryDLQ: async (id) => {
    await sbFetch<void>(`/dlq/${encodeURIComponent(id)}/retry`, { method: 'POST' });
    await get().fetchDLQ();
  },

  discardDLQ: async (id) => {
    await sbFetch<void>(`/dlq/${encodeURIComponent(id)}/discard`, { method: 'POST' });
    await get().fetchDLQ();
  },

  // SQL Console
  sqlQuery: '',
  sqlResults: null,
  sqlError: null,
  sqlHistory: loadSqlHistory(),
  sqlLoading: false,

  setSqlQuery: (q) => set({ sqlQuery: q }),

  executeSql: async () => {
    const query = get().sqlQuery.trim();
    if (!query) return;
    set({ sqlLoading: true, sqlError: null, sqlResults: null });
    try {
      const data = await sbFetch<SQLResult>('/query', {
        method: 'POST',
        body: JSON.stringify({ query }),
      });
      const history = [...get().sqlHistory.filter((h) => h !== query), query];
      saveSqlHistory(history);
      set({ sqlResults: data, sqlLoading: false, sqlHistory: history });
    } catch (err) {
      set({ sqlError: err instanceof Error ? err.message : String(err), sqlLoading: false });
    }
  },
}));

export default useStoreBrowserStore;
