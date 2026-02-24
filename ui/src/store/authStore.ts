import { create } from 'zustand';

export interface User {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  role?: string;
  active: boolean;
  created_at: string;
  updated_at: string;
}

interface AuthStore {
  user: User | null;
  token: string | null;
  refreshToken: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  setupRequired: boolean | null;
  setupLoading: boolean;

  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => void;
  refreshAuth: () => Promise<void>;
  loadUser: () => Promise<void>;
  oauthLogin: (provider: string) => void;
  setTokenFromCallback: (token: string, refreshToken: string) => void;
  clearError: () => void;
  checkSetupStatus: () => Promise<void>;
  setupAdmin: (email: string, password: string, name: string) => Promise<void>;
}

const API_BASE = '/api/v1';

async function authFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const token = localStorage.getItem('auth_token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: { ...headers, ...(options?.headers as Record<string, string>) },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const message = body?.error ?? body?.message ?? `HTTP ${res.status}`;
    throw new Error(message);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null;

function scheduleRefresh(expiresIn: number, refreshFn: () => Promise<void>) {
  if (refreshTimer) clearTimeout(refreshTimer);
  // Refresh 60 seconds before expiry, minimum 10 seconds
  const delay = Math.max((expiresIn - 60) * 1000, 10000);
  refreshTimer = setTimeout(() => {
    refreshFn().catch(() => {});
  }, delay);
}

const useAuthStore = create<AuthStore>((set, get) => ({
  user: null,
  token: localStorage.getItem('auth_token'),
  refreshToken: localStorage.getItem('auth_refresh_token'),
  isAuthenticated: !!localStorage.getItem('auth_token'),
  isLoading: false,
  error: null,
  setupRequired: null,
  setupLoading: false,

  login: async (email, password) => {
    set({ isLoading: true, error: null });
    try {
      const data = await authFetch<{
        access_token: string;
        refresh_token: string;
        expires_in: number;
      }>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      });
      localStorage.setItem('auth_token', data.access_token);
      localStorage.setItem('auth_refresh_token', data.refresh_token);
      set({
        token: data.access_token,
        refreshToken: data.refresh_token,
        isAuthenticated: true,
        isLoading: false,
      });
      scheduleRefresh(data.expires_in, get().refreshAuth);
      await get().loadUser();
    } catch (err) {
      const raw = err instanceof Error ? err.message : 'Login failed';
      const friendly = /invalid.*(cred|password|user)|unauthorized|401/i.test(raw)
        ? 'Invalid email or password'
        : raw;
      set({ isLoading: false, error: friendly });
    }
  },

  register: async (email, password, displayName) => {
    set({ isLoading: true, error: null });
    try {
      const data = await authFetch<{
        access_token: string;
        refresh_token: string;
        expires_in: number;
      }>('/auth/register', {
        method: 'POST',
        body: JSON.stringify({ email, password, display_name: displayName }),
      });
      localStorage.setItem('auth_token', data.access_token);
      localStorage.setItem('auth_refresh_token', data.refresh_token);
      set({
        token: data.access_token,
        refreshToken: data.refresh_token,
        isAuthenticated: true,
        isLoading: false,
      });
      scheduleRefresh(data.expires_in, get().refreshAuth);
      await get().loadUser();
    } catch (err) {
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : 'Registration failed',
      });
    }
  },

  logout: () => {
    const token = localStorage.getItem('auth_token');
    if (token) {
      fetch(`${API_BASE}/auth/logout`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
      }).catch(() => {});
    }
    if (refreshTimer) clearTimeout(refreshTimer);
    localStorage.removeItem('auth_token');
    localStorage.removeItem('auth_refresh_token');
    set({
      user: null,
      token: null,
      refreshToken: null,
      isAuthenticated: false,
      error: null,
    });
  },

  refreshAuth: async () => {
    const rt = get().refreshToken || localStorage.getItem('auth_refresh_token');
    if (!rt) return;
    try {
      const data = await authFetch<{
        access_token: string;
        refresh_token: string;
        expires_in: number;
      }>('/auth/refresh', {
        method: 'POST',
        body: JSON.stringify({ refresh_token: rt }),
      });
      localStorage.setItem('auth_token', data.access_token);
      localStorage.setItem('auth_refresh_token', data.refresh_token);
      set({
        token: data.access_token,
        refreshToken: data.refresh_token,
        isAuthenticated: true,
      });
      scheduleRefresh(data.expires_in, get().refreshAuth);
    } catch {
      get().logout();
    }
  },

  loadUser: async () => {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 5000);
      const user = await authFetch<User>('/auth/me', { signal: controller.signal });
      clearTimeout(timer);
      set({ user });
    } catch (err) {
      // If 401/403, token is invalid — force logout to show login screen
      const msg = err instanceof Error ? err.message : '';
      if (msg.includes('401') || msg.includes('403') || msg.includes('user not found') || msg.includes('Unauthorized')) {
        get().logout();
      }
      // On network error/timeout, keep current auth state (token might still be valid)
    }
  },

  oauthLogin: (provider) => {
    window.location.href = `${API_BASE}/auth/oauth2/${encodeURIComponent(provider)}`;
  },

  setTokenFromCallback: (token, refreshTokenVal) => {
    localStorage.setItem('auth_token', token);
    localStorage.setItem('auth_refresh_token', refreshTokenVal);
    set({
      token,
      refreshToken: refreshTokenVal,
      isAuthenticated: true,
    });
    get().loadUser();
  },

  checkSetupStatus: async () => {
    set({ setupLoading: true });
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 3000);
      const data = await authFetch<{ needsSetup: boolean; userCount: number }>(
        '/auth/setup-status',
        { signal: controller.signal },
      );
      clearTimeout(timer);
      set({ setupRequired: data.needsSetup, setupLoading: false });
    } catch {
      // If the endpoint isn't available or times out, assume setup is not required
      set({ setupRequired: false, setupLoading: false });
    }
  },

  setupAdmin: async (email, password, name) => {
    set({ isLoading: true, error: null });
    try {
      const data = await authFetch<{
        access_token: string;
        refresh_token: string;
        expires_in: number;
      }>('/auth/setup', {
        method: 'POST',
        body: JSON.stringify({ email, password, name }),
      });
      localStorage.setItem('auth_token', data.access_token);
      localStorage.setItem('auth_refresh_token', data.refresh_token);
      set({
        token: data.access_token,
        refreshToken: data.refresh_token,
        isAuthenticated: true,
        isLoading: false,
        setupRequired: false,
      });
      scheduleRefresh(data.expires_in, get().refreshAuth);
      await get().loadUser();
    } catch (err) {
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : 'Setup failed',
      });
    }
  },

  clearError: () => set({ error: null }),
}));

export default useAuthStore;
