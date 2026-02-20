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
  requiredRole?: string;       // minimum role: "viewer", "editor", "admin", "operator"
  requiredPermission?: string; // specific permission key, e.g. "plugins.manage"
  apiEndpoint?: string;        // JSON data source for template-based pages
  template?: string;           // predefined template: "data-table", "chart-dashboard", "form", "detail-view"
}

export interface PluginDependency {
  name: string;
  minVersion: string;
}

export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  enabled: boolean;
  dependencies: PluginDependency[];
  uiPages: UIPageDef[];
  enabledAt?: string;
  disabledAt?: string;
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
  { id: 'environments',   label: 'Environments',  icon: '\u2601\uFE0F',  category: 'global', order: 4 },
  { id: 'feature-flags',  label: 'Feature Flags', icon: '\u{1F6A9}', category: 'global', order: 5 },
  { id: 'settings',       label: 'Settings',      icon: '\u2699\uFE0F',  category: 'global', order: 6 },
  // Plugin pages
  { id: 'store-browser', label: 'Store Browser',  icon: '\u{1F5C4}\uFE0F',  category: 'plugin', order: 0 },
  { id: 'docs',          label: 'Documentation',  icon: '\u{1F4D6}', category: 'plugin', order: 1 },
  // Workflow pages
  { id: 'executions', label: 'Executions', icon: '\u25B6\uFE0F',  category: 'workflow', order: 0 },
  { id: 'logs',       label: 'Logs',       icon: '\u{1F4C3}', category: 'workflow', order: 1 },
  { id: 'events',     label: 'Events',     icon: '\u26A1',    category: 'workflow', order: 2 },
];

// ---------------------------------------------------------------------------
// Role hierarchy for permission checks
// ---------------------------------------------------------------------------

const ROLE_HIERARCHY: Record<string, number> = {
  viewer: 0,
  operator: 1,
  editor: 2,
  admin: 3,
};

/** Check if a user's role meets the minimum required role. */
function meetsRoleRequirement(userRole: string | undefined, requiredRole: string | undefined): boolean {
  if (!requiredRole) return true; // no requirement = everyone can see
  if (!userRole) return false;    // no role = can't meet any requirement
  const userLevel = ROLE_HIERARCHY[userRole] ?? -1;
  const requiredLevel = ROLE_HIERARCHY[requiredRole] ?? 999;
  return userLevel >= requiredLevel;
}

/** Check if a user has a specific permission. */
function hasPermission(userPermissions: string[] | undefined, requiredPermission: string | undefined): boolean {
  if (!requiredPermission) return true; // no requirement = everyone can see
  if (!userPermissions || userPermissions.length === 0) return false;
  return userPermissions.includes(requiredPermission) || userPermissions.includes('*');
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

interface PluginStore {
  plugins: PluginInfo[];
  loading: boolean;
  loaded: boolean;
  enabling: Record<string, boolean>;
  error: string | null;

  /** User role and permissions for filtering pages. */
  userRole: string | undefined;
  userPermissions: string[];

  /** All UI pages from enabled plugins, sorted by order within category. */
  enabledPages: UIPageDef[];

  fetchPlugins: () => Promise<void>;
  enablePlugin: (name: string) => Promise<void>;
  disablePlugin: (name: string) => Promise<void>;
  setUserAccess: (role: string | undefined, permissions?: string[]) => void;
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

/** Derive enabledPages from current plugin list, always including FALLBACK_PAGES
 *  as the core navigation base. Plugin pages are merged in, with plugin pages
 *  overriding fallback pages that share the same id.
 *  Filters pages by user role and permissions when provided. */
function deriveEnabledPages(
  plugins: PluginInfo[],
  userRole?: string,
  userPermissions?: string[],
): UIPageDef[] {
  const enabledPlugins = plugins.filter((p) => p.enabled);
  const pagesFromPlugins = enabledPlugins.flatMap((p) => p.uiPages ?? []);

  // Always start with fallback core navigation, then merge in plugin pages.
  // Plugin pages with the same id override fallback pages.
  const pageMap = new Map<string, UIPageDef>();
  for (const page of FALLBACK_PAGES) {
    pageMap.set(page.id, page);
  }
  for (const page of pagesFromPlugins) {
    pageMap.set(page.id, page);
  }
  const pages = Array.from(pageMap.values());

  // Filter by role and permissions
  return pages.filter((page) => {
    if (!meetsRoleRequirement(userRole, page.requiredRole)) return false;
    if (!hasPermission(userPermissions, page.requiredPermission)) return false;
    return true;
  });
}

const usePluginStore = create<PluginStore>((set, get) => ({
  plugins: [],
  loading: false,
  loaded: false,
  enabling: {},
  error: null,
  userRole: undefined,
  userPermissions: [],
  enabledPages: FALLBACK_PAGES,

  fetchPlugins: async () => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const res = await fetch('/api/v1/admin/plugins', { headers: authHeaders() });
      if (!res.ok) {
        console.warn('Failed to fetch plugins, using fallback navigation');
        const { userRole, userPermissions } = get();
        set({ loading: false, loaded: true, enabledPages: deriveEnabledPages([], userRole, userPermissions) });
        return;
      }
      const plugins: PluginInfo[] = await res.json();
      const { userRole, userPermissions } = get();
      set({
        plugins,
        loaded: true,
        loading: false,
        enabledPages: deriveEnabledPages(plugins, userRole, userPermissions),
      });
    } catch (e) {
      console.warn('Error fetching plugins:', e);
      const { userRole, userPermissions } = get();
      set({ loading: false, loaded: true, enabledPages: deriveEnabledPages([], userRole, userPermissions) });
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

  setUserAccess: (role, permissions = []) => {
    const { plugins } = get();
    set({
      userRole: role,
      userPermissions: permissions,
      enabledPages: deriveEnabledPages(plugins, role, permissions),
    });
  },

  clearError: () => set({ error: null }),
}));

export { FALLBACK_PAGES, meetsRoleRequirement, hasPermission };
export default usePluginStore;
