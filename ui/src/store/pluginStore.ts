import { create } from 'zustand';

// ---------------------------------------------------------------------------
// Types matching the backend plugin system (see ADMIN_PLUGIN_ARCHITECTURE.md)
// ---------------------------------------------------------------------------

export interface UIPageDef {
  id: string;       // matches ActiveView, e.g. "dashboard", "editor", "store-browser"
  label: string;    // display name
  icon: string;     // emoji or icon identifier
  category: string; // "global" | "workflow" | "plugin"
  order: number;    // sort order within category
}

export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  enabled: boolean;
  dependencies: string[];
  uiPages: UIPageDef[];
}

// ---------------------------------------------------------------------------
// TEMPORARY: Static fallback until backend plugin system returns UI pages.
// Remove once all plugins implement UIPages().
// ---------------------------------------------------------------------------

const FALLBACK_PAGES: UIPageDef[] = [
  // Global pages
  { id: 'dashboard',    label: 'Dashboard',     icon: '\u{1F4CA}', category: 'global', order: 0 },
  { id: 'editor',       label: 'Editor',        icon: '\u{1F4DD}', category: 'global', order: 1 },
  { id: 'marketplace',  label: 'Marketplace',   icon: '\u{1F6D2}', category: 'global', order: 2 },
  { id: 'templates',    label: 'Templates',     icon: '\u{1F4C4}', category: 'global', order: 3 },
  { id: 'environments', label: 'Environments',  icon: '\u2601\uFE0F',  category: 'global', order: 4 },
  { id: 'settings',     label: 'Settings',      icon: '\u2699\uFE0F',  category: 'global', order: 5 },
  // Plugin pages
  { id: 'store-browser', label: 'Store Browser',  icon: '\u{1F5C4}\uFE0F',  category: 'plugin', order: 0 },
  { id: 'docs',          label: 'Documentation',  icon: '\u{1F4D6}', category: 'plugin', order: 1 },
  // Workflow pages
  { id: 'executions', label: 'Executions', icon: '\u25B6\uFE0F',  category: 'workflow', order: 0 },
  { id: 'logs',       label: 'Logs',       icon: '\u{1F4C3}', category: 'workflow', order: 1 },
  { id: 'events',     label: 'Events',     icon: '\u26A1',    category: 'workflow', order: 2 },
];

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

interface PluginStore {
  plugins: PluginInfo[];
  loading: boolean;
  loaded: boolean;
  enabling: Record<string, boolean>;
  error: string | null;

  /** All UI pages from enabled plugins, sorted by order within category. */
  enabledPages: UIPageDef[];

  fetchPlugins: () => Promise<void>;
  enablePlugin: (name: string) => Promise<void>;
  disablePlugin: (name: string) => Promise<void>;
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

/** Derive enabledPages from current plugin list, falling back to FALLBACK_PAGES. */
function deriveEnabledPages(plugins: PluginInfo[]): UIPageDef[] {
  const enabledPlugins = plugins.filter((p) => p.enabled);
  const pagesFromPlugins = enabledPlugins.flatMap((p) => p.uiPages ?? []);

  if (pagesFromPlugins.length > 0) {
    return pagesFromPlugins;
  }

  // TEMPORARY: Use fallback when backend returns no UI pages yet.
  return FALLBACK_PAGES;
}

const usePluginStore = create<PluginStore>((set, get) => ({
  plugins: [],
  loading: false,
  loaded: false,
  enabling: {},
  error: null,
  enabledPages: FALLBACK_PAGES,

  fetchPlugins: async () => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const res = await fetch('/api/v1/admin/plugins', { headers: authHeaders() });
      if (!res.ok) {
        console.warn('Failed to fetch plugins, using fallback navigation');
        set({ loading: false, loaded: true, enabledPages: FALLBACK_PAGES });
        return;
      }
      const plugins: PluginInfo[] = await res.json();
      set({
        plugins,
        loaded: true,
        loading: false,
        enabledPages: deriveEnabledPages(plugins),
      });
    } catch (e) {
      console.warn('Error fetching plugins:', e);
      set({ loading: false, loaded: true, enabledPages: FALLBACK_PAGES });
    }
  },

  enablePlugin: async (name: string) => {
    set((s) => ({ enabling: { ...s.enabling, [name]: true }, error: null }));
    try {
      const res = await fetch(`/api/v1/admin/plugins/${encodeURIComponent(name)}/enable`, {
        method: 'POST',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(text || `Enable failed: ${res.status}`);
      }
      set((s) => ({ enabling: { ...s.enabling, [name]: false } }));
      // Refresh full plugin list to get updated enabled state + dependency changes
      await get().fetchPlugins();
    } catch (e) {
      set((s) => ({
        enabling: { ...s.enabling, [name]: false },
        error: e instanceof Error ? e.message : 'Enable failed',
      }));
    }
  },

  disablePlugin: async (name: string) => {
    set((s) => ({ enabling: { ...s.enabling, [name]: true }, error: null }));
    try {
      const res = await fetch(`/api/v1/admin/plugins/${encodeURIComponent(name)}/disable`, {
        method: 'POST',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(text || `Disable failed: ${res.status}`);
      }
      set((s) => ({ enabling: { ...s.enabling, [name]: false } }));
      // Refresh full plugin list to get updated enabled state + dependent changes
      await get().fetchPlugins();
    } catch (e) {
      set((s) => ({
        enabling: { ...s.enabling, [name]: false },
        error: e instanceof Error ? e.message : 'Disable failed',
      }));
    }
  },

  clearError: () => set({ error: null }),
}));

export { FALLBACK_PAGES };
export default usePluginStore;
