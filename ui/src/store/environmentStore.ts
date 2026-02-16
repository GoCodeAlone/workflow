import { create } from 'zustand';

export interface Environment {
  id: string;
  workflow_id: string;
  name: string;
  provider: string;
  region: string;
  config: Record<string, unknown>;
  secrets?: Record<string, string>;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface ConnectionTestResult {
  success: boolean;
  message: string;
  latency: number;
}

interface EnvironmentFilter {
  workflow_id?: string;
  provider?: string;
  status?: string;
}

interface EnvironmentStore {
  environments: Environment[];
  loading: boolean;
  error: string | null;
  selectedEnvironment: Environment | null;

  fetchEnvironments: (filter?: EnvironmentFilter) => Promise<void>;
  createEnvironment: (env: Partial<Environment>) => Promise<void>;
  updateEnvironment: (id: string, env: Partial<Environment>) => Promise<void>;
  deleteEnvironment: (id: string) => Promise<void>;
  testConnection: (id: string) => Promise<ConnectionTestResult>;
  setSelectedEnvironment: (env: Environment | null) => void;
}

function authHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('auth_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

const useEnvironmentStore = create<EnvironmentStore>((set, get) => ({
  environments: [],
  loading: false,
  error: null,
  selectedEnvironment: null,

  fetchEnvironments: async (filter?: EnvironmentFilter) => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const params = new URLSearchParams();
      if (filter?.workflow_id) params.set('workflow_id', filter.workflow_id);
      if (filter?.provider) params.set('provider', filter.provider);
      if (filter?.status) params.set('status', filter.status);
      const qs = params.toString();
      const url = '/api/v1/admin/environments' + (qs ? `?${qs}` : '');
      const res = await fetch(url, { headers: authHeaders() });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to fetch environments (${res.status})`);
      }
      const data = await res.json();
      const environments: Environment[] = Array.isArray(data) ? data : [];
      set({ environments, loading: false });
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : String(e) });
    }
  },

  createEnvironment: async (env: Partial<Environment>) => {
    set({ error: null });
    try {
      const res = await fetch('/api/v1/admin/environments', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify(env),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to create environment (${res.status})`);
      }
      const created: Environment = await res.json();
      set((s) => ({ environments: [...s.environments, created] }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  updateEnvironment: async (id: string, env: Partial<Environment>) => {
    set({ error: null });
    try {
      const res = await fetch(`/api/v1/admin/environments/${id}`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify(env),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to update environment (${res.status})`);
      }
      const updated: Environment = await res.json();
      set((s) => ({
        environments: s.environments.map((e) => (e.id === id ? updated : e)),
        selectedEnvironment: s.selectedEnvironment?.id === id ? updated : s.selectedEnvironment,
      }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  deleteEnvironment: async (id: string) => {
    set({ error: null });
    try {
      const res = await fetch(`/api/v1/admin/environments/${id}`, {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Failed to delete environment (${res.status})`);
      }
      set((s) => ({
        environments: s.environments.filter((e) => e.id !== id),
        selectedEnvironment: s.selectedEnvironment?.id === id ? null : s.selectedEnvironment,
      }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) });
      throw e;
    }
  },

  testConnection: async (id: string) => {
    const res = await fetch(`/api/v1/admin/environments/${id}/test`, {
      method: 'POST',
      headers: authHeaders(),
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `Test failed (${res.status})`);
    }
    return await res.json() as ConnectionTestResult;
  },

  setSelectedEnvironment: (env: Environment | null) => {
    set({ selectedEnvironment: env });
  },
}));

export default useEnvironmentStore;
