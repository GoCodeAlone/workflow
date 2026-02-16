import { create } from 'zustand';

export interface MarketplacePlugin {
  name: string;
  version: string;
  author: string;
  description: string;
  tags: string[];
  installed: boolean;
}

interface MarketplaceStore {
  plugins: MarketplacePlugin[];
  searchResults: MarketplacePlugin[];
  loading: boolean;
  searching: boolean;
  installing: Record<string, boolean>;
  error: string | null;

  fetchInstalled: () => Promise<void>;
  search: (query: string) => Promise<void>;
  install: (name: string, version: string) => Promise<void>;
  uninstall: (name: string) => Promise<void>;
  clearError: () => void;
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('auth_token');
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

const useMarketplaceStore = create<MarketplaceStore>((set, get) => ({
  plugins: [],
  searchResults: [],
  loading: false,
  searching: false,
  installing: {},
  error: null,

  fetchInstalled: async () => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const res = await fetch('/api/v1/admin/plugins', { headers: authHeaders() });
      if (!res.ok) {
        throw new Error(`Failed to fetch plugins: ${res.status}`);
      }
      const plugins: MarketplacePlugin[] = await res.json();
      set({ plugins, loading: false });
    } catch (e) {
      set({
        loading: false,
        error: e instanceof Error ? e.message : 'Failed to fetch plugins',
      });
    }
  },

  search: async (query: string) => {
    set({ searching: true, error: null });
    try {
      const params = new URLSearchParams();
      if (query) params.set('q', query);
      const res = await fetch(`/api/v1/admin/plugins/registry/search?${params}`, {
        headers: authHeaders(),
      });
      if (!res.ok) {
        throw new Error(`Search failed: ${res.status}`);
      }
      const searchResults: MarketplacePlugin[] = await res.json();
      set({ searchResults, searching: false });
    } catch (e) {
      set({
        searching: false,
        error: e instanceof Error ? e.message : 'Search failed',
      });
    }
  },

  install: async (name: string, version: string) => {
    set((s) => ({ installing: { ...s.installing, [name]: true }, error: null }));
    try {
      const res = await fetch('/api/v1/admin/plugins/registry/install', {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify({ name, version }),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(text || `Install failed: ${res.status}`);
      }
      set((s) => ({ installing: { ...s.installing, [name]: false } }));
      // Refresh both lists
      await get().fetchInstalled();
      // Update search results to reflect new installed state
      set((s) => ({
        searchResults: s.searchResults.map((p) =>
          p.name === name ? { ...p, installed: true } : p,
        ),
      }));
    } catch (e) {
      set((s) => ({
        installing: { ...s.installing, [name]: false },
        error: e instanceof Error ? e.message : 'Install failed',
      }));
    }
  },

  uninstall: async (name: string) => {
    set((s) => ({ installing: { ...s.installing, [name]: true }, error: null }));
    try {
      const res = await fetch(`/api/v1/admin/plugins/${encodeURIComponent(name)}`, {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(text || `Uninstall failed: ${res.status}`);
      }
      set((s) => ({ installing: { ...s.installing, [name]: false } }));
      // Refresh installed list
      await get().fetchInstalled();
      // Update search results to reflect uninstalled state
      set((s) => ({
        searchResults: s.searchResults.map((p) =>
          p.name === name ? { ...p, installed: false } : p,
        ),
      }));
    } catch (e) {
      set((s) => ({
        installing: { ...s.installing, [name]: false },
        error: e instanceof Error ? e.message : 'Uninstall failed',
      }));
    }
  },

  clearError: () => set({ error: null }),
}));

export default useMarketplaceStore;
