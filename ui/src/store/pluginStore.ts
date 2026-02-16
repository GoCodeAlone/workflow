import { create } from 'zustand';

interface PluginUIPage {
  id: string;
  label: string;
  icon: string;
  category: string;
}

interface PluginInfo {
  name: string;
  version: string;
  description: string;
  ui_pages: PluginUIPage[];
}

interface PluginStore {
  plugins: PluginInfo[];
  loading: boolean;
  loaded: boolean;
  fetchPlugins: () => Promise<void>;
}

const usePluginStore = create<PluginStore>((set, get) => ({
  plugins: [],
  loading: false,
  loaded: false,

  fetchPlugins: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = {};
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }
      const res = await fetch('/api/v1/admin/plugins', { headers });
      if (!res.ok) {
        console.warn('Failed to fetch plugins, plugin nav will be hidden');
        set({ loading: false, loaded: true });
        return;
      }
      const plugins: PluginInfo[] = await res.json();
      set({ plugins, loaded: true, loading: false });
    } catch (e) {
      console.warn('Error fetching plugins:', e);
      set({ loading: false, loaded: true });
    }
  },
}));

export default usePluginStore;
