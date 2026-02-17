import { create } from 'zustand';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type FlagType = 'boolean' | 'string' | 'number' | 'json';

export interface TargetingCondition {
  attribute: string;
  operator: 'eq' | 'neq' | 'in' | 'contains' | 'startsWith' | 'gt' | 'lt';
  value: string;
}

export interface TargetingRule {
  conditions: TargetingCondition[];
  value: unknown;
  percentage?: number;
}

export interface FlagOverride {
  type: 'user' | 'group';
  key: string;
  value: unknown;
}

export interface FlagDefinition {
  key: string;
  name: string;
  description: string;
  type: FlagType;
  enabled: boolean;
  default_value: unknown;
  targeting_rules: TargetingRule[];
  overrides: FlagOverride[];
  tags: string[];
  scope: string;
  created_at: string;
  updated_at: string;
}

export interface CreateFlagRequest {
  key: string;
  name: string;
  description?: string;
  type: FlagType;
  enabled?: boolean;
  default_value: unknown;
  tags?: string[];
  scope?: string;
}

export interface EvalContext {
  user?: string;
  group?: string;
  attributes?: Record<string, string>;
}

export type FlagValue = unknown;

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

interface FeatureFlagState {
  flags: FlagDefinition[];
  loading: boolean;
  error: string | null;
  sseConnected: boolean;

  fetchFlags: () => Promise<void>;
  createFlag: (flag: CreateFlagRequest) => Promise<void>;
  updateFlag: (key: string, updates: Partial<FlagDefinition>) => Promise<void>;
  deleteFlag: (key: string) => Promise<void>;
  setOverrides: (key: string, overrides: FlagOverride[]) => Promise<void>;
  evaluateFlag: (key: string, context: EvalContext) => Promise<FlagValue>;
  connectSSE: () => void;
  disconnectSSE: () => void;
}

function authHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('auth_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

const API_BASE = '/api/v1/admin/feature-flags';

let sseSource: EventSource | null = null;

const useFeatureFlagStore = create<FeatureFlagState>((set, get) => ({
  flags: [],
  loading: false,
  error: null,
  sseConnected: false,

  fetchFlags: async () => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const res = await fetch(API_BASE, { headers: authHeaders() });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to fetch flags (${res.status})`);
      }
      const data = await res.json();
      const flags: FlagDefinition[] = Array.isArray(data) ? data : [];
      set({ flags, loading: false });
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : String(e) });
    }
  },

  createFlag: async (flag: CreateFlagRequest) => {
    set({ error: null });
    try {
      const res = await fetch(API_BASE, {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify(flag),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to create flag (${res.status})`);
      }
      const created: FlagDefinition = await res.json();
      set((s) => ({ flags: [...s.flags, created] }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  updateFlag: async (key: string, updates: Partial<FlagDefinition>) => {
    set({ error: null });
    try {
      const res = await fetch(`${API_BASE}/${encodeURIComponent(key)}`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify(updates),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to update flag (${res.status})`);
      }
      const updated: FlagDefinition = await res.json();
      set((s) => ({
        flags: s.flags.map((f) => (f.key === key ? updated : f)),
      }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  deleteFlag: async (key: string) => {
    set({ error: null });
    try {
      const res = await fetch(`${API_BASE}/${encodeURIComponent(key)}`, {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to delete flag (${res.status})`);
      }
      set((s) => ({ flags: s.flags.filter((f) => f.key !== key) }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  setOverrides: async (key: string, overrides: FlagOverride[]) => {
    set({ error: null });
    try {
      const res = await fetch(`${API_BASE}/${encodeURIComponent(key)}/overrides`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify(overrides),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to set overrides (${res.status})`);
      }
      const updated: FlagDefinition = await res.json();
      set((s) => ({
        flags: s.flags.map((f) => (f.key === key ? updated : f)),
      }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  evaluateFlag: async (key: string, context: EvalContext) => {
    const params = new URLSearchParams();
    if (context.user) params.set('user', context.user);
    if (context.group) params.set('group', context.group);
    if (context.attributes) {
      for (const [k, v] of Object.entries(context.attributes)) {
        params.set(`attr.${k}`, v);
      }
    }
    const qs = params.toString();
    const url = `${API_BASE}/${encodeURIComponent(key)}/evaluate${qs ? `?${qs}` : ''}`;
    const res = await fetch(url, { headers: authHeaders() });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `Evaluation failed (${res.status})`);
    }
    const data = await res.json();
    return data.value;
  },

  connectSSE: () => {
    get().disconnectSSE();
    const token = localStorage.getItem('auth_token') || '';
    const url = `${API_BASE}/stream${token ? `?token=${encodeURIComponent(token)}` : ''}`;
    const es = new EventSource(url);
    sseSource = es;

    es.addEventListener('flag.updated', (event) => {
      try {
        const updated: FlagDefinition = JSON.parse(event.data);
        set((s) => ({
          flags: s.flags.map((f) => (f.key === updated.key ? updated : f)),
        }));
      } catch {
        // ignore parse errors
      }
    });

    es.addEventListener('flag.created', (event) => {
      try {
        const created: FlagDefinition = JSON.parse(event.data);
        set((s) => {
          if (s.flags.some((f) => f.key === created.key)) {
            return { flags: s.flags.map((f) => (f.key === created.key ? created : f)) };
          }
          return { flags: [...s.flags, created] };
        });
      } catch {
        // ignore parse errors
      }
    });

    es.addEventListener('flag.deleted', (event) => {
      try {
        const data = JSON.parse(event.data);
        const key = data.key || data;
        set((s) => ({ flags: s.flags.filter((f) => f.key !== key) }));
      } catch {
        // ignore parse errors
      }
    });

    es.onopen = () => {
      set({ sseConnected: true });
    };

    es.onerror = () => {
      set({ sseConnected: false });
    };
  },

  disconnectSSE: () => {
    if (sseSource) {
      sseSource.close();
      sseSource = null;
    }
    set({ sseConnected: false });
  },
}));

export default useFeatureFlagStore;
