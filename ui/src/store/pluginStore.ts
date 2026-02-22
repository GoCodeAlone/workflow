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

/** Derive enabledPages from current plugin list.
 *  Pages from enabled plugins are collected and deduplicated by id (last writer wins).
 *  Filters pages by user role and permissions when provided. */
function deriveEnabledPages(
  plugins: PluginInfo[],
  userRole?: string,
  userPermissions?: string[],
): UIPageDef[] {
  const enabledPlugins = plugins.filter((p) => p.enabled);
  const pagesFromPlugins = enabledPlugins.flatMap((p) => p.uiPages ?? []);

  // Deduplicate by id — last plugin page with a given id wins.
  const pageMap = new Map<string, UIPageDef>();
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
  enabledPages: [],

  fetchPlugins: async () => {
    if (get().loading) return;
    set({ loading: true, error: null });
    try {
      const res = await fetch('/api/v1/admin/plugins', { headers: authHeaders() });
      if (!res.ok) {
        console.warn('Failed to fetch plugins');
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

export { meetsRoleRequirement, hasPermission };
export default usePluginStore;
