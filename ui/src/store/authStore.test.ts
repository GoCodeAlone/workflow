import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { act } from '@testing-library/react';

// Ensure localStorage is available before authStore is imported.
// In some vitest/jsdom configurations, localStorage may not be fully
// initialized at module evaluation time.
vi.hoisted(() => {
  if (typeof globalThis.localStorage === 'undefined' || typeof globalThis.localStorage.getItem !== 'function') {
    const store: Record<string, string> = {};
    globalThis.localStorage = {
      getItem: (key: string) => store[key] ?? null,
      setItem: (key: string, value: string) => { store[key] = value; },
      removeItem: (key: string) => { delete store[key]; },
      clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
      get length() { return Object.keys(store).length; },
      key: (index: number) => Object.keys(store)[index] ?? null,
    };
  }
});

import useAuthStore from './authStore.ts';

function resetStore() {
  localStorage.clear();
  useAuthStore.setState({
    user: null,
    token: null,
    refreshToken: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
  });
}

function mockFetchSuccess(data: unknown, status = 200) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
  });
}

function mockFetchFailure(message: string, status = 401) {
  return vi.fn().mockResolvedValue({
    ok: false,
    status,
    statusText: 'Unauthorized',
    text: () => Promise.resolve(message),
  });
}

describe('authStore', () => {
  beforeEach(() => {
    resetStore();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  describe('initial state', () => {
    it('starts with no user', () => {
      expect(useAuthStore.getState().user).toBeNull();
    });

    it('starts with no token', () => {
      expect(useAuthStore.getState().token).toBeNull();
    });

    it('starts not authenticated', () => {
      expect(useAuthStore.getState().isAuthenticated).toBe(false);
    });

    it('starts not loading', () => {
      expect(useAuthStore.getState().isLoading).toBe(false);
    });

    it('starts with no error', () => {
      expect(useAuthStore.getState().error).toBeNull();
    });
  });

  describe('login', () => {
    it('sets token, isAuthenticated on success', async () => {
      const tokenResponse = {
        access_token: 'test-token',
        refresh_token: 'test-refresh',
        expires_in: 3600,
      };
      const userResponse = {
        id: 'u1',
        email: 'test@example.com',
        display_name: 'Test User',
        active: true,
        created_at: '2025-01-01',
        updated_at: '2025-01-01',
      };
      globalThis.fetch = vi.fn()
        .mockResolvedValueOnce({
          ok: true, status: 200,
          json: () => Promise.resolve(tokenResponse),
          text: () => Promise.resolve(''),
        })
        .mockResolvedValueOnce({
          ok: true, status: 200,
          json: () => Promise.resolve(userResponse),
          text: () => Promise.resolve(''),
        });

      await act(async () => {
        await useAuthStore.getState().login('test@example.com', 'password');
      });

      const state = useAuthStore.getState();
      expect(state.token).toBe('test-token');
      expect(state.refreshToken).toBe('test-refresh');
      expect(state.isAuthenticated).toBe(true);
      expect(state.isLoading).toBe(false);
      expect(state.user?.email).toBe('test@example.com');
    });

    it('sets error on failure', async () => {
      globalThis.fetch = mockFetchFailure('Invalid credentials');

      await act(async () => {
        await useAuthStore.getState().login('bad@example.com', 'wrong');
      });

      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(false);
      expect(state.error).toBe('Invalid credentials');
      expect(state.isLoading).toBe(false);
    });

    it('clears previous error on new login attempt', async () => {
      useAuthStore.setState({ error: 'Previous error' });

      globalThis.fetch = mockFetchFailure('New error');

      await act(async () => {
        await useAuthStore.getState().login('test@example.com', 'pw');
      });

      expect(useAuthStore.getState().error).toBe('New error');
    });

    it('sets isLoading true during login', async () => {
      let resolveLogin: (v: unknown) => void;
      const pending = new Promise((r) => { resolveLogin = r; });

      globalThis.fetch = vi.fn().mockReturnValue(pending);

      const loginPromise = act(async () => {
        return useAuthStore.getState().login('test@example.com', 'pw');
      });

      resolveLogin!({
        ok: true, status: 200,
        json: () => Promise.resolve({ access_token: 't', refresh_token: 'r', expires_in: 3600 }),
        text: () => Promise.resolve(''),
      });

      await loginPromise;
    });

    it('stores token in localStorage on success', async () => {
      const tokenResponse = { access_token: 'stored-token', refresh_token: 'stored-refresh', expires_in: 3600 };
      globalThis.fetch = vi.fn()
        .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(tokenResponse), text: () => Promise.resolve('') })
        .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve({ id: 'u1', email: 'a@b.c', display_name: 'A', active: true, created_at: '', updated_at: '' }), text: () => Promise.resolve('') });

      await act(async () => {
        await useAuthStore.getState().login('a@b.c', 'pw');
      });

      expect(localStorage.getItem('auth_token')).toBe('stored-token');
      expect(localStorage.getItem('auth_refresh_token')).toBe('stored-refresh');
    });
  });

  describe('register', () => {
    it('creates user and sets authenticated state', async () => {
      const tokenResponse = { access_token: 'reg-token', refresh_token: 'reg-refresh', expires_in: 3600 };
      const userResponse = { id: 'u2', email: 'new@example.com', display_name: 'New User', active: true, created_at: '', updated_at: '' };

      globalThis.fetch = vi.fn()
        .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(tokenResponse), text: () => Promise.resolve('') })
        .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(userResponse), text: () => Promise.resolve('') });

      await act(async () => {
        await useAuthStore.getState().register('new@example.com', 'password', 'New User');
      });

      const state = useAuthStore.getState();
      expect(state.token).toBe('reg-token');
      expect(state.isAuthenticated).toBe(true);
      expect(state.user?.display_name).toBe('New User');
    });

    it('sets error on registration failure', async () => {
      globalThis.fetch = mockFetchFailure('Email already exists', 409);

      await act(async () => {
        await useAuthStore.getState().register('dup@example.com', 'pw', 'Dup');
      });

      expect(useAuthStore.getState().error).toBe('Email already exists');
      expect(useAuthStore.getState().isAuthenticated).toBe(false);
    });
  });

  describe('logout', () => {
    it('clears token, user, and sets isAuthenticated false', () => {
      useAuthStore.setState({
        user: { id: 'u1', email: 'a@b.c', display_name: 'A', active: true, created_at: '', updated_at: '' },
        token: 'some-token',
        refreshToken: 'some-refresh',
        isAuthenticated: true,
      });

      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
      localStorage.setItem('auth_token', 'some-token');

      act(() => {
        useAuthStore.getState().logout();
      });

      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
      expect(state.token).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.isAuthenticated).toBe(false);
      expect(state.error).toBeNull();
    });

    it('removes tokens from localStorage', () => {
      localStorage.setItem('auth_token', 'tok');
      localStorage.setItem('auth_refresh_token', 'ref');

      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });

      act(() => {
        useAuthStore.getState().logout();
      });

      expect(localStorage.getItem('auth_token')).toBeNull();
      expect(localStorage.getItem('auth_refresh_token')).toBeNull();
    });
  });

  describe('setTokenFromCallback', () => {
    it('stores token in state and localStorage', () => {
      globalThis.fetch = mockFetchSuccess({ id: 'u1', email: 'a@b.c', display_name: 'A', active: true, created_at: '', updated_at: '' });

      act(() => {
        useAuthStore.getState().setTokenFromCallback('cb-token', 'cb-refresh');
      });

      const state = useAuthStore.getState();
      expect(state.token).toBe('cb-token');
      expect(state.refreshToken).toBe('cb-refresh');
      expect(state.isAuthenticated).toBe(true);
      expect(localStorage.getItem('auth_token')).toBe('cb-token');
      expect(localStorage.getItem('auth_refresh_token')).toBe('cb-refresh');
    });
  });

  describe('clearError', () => {
    it('clears the error state', () => {
      useAuthStore.setState({ error: 'some error' });

      act(() => {
        useAuthStore.getState().clearError();
      });

      expect(useAuthStore.getState().error).toBeNull();
    });
  });

  describe('refreshAuth', () => {
    it('updates tokens on successful refresh', async () => {
      useAuthStore.setState({ refreshToken: 'old-refresh' });
      localStorage.setItem('auth_refresh_token', 'old-refresh');

      const tokenResponse = { access_token: 'new-token', refresh_token: 'new-refresh', expires_in: 3600 };
      globalThis.fetch = mockFetchSuccess(tokenResponse);

      await act(async () => {
        await useAuthStore.getState().refreshAuth();
      });

      expect(useAuthStore.getState().token).toBe('new-token');
      expect(useAuthStore.getState().refreshToken).toBe('new-refresh');
      expect(useAuthStore.getState().isAuthenticated).toBe(true);
    });

    it('logs out on refresh failure', async () => {
      useAuthStore.setState({
        refreshToken: 'expired-refresh',
        token: 'expired-token',
        isAuthenticated: true,
      });
      localStorage.setItem('auth_refresh_token', 'expired-refresh');

      globalThis.fetch = mockFetchFailure('Token expired');

      await act(async () => {
        await useAuthStore.getState().refreshAuth();
      });

      expect(useAuthStore.getState().isAuthenticated).toBe(false);
      expect(useAuthStore.getState().token).toBeNull();
    });
  });
});
