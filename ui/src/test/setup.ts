import '@testing-library/jest-dom/vitest';

// Ensure localStorage is available for zustand persist middleware in tests.
// jsdom's Storage implementation can be incomplete; provide a full in-memory shim.
if (typeof globalThis.localStorage === 'undefined' || !globalThis.localStorage.setItem) {
  const store: Record<string, string> = {};
  globalThis.localStorage = {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = String(value); },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
    get length() { return Object.keys(store).length; },
    key: (index: number) => Object.keys(store)[index] ?? null,
  };
}
